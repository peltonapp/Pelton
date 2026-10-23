package storage

import (
	"context"
	"slices"
	"testing"
	"time"
)

// The bulk download plans from MessageBodyStates: rows older than the cutoff
// stay out, stubs come back as incomplete, and a row queued for deletion is
// never planned since the server copy is about to go.
func TestMessageBodyStatesFiltersByDateAndPendingDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
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
	if !slices.Equal(recent, want) {
		t.Fatalf("since a year ago: got %+v, want %+v", recent, want)
	}

	all, err := db.MessageBodyStates(ctx, folder.ID, time.Time{})
	if err != nil {
		t.Fatalf("MessageBodyStates zero since: %v", err)
	}
	if !slices.EqualFunc(all, []int64{complete, stub, old}, func(s MessageBodyState, id int64) bool { return s.ID == id }) {
		t.Fatalf("zero since: got %+v, want ids [%d %d %d]", all, complete, stub, old)
	}
}
