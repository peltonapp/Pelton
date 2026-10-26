package storage

import (
	"context"
	"testing"
	"time"
)

func TestSpamOrigin(t *testing.T) {
	ctx := context.Background()

	t.Run("round trip and replace", func(t *testing.T) {
		db := newTestDB(t)
		accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Work")
		if err := db.RecordSpamOrigin(ctx, accountID, "<a@x>", ids["INBOX"]); err != nil {
			t.Fatalf("record: %v", err)
		}
		// marking the same message again, from another folder, keeps the latest.
		if err := db.RecordSpamOrigin(ctx, accountID, "<a@x>", ids["Work"]); err != nil {
			t.Fatalf("record again: %v", err)
		}
		got, ok, err := db.SpamOrigin(ctx, accountID, "<a@x>")
		if err != nil || !ok || got != ids["Work"] {
			t.Fatalf("SpamOrigin = %d, %v, %v; want %d, true, nil", got, ok, err, ids["Work"])
		}
		if err := db.ForgetSpamOrigin(ctx, accountID, "<a@x>"); err != nil {
			t.Fatalf("forget: %v", err)
		}
		if _, ok, _ := db.SpamOrigin(ctx, accountID, "<a@x>"); ok {
			t.Error("record still there after forget")
		}
	})

	t.Run("unknown message", func(t *testing.T) {
		db := newTestDB(t)
		accountID, _ := newFolderTestAccount(t, db, "/", "INBOX")
		if _, ok, err := db.SpamOrigin(ctx, accountID, "<none@x>"); ok || err != nil {
			t.Errorf("SpamOrigin = %v, %v; want false, nil", ok, err)
		}
	})

	// a deleted folder must not leave a record pointing at a folder id that may
	// be reused for something else.
	t.Run("folder deletion drops the record", func(t *testing.T) {
		db := newTestDB(t)
		accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Work")
		if err := db.RecordSpamOrigin(ctx, accountID, "<a@x>", ids["Work"]); err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := db.DeleteFolder(ctx, ids["Work"]); err != nil {
			t.Fatalf("delete folder: %v", err)
		}
		if _, ok, _ := db.SpamOrigin(ctx, accountID, "<a@x>"); ok {
			t.Error("record survived its folder")
		}
	})

	t.Run("old records expire and are pruned", func(t *testing.T) {
		db := newTestDB(t)
		accountID, ids := newFolderTestAccount(t, db, "/", "INBOX")
		old := formatTime(time.Now().UTC().Add(-spamOriginTTL - time.Hour))
		if _, err := db.sql.ExecContext(ctx,
			`INSERT INTO spam_origins (account_id, message_id, folder_id, marked_at) VALUES (?, ?, ?, ?)`,
			accountID, "<old@x>", ids["INBOX"], old); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, ok, _ := db.SpamOrigin(ctx, accountID, "<old@x>"); ok {
			t.Error("expired record still returned")
		}
		if err := db.RecordSpamOrigin(ctx, accountID, "<new@x>", ids["INBOX"]); err != nil {
			t.Fatalf("record: %v", err)
		}
		var n int
		if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM spam_origins`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Errorf("%d records left, want 1 (the expired one pruned)", n)
		}
	})
}
