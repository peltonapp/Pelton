package imap

import (
	"cmp"
	"context"
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
	_ psync.RequestCounter  = (*Adapter)(nil)
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
// UIDVALIDITY change, or when SEARCH lists no UIDs for a mailbox SELECT
// reports as not empty.
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
	if mbox.HighestModSeq == 0 || mbox.UIDValidity != cur.UIDValidity {
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

// FetchListMeta selects the mailbox and fetches UID, FLAGS and ENVELOPE for
// the given ids, so the engine can store list stubs for exactly the messages
// it is about to show. Ids the server no longer has are left out.
func (a *Adapter) FetchListMeta(_ context.Context, box psync.RemoteMailbox, ids []string) ([]psync.Header, error) {
	uids, err := parseUIDs(ids)
	if err != nil {
		return nil, err
	}
	a.requests.Add(2)
	if _, err := a.c.Select(box.RemoteID); err != nil {
		return nil, err
	}
	got, err := a.c.FetchHeaders(uids, &goimap.FetchOptions{Envelope: true, Flags: true, UID: true})
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
		})
	}
	return out, nil
}

// Fetch selects mailboxID and downloads the full messages for remoteIDs (UIDs).
// A message that fails to parse is skipped and the first such error is
// returned alongside the messages that did parse.
func (a *Adapter) Fetch(_ context.Context, mailboxID string, remoteIDs []string) ([]psync.Fetched, error) {
	if _, err := a.c.Select(mailboxID); err != nil {
		return nil, err
	}
	uids, err := parseUIDs(remoteIDs)
	if err != nil {
		return nil, err
	}
	var (
		out      []psync.Fetched
		firstErr error
	)
	err = a.c.FetchMessages(uids, func(uid goimap.UID, msg *Message, parseErr error) error {
		if parseErr != nil {
			if firstErr == nil {
				firstErr = parseErr
			}
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
	return out, firstErr
}

// SetFlags makes the managed flags of one message match flags, adding the set
// ones and removing the rest. Flags the adapter does not manage are untouched.
func (a *Adapter) SetFlags(_ context.Context, mailboxID, remoteID string, flags storage.Flag) error {
	if _, err := a.c.Select(mailboxID); err != nil {
		return err
	}
	uid, err := parseUID(remoteID)
	if err != nil {
		return err
	}
	var add, remove []goimap.Flag
	for _, m := range managedFlags {
		if flags.Has(m.bit) {
			add = append(add, m.flag)
		} else {
			remove = append(remove, m.flag)
		}
	}
	if len(add) > 0 {
		if err := a.c.AddFlags(uid, add...); err != nil {
			return err
		}
	}
	if len(remove) > 0 {
		if err := a.c.RemoveFlags(uid, remove...); err != nil {
			return err
		}
	}
	return nil
}

// Move moves messages by UID from sourceMailboxID to destMailboxID.
func (a *Adapter) Move(_ context.Context, sourceMailboxID string, remoteIDs []string, destMailboxID string) error {
	if _, err := a.c.Select(sourceMailboxID); err != nil {
		return err
	}
	uids, err := parseUIDs(remoteIDs)
	if err != nil {
		return err
	}
	return a.c.MoveMessages(uids, destMailboxID)
}

// Delete permanently removes messages by UID from mailboxID.
func (a *Adapter) Delete(_ context.Context, mailboxID string, remoteIDs []string) error {
	if _, err := a.c.Select(mailboxID); err != nil {
		return err
	}
	uids, err := parseUIDs(remoteIDs)
	if err != nil {
		return err
	}
	return a.c.DeleteMessages(uids...)
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
	atts := make([]psync.Attachment, 0, len(msg.Attachments))
	for _, a := range msg.Attachments {
		atts = append(atts, psync.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			ContentID:   a.ContentID,
			Content:     a.Content,
		})
	}
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
		Attachments:         atts,
	}
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
