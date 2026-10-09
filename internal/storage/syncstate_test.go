package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFolderHasOlderOnServerFloorID(t *testing.T) {
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
	folderID, err := db.CreateFolder(ctx, &Folder{
		AccountID: accountID, Name: "INBOX", IMAPPath: "Mb1", RemoteID: "Mb1",
	})
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
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
