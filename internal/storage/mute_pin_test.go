package storage

import (
	"context"
	"testing"
)

func TestMuteHidesConversationFromInbox(t *testing.T) {
	f := newThreadFixture(t)
	ctx := context.Background()
	first := f.add(Message{MessageID: "<p@x>", Subject: "plans", FromAddress: "ada@x.test"}, 1)
	other := f.add(Message{MessageID: "<q@x>", Subject: "other", FromAddress: "bob@x.test"}, 2)
	if err := f.db.SetThreadMuted(ctx, first.ThreadID, true); err != nil {
		t.Fatal(err)
	}
	reply := f.add(Message{MessageID: "<r@x>", Subject: "Re: plans", FromAddress: "ada@x.test", InReplyTo: []string{"<p@x>"}}, 3)
	if f.thread(reply) != first.ThreadID {
		t.Fatalf("reply landed in thread %d, want %d", f.thread(reply), first.ThreadID)
	}

	inbox := MessageQuery{FolderIDs: []int64{f.inbox}, HideMuted: true}
	sf := &splitFixture{threadFixture: f}
	sameIDs(t, "inbox", sf.ids(inbox), other.ID)
	// everywhere else the conversation is still there, marked muted.
	all, err := f.db.QueryMessages(ctx, MessageQuery{FolderIDs: []int64{f.inbox}})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || !all[0].Muted || all[1].Muted {
		t.Errorf("unfiltered list = %+v, want 3 rows with the reply muted", all)
	}
	total, unread, err := f.db.FolderCountsHidingMuted(ctx, f.inbox)
	if err != nil || total != 1 || unread != 1 {
		t.Errorf("counts = %d, %d, %v; want 1, 1", total, unread, err)
	}

	if err := f.db.SetThreadMuted(ctx, first.ThreadID, false); err != nil {
		t.Fatal(err)
	}
	sameIDs(t, "unmuted inbox", sf.ids(inbox), reply.ID, other.ID, first.ID)
}

func TestMuteSurvivesThreadMerge(t *testing.T) {
	f := newThreadFixture(t)
	ctx := context.Background()
	a := f.add(Message{MessageID: "<a@x>", Subject: "one", FromAddress: "ada@x.test"}, 1)
	b := f.add(Message{MessageID: "<b@x>", Subject: "two", FromAddress: "bob@x.test"}, 2)
	if a.ThreadID == b.ThreadID {
		t.Fatal("fixture threads should start apart")
	}
	if err := f.db.SetThreadMuted(ctx, b.ThreadID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SaveNote(ctx, b.ID, true, "from b"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SaveNote(ctx, a.ID, true, "from a"); err != nil {
		t.Fatal(err)
	}
	// a message referencing both joins the two conversations.
	f.add(Message{MessageID: "<c@x>", Subject: "Re: one", FromAddress: "cy@x.test", References: []string{"<a@x>", "<b@x>"}}, 3)
	merged := f.thread(b)
	if merged != f.thread(a) {
		t.Fatal("threads did not merge")
	}
	muted, err := f.db.ThreadMuted(ctx, merged)
	if err != nil || !muted {
		t.Errorf("merged thread muted = %v, %v; want true", muted, err)
	}
	note, err := f.db.GetNote(ctx, a.ID, true)
	if err != nil || note == nil || note.Body != "from a\n\nfrom b" {
		t.Errorf("merged note = %+v, %v", note, err)
	}
}

func TestPinnedGroupAcrossOrders(t *testing.T) {
	f := newThreadFixture(t)
	ctx := context.Background()
	old := f.add(Message{MessageID: "<old@x>", Subject: "old", FromAddress: "a@x.test"}, 1)
	mid := f.add(Message{MessageID: "<mid@x>", Subject: "mid", FromAddress: "b@x.test"}, 2)
	recent := f.add(Message{MessageID: "<new@x>", Subject: "new", FromAddress: "c@x.test"}, 3)
	// the same message filed in a second folder follows the pin.
	archive := f.folder("Archive")
	copyOfOld := f.add(Message{MessageID: "<old@x>", Subject: "old", FromAddress: "a@x.test", FolderID: archive}, 1)

	if err := f.db.SetPinned(ctx, old.ID, true); err != nil {
		t.Fatal(err)
	}
	sf := &splitFixture{threadFixture: f}
	folders := []int64{f.inbox, archive}
	sameIDs(t, "pinned", sf.ids(MessageQuery{FolderIDs: folders, Pin: PinOnly}), copyOfOld.ID, old.ID)
	sameIDs(t, "rest", sf.ids(MessageQuery{FolderIDs: folders, Pin: PinExclude}), recent.ID, mid.ID)

	got, err := f.db.GetMessage(ctx, copyOfOld.ID)
	if err != nil || !got.Pinned {
		t.Errorf("copy pinned = %v, %v", got, err)
	}
	pins, err := f.db.FolderPinStates(ctx, f.inbox)
	if err != nil || !pins[old.UID].Pending || !pins[old.UID].Pinned {
		t.Errorf("pin state = %+v, %v; want pinned and pending", pins, err)
	}
	if err := f.db.AdoptPin(ctx, old.ID, true); err != nil {
		t.Fatal(err)
	}
	pins, _ = f.db.FolderPinStates(ctx, f.inbox)
	if pins[old.UID].Pending {
		t.Error("adopted pin still pending")
	}

	// by conversation, a pinned reply pins the whole thread.
	reply := f.add(Message{MessageID: "<r@x>", Subject: "Re: mid", FromAddress: "a@x.test", InReplyTo: []string{"<mid@x>"}}, 4)
	if err := f.db.SetPinned(ctx, mid.ID, true); err != nil {
		t.Fatal(err)
	}
	tq := ThreadQuery{Query: MessageQuery{FolderIDs: []int64{f.inbox}, Pin: PinOnly}}
	pages, err := f.db.QueryThreads(ctx, tq)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || pages[0].Head.ID != reply.ID || pages[1].Head.ID != old.ID {
		t.Errorf("pinned threads = %+v", pages)
	}
	tq.Query.Pin = PinExclude
	pages, err = f.db.QueryThreads(ctx, tq)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Head.ID != recent.ID {
		t.Errorf("unpinned threads = %+v", pages)
	}
	if err := f.db.SetThreadPinned(ctx, f.thread(mid), false); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.db.CountThreads(ctx, tq); n != 2 {
		t.Errorf("after unpinning the thread, %d unpinned threads, want 2", n)
	}
}
