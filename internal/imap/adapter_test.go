package imap

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// fakeSyncClient stubs the IMAP surface the adapter drives.
type fakeSyncClient struct {
	addr string

	selectPath string
	mailbox    *Mailbox
	selectErr  error

	flags    []MessageHeader
	flagsErr error

	added   []goimap.Flag
	removed []goimap.Flag
	addErr  error
	remErr  error

	folders []Folder

	// envelopes are returned by FetchHeaders when the caller asks for ENVELOPE.
	envelopes map[goimap.UID]MessageHeader
	// headerFetches records each FetchHeaders call, including the options.
	headerFetches []recordedHeaderFetch

	// changedSince is what FetchFlagsChangedSince returns; changedArg records the modseq asked for.
	changedSince []MessageHeader
	changedArg   uint64
	changedCalls int
	// allUIDs is what SearchAllUIDs returns.
	allUIDs     []goimap.UID
	searchCalls int
	selectCalls int
	// unparsable lists UIDs whose body FetchMessages reports as a parse
	// error; every other UID asked for comes back as a message.
	unparsable map[goimap.UID]bool
	// sources are the raw messages FetchPartial serves; partials records
	// each call as [uid, offset, length].
	sources  map[goimap.UID][]byte
	partials [][3]int64
}

// recordedHeaderFetch is one FETCH the adapter issued for list-stub metadata.
type recordedHeaderFetch struct {
	uids    []goimap.UID
	options *goimap.FetchOptions
}

func (f *fakeSyncClient) Addr() string { return f.addr }

func (f *fakeSyncClient) Select(mailbox string) (*Mailbox, error) {
	f.selectPath = mailbox
	f.selectCalls++
	if f.selectErr != nil {
		return nil, f.selectErr
	}
	if f.mailbox != nil {
		return f.mailbox, nil
	}
	return &Mailbox{Name: mailbox}, nil
}

func (f *fakeSyncClient) FetchAllFlags() ([]MessageHeader, error) {
	return f.flags, f.flagsErr
}

func (f *fakeSyncClient) FetchHeaders(uids []goimap.UID, options *goimap.FetchOptions) ([]MessageHeader, error) {
	rec := recordedHeaderFetch{uids: append([]goimap.UID(nil), uids...)}
	if options != nil {
		cp := *options
		rec.options = &cp
	}
	f.headerFetches = append(f.headerFetches, rec)

	out := make([]MessageHeader, 0, len(uids))
	for _, uid := range uids {
		if options != nil && options.Envelope {
			if h, ok := f.envelopes[uid]; ok {
				out = append(out, h)
				continue
			}
		}
		h := MessageHeader{UID: uid}
		if options != nil && options.Flags {
			for _, fh := range f.flags {
				if fh.UID == uid {
					h.Flags = fh.Flags
				}
			}
		}
		out = append(out, h)
	}
	return out, nil
}

func (f *fakeSyncClient) FetchFlagsChangedSince(modSeq uint64) ([]MessageHeader, error) {
	f.changedCalls++
	f.changedArg = modSeq
	return f.changedSince, nil
}

func (f *fakeSyncClient) SearchAllUIDs() ([]goimap.UID, error) {
	f.searchCalls++
	return f.allUIDs, nil
}

func (f *fakeSyncClient) FetchMessages(uids []goimap.UID, fn func(goimap.UID, *Message, error) error) error {
	for _, uid := range uids {
		var err error
		msg := &Message{UID: uid, Subject: "s"}
		if f.unparsable[uid] {
			msg, err = nil, errors.New("malformed MIME")
		}
		if err := fn(uid, msg, err); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSyncClient) FetchPartial(uid goimap.UID, offset, length int64) (*Message, error) {
	f.partials = append(f.partials, [3]int64{int64(uid), offset, length})
	src, ok := f.sources[uid]
	if !ok {
		return nil, ErrNoMessage
	}
	start := min(offset, int64(len(src)))
	end := min(start+length, int64(len(src)))
	return &Message{UID: uid, Subject: "big", Flags: []goimap.Flag{goimap.FlagSeen}, Raw: src[start:end]}, nil
}

func (f *fakeSyncClient) AddFlags(_ goimap.UID, flags ...goimap.Flag) error {
	f.added = append(f.added, flags...)
	return f.addErr
}

func (f *fakeSyncClient) RemoveFlags(_ goimap.UID, flags ...goimap.Flag) error {
	f.removed = append(f.removed, flags...)
	return f.remErr
}

func (f *fakeSyncClient) DeleteMessages(...goimap.UID) error      { return nil }
func (f *fakeSyncClient) MoveMessages([]goimap.UID, string) error { return nil }
func (f *fakeSyncClient) CreateFolder(string) error               { return nil }
func (f *fakeSyncClient) RenameFolder(string, string) error       { return nil }
func (f *fakeSyncClient) DeleteFolder(string) error               { return nil }
func (f *fakeSyncClient) ListFolders() ([]Folder, error)          { return f.folders, nil }

func TestAdapterMapsRemoteIDs(t *testing.T) {
	ctx := context.Background()
	fake := &fakeSyncClient{
		mailbox: &Mailbox{Name: "INBOX", UIDValidity: 9},
		flags: []MessageHeader{{
			UID:   15,
			Flags: []goimap.Flag{goimap.FlagSeen, goimap.FlagFlagged, goimap.FlagAnswered},
		}},
	}
	adapter := newAdapter(fake)

	headers, stateToken, generation, err := adapter.ListMessages(ctx, psync.RemoteMailbox{RemoteID: "INBOX"})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if stateToken != "" {
		t.Errorf("stateToken = %q, want empty", stateToken)
	}
	if generation != "9" {
		t.Errorf("generation = %q, want 9", generation)
	}
	if fake.selectPath != "INBOX" {
		t.Errorf("selected %q, want INBOX", fake.selectPath)
	}
	if len(headers) != 1 {
		t.Fatalf("got %d headers, want 1", len(headers))
	}
	h := headers[0]
	if h.RemoteID != "15" {
		t.Errorf("RemoteID = %q, want 15", h.RemoteID)
	}
	if h.LegacyUID != 15 {
		t.Errorf("LegacyUID = %d, want 15", h.LegacyUID)
	}
	wantFlags := storage.FlagSeen | storage.FlagFlagged
	if h.Flags != wantFlags {
		t.Errorf("Flags = %v, want %v (\\Answered dropped)", h.Flags, wantFlags)
	}

	if err := adapter.SetFlags(ctx, "INBOX", "15", storage.FlagSeen); err != nil {
		t.Fatalf("SetFlags: %v", err)
	}
	if len(fake.added) != 1 || fake.added[0] != goimap.FlagSeen {
		t.Errorf("AddFlags got %v, want only \\Seen", fake.added)
	}
	for _, flag := range fake.added {
		if flag == goimap.FlagAnswered {
			t.Error("SetFlags must not send \\Answered")
		}
	}
	// The push only adds: \Flagged or \Deleted on the server may have been
	// set by another client since the snapshot.
	if len(fake.removed) != 0 {
		t.Errorf("SetFlags removed %v; the sync push must never remove flags", fake.removed)
	}
}

// trackingFake reports the selected mailbox like *Client does.
type trackingFake struct {
	*fakeSyncClient
	cur *Mailbox
}

func (f *trackingFake) Select(mailbox string) (*Mailbox, error) {
	m, err := f.fakeSyncClient.Select(mailbox)
	if err == nil {
		cp := *m
		cp.Name = mailbox
		f.cur = &cp
	}
	return f.cur, err
}

func (f *trackingFake) SelectedMailbox() *Mailbox { return f.cur }

// A flag push into the mailbox the connection already has open sends no
// SELECT; one with another UIDVALIDITY than the ids' refuses; no flags to
// add sends nothing.
func TestSetFlagsSelectsOnlyWhenNeeded(t *testing.T) {
	fake := &trackingFake{fakeSyncClient: &fakeSyncClient{mailbox: &Mailbox{UIDValidity: 7}}}
	a := newAdapter(fake)
	ctx := psync.WithGeneration(context.Background(), "INBOX", "7")
	for range 2 {
		if err := a.SetFlags(ctx, "INBOX", "3", storage.FlagSeen); err != nil {
			t.Fatal(err)
		}
	}
	if fake.selectCalls != 1 {
		t.Errorf("%d SELECTs for two pushes into the open mailbox, want 1", fake.selectCalls)
	}
	if err := a.SetFlags(ctx, "Archive", "3", storage.FlagSeen); err != nil || fake.selectCalls != 2 {
		t.Fatalf("push into another mailbox: err %v, %d selects", err, fake.selectCalls)
	}

	fake.added = nil
	stale := psync.WithGeneration(context.Background(), "INBOX", "6")
	if err := a.SetFlags(stale, "INBOX", "3", storage.FlagSeen); !errors.Is(err, psync.ErrGenerationChanged) {
		t.Fatalf("err = %v, want ErrGenerationChanged", err)
	}
	if len(fake.added) != 0 {
		t.Errorf("flags stored despite the UIDVALIDITY change: %v", fake.added)
	}

	selects := fake.selectCalls
	if err := a.SetFlags(ctx, "Junk", "3", 0); err != nil || fake.selectCalls != selects || len(fake.added) != 0 {
		t.Errorf("empty push: err %v, selects %d->%d, added %v", err, selects, fake.selectCalls, fake.added)
	}
}

func TestListMetaHeaderFromBuffer(t *testing.T) {
	when := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	h := headerFromBuffer(&imapclient.FetchMessageBuffer{
		UID: 8,
		Envelope: &goimap.Envelope{
			Subject: "Hello",
			From:    []goimap.Address{{Name: "Ada", Mailbox: "ada", Host: "ex"}},
			To:      []goimap.Address{{Mailbox: "bob", Host: "ex"}},
			Date:    when,
		},
	})
	if !h.HasEnvelope || h.Subject != "Hello" || h.FromName != "Ada" || h.From != "Ada <ada@ex>" || h.To != "bob@ex" || !h.Date.Equal(when) {
		t.Fatalf("envelope header = %+v", h)
	}

	if sized := headerFromBuffer(&imapclient.FetchMessageBuffer{UID: 2, RFC822Size: 777}); sized.Size != 777 {
		t.Fatalf("size = %d, want RFC822.SIZE 777", sized.Size)
	}

	flagsOnly := headerFromBuffer(&imapclient.FetchMessageBuffer{UID: 1, Flags: []goimap.Flag{goimap.FlagSeen}})
	if flagsOnly.HasEnvelope || flagsOnly.Subject != "" {
		t.Fatalf("flags-only header = %+v, want no envelope", flagsOnly)
	}
}

func uidStrings(uids []goimap.UID) string {
	parts := make([]string, len(uids))
	for i, uid := range uids {
		parts[i] = strconv.FormatUint(uint64(uid), 10)
	}
	return strings.Join(parts, ",")
}

func TestAdapterListMailboxesMapsInboxRole(t *testing.T) {
	fake := &fakeSyncClient{
		folders: []Folder{
			{Name: "INBOX", Delimiter: '/', Attrs: nil},
			{Name: "Sent", Delimiter: '/', Attrs: []goimap.MailboxAttr{goimap.MailboxAttrSent}},
			{Name: "Parent", Delimiter: '/', Attrs: []goimap.MailboxAttr{goimap.MailboxAttrNoSelect}},
			{Name: "Parent/Child", Delimiter: '/', Attrs: nil},
		},
	}
	adapter := newAdapter(fake)

	mailboxes, err := adapter.ListMailboxes(context.Background())
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	byID := map[string]psync.Mailbox{}
	for _, mb := range mailboxes {
		byID[mb.RemoteID] = mb
	}

	inbox := byID["INBOX"]
	if inbox.Role != `\Inbox` {
		t.Errorf("INBOX Role = %q, want \\Inbox", inbox.Role)
	}
	if !inbox.Selectable {
		t.Error("INBOX should be selectable")
	}
	if inbox.Generation != "" {
		t.Errorf("INBOX Generation = %q, want empty (LIST has no UIDVALIDITY)", inbox.Generation)
	}

	sent := byID["Sent"]
	if sent.Role != `\Sent` {
		t.Errorf("Sent Role = %q, want \\Sent", sent.Role)
	}

	parent := byID["Parent"]
	if parent.Selectable {
		t.Error("\\Noselect parent should not be selectable")
	}

	child := byID["Parent/Child"]
	if child.Name != "Child" {
		t.Errorf("child Name = %q, want Child", child.Name)
	}
	if child.ParentID != "Parent" {
		t.Errorf("child ParentID = %q, want Parent", child.ParentID)
	}
}

func TestSplitFolderPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		delim      rune
		wantName   string
		wantParent string
	}{
		{"nested", "Work/2026/Q1", '/', "Q1", "Work/2026"},
		{"no delimiter", "Inbox", '/', "Inbox", ""},
		{"flat server", "A/B", 0, "A/B", ""},
		{"delimiter first", "/a", '/', "a", ""},
		{"delimiter last", "a/", '/', "", "a"},
		{"dot delimiter", "INBOX.Archive.Old", '.', "Old", "INBOX.Archive"},
		{"multi-byte delimiter", "a›b›c", '›', "c", "a›b"},
		{"empty", "", '/', "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, parent := splitFolderPath(tt.path, tt.delim)
			if name != tt.wantName || parent != tt.wantParent {
				t.Errorf("splitFolderPath(%q, %q) = (%q, %q), want (%q, %q)",
					tt.path, tt.delim, name, parent, tt.wantName, tt.wantParent)
			}
		})
	}
}

func condMailbox(modseq uint64, uidNext goimap.UID, messages uint32) *Mailbox {
	return &Mailbox{Name: "INBOX", UIDValidity: 9, HighestModSeq: modseq, UIDNext: uidNext, NumMessages: messages}
}

// goodCursor is the stored cursor of a CONDSTORE INBOX at modseq 100 with 50
// messages and UIDNEXT 51.
var goodCursor = imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()

// listChanges asks for the delta since goodCursor.
func listChanges(fake *fakeSyncClient, ensure []string) (psync.Delta, error) {
	return newAdapter(fake).ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: goodCursor}, ensure)
}

func TestListChangesUnchangedCostsOneSelect(t *testing.T) {
	fake := &fakeSyncClient{mailbox: condMailbox(100, 51, 50)}
	d, err := listChanges(fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Unchanged || d.Cursor != goodCursor || fake.changedCalls != 0 || fake.searchCalls != 0 || fake.selectCalls != 1 {
		t.Fatalf("delta %+v, changed=%d search=%d select=%d", d, fake.changedCalls, fake.searchCalls, fake.selectCalls)
	}
}

func TestListChangesReportsFlagChangesWithoutMembers(t *testing.T) {
	fake := &fakeSyncClient{
		mailbox:      condMailbox(120, 51, 50),
		changedSince: []MessageHeader{{UID: 7, Flags: []goimap.Flag{goimap.FlagSeen}}, {UID: 40}},
	}
	d, err := listChanges(fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fake.changedArg != 100 {
		t.Fatalf("CHANGEDSINCE %d, want 100", fake.changedArg)
	}
	if len(d.Changed) != 2 || d.Changed[0].RemoteID != "40" || d.Changed[1].RemoteID != "7" || d.Changed[1].Flags != storage.FlagSeen {
		t.Fatalf("Changed = %+v, want 40 then 7 with \\Seen", d.Changed)
	}
	if d.Members != nil || fake.searchCalls != 0 {
		t.Fatal("no arrival or expunge: members must not be listed")
	}
}

func TestListChangesExpungePlusArrivalFetchesMembers(t *testing.T) {
	// One message expunged, one arrived: MESSAGES is the same, UIDNEXT moved.
	fake := &fakeSyncClient{mailbox: condMailbox(100, 52, 50), allUIDs: []goimap.UID{2, 3, 51}}
	d, err := listChanges(fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fake.searchCalls != 1 || len(d.Members) != 3 || d.Members[2].LegacyUID != 51 {
		t.Fatalf("Members = %+v (search calls %d)", d.Members, fake.searchCalls)
	}
	if d.Unchanged {
		t.Fatal("moved mailbox reported Unchanged")
	}
}

func TestListChangesEmptiedMailboxListsEmptyMembers(t *testing.T) {
	fake := &fakeSyncClient{mailbox: condMailbox(100, 51, 0)}
	d, err := listChanges(fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Unchanged || d.Members == nil || len(d.Members) != 0 {
		t.Fatalf("Members = %#v, Unchanged = %v; want non-nil empty", d.Members, d.Unchanged)
	}
}

func TestListChangesNeedsFullList(t *testing.T) {
	tests := []struct {
		name    string
		mailbox *Mailbox
		token   string
	}{
		{"no cursor", condMailbox(100, 51, 50), ""},
		{"foreign token", condMailbox(100, 51, 50), "st-other"},
		{"no condstore", &Mailbox{Name: "INBOX", UIDValidity: 9, UIDNext: 51, NumMessages: 50}, goodCursor},
		{"uidvalidity changed", &Mailbox{Name: "INBOX", UIDValidity: 10, HighestModSeq: 100, UIDNext: 51, NumMessages: 50}, goodCursor},
		// Same UIDVALIDITY but an older HIGHESTMODSEQ: the mailbox was
		// restored, and CHANGEDSINCE the stored modseq would miss changes.
		{"modseq went backwards", condMailbox(90, 51, 50), goodCursor},
		// A SEARCH that comes back empty for a mailbox SELECT says is not empty is a
		// parser or server quirk; taken at face value it would delete every cached
		// row, so the delta gives way to a full list.
		{"empty search on non-empty mailbox", condMailbox(100, 52, 50), goodCursor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ad := newAdapter(&fakeSyncClient{mailbox: tt.mailbox})
			_, err := ad.ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: tt.token}, nil)
			if !errors.Is(err, psync.ErrNeedFullList) {
				t.Fatalf("err = %v, want ErrNeedFullList", err)
			}
		})
	}
}

func TestListChangesReportsEnsureFlags(t *testing.T) {
	fake := &fakeSyncClient{
		mailbox: condMailbox(100, 51, 50),
		flags:   []MessageHeader{{UID: 7, Flags: []goimap.Flag{goimap.FlagFlagged}}},
	}
	d, err := listChanges(fake, []string{"7"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Unchanged || len(d.Changed) != 1 || d.Changed[0].Flags != storage.FlagFlagged {
		t.Fatalf("delta = %+v, want 7 with its server flags", d)
	}
}

func TestListMessagesReturnsCursorWithoutEnvelopes(t *testing.T) {
	fake := &fakeSyncClient{
		mailbox: condMailbox(100, 3, 2),
		flags:   []MessageHeader{{UID: 1}, {UID: 2}},
	}
	headers, token, _, err := newAdapter(fake).ListMessages(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.headerFetches) != 0 {
		t.Fatalf("full list fetched envelopes: %+v", fake.headerFetches)
	}
	if len(headers) != 2 || headers[0].HasListMeta {
		t.Fatalf("headers = %+v, want flags only", headers)
	}
	if token != (imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 3, Messages: 2}).String() {
		t.Fatalf("cursor = %q", token)
	}
}

func TestFetchListMetaAsksOnlyForGivenIDs(t *testing.T) {
	when := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	fake := &fakeSyncClient{
		mailbox: &Mailbox{Name: "INBOX", UIDValidity: 9},
		envelopes: map[goimap.UID]MessageHeader{5: {
			UID: 5, HasEnvelope: true, Subject: "hi", From: "Ada <ada@ex>", FromName: "Ada", To: "bob@ex", Date: when,
			Flags: []goimap.Flag{goimap.FlagSeen}, Size: 4321,
		}},
	}
	got, err := newAdapter(fake).FetchListMeta(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX"}, []string{"5", "4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.headerFetches) != 1 || uidStrings(fake.headerFetches[0].uids) != "5,4" || !fake.headerFetches[0].options.Envelope || !fake.headerFetches[0].options.RFC822Size {
		t.Fatalf("fetches = %+v", fake.headerFetches)
	}
	if len(fake.headerFetches[0].options.BodySection) != 0 {
		t.Fatal("list meta must not ask for body sections")
	}
	if len(got) != 1 || got[0].RemoteID != "5" || !got[0].HasListMeta || got[0].Flags != storage.FlagSeen {
		t.Fatalf("headers = %+v, want only 5 (4 has no envelope)", got)
	}
	g := got[0]
	if g.Subject != "hi" || g.From != "Ada <ada@ex>" || g.FromName != "Ada" || g.To != "bob@ex" || !g.Date.Equal(when) || g.Size != 4321 {
		t.Fatalf("envelope mapping = %+v", g)
	}
}

// A body that does not parse is reported per message, so the engine can go
// on with the rest of the folder.
func TestFetchReportsUnparsableMessages(t *testing.T) {
	fake := &fakeSyncClient{unparsable: map[goimap.UID]bool{2: true, 4: true}}
	got, err := newAdapter(fake).Fetch(context.Background(), "INBOX", []string{"1", "2", "3", "4"})
	var me *psync.MessageError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want *MessageError", err)
	}
	if strings.Join(me.RemoteIDs, ",") != "2,4" {
		t.Fatalf("unreadable = %v, want 2,4", me.RemoteIDs)
	}
	if len(got) != 2 || got[0].RemoteID != "1" || got[1].RemoteID != "3" {
		t.Fatalf("fetched %+v, want 1 and 3", got)
	}
}

// A large message comes in chunks: each FetchChunk asks for one range, a
// short chunk is the end, and ParseChunked reads the whole source.
func TestFetchChunkAndParse(t *testing.T) {
	src := []byte("From: ada@ex\r\nSubject: big\r\n\r\n" + strings.Repeat("x", 100) + "\r\n")
	fake := &fakeSyncClient{sources: map[goimap.UID][]byte{7: src}}
	a := newAdapter(fake)
	ctx := context.Background()

	var raw []byte
	var last psync.Fetched
	for {
		chunk, err := a.FetchChunk(ctx, "INBOX", "7", int64(len(raw)), 40)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, chunk.Raw...)
		last = chunk
		if len(chunk.Raw) < 40 {
			break
		}
	}
	if string(raw) != string(src) {
		t.Fatalf("assembled %q, want the source", raw)
	}
	if fake.selectPath != "INBOX" || len(fake.partials) != (len(src)+39)/40 {
		t.Fatalf("partials %v", fake.partials)
	}
	last.Raw = raw
	got, err := a.ParseChunked(last)
	if err != nil {
		t.Fatal(err)
	}
	if got.RemoteID != "7" || got.LegacyUID != 7 || got.Flags != storage.FlagSeen || !strings.HasPrefix(got.Text, "xxx") || got.Size != int64(len(src)) {
		t.Fatalf("parsed %+v", got)
	}

	if _, err := a.FetchChunk(ctx, "INBOX", "8", 0, 40); !errors.Is(err, psync.ErrNotOnServer) {
		t.Fatalf("missing uid: err %v, want ErrNotOnServer", err)
	}
}

// Every fetch path refuses a mailbox whose UIDVALIDITY is not the generation
// the ids came from, before fetching anything.
func TestFetchPathsCheckGeneration(t *testing.T) {
	newFake := func() *fakeSyncClient {
		return &fakeSyncClient{mailbox: &Mailbox{UIDValidity: 9}, sources: map[goimap.UID][]byte{3: []byte("abc")}}
	}
	ctx := psync.WithGeneration(context.Background(), "INBOX", "8")

	fake := newFake()
	if got, err := newAdapter(fake).Fetch(ctx, "INBOX", []string{"3"}); !errors.Is(err, psync.ErrGenerationChanged) || len(got) != 0 {
		t.Errorf("Fetch: %d messages, err %v", len(got), err)
	}
	if _, err := newAdapter(fake).FetchChunk(ctx, "INBOX", "3", 0, 10); !errors.Is(err, psync.ErrGenerationChanged) || len(fake.partials) != 0 {
		t.Errorf("FetchChunk: err %v, %d partial fetches", err, len(fake.partials))
	}
	box := psync.RemoteMailbox{RemoteID: "INBOX", Generation: "8"}
	if _, err := newAdapter(fake).FetchListMeta(context.Background(), box, []string{"3"}); !errors.Is(err, psync.ErrGenerationChanged) || len(fake.headerFetches) != 0 {
		t.Errorf("FetchListMeta: err %v, %d fetches", err, len(fake.headerFetches))
	}

	// The matching generation, or none, fetches.
	ok := psync.WithGeneration(context.Background(), "INBOX", "9")
	if got, err := newAdapter(newFake()).Fetch(ok, "INBOX", []string{"3"}); err != nil || len(got) != 1 {
		t.Errorf("Fetch with the right generation: %d, %v", len(got), err)
	}
	if got, err := newAdapter(newFake()).Fetch(context.Background(), "INBOX", []string{"3"}); err != nil || len(got) != 1 {
		t.Errorf("Fetch without a generation: %d, %v", len(got), err)
	}
}

// A tagged NO or BAD to a push is psync.ErrRefused and keeps its text; a
// transport error and a changed UIDVALIDITY are not.
func TestSetFlagsMarksOnlyServerRefusals(t *testing.T) {
	no := &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "mailbox is read-only"}
	bad := &goimap.Error{Type: goimap.StatusResponseTypeBad, Text: "syntax"}
	for _, tc := range []struct {
		name        string
		addErr      error
		generation  string
		wantRefused bool
	}{
		{"NO", fmt.Errorf("imap: store flags on uid 3: %w", no), "", true},
		{"BAD", fmt.Errorf("imap: store flags on uid 3: %w", bad), "", true},
		{"connection lost", errors.New("connection reset by peer"), "", false},
		{"generation changed", nil, "8", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSyncClient{mailbox: &Mailbox{UIDValidity: 9}, addErr: tc.addErr}
			ctx := psync.WithGeneration(context.Background(), "INBOX", tc.generation)
			err := newAdapter(fake).SetFlags(ctx, "INBOX", "3", storage.FlagSeen)
			if err == nil {
				t.Fatal("no error")
			}
			if got := errors.Is(err, psync.ErrRefused); got != tc.wantRefused {
				t.Fatalf("errors.Is(%v, ErrRefused) = %v, want %v", err, got, tc.wantRefused)
			}
			if tc.addErr != nil && err.Error() != tc.addErr.Error() {
				t.Errorf("text %q, want %q", err, tc.addErr)
			}
		})
	}
}
