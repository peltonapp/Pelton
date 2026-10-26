package storage

import (
	"context"
	"testing"
)

func TestFolderHasOlderOnServerFloorID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _, folder := newBodyTestFolder(t)
	folderID := folder.ID
	if err := db.SetFolderSyncWindow(ctx, folderID, "msg-floor", true); err != nil {
		t.Fatalf("set window: %v", err)
	}
	hasOlder, err := db.FolderHasOlderOnServer(ctx, folderID)
	if err != nil {
		t.Fatalf("FolderHasOlderOnServer: %v", err)
	}
	if !hasOlder {
		t.Fatal("an opaque floor in sync_floor_id must count as hasOlder when sync_floor_uid is 0")
	}
}

// The engine before the remote-id window only writes sync_floor_uid. Clearing
// it must also clear the id floor it mirrored (migrated from it by 0037), or
// AnyFolderHasOlder stays true and "load older" is offered for nothing.
func TestSetFolderSyncFloorUIDMaintainsFloorID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _, folder := newBodyTestFolder(t)
	older := func() bool {
		t.Helper()
		ok, err := db.AnyFolderHasOlder(ctx, []int64{folder.ID})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if err := db.SetFolderSyncFloorUID(ctx, folder.ID, 500); err != nil {
		t.Fatal(err)
	}
	f, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.SyncFloorID != "500" || !older() {
		t.Fatalf("floor id %q after uid 500", f.SyncFloorID)
	}
	if err := db.SetFolderSyncFloorUID(ctx, folder.ID, 0); err != nil {
		t.Fatal(err)
	}
	if older() {
		t.Fatal("cleared uid floor still reports older mail")
	}

	// An opaque floor from SetFolderSyncWindow survives a uid clear.
	if err := db.SetFolderSyncWindow(ctx, folder.ID, "msg-floor", true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFolderSyncFloorUID(ctx, folder.ID, 0); err != nil {
		t.Fatal(err)
	}
	if !older() {
		t.Fatal("opaque floor cleared by a uid clear")
	}
}
