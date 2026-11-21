package storage

import (
	"context"
	"testing"
	"time"
)

// gmail hands a starred inbox message out once per label: INBOX, All Mail,
// Starred and Important each hold a copy with the same Message-ID (#487).
func gmailCopies(t *testing.T, db *DB) (folders []int64, copies []int64, other int64) {
	t.Helper()
	ctx := context.Background()
	acc, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2023, 5, 20, 9, 0, 0, 0, time.UTC)
	uid := uint32(0)
	for _, name := range []string{"INBOX", "[Gmail]/All Mail", "[Gmail]/Starred", "[Gmail]/Important"} {
		f, err := db.EnsureLocalFolder(ctx, acc.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		folders = append(folders, f.ID)
		uid++
		id, err := db.InsertMessage(ctx, &Message{AccountID: acc.ID, FolderID: f.ID, UID: uid,
			MessageID: "<starred@x>", Subject: "starred", Date: date, Flags: FlagFlagged})
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, id)
	}
	uid++
	other, err = db.InsertMessage(ctx, &Message{AccountID: acc.ID, FolderID: folders[1], UID: uid,
		MessageID: "<other@x>", Subject: "other", Date: date.Add(-time.Hour), Flags: FlagFlagged})
	if err != nil {
		t.Fatal(err)
	}
	return folders, copies, other
}

func TestOneCopyListsAMessageOnce(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	folders, copies, other := gmailCopies(t, db)

	q := MessageQuery{FolderIDs: folders, RequireFlags: FlagFlagged, OneCopy: true, Limit: 50}
	msgs, err := db.QueryMessages(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].ID != copies[0] || msgs[1].ID != other {
		t.Fatalf("got %d messages %+v, want the first copy and the other message", len(msgs), msgs)
	}
	n, err := db.CountMessages(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	ids, err := db.QueryMessageIDs(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Errorf("select-all ids = %v, want 2", ids)
	}

	// without OneCopy every copy is its own row, as a single folder wants.
	q.OneCopy = false
	if n, _ := db.CountMessages(ctx, q); n != 5 {
		t.Errorf("count without OneCopy = %d, want 5", n)
	}
}

// a copy that left the view, unflagged or deleted, must not hide the ones still
// in it.
func TestOneCopyFallsBackToACopyStillInTheView(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	folders, copies, _ := gmailCopies(t, db)

	if err := db.MarkFlagsPending(ctx, copies[0], 0); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkDeletePending(ctx, copies[1]); err != nil {
		t.Fatal(err)
	}
	msgs, err := db.QueryMessages(ctx, MessageQuery{FolderIDs: folders, RequireFlags: FlagFlagged, OneCopy: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].ID != copies[2] {
		t.Fatalf("got %+v, want the third copy listed", msgs)
	}
}

func TestMarkCopiesFlagPendingFollowsTheMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	folders, copies, other := gmailCopies(t, db)
	first, err := db.GetMessage(ctx, copies[0])
	if err != nil {
		t.Fatal(err)
	}

	if err := db.MarkFlagsPending(ctx, copies[0], first.Flags&^FlagFlagged); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkCopiesFlagPending(ctx, first.AccountID, copies[0], first.MessageID, FlagFlagged, false); err != nil {
		t.Fatal(err)
	}
	for _, id := range copies {
		m, err := db.GetMessage(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if m.Flags.Has(FlagFlagged) {
			t.Errorf("copy %d is still flagged", id)
		}
	}
	if m, _ := db.GetMessage(ctx, other); !m.Flags.Has(FlagFlagged) {
		t.Error("a different message lost its flag")
	}
	msgs, err := db.QueryMessages(ctx, MessageQuery{FolderIDs: folders, RequireFlags: FlagFlagged, OneCopy: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != other {
		t.Fatalf("got %+v, want only the other message left", msgs)
	}

	states, err := db.ListMessageStates(ctx, folders[2])
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states[0].PendingFlags {
		t.Errorf("the Starred copy is not queued for push: %+v", states)
	}

	// flagging again brings every copy back, still listed once.
	if err := db.MarkCopiesFlagPending(ctx, first.AccountID, copies[0], first.MessageID, FlagFlagged, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range copies[1:] {
		if m, _ := db.GetMessage(ctx, id); !m.Flags.Has(FlagFlagged) {
			t.Errorf("copy %d was not flagged again", id)
		}
	}
}
