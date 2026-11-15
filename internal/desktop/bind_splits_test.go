package desktop

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

func listIDs(t *testing.T, a *App, req ListMessagesRequest) (rows, pinned []int64) {
	t.Helper()
	req.Limit = 50
	list, err := a.ListMessages(req)
	if err != nil {
		t.Fatalf("ListMessages(%+v): %v", req, err)
	}
	for _, m := range list.Messages {
		rows = append(rows, m.ID)
	}
	for _, m := range list.Pinned {
		pinned = append(pinned, m.ID)
	}
	return rows, pinned
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSplitInboxListsAndCounts(t *testing.T) {
	b := newThreadTestMailbox(t)
	a := b.app
	vip := b.add(t, b.inbox, storage.Message{MessageID: "v@x", Subject: "review", FromAddress: "Boss <boss@corp.test>"}, 1)
	news := b.add(t, b.inbox, storage.Message{MessageID: "n@x", Subject: "weekly", FromAddress: "news@shop.test", ListUnsubscribe: "<mailto:u@shop.test>", Flags: storage.FlagSeen}, 2)
	bot := b.add(t, b.inbox, storage.Message{MessageID: "b@x", Subject: "build", FromAddress: "noreply@ci.test"}, 3)
	other := b.add(t, b.inbox, storage.Message{MessageID: "o@x", Subject: "hi", FromAddress: "pat@x.test"}, 4)
	if err := a.AddVIPSender("boss@corp.test"); err != nil {
		t.Fatal(err)
	}

	// off by default: a split key changes nothing.
	rows, _ := listIDs(t, a, ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID, Split: "important"})
	if !equalIDs(rows, []int64{other, bot, news, vip}) {
		t.Fatalf("split off: rows %v", rows)
	}

	if err := a.SetSetting(settingSplitInbox, "true"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string][]int64{
		"important":     {vip},
		"newsletters":   {news},
		"notifications": {bot},
		"other":         {other},
	} {
		for _, req := range []ListMessagesRequest{
			{Kind: "folder", FolderID: b.inbox.ID, Split: key},
			{Kind: "view", View: viewInbox, Split: key},
		} {
			if rows, _ := listIDs(t, a, req); !equalIDs(rows, want) {
				t.Errorf("%s %s: rows %v, want %v", req.Kind, key, rows, want)
			}
		}
	}
	// a non-inbox folder ignores the split.
	if rows, _ := listIDs(t, a, ListMessagesRequest{Kind: "folder", FolderID: b.sent.ID, Split: "important"}); len(rows) != 0 {
		t.Errorf("sent rows %v", rows)
	}

	tabs, err := a.SplitCounts(ListMessagesRequest{Kind: "view", View: viewInbox})
	if err != nil {
		t.Fatal(err)
	}
	want := []SplitTabDTO{
		{Key: "important", Total: 1, Unread: 1},
		{Key: "newsletters", Total: 1, Unread: 0},
		{Key: "notifications", Total: 1, Unread: 1},
		{Key: "other", Total: 1, Unread: 1},
	}
	if len(tabs) != len(want) {
		t.Fatalf("tabs %+v", tabs)
	}
	for i := range want {
		if tabs[i] != want[i] {
			t.Errorf("tab %d = %+v, want %+v", i, tabs[i], want[i])
		}
	}

	// a saved view as the first tab takes what it matches from later tabs.
	view, err := a.SaveView(ViewDTO{Name: "CI", QuerySubject: "build"})
	if err != nil {
		t.Fatal(err)
	}
	viewKey := "view:" + strconv.FormatInt(view.ID, 10)
	if err := a.SetSetting(settingSplitTabs, viewKey+",important,newsletters,notifications,view:999"); err != nil {
		t.Fatal(err)
	}
	tabs, err = a.SplitCounts(ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 5 || tabs[0].Key != viewKey || tabs[0].Total != 1 || tabs[3].Total != 0 {
		t.Errorf("tabs with a view = %+v", tabs)
	}
}

func TestMutePinAndNotesThroughBindings(t *testing.T) {
	b := newThreadTestMailbox(t)
	a := b.app
	idx, err := search.Open(filepath.Join(t.TempDir(), "search.bleve"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	a.index = idx

	first := b.add(t, b.inbox, storage.Message{MessageID: "a@x", Subject: "plans", FromAddress: "ada@x.test"}, 1)
	reply := b.add(t, b.inbox, storage.Message{MessageID: "b@x", Subject: "Re: plans", FromAddress: "ada@x.test", InReplyTo: []string{"a@x"}}, 2)
	other := b.add(t, b.inbox, storage.Message{MessageID: "c@x", Subject: "lunch", FromAddress: "bob@x.test"}, 3)
	older := b.add(t, b.inbox, storage.Message{MessageID: "d@x", Subject: "old news", FromAddress: "cy@x.test"}, 0)

	// muting hides the conversation from the inbox, but not from the folder's
	// other readers.
	if err := a.SetConversationMuted(first, true); err != nil {
		t.Fatal(err)
	}
	rows, _ := listIDs(t, a, ListMessagesRequest{Kind: "view", View: viewInbox})
	if !equalIDs(rows, []int64{other, older}) {
		t.Errorf("muted inbox rows %v", rows)
	}
	views, err := a.ListUnifiedViews()
	if err != nil {
		t.Fatal(err)
	}
	if views[0].Key != viewInbox || views[0].UnreadCount != 2 {
		t.Errorf("inbox badge %+v, want 2 unread", views[0])
	}
	msg, err := a.GetMessage(reply)
	if err != nil || !msg.Muted {
		t.Errorf("reply muted = %v, %v", msg.Muted, err)
	}
	if err := a.SetConversationMuted(reply, false); err != nil {
		t.Fatal(err)
	}

	// the pinned group leads the first page and the pages leave it out.
	if err := a.SetPinned(older, true, false); err != nil {
		t.Fatal(err)
	}
	rows, pinned := listIDs(t, a, ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID})
	if !equalIDs(pinned, []int64{older}) || !equalIDs(rows, []int64{other, reply, first}) {
		t.Errorf("pinned %v rows %v", pinned, rows)
	}
	list, err := a.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID, Limit: 1, Offset: 1})
	if err != nil || len(list.Pinned) != 0 || len(list.Messages) != 1 || list.Messages[0].ID != reply {
		t.Errorf("second page %+v, %v", list, err)
	}
	// a conversation pins as a whole and unpins as a whole.
	if err := a.SetPinned(first, true, true); err != nil {
		t.Fatal(err)
	}
	threads, err := a.ListThreads(ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads.Pinned) != 2 || threads.Pinned[0].Head.ID != reply || !threads.Pinned[0].Pinned || len(threads.Threads) != 1 {
		t.Errorf("pinned threads %+v, rest %+v", threads.Pinned, threads.Threads)
	}
	if err := a.SetPinned(reply, false, true); err != nil {
		t.Fatal(err)
	}
	if _, pinned := listIDs(t, a, ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID}); !equalIDs(pinned, []int64{older}) {
		t.Errorf("after unpinning the conversation pinned %v", pinned)
	}

	// notes: saved locally, searchable by note: and has:note.
	if _, err := a.SaveNote(other, false, "Ask about the venue"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveNote(reply, true, "Bring slides"); err != nil {
		t.Fatal(err)
	}
	if note, err := a.GetNote(other, false); err != nil || note.Body != "Ask about the venue" || note.UpdatedAt == "" {
		t.Errorf("note %+v, %v", note, err)
	}
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	search := func(req SearchRequestDTO) []int64 {
		t.Helper()
		req.Limit = 50
		req.Sort = "newest"
		res, err := a.Search(req)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, m := range res.Messages {
			out = append(out, m.ID)
		}
		return out
	}
	if got := search(SearchRequestDTO{Note: "venue"}); !equalIDs(got, []int64{other}) {
		t.Errorf("note:venue %v", got)
	}
	if got := search(SearchRequestDTO{HasNote: true}); !equalIDs(got, []int64{other, reply, first}) {
		t.Errorf("has:note %v", got)
	}
	if got := search(SearchRequestDTO{Query: "plans", HasNote: true}); !equalIDs(got, []int64{reply, first}) {
		t.Errorf("plans has:note %v", got)
	}
	if got := search(SearchRequestDTO{Note: "nothing like it"}); len(got) != 0 {
		t.Errorf("unmatched note search %v", got)
	}
	if err := a.DeleteNote(other, false); err != nil {
		t.Fatal(err)
	}
	if note, _ := a.GetNote(other, false); note.Body != "" {
		t.Errorf("deleted note %+v", note)
	}
}
