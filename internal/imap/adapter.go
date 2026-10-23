package imap

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// syncClient is the IMAP surface the adapter drives. *Client satisfies it; tests
// supply a fake so mapping logic can be checked without a network listener.
type syncClient interface {
	Addr() string
	Select(mailbox string) (*Mailbox, error)
	FetchAllFlags() ([]MessageHeader, error)
	// FetchHeaders loads the requested items for uids in one UID FETCH.
	// List stubs pass ENVELOPE and UID and no body section.
	FetchHeaders(uids []goimap.UID, options *goimap.FetchOptions) ([]MessageHeader, error)
	// FetchFlagsChangedSince loads UID and flags of messages changed after modSeq.
	FetchFlagsChangedSince(modSeq uint64) ([]MessageHeader, error)
	// SearchAllUIDs lists every UID in the selected mailbox.
	SearchAllUIDs() ([]goimap.UID, error)
	FetchMessages(uids []goimap.UID, fn func(uid goimap.UID, msg *Message, err error) error) error
	// FetchPartial fetches length bytes of one message's source from offset.
	FetchPartial(uid goimap.UID, offset, length int64) (*Message, error)
	AddFlags(uid goimap.UID, flags ...goimap.Flag) error
	RemoveFlags(uid goimap.UID, flags ...goimap.Flag) error
	DeleteMessages(uids ...goimap.UID) error
	MoveMessages(uids []goimap.UID, mailbox string) error
	CreateFolder(path string) error
	RenameFolder(path, newPath string) error
	DeleteFolder(path string) error
	ListFolders() ([]Folder, error)
}

// labelKeywords maps color indices 1..8 to Thunderbird-style IMAP keywords.
var labelKeywords = []goimap.Flag{
	"$Label1", "$Label2", "$Label3", "$Label4",
	"$Label5", "$Label6", "$Label7", "$Label8",
}

// managedFlags are the only IMAP flags SetFlags adds or removes.
var managedFlags = []struct {
	bit  storage.Flag
	flag goimap.Flag
}{
	{storage.FlagSeen, goimap.FlagSeen},
	{storage.FlagFlagged, goimap.FlagFlagged},
	{storage.FlagDeleted, goimap.FlagDeleted},
}

// Adapter presents an IMAP client as a sync.Adapter (and ColorSource).
type Adapter struct {
	c        syncClient
	colors   map[string]map[string]int // mailbox path → remote id → color 1..8
	requests atomic.Int64              // IMAP commands sent by the list paths, for the sync log
}

// NewAdapter wraps an IMAP client for the sync engine. *Client satisfies the
// surface; the desktop mailClient seam is a superset and can be passed too.
func NewAdapter(client syncClient) *Adapter {
	return newAdapter(client)
}

func newAdapter(c syncClient) *Adapter {
	return &Adapter{
		c:      c,
		colors: make(map[string]map[string]int),
	}
}

var (
	_ psync.Adapter     = (*Adapter)(nil)
	_ psync.ColorSource = (*Adapter)(nil)

	_ psync.DeltaLister     = (*Adapter)(nil)
	_ psync.ListMetaFetcher = (*Adapter)(nil)
	_ psync.OlderLister     = (*Adapter)(nil)
	_ psync.RequestCounter  = (*Adapter)(nil)
	_ psync.ChunkFetcher    = (*Adapter)(nil)
)

// Requests returns how many IMAP commands the list paths have sent.
func (a *Adapter) Requests() int64 { return a.requests.Load() }

// Addr returns the server address the client is connected to.
func (a *Adapter) Addr() string { return a.c.Addr() }

// ListMailboxes returns every folder from LIST, split into name and parent by
// the server's delimiter. Generation stays empty until Select reports UIDVALIDITY.
func (a *Adapter) ListMailboxes(context.Context) ([]psync.Mailbox, error) {
	folders, err := a.c.ListFolders()
	if err != nil {
		return nil, err
	}
	out := make([]psync.Mailbox, 0, len(folders))
	for _, f := range folders {
		name, parent := splitFolderPath(f.Name, f.Delimiter)
		out = append(out, psync.Mailbox{
			RemoteID:   f.Name,
			Name:       name,
			ParentID:   parent,
			Role:       folderSpecialUse(f),
			Generation: "", // LIST does not carry UIDVALIDITY; Select fills it later
			Selectable: f.Selectable(),
		})
	}
	return out, nil
}

// ListMessages selects the mailbox and returns every message's UID and flags,
// newest first, with UIDVALIDITY as generation and, when the server has
// CONDSTORE, a cursor for the next ListChanges as the state token. Envelopes
// are not fetched here: the engine asks FetchListMeta for the few messages
// that become list stubs. It also records the mailbox's color labels.
func (a *Adapter) ListMessages(_ context.Context, box psync.RemoteMailbox) ([]psync.Header, string, string, error) {
	a.requests.Add(2)
	mbox, err := a.c.Select(box.RemoteID)
	if err != nil {
		return nil, "", "", err
	}
	flags, err := a.c.FetchAllFlags()
	if err != nil {
		return nil, "", "", err
	}
	headers, colors := flagHeaders(flags)
	a.colors[box.RemoteID] = colors
	return headers, cursorFor(mbox), strconv.FormatUint(uint64(mbox.UIDValidity), 10), nil
}

// flagHeaders turns UID+FLAGS fetch results into headers, highest UID first,
// and collects the color labels among them.
func flagHeaders(flags []MessageHeader) ([]psync.Header, map[string]int) {
	sorted := slices.Clone(flags)
	slices.SortFunc(sorted, func(x, y MessageHeader) int { return cmp.Compare(y.UID, x.UID) })
	out := make([]psync.Header, 0, len(sorted))
	colors := make(map[string]int)
	for _, h := range sorted {
		remoteID := strconv.FormatUint(uint64(h.UID), 10)
		out = append(out, psync.Header{RemoteID: remoteID, LegacyUID: uint32(h.UID), Flags: imapFlagsToStorage(h.Flags)})
		if color := colorFromFlags(h.Flags); color > 0 {
			colors[remoteID] = color
		}
	}
	return out, colors
}

// ListChanges reports what changed in a mailbox since the cursor in
// box.StateToken, using CONDSTORE. When HIGHESTMODSEQ, UIDNEXT and MESSAGES
// all match the cursor and no ensure ids are asked for, nothing changed and
// the cost is one SELECT. Flag changes come from FETCH CHANGEDSINCE. When
// UIDNEXT or MESSAGES moved, something arrived or was expunged, so every UID
// is listed in Members (there is no QRESYNC VANISHED to name expunges). It
// returns ErrNeedFullList without a cursor, without CONDSTORE, after a
// UIDVALIDITY change, when HIGHESTMODSEQ went backwards (a restored mailbox
// whose changes CHANGEDSINCE cannot name), or when SEARCH lists no UIDs for a
// mailbox SELECT reports as not empty.
func (a *Adapter) ListChanges(_ context.Context, box psync.RemoteMailbox, ensure []string) (psync.Delta, error) {
	cur, ok := parseCursor(box.StateToken)
	if !ok {
		return psync.Delta{}, psync.ErrNeedFullList
	}
	a.requests.Add(1)
	mbox, err := a.c.Select(box.RemoteID)
	if err != nil {
		return psync.Delta{}, err
	}
	if mbox.HighestModSeq == 0 || mbox.UIDValidity != cur.UIDValidity || mbox.HighestModSeq < cur.HighestModSeq {
		return psync.Delta{}, psync.ErrNeedFullList
	}
	d := psync.Delta{Cursor: cursorFor(mbox)}
	moved := uint32(mbox.UIDNext) != cur.UIDNext || mbox.NumMessages != cur.Messages
	if !moved && mbox.HighestModSeq == cur.HighestModSeq && len(ensure) == 0 {
		a.colors[box.RemoteID] = map[string]int{}
		d.Unchanged = true
		return d, nil
	}

	byUID := make(map[goimap.UID]MessageHeader)
	if mbox.HighestModSeq != cur.HighestModSeq {
		a.requests.Add(1)
		changed, err := a.c.FetchFlagsChangedSince(cur.HighestModSeq)
		if err != nil {
			return psync.Delta{}, err
		}
		for _, h := range changed {
			byUID[h.UID] = h
		}
	}
	if len(ensure) > 0 {
		uids, err := parseUIDs(ensure)
		if err != nil {
			return psync.Delta{}, err
		}
		var missing []goimap.UID
		for _, uid := range uids {
			if _, ok := byUID[uid]; !ok {
				missing = append(missing, uid)
			}
		}
		if len(missing) > 0 {
			a.requests.Add(1)
			got, err := a.c.FetchHeaders(missing, &goimap.FetchOptions{Flags: true, UID: true})
			if err != nil {
				return psync.Delta{}, err
			}
			for _, h := range got {
				byUID[h.UID] = h
			}
		}
	}
	changed := make([]MessageHeader, 0, len(byUID))
	for _, h := range byUID {
		changed = append(changed, h)
	}
	var colors map[string]int
	d.Changed, colors = flagHeaders(changed)
	a.colors[box.RemoteID] = colors

	if moved {
		a.requests.Add(1)
		uids, err := a.c.SearchAllUIDs()
		if err != nil {
			return psync.Delta{}, err
		}
		if len(uids) == 0 && mbox.NumMessages > 0 {
			// SELECT says the mailbox has mail, so an empty SEARCH is a
			// server or parser quirk. Taken as the member list it would
			// delete every cached row.
			return psync.Delta{}, psync.ErrNeedFullList
		}
		d.Members = make([]psync.Header, 0, len(uids))
		for _, uid := range uids {
			d.Members = append(d.Members, psync.Header{RemoteID: strconv.FormatUint(uint64(uid), 10), LegacyUID: uint32(uid)})
		}
	}
	return d, nil
}

// Bounds of ListBelow's look below the floor: the first UID range spans n
// UIDs, each next one twice the last, up to olderSpanMax times n, and it
// looks olderSteps times at most. UID+FLAGS rows fetched stay under
// (olderSpanMax+1)*n; ENVELOPE rows under n. Without ESEARCH or a sequence
// number for the floor no server tells how sparse its UIDs are, so a folder
// whose UIDs thin out a lot below the floor gets fewer than n, and the
// normal lane's full list admits the rest.
const (
	olderSteps   = 6
	olderSpanMax = 4
)

// ListBelow selects the mailbox (see selectBox) and returns up to n of the
// newest messages with a UID below below, newest first, with list meta. It
// walks down from below-1 in adjacent UID ranges (UID FETCH lo:hi (UID
// FLAGS)), which return only the UIDs that exist, until it has n or it
// reaches UID 1, then fetches the envelopes of those n. Its work is bounded
// whatever the folder holds: no SEARCH ALL, no FETCH of every message.
func (a *Adapter) ListBelow(_ context.Context, box psync.RemoteMailbox, below string, n int) (psync.OlderPage, error) {
	floor, err := parseUID(below)
	if err != nil {
		return psync.OlderPage{}, err
	}
	if _, err := a.selectBox(box.RemoteID, box.Generation); err != nil {
		return psync.OlderPage{}, err
	}
	if n <= 0 {
		return psync.OlderPage{}, nil
	}
	var found []MessageHeader
	hi := floor // the range is lo..hi-1; nothing at or above hi is wanted
	span := goimap.UID(n)
	for step := 0; step < olderSteps && hi > 1 && len(found) < n; step++ {
		lo := goimap.UID(1)
		if hi > span {
			lo = hi - span
		}
		uids := make([]goimap.UID, 0, hi-lo)
		for u := lo; u < hi; u++ {
			uids = append(uids, u)
		}
		a.requests.Add(1)
		got, err := a.c.FetchHeaders(uids, &goimap.FetchOptions{UID: true, Flags: true})
		if err != nil {
			return psync.OlderPage{}, err
		}
		window := make([]MessageHeader, 0, len(got))
		for _, h := range got {
			if h.UID >= lo && h.UID < hi {
				window = append(window, h)
			}
		}
		slices.SortFunc(window, func(x, y MessageHeader) int { return cmp.Compare(y.UID, x.UID) })
		found = append(found, slices.CompactFunc(window, func(x, y MessageHeader) bool { return x.UID == y.UID })...)
		hi = lo
		span = min(span*2, goimap.UID(olderSpanMax*n))
	}
	// Bottom only from the ranges themselves: a SELECT the session kept
	// (selectBox) may hold an old message count.
	page := psync.OlderPage{Bottom: hi <= 1}
	if len(found) > n {
		found, page.Bottom = found[:n], false
	}
	if len(found) == 0 {
		return page, nil
	}
	ids := make([]string, len(found))
	for i, h := range found {
		ids[i] = strconv.FormatUint(uint64(h.UID), 10)
	}
	meta, err := a.listMeta(ids)
	if err != nil {
		return psync.OlderPage{}, err
	}
	slices.SortFunc(meta, func(x, y psync.Header) int { return cmp.Compare(y.LegacyUID, x.LegacyUID) })
	page.Headers = meta
	return page, nil
}

// FetchListMeta selects the mailbox (see selectBox) and fetches UID, FLAGS,
// ENVELOPE and RFC822.SIZE for the given ids, so the engine can store list
// stubs for exactly the messages it is about to show. Ids the server no
// longer has are left out. A UIDVALIDITY other than box.Generation returns
// psync.ErrGenerationChanged.
func (a *Adapter) FetchListMeta(_ context.Context, box psync.RemoteMailbox, ids []string) ([]psync.Header, error) {
	if _, err := parseUIDs(ids); err != nil {
		return nil, err
	}
	if _, err := a.selectBox(box.RemoteID, box.Generation); err != nil {
		return nil, err
	}
	return a.listMeta(ids)
}

// listMeta is FetchListMeta on the mailbox already selected.
func (a *Adapter) listMeta(ids []string) ([]psync.Header, error) {
	uids, err := parseUIDs(ids)
	if err != nil {
		return nil, err
	}
	a.requests.Add(1)
	got, err := a.c.FetchHeaders(uids, &goimap.FetchOptions{Envelope: true, Flags: true, UID: true, RFC822Size: true})
	if err != nil {
		return nil, err
	}
	out := make([]psync.Header, 0, len(got))
	for _, h := range got {
		if !h.HasEnvelope {
			continue
		}
		out = append(out, psync.Header{
			RemoteID:    strconv.FormatUint(uint64(h.UID), 10),
			LegacyUID:   uint32(h.UID),
			Flags:       imapFlagsToStorage(h.Flags),
			HasListMeta: true,
			Subject:     h.Subject,
			From:        h.From,
			FromName:    h.FromName,
			To:          h.To,
			Date:        h.Date,
			Size:        h.Size,
		})
	}
	return out, nil
}

// Fetch selects mailboxID (see selectBox) and downloads the full messages for
// remoteIDs (UIDs). Messages that fail to parse are skipped and reported as
// one *psync.MessageError alongside the messages that did parse. A
// UIDVALIDITY other than the generation on ctx (psync.WithGeneration)
// returns psync.ErrGenerationChanged before anything is fetched.
func (a *Adapter) Fetch(ctx context.Context, mailboxID string, remoteIDs []string) ([]psync.Fetched, error) {
	if _, err := a.selectBox(mailboxID, psync.ExpectedGeneration(ctx, mailboxID)); err != nil {
		return nil, err
	}
	uids, err := parseUIDs(remoteIDs)
	if err != nil {
		return nil, err
	}
	var (
		out        []psync.Fetched
		unreadable []string
		firstErr   error
	)
	err = a.c.FetchMessages(uids, func(uid goimap.UID, msg *Message, parseErr error) error {
		if parseErr != nil {
			if firstErr == nil {
				firstErr = parseErr
			}
			unreadable = append(unreadable, strconv.FormatUint(uint64(uid), 10))
			return nil
		}
		if msg == nil {
			return nil
		}
		out = append(out, messageToFetched(msg))
		return nil
	})
	if err != nil {
		return out, err
	}
	if firstErr != nil {
		return out, &psync.MessageError{RemoteIDs: unreadable, Err: firstErr}
	}
	return out, nil
}

// SetFlags adds the managed flags set in flags to one message (+FLAGS) and
// removes nothing: the engine pushes the union of local and server flags, so
// a managed flag absent from flags was set by another client since the
// snapshot, and a -FLAGS would erase it. Flags the adapter does not manage
// are untouched. Nothing set means no command.
func (a *Adapter) SetFlags(ctx context.Context, mailboxID, remoteID string, flags storage.Flag) error {
	uid, err := parseUID(remoteID)
	if err != nil {
		return err
	}
	var add []goimap.Flag
	for _, m := range managedFlags {
		if flags.Has(m.bit) {
			add = append(add, m.flag)
		}
	}
	if len(add) == 0 {
		return nil
	}
	if _, err := a.selectBox(mailboxID, psync.ExpectedGeneration(ctx, mailboxID)); err != nil {
		return refused(err)
	}
	return refused(a.c.AddFlags(uid, add...))
}

// refusedError is a server's NO or BAD to a command; errors.Is matches it
// against psync.ErrRefused and the text is the original error's.
type refusedError struct{ error }

func (e refusedError) Unwrap() error        { return e.error }
func (e refusedError) Is(target error) bool { return target == psync.ErrRefused }

// refused marks err as psync.ErrRefused when the server answered the command
// with a tagged NO or BAD. Transport and local errors are returned unchanged.
func refused(err error) error {
	var status *goimap.Error
	if errors.As(err, &status) && (status.Type == goimap.StatusResponseTypeNo || status.Type == goimap.StatusResponseTypeBad) {
		return refusedError{err}
	}
	return err
}

// currentMailbox is the optional client surface that reports the selected
// mailbox; *Client has it. A client without it is selected every time.
type currentMailbox interface {
	SelectedMailbox() *Mailbox
}

// selectBox makes mailboxID the selected mailbox, skipping the SELECT when
// the connection already has it open. With generation set, a mailbox whose
// UIDVALIDITY differs returns psync.ErrGenerationChanged, so stored ids are
// never used against other messages. The list paths always SELECT: they
// need the fresh counts SELECT reports.
func (a *Adapter) selectBox(mailboxID, generation string) (*Mailbox, error) {
	var mbox *Mailbox
	if cm, ok := a.c.(currentMailbox); ok {
		if cur := cm.SelectedMailbox(); cur != nil && cur.Name == mailboxID {
			mbox = cur
		}
	}
	if mbox == nil {
		a.requests.Add(1)
		var err error
		if mbox, err = a.c.Select(mailboxID); err != nil {
			return nil, err
		}
	}
	if got := strconv.FormatUint(uint64(mbox.UIDValidity), 10); generation != "" && got != generation {
		return nil, fmt.Errorf("imap adapter: %q has UIDVALIDITY %s, ids are from %s: %w",
			mailboxID, got, generation, psync.ErrGenerationChanged)
	}
	return mbox, nil
}

// Move moves messages by UID from sourceMailboxID to destMailboxID.
func (a *Adapter) Move(_ context.Context, sourceMailboxID string, remoteIDs []string, destMailboxID string) error {
	if _, err := a.c.Select(sourceMailboxID); err != nil {
		return refused(err)
	}
	uids, err := parseUIDs(remoteIDs)
	if err != nil {
		return err
	}
	return refused(a.c.MoveMessages(uids, destMailboxID))
}

// Delete permanently removes messages by UID from mailboxID.
func (a *Adapter) Delete(_ context.Context, mailboxID string, remoteIDs []string) error {
	if _, err := a.c.Select(mailboxID); err != nil {
		return refused(err)
	}
	uids, err := parseUIDs(remoteIDs)
	if err != nil {
		return err
	}
	return refused(a.c.DeleteMessages(uids...))
}

// CreateMailbox creates name under parentID (top level when empty), joined with
// the parent's delimiter, and returns the new mailbox.
func (a *Adapter) CreateMailbox(_ context.Context, name, parentID string) (psync.Mailbox, error) {
	path := name
	if parentID != "" {
		delim, err := a.delimiterOf(parentID)
		if err != nil {
			return psync.Mailbox{}, err
		}
		path = parentID + delim + name
	}
	if err := a.c.CreateFolder(path); err != nil {
		return psync.Mailbox{}, err
	}
	return psync.Mailbox{
		RemoteID:   path,
		Name:       name,
		ParentID:   parentID,
		Selectable: true,
	}, nil
}

// RenameMailbox renames a folder in place, keeping its parent.
func (a *Adapter) RenameMailbox(_ context.Context, remoteID, name string) error {
	newPath := name
	if parent, delim, ok := parentOf(remoteID, a); ok && parent != "" {
		newPath = parent + delim + name
	}
	return a.c.RenameFolder(remoteID, newPath)
}

// DeleteMailbox deletes a folder on the server.
func (a *Adapter) DeleteMailbox(_ context.Context, remoteID string) error {
	return a.c.DeleteFolder(remoteID)
}

// Colors returns the $Label1..$Label8 map captured by the last ListMessages for
// mailboxID, or nil when that mailbox has not been listed yet.
func (a *Adapter) Colors(mailboxID string) map[string]int {
	return a.colors[mailboxID]
}

func (a *Adapter) delimiterOf(path string) (string, error) {
	folders, err := a.c.ListFolders()
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		if f.Name == path {
			if f.Delimiter == 0 {
				return "/", nil
			}
			return string(f.Delimiter), nil
		}
	}
	return "/", nil
}

func parentOf(path string, a *Adapter) (parent, delim string, ok bool) {
	folders, err := a.c.ListFolders()
	if err != nil {
		return "", "", false
	}
	for _, f := range folders {
		if f.Name != path {
			continue
		}
		_, parent = splitFolderPath(f.Name, f.Delimiter)
		if f.Delimiter == 0 {
			return parent, "/", true
		}
		return parent, string(f.Delimiter), true
	}
	return "", "", false
}

func splitFolderPath(path string, delim rune) (name, parent string) {
	if delim == 0 {
		return path, ""
	}
	if p, n, ok := strings.CutLast(path, string(delim)); ok {
		return n, p
	}
	return path, ""
}

func folderSpecialUse(f Folder) string {
	for _, attr := range f.Attrs {
		switch strings.ToLower(strings.TrimPrefix(string(attr), "\\")) {
		case "inbox":
			return `\Inbox`
		case "sent":
			return `\Sent`
		case "drafts":
			return `\Drafts`
		case "trash":
			return `\Trash`
		case "junk":
			return `\Junk`
		case "archive":
			return `\Archive`
		}
	}
	if strings.EqualFold(f.Name, "INBOX") {
		return `\Inbox`
	}
	return ""
}

func imapFlagsToStorage(flags []goimap.Flag) storage.Flag {
	var out storage.Flag
	for _, f := range flags {
		switch f {
		case goimap.FlagSeen:
			out |= storage.FlagSeen
		case goimap.FlagFlagged:
			out |= storage.FlagFlagged
		case goimap.FlagDeleted:
			out |= storage.FlagDeleted
		}
	}
	return out
}

func colorFromFlags(flags []goimap.Flag) int {
	for _, f := range flags {
		for i, lf := range labelKeywords {
			if strings.EqualFold(string(f), string(lf)) {
				return i + 1
			}
		}
	}
	return 0
}

func messageToFetched(msg *Message) psync.Fetched {
	uid := uint32(msg.UID)
	return psync.Fetched{
		RemoteID:            strconv.FormatUint(uint64(uid), 10),
		LegacyUID:           uid,
		Flags:               imapFlagsToStorage(msg.Flags),
		Raw:                 msg.Raw,
		MessageID:           msg.MessageID,
		Subject:             msg.Subject,
		From:                msg.From,
		To:                  msg.To,
		Cc:                  msg.Cc,
		Text:                msg.Text,
		HTML:                msg.HTML,
		ListUnsubscribe:     msg.ListUnsubscribe,
		ReplyTo:             msg.ReplyTo,
		CharsetGuess:        msg.CharsetGuess,
		Date:                msg.Date,
		Size:                msg.Size,
		ListUnsubscribePost: msg.ListUnsubscribePost,
		AuthResults:         msg.AuthResults,
		Attachments:         fetchedAttachments(msg.Attachments),
	}
}

func fetchedAttachments(in []Attachment) []psync.Attachment {
	out := make([]psync.Attachment, 0, len(in))
	for _, a := range in {
		out = append(out, psync.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			ContentID:   a.ContentID,
			Content:     a.Content,
		})
	}
	return out
}

func parseUID(remoteID string) (goimap.UID, error) {
	n, err := strconv.ParseUint(remoteID, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("imap adapter: remote id %q is not a uid: %w", remoteID, err)
	}
	return goimap.UID(n), nil
}

func parseUIDs(remoteIDs []string) ([]goimap.UID, error) {
	uids := make([]goimap.UID, 0, len(remoteIDs))
	for _, id := range remoteIDs {
		uid, err := parseUID(id)
		if err != nil {
			return nil, err
		}
		uids = append(uids, uid)
	}
	return uids, nil
}
