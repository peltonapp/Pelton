package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func newFolderTestDB(t *testing.T) (*DB, int64) {
	t.Helper()
	ctx := context.Background()
	db, err := openMigrated(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	id, err := db.CreateAccount(ctx, &Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return db, id
}

// TestFoldersSyncByDefault is the migration guard: every folder that already
// existed, and every one discovered later, has to keep syncing. Only an
// explicit uncheck changes that.
func TestFoldersSyncByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID := newFolderTestDB(t)

	f := &Folder{AccountID: accountID, Name: "Archive", IMAPPath: "Archive"}
	if _, err := db.CreateFolder(ctx, f); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	got, err := db.GetFolder(ctx, f.ID)
	if err != nil {
		t.Fatalf("get folder: %v", err)
	}
	if got.SyncExcluded {
		t.Error("a newly discovered folder is excluded from sync, want included")
	}
}

func TestSetFolderSyncExcluded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID := newFolderTestDB(t)

	archive := &Folder{AccountID: accountID, Name: "Archive", IMAPPath: "Archive"}
	inbox := &Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	for _, f := range []*Folder{archive, inbox} {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatalf("create folder %s: %v", f.Name, err)
		}
	}

	if err := db.SetFolderSyncExcluded(ctx, archive.ID, true); err != nil {
		t.Fatalf("exclude: %v", err)
	}

	folders, err := db.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byName := map[string]Folder{}
	for _, f := range folders {
		byName[f.Name] = f
	}
	if !byName["Archive"].SyncExcluded {
		t.Error("Archive should be excluded")
	}
	if byName["INBOX"].SyncExcluded {
		t.Error("excluding one folder excluded another")
	}

	// and back again, since unchecking by mistake has to be undoable.
	if err := db.SetFolderSyncExcluded(ctx, archive.ID, false); err != nil {
		t.Fatalf("re-include: %v", err)
	}
	got, err := db.GetFolder(ctx, archive.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SyncExcluded {
		t.Error("re-including did not take")
	}
}

// TestExcludingKeepsCachedMail pins the promise the ui makes: unchecking a
// folder stops it being updated, it does not throw away what is already there.
func TestExcludingKeepsCachedMail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID := newFolderTestDB(t)

	f := &Folder{AccountID: accountID, Name: "Archive", IMAPPath: "Archive"}
	if _, err := db.CreateFolder(ctx, f); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	msg := &Message{AccountID: accountID, FolderID: f.ID, UID: 1, Subject: "kept"}
	if _, err := db.InsertMessageWithAttachments(ctx, msg, nil); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if err := db.SetFolderSyncExcluded(ctx, f.ID, true); err != nil {
		t.Fatalf("exclude: %v", err)
	}

	msgs, err := db.ListMessages(ctx, f.ID, 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("excluding the folder dropped its cached mail: got %d messages, want 1", len(msgs))
	}
}
