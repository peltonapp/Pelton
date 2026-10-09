package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func trashTestApp(t *testing.T) (*App, *storage.DB, context.Context) {
	t.Helper()
	ctx, stopBackground := testContext(t)
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t.Cleanup(stopBackground)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}, db, ctx
}

// TestSyncEngineKnowsWhereTheTrashIs: without this wiring every delete would
// fall back to a permanent expunge, which is exactly what this change is
// getting away from.
func TestSyncEngineKnowsWhereTheTrashIs(t *testing.T) {
	a, db, ctx := trashTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	trash := &storage.Folder{AccountID: accountID, Name: "Trash", IMAPPath: "Trash"}
	for _, f := range []*storage.Folder{inbox, trash} {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatalf("create folder: %v", err)
		}
	}

	engine := a.newSyncEngine(nil, accountID)
	if engine.TrashRemoteID != "Trash" {
		t.Errorf("TrashRemoteID = %q, want %q", engine.TrashRemoteID, "Trash")
	}
	if engine.TrashFolderID != trash.ID {
		t.Errorf("TrashFolderID = %d, want %d", engine.TrashFolderID, trash.ID)
	}
}

// TestSyncEngineWithNoTrashFolder: an account whose server has no trash keeps
// the permanent delete, since there is nowhere to move mail to.
func TestSyncEngineWithNoTrashFolder(t *testing.T) {
	a, db, ctx := trashTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, inbox); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	engine := a.newSyncEngine(nil, accountID)
	if engine.TrashRemoteID != "" {
		t.Errorf("TrashRemoteID = %q, want it empty", engine.TrashRemoteID)
	}
}

// TestFindTrashFolderFollowsARoleOverride: the user can tell Pelton which
// folder is the trash, and a delete has to respect that rather than the name.
func TestFindTrashFolderFollowsARoleOverride(t *testing.T) {
	a, db, ctx := trashTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	bin := &storage.Folder{AccountID: accountID, Name: "Papierkorb", IMAPPath: "Papierkorb"}
	for _, f := range []*storage.Folder{inbox, bin} {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatalf("create folder: %v", err)
		}
	}
	if err := db.SetFolderRoleOverride(ctx, bin.ID, roleTrash); err != nil {
		t.Fatalf("set role: %v", err)
	}

	folder, ok := a.findTrashFolder(accountID)
	if !ok {
		t.Fatal("findTrashFolder found nothing")
	}
	if folder.ID != bin.ID {
		t.Errorf("trash is folder %d, want %d", folder.ID, bin.ID)
	}
}
