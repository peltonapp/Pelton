package desktop

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// offlineStubApp returns an App holding one body-less stub message.
func offlineStubApp(t *testing.T) (*App, *storage.DB, int64) {
	t.Helper()
	ctx, stopBackground := testContext(t)
	t.Cleanup(stopBackground)

	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	accountID, err := db.CreateAccount(ctx, &storage.Account{
		Email:    "reader@example.com",
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"}
	if _, err := db.CreateFolder(ctx, folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	msgID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID,
		FolderID:  folder.ID,
		RemoteID:  "uid7",
		Subject:   "Stub subject",
	})
	if err != nil {
		t.Fatalf("insert stub: %v", err)
	}
	app := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}
	app.ensureAccountScheduler(ctx, accountID)
	return app, db, msgID
}

func TestDownloadMessageOfflineFetchesStubBody(t *testing.T) {
	app, db, id := offlineStubApp(t)
	app.onDemandFetchForTest = func(ctx context.Context, acc storage.Account, f storage.Folder, remoteIDs []string) error {
		_, err := db.InsertMessageWithAttachments(ctx, &storage.Message{
			AccountID: acc.ID,
			FolderID:  f.ID,
			RemoteID:  remoteIDs[0],
			Subject:   "Stub subject",
			BodyPlain: "full body",
		}, nil)
		return err
	}

	if err := app.DownloadMessageOffline(id); err != nil {
		t.Fatalf("DownloadMessageOffline: %v", err)
	}
	m, err := db.GetMessage(app.ctx, id)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if !m.BodyComplete || !m.Offline {
		t.Fatalf("BodyComplete=%v Offline=%v, want both true", m.BodyComplete, m.Offline)
	}
}

func TestDownloadMessageOfflineFetchFailureDoesNotPin(t *testing.T) {
	app, db, id := offlineStubApp(t)
	app.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error {
		return errors.New("offline")
	}

	if err := app.DownloadMessageOffline(id); err == nil {
		t.Fatal("DownloadMessageOffline succeeded, want the fetch error")
	}
	m, err := db.GetMessage(app.ctx, id)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if m.Offline {
		t.Fatal("message pinned offline although its body was never fetched")
	}
}
