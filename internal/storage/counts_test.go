package storage

import (
	"context"
	"testing"
	"time"
)

// a deleted message waits for the next sync to be expunged; until then it is
// hidden from the list, and the badges must not count it either.
func TestCountsLeaveOutHiddenRows(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	acc, _ := db.EnsureLocalAccount(ctx)
	f, _ := db.EnsureLocalFolder(ctx, acc.ID, "INBOX")

	var ids []int64
	for uid := uint32(1); uid <= 3; uid++ {
		id, err := db.InsertMessage(ctx, &Message{
			AccountID: acc.ID, FolderID: f.ID, UID: uid,
			MessageID: "<" + string(rune('a'+uid)) + "@x>", Date: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := db.MarkDeletePending(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}

	total, unread, err := db.FolderCounts(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || unread != 2 {
		t.Fatalf("folder counts = %d total, %d unread, want 2 and 2", total, unread)
	}
	n, err := db.UnreadCount(ctx, []int64{f.ID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("unread count = %d, want 2", n)
	}
}
