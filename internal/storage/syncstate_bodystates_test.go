package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// The bulk download plans from MessageBodyStates: rows older than the cutoff
// stay out, stubs come back as incomplete, and a row queued for deletion is
// never planned since the server copy is about to go.
func TestMessageBodyStatesFiltersByDateAndPendingDelete(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@b.test"})
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "Mb1", RemoteID: "Mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatalf("folder: %v", err)
	}
	now := time.Now()
	complete, err := db.InsertMessage(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, UID: 1, RemoteID: "E1",
		Date: now, BodyComplete: true,
	})
	if err != nil {
		t.Fatalf("insert complete: %v", err)
	}
	stub, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "E2", Date: now,
	})
	if err != nil {
		t.Fatalf("insert stub: %v", err)
	}
	old, err := db.InsertMessage(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "E3",
		Date: now.AddDate(-2, 0, 0), BodyComplete: true,
	})
	if err != nil {
		t.Fatalf("insert old: %v", err)
	}
	deleted, err := db.InsertMessage(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "E4",
		Date: now, BodyComplete: true,
	})
	if err != nil {
		t.Fatalf("insert deleted: %v", err)
	}
	if err := db.MarkDeletePending(ctx, deleted); err != nil {
		t.Fatalf("mark delete: %v", err)
	}

	recent, err := db.MessageBodyStates(ctx, folder.ID, now.AddDate(-1, 0, 0))
	if err != nil {
		t.Fatalf("MessageBodyStates: %v", err)
	}
	want := []MessageBodyState{
		{ID: complete, UID: 1, RemoteID: "E1", BodyComplete: true},
		{ID: stub, RemoteID: "E2", BodyComplete: false},
	}
	if len(recent) != len(want) {
		t.Fatalf("since a year ago: got %+v, want %+v", recent, want)
	}
	for i := range want {
		if recent[i] != want[i] {
			t.Errorf("since a year ago [%d]: got %+v, want %+v", i, recent[i], want[i])
		}
	}

	all, err := db.MessageBodyStates(ctx, folder.ID, time.Time{})
	if err != nil {
		t.Fatalf("MessageBodyStates zero since: %v", err)
	}
	var ids []int64
	for _, s := range all {
		ids = append(ids, s.ID)
	}
	if len(ids) != 3 || ids[0] != complete || ids[1] != stub || ids[2] != old {
		t.Fatalf("zero since: got ids %v, want [%d %d %d]", ids, complete, stub, old)
	}
}
