package sync

import (
	"context"
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
)

// TestSyncOnlyDeletesWhatTheUserDeleted is the guard on the whole destructive
// path: a folder sync must hand the imap layer exactly the uids the user marked
// for deletion, and never widen that set because the cache and the server
// disagree about anything else.
func TestSyncOnlyDeletesWhatTheUserDeleted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3, 4, 5}}
	engine := NewEngine(client, db, nil)

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
	var target uint32
	for _, s := range states {
		if s.UID == 3 {
			target = s.UID
			if err := db.MarkDeletePending(ctx, s.ID); err != nil {
				t.Fatalf("mark pending: %v", err)
			}
		}
	}
	if target == 0 {
		t.Fatal("uid 3 was not cached")
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(client.deleted, []imap.UID{3}) {
		t.Errorf("sync asked the server to delete %v, want just [3]", client.deleted)
	}
}

// TestSyncDeletesNothingOnTheServerWhenTheCacheIsStale covers the case that
// would be worst: the server dropped messages the cache still has. That is a
// local cleanup, and it must never turn into a server-side delete.
func TestSyncDeletesNothingOnTheServerWhenTheCacheIsStale(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3, 4, 5}}
	engine := NewEngine(client, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// the server now reports an empty mailbox, the shape a bad SELECT or another
	// client emptying the folder would produce.
	client.uids = nil
	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Deleted != 5 {
		t.Errorf("dropped %d cached messages, want 5", res.Deleted)
	}
	if len(client.deleted) != 0 {
		t.Errorf("sync deleted %v on the server, want nothing", client.deleted)
	}
}

// TestSyncDoesNotPushADeleteTheServerAlreadyApplied: the message is gone
// upstream, so there is nothing to expunge and the local row just goes.
func TestSyncDoesNotPushADeleteTheServerAlreadyApplied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2}}
	engine := NewEngine(client, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	for _, s := range states {
		if s.UID == 2 {
			if err := db.MarkDeletePending(ctx, s.ID); err != nil {
				t.Fatalf("mark pending: %v", err)
			}
		}
	}

	client.uids = []uint32{1}
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(client.deleted) != 0 {
		t.Errorf("sync deleted %v on the server, want nothing", client.deleted)
	}
}

// deleteOne marks the cached message with the given uid for deletion.
func deleteOne(t *testing.T, db *storage.DB, folderID int64, uid uint32) {
	t.Helper()
	states, err := db.ListMessageStates(context.Background(), folderID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	for _, s := range states {
		if s.UID == uid {
			if err := db.MarkDeletePending(context.Background(), s.ID); err != nil {
				t.Fatalf("mark pending: %v", err)
			}
			return
		}
	}
	t.Fatalf("uid %d is not cached", uid)
}

// TestDeleteMovesToTrash is the behaviour people expect of a delete: the
// message goes to the trash, where it can still be recovered, rather than being
// destroyed on the spot.
func TestDeleteMovesToTrash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3}}
	engine := NewEngine(client, db, nil)
	engine.TrashPath = "Trash"
	engine.TrashFolderID = folder.ID + 1

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	deleteOne(t, db, folder.ID, 2)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(client.moved, []imap.UID{2}) {
		t.Errorf("moved %v to the trash, want [2]", client.moved)
	}
	if client.movedTo != "Trash" {
		t.Errorf("moved to %q, want %q", client.movedTo, "Trash")
	}
	if len(client.deleted) != 0 {
		t.Errorf("expunged %v as well, want nothing", client.deleted)
	}
}

// TestDeleteFromTrashIsPermanent: the message is already in the trash, so there
// is nowhere left to move it. This is emptying the trash, and deleting a single
// message from inside it.
func TestDeleteFromTrashIsPermanent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2}}
	engine := NewEngine(client, db, nil)
	engine.TrashPath = folder.IMAPPath
	engine.TrashFolderID = folder.ID

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	deleteOne(t, db, folder.ID, 1)
	deleteOne(t, db, folder.ID, 2)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(client.deleted, []imap.UID{2, 1}) && !slices.Equal(client.deleted, []imap.UID{1, 2}) {
		t.Errorf("expunged %v, want both uids", client.deleted)
	}
	if len(client.moved) != 0 {
		t.Errorf("moved %v out of the trash, want nothing", client.moved)
	}
}

// TestDeleteWithoutATrashFolderIsPermanent covers an account whose server has
// no trash: there is nothing to move to, so the delete is the old expunge.
func TestDeleteWithoutATrashFolderIsPermanent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2}}
	engine := NewEngine(client, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	deleteOne(t, db, folder.ID, 1)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if !slices.Equal(client.deleted, []imap.UID{1}) {
		t.Errorf("expunged %v, want [1]", client.deleted)
	}
	if len(client.moved) != 0 {
		t.Errorf("moved %v, want nothing: there is no trash folder", client.moved)
	}
}

// TestTrashableGuardsOnPathAndID: a folder that merely shares the trash's path,
// or its id, is the trash as far as this decision goes. Getting it wrong either
// way is bad: expunging what should have been moved destroys mail, and moving
// the trash into itself fails or loops.
func TestTrashable(t *testing.T) {
	t.Parallel()
	engine := &Engine{TrashPath: "Trash", TrashFolderID: 7}
	tests := []struct {
		name   string
		folder storage.Folder
		want   bool
	}{
		{"an ordinary folder", storage.Folder{ID: 3, IMAPPath: "INBOX"}, true},
		{"the trash by id", storage.Folder{ID: 7, IMAPPath: "Trash"}, false},
		{"the trash by path alone", storage.Folder{ID: 9, IMAPPath: "Trash"}, false},
		{"the trash by id alone", storage.Folder{ID: 7, IMAPPath: "Bin"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := engine.trashable(tt.folder); got != tt.want {
				t.Errorf("trashable() = %t, want %t", got, tt.want)
			}
		})
	}
	none := &Engine{}
	if none.trashable(storage.Folder{ID: 3, IMAPPath: "INBOX"}) {
		t.Error("trashable() is true with no trash folder configured")
	}
}
