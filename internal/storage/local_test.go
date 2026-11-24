package storage

import (
	"context"
	"errors"
	"testing"
)

// the Local Folders section is meant to be invisible until something is
// imported, which rests on the account not existing before then.
func TestLocalAccountIsAbsentUntilEnsured(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.LocalAccount(ctx); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("local account before any import = %v, want ErrAccountNotFound", err)
	}

	first, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !first.Local {
		t.Fatal("the account was created without the local flag, so sync would try to connect to it")
	}

	second, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second ensure created a new account (%d, was %d)", second.ID, first.ID)
	}
}

// an ordinary account must not be mistaken for the local one, and the local one
// must not leak into a list that is about to be synced.
func TestLocalFlagIsPerAccount(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateAccount(ctx, &Account{Email: "real@example.com", IMAPHost: "imap.example.com"}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := db.EnsureLocalAccount(ctx); err != nil {
		t.Fatalf("ensure local: %v", err)
	}

	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(accounts))
	}
	local := 0
	for _, acc := range accounts {
		if acc.Local {
			local++
			if acc.Email != LocalAccountEmail {
				t.Fatalf("local account email = %q, want %q", acc.Email, LocalAccountEmail)
			}
		}
	}
	if local != 1 {
		t.Fatalf("got %d local accounts, want exactly 1", local)
	}
}

func TestEnsureLocalFolderReusesExisting(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	account, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatalf("ensure local: %v", err)
	}

	first, err := db.EnsureLocalFolder(ctx, account.ID, "Archive")
	if err != nil {
		t.Fatalf("ensure folder: %v", err)
	}
	second, err := db.EnsureLocalFolder(ctx, account.ID, "Archive")
	if err != nil {
		t.Fatalf("ensure folder again: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("a second import made a second folder (%d, was %d)", second.ID, first.ID)
	}

	other, err := db.EnsureLocalFolder(ctx, account.ID, "Sent")
	if err != nil {
		t.Fatalf("ensure other folder: %v", err)
	}
	if other.ID == first.ID {
		t.Fatal("a differently named folder reused the first one")
	}
}

// uids are what keeps two imported messages apart in one folder, so the counter
// has to start above zero and follow what is already stored.
func TestNextLocalUID(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	account, _ := db.EnsureLocalAccount(ctx)
	folder, err := db.EnsureLocalFolder(ctx, account.ID, "Archive")
	if err != nil {
		t.Fatalf("ensure folder: %v", err)
	}

	uid, err := db.NextLocalUID(ctx, folder.ID)
	if err != nil {
		t.Fatalf("next uid: %v", err)
	}
	if uid != 1 {
		t.Fatalf("first uid = %d, want 1", uid)
	}

	m := Message{AccountID: account.ID, FolderID: folder.ID, UID: 7, MessageID: "a@example.com"}
	if _, err := db.InsertMessage(ctx, &m); err != nil {
		t.Fatalf("insert: %v", err)
	}
	uid, err = db.NextLocalUID(ctx, folder.ID)
	if err != nil {
		t.Fatalf("next uid: %v", err)
	}
	if uid != 8 {
		t.Fatalf("next uid = %d, want 8", uid)
	}
}

func TestHasLocalMessage(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	account, _ := db.EnsureLocalAccount(ctx)
	folder, _ := db.EnsureLocalFolder(ctx, account.ID, "Archive")
	other, _ := db.EnsureLocalFolder(ctx, account.ID, "Sent")

	m := Message{AccountID: account.ID, FolderID: folder.ID, UID: 1, MessageID: "a@example.com"}
	if _, err := db.InsertMessage(ctx, &m); err != nil {
		t.Fatalf("insert: %v", err)
	}

	found, err := db.HasLocalMessage(ctx, folder.ID, "a@example.com")
	if err != nil || !found {
		t.Fatalf("HasLocalMessage = %v, %v, want true", found, err)
	}
	// the check is per folder: the same message imported into two folders is
	// two deliberate copies, not a duplicate.
	found, err = db.HasLocalMessage(ctx, other.ID, "a@example.com")
	if err != nil || found {
		t.Fatalf("HasLocalMessage in another folder = %v, %v, want false", found, err)
	}
	// a message with no Message-ID has nothing to match on, so it is never a
	// duplicate; reporting true would drop every such message after the first.
	found, err = db.HasLocalMessage(ctx, folder.ID, "")
	if err != nil || found {
		t.Fatalf("HasLocalMessage with no id = %v, %v, want false", found, err)
	}
}
