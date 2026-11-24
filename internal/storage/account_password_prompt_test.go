package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newAccountTestDB(t *testing.T) (*DB, int64) {
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

// TestPasswordPromptAsksByDefault is the migration guard: an account that
// existed before the column, and every one created after it, still gets asked.
func TestPasswordPromptAsksByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.PasswordPromptDismissed {
		t.Error("a new account starts with the password prompt dismissed, want asking")
	}
}

func TestSetAccountPasswordPromptDismissed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	for _, want := range []bool{true, false} {
		if err := db.SetAccountPasswordPromptDismissed(ctx, id, want); err != nil {
			t.Fatalf("set dismissed %v: %v", want, err)
		}
		got, err := db.GetAccount(ctx, id)
		if err != nil {
			t.Fatalf("get account: %v", err)
		}
		if got.PasswordPromptDismissed != want {
			t.Errorf("PasswordPromptDismissed = %v, want %v", got.PasswordPromptDismissed, want)
		}
	}
}

func TestSetAccountPasswordPromptDismissedUnknownAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := newAccountTestDB(t)

	err := db.SetAccountPasswordPromptDismissed(ctx, 9999, true)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("err = %v, want ErrAccountNotFound", err)
	}
}
