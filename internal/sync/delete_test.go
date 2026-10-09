package sync

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

var errFetchBoom = errors.New("fetch boom")

// TestSyncOnlyDeletesWhatTheUserDeleted is the guard on the whole destructive
// path: a folder sync must hand the adapter exactly the ids the user marked
// for deletion, and never widen that set because the cache and the server
// disagree about anything else.
func TestSyncOnlyDeletesWhatTheUserDeleted(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5)}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	if len(states) != 5 {
		t.Fatalf("cached %d messages, want 5", len(states))
	}
	// the user deletes exactly one of them.
	var target string
	for _, s := range states {
		if s.RemoteID == "3" {
			target = s.RemoteID
			if err := db.MarkDeletePending(ctx, s.ID); err != nil {
				t.Fatalf("mark pending: %v", err)
			}
		}
	}
	if target == "" {
		t.Fatal("id 3 was not cached")
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(adapter.deleted, []string{"3"}) {
		t.Errorf("sync asked the server to delete %v, want just [3]", adapter.deleted)
	}
}

// TestSyncDeletesNothingOnTheServerWhenTheCacheIsStale covers the case that
// would be worst: the server dropped messages the cache still has. That is a
// local cleanup, and it must never turn into a server-side delete.
func TestSyncDeletesNothingOnTheServerWhenTheCacheIsStale(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5)}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// the server now reports an empty mailbox, the shape a bad SELECT or another
	// client emptying the folder would produce.
	adapter.ids = nil
	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Deleted != 5 {
		t.Errorf("dropped %d cached messages, want 5", res.Deleted)
	}
	if len(adapter.deleted) != 0 {
		t.Errorf("sync deleted %v on the server, want nothing", adapter.deleted)
	}
}

// TestSyncDoesNotPushADeleteTheServerAlreadyApplied: the message is gone
// upstream, so there is nothing to expunge and the local row just goes.
func TestSyncDoesNotPushADeleteTheServerAlreadyApplied(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2)}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	for _, s := range states {
		if s.RemoteID == "2" {
			if err := db.MarkDeletePending(ctx, s.ID); err != nil {
				t.Fatalf("mark pending: %v", err)
			}
		}
	}

	adapter.ids = fakeIDs(1)
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(adapter.deleted) != 0 {
		t.Errorf("sync deleted %v on the server, want nothing", adapter.deleted)
	}
}

// deleteOne marks the cached message with the given remote id for deletion.
func deleteOne(t *testing.T, db *storage.DB, folderID int64, remoteID string) {
	t.Helper()
	states, err := db.ListMessageStates(context.Background(), folderID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	for _, s := range states {
		if s.RemoteID == remoteID {
			if err := db.MarkDeletePending(context.Background(), s.ID); err != nil {
				t.Fatalf("mark pending: %v", err)
			}
			return
		}
	}
	t.Fatalf("remote id %q is not cached", remoteID)
}

// TestDeleteMovesToTrash is the behaviour people expect of a delete: the
// message goes to the trash, where it can still be recovered, rather than being
// destroyed on the spot.
func TestDeleteMovesToTrash(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3)}
	engine := NewEngine(adapter, db, nil)
	engine.TrashRemoteID = "Trash"
	engine.TrashFolderID = folder.ID + 1

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	deleteOne(t, db, folder.ID, "2")

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(adapter.moved, []string{"2"}) {
		t.Errorf("moved %v to the trash, want [2]", adapter.moved)
	}
	if adapter.movedTo != "Trash" {
		t.Errorf("moved to %q, want %q", adapter.movedTo, "Trash")
	}
	if len(adapter.deleted) != 0 {
		t.Errorf("expunged %v as well, want nothing", adapter.deleted)
	}
}

// TestDeleteFromTrashIsPermanent: the message is already in the trash, so there
// is nowhere left to move it. This is emptying the trash, and deleting a single
// message from inside it.
func TestDeleteFromTrashIsPermanent(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2)}
	engine := NewEngine(adapter, db, nil)
	engine.TrashRemoteID = folder.RemoteID
	engine.TrashFolderID = folder.ID

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	deleteOne(t, db, folder.ID, "1")
	deleteOne(t, db, folder.ID, "2")

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(adapter.deleted, []string{"2", "1"}) && !slices.Equal(adapter.deleted, []string{"1", "2"}) {
		t.Errorf("expunged %v, want both ids", adapter.deleted)
	}
	if len(adapter.moved) != 0 {
		t.Errorf("moved %v out of the trash, want nothing", adapter.moved)
	}
}

// TestDeleteWithoutATrashFolderIsPermanent covers an account whose server has
// no trash: there is nothing to move to, so the delete is the old expunge.
func TestDeleteWithoutATrashFolderIsPermanent(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2)}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	deleteOne(t, db, folder.ID, "1")

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(adapter.deleted, []string{"1"}) {
		t.Errorf("expunged %v, want [1]", adapter.deleted)
	}
	if len(adapter.moved) != 0 {
		t.Errorf("moved %v, want nothing: there is no trash folder", adapter.moved)
	}
}

// TestTrashableGuardsOnPathAndID: a folder that merely shares the trash's remote
// id, or its id, is the trash as far as this decision goes. Getting it wrong
// either way is bad: expunging what should have been moved destroys mail, and
// moving the trash into itself fails or loops.
func TestTrashable(t *testing.T) {
	engine := &Engine{TrashRemoteID: "Trash", TrashFolderID: 7}
	tests := []struct {
		name   string
		folder storage.Folder
		want   bool
	}{
		{"an ordinary folder", storage.Folder{ID: 3, RemoteID: "INBOX"}, true},
		{"the trash by id", storage.Folder{ID: 7, RemoteID: "Trash"}, false},
		{"the trash by remote id alone", storage.Folder{ID: 9, RemoteID: "Trash"}, false},
		{"the trash by id alone", storage.Folder{ID: 7, RemoteID: "Bin"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := engine.trashable(tt.folder); got != tt.want {
				t.Errorf("trashable() = %t, want %t", got, tt.want)
			}
		})
	}
	none := &Engine{}
	if none.trashable(storage.Folder{ID: 3, RemoteID: "INBOX"}) {
		t.Error("trashable() is true with no trash folder configured")
	}
}

// A Fetch error after some bodies are stored must not skip pending deletes:
// the folder still pushes what the user already marked for deletion, then
// returns the first fetch error.
func TestSyncPushesPendingDeletesAfterFetchFailure(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3)}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	deleteOne(t, db, folder.ID, "2")

	// New mail arrives; Fetch stores the first new body then fails mid-batch.
	adapter.ids = fakeIDs(1, 2, 3, 4, 5)
	adapter.fetchErr = errFetchBoom
	adapter.fetchFailAfter = 1
	adapter.fetched = nil
	adapter.deleted = nil

	_, err := engine.SyncFolder(ctx, folder)
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if !slices.Equal(adapter.deleted, []string{"2"}) {
		t.Errorf("deleted %v after fetch failure, want [2]", adapter.deleted)
	}
	if len(adapter.fetched) != 1 || adapter.fetched[0] != "5" {
		t.Errorf("fetched %v, want newest-first partial [5]", adapter.fetched)
	}
	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	got := map[string]bool{}
	for _, m := range msgs {
		got[m.RemoteID] = true
	}
	if !got["5"] {
		t.Error("partial fetch result remote_id 5 was not stored")
	}
	if got["2"] {
		t.Error("pending delete for remote_id 2 was not applied locally")
	}
}
