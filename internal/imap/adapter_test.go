package imap

import (
	"context"
	"errors"
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

func (f *fakeSyncClient) FetchMessages([]goimap.UID, func(goimap.UID, *Message, error) error) error {
	return nil
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
	// FlagFlagged and FlagDeleted are clear, so those managed flags are removed.
	wantRemoved := map[goimap.Flag]bool{goimap.FlagFlagged: true, goimap.FlagDeleted: true}
	for _, flag := range fake.removed {
		if !wantRemoved[flag] {
			t.Errorf("unexpected RemoveFlags %q", flag)
		}
		delete(wantRemoved, flag)
	}
	for flag := range wantRemoved {
		t.Errorf("RemoveFlags missing %q", flag)
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

func TestListChangesUnchangedCostsOneSelect(t *testing.T) {
	fake := &fakeSyncClient{mailbox: condMailbox(100, 51, 50)}
	ad := newAdapter(fake)
	cursor := imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()
	d, err := ad.ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: cursor}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Unchanged || d.Cursor != cursor || fake.changedCalls != 0 || fake.searchCalls != 0 || fake.selectCalls != 1 {
		t.Fatalf("delta %+v, changed=%d search=%d select=%d", d, fake.changedCalls, fake.searchCalls, fake.selectCalls)
	}
}

func TestListChangesReportsFlagChangesWithoutMembers(t *testing.T) {
	fake := &fakeSyncClient{
		mailbox:      condMailbox(120, 51, 50),
		changedSince: []MessageHeader{{UID: 7, Flags: []goimap.Flag{goimap.FlagSeen}}, {UID: 40}},
	}
	ad := newAdapter(fake)
	cursor := imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()
	d, err := ad.ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: cursor}, nil)
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
	ad := newAdapter(fake)
	cursor := imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()
	d, err := ad.ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: cursor}, nil)
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
	ad := newAdapter(fake)
	cursor := imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()
	d, err := ad.ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: cursor}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Unchanged || d.Members == nil || len(d.Members) != 0 {
		t.Fatalf("Members = %#v, Unchanged = %v; want non-nil empty", d.Members, d.Unchanged)
	}
}

func TestListChangesNeedsFullList(t *testing.T) {
	good := imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()
	tests := []struct {
		name    string
		mailbox *Mailbox
		token   string
	}{
		{"no cursor", condMailbox(100, 51, 50), ""},
		{"foreign token", condMailbox(100, 51, 50), "st-other"},
		{"no condstore", &Mailbox{Name: "INBOX", UIDValidity: 9, UIDNext: 51, NumMessages: 50}, good},
		{"uidvalidity changed", &Mailbox{Name: "INBOX", UIDValidity: 10, HighestModSeq: 100, UIDNext: 51, NumMessages: 50}, good},
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
	ad := newAdapter(fake)
	cursor := imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()
	d, err := ad.ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: cursor}, []string{"7"})
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
			Flags: []goimap.Flag{goimap.FlagSeen},
		}},
	}
	got, err := newAdapter(fake).FetchListMeta(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX"}, []string{"5", "4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.headerFetches) != 1 || uidStrings(fake.headerFetches[0].uids) != "5,4" || !fake.headerFetches[0].options.Envelope {
		t.Fatalf("fetches = %+v", fake.headerFetches)
	}
	if len(fake.headerFetches[0].options.BodySection) != 0 {
		t.Fatal("list meta must not ask for body sections")
	}
	if len(got) != 1 || got[0].RemoteID != "5" || !got[0].HasListMeta || got[0].Flags != storage.FlagSeen {
		t.Fatalf("headers = %+v, want only 5 (4 has no envelope)", got)
	}
	g := got[0]
	if g.Subject != "hi" || g.From != "Ada <ada@ex>" || g.FromName != "Ada" || g.To != "bob@ex" || !g.Date.Equal(when) {
		t.Fatalf("envelope mapping = %+v", g)
	}
}

// A SEARCH that comes back empty for a mailbox SELECT says is not empty is a
// parser or server quirk; taken at face value it would delete every cached
// row, so the delta gives way to a full list.
func TestListChangesEmptySearchOnNonEmptyMailboxNeedsFullList(t *testing.T) {
	fake := &fakeSyncClient{mailbox: condMailbox(100, 52, 50)}
	ad := newAdapter(fake)
	cursor := imapCursor{UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, Messages: 50}.String()
	d, err := ad.ListChanges(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", StateToken: cursor}, nil)
	if !errors.Is(err, psync.ErrNeedFullList) {
		t.Fatalf("err = %v, delta %+v; want ErrNeedFullList", err, d)
	}
}
