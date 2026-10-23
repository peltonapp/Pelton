package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func moveTestApp(t *testing.T) (*App, *storage.DB, context.Context) {
	t.Helper()
	ctx, stopBackground := testContext(t)
	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
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

// moveTestAccount builds an account with an inbox and an archive folder, and one
// message sitting in archiveFolder when inArchive is set, otherwise in the inbox.
func moveTestAccount(t *testing.T, db *storage.DB, ctx context.Context, inArchive bool) (storage.Folder, storage.Folder, int64) {
	t.Helper()
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	archive := &storage.Folder{AccountID: accountID, Name: "Archive", IMAPPath: "Archive"}
	for _, f := range []*storage.Folder{inbox, archive} {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatalf("create folder: %v", err)
		}
	}
	home := inbox
	if inArchive {
		home = archive
	}
	id, err := db.InsertMessage(ctx, &storage.Message{
		AccountID: accountID,
		FolderID:  home.ID,
		UID:       1,
		MessageID: "<one@example.com>",
		Subject:   "hello",
		Date:      time.Now(),
	})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	return *inbox, *archive, id
}

// The bug: archiving a message that is already in Archive reported success
// without doing anything, and the ui drops the row on success. The message was
// still on the server and still in the local cache, but it was gone from the
// list until the next sync put it back, which reads as lost mail.
func TestArchiveMessageAlreadyInArchiveFails(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	_, _, messageID := moveTestAccount(t, db, ctx, true)

	if _, err := a.ArchiveMessage(messageID); err == nil {
		t.Fatal("ArchiveMessage into the folder the message is already in returned no error")
	}

	if _, err := db.GetMessage(ctx, messageID); err != nil {
		t.Errorf("the message was dropped from the cache anyway: %v", err)
	}
}

// The same refusal covers the explicit move, which reaches moveMessageTo by the
// other path.
func TestMoveMessageToCurrentFolderFails(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, messageID := moveTestAccount(t, db, ctx, false)

	if _, err := a.MoveMessage(messageID, inbox.ID); err == nil {
		t.Fatal("MoveMessage into the message's own folder returned no error")
	}

	if _, err := db.GetMessage(ctx, messageID); err != nil {
		t.Errorf("the message was dropped from the cache anyway: %v", err)
	}
}
