package storage

import (
	"context"
	"testing"
)

// useProfile points the store at a profile the way the app does on a switch.
func useProfile(t *testing.T, db *DB, p Profile) {
	t.Helper()
	main, err := db.MainProfile(context.Background())
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	db.UseProfile(p, main.ID)
}

// newLayoutProfile creates a second profile and returns it.
func newLayoutProfile(t *testing.T, db *DB, name string, shareLayout bool) Profile {
	t.Helper()
	p := Profile{Name: name, ShareLayout: shareLayout}
	if _, err := db.CreateProfile(context.Background(), &p); err != nil {
		t.Fatalf("create profile %q: %v", name, err)
	}
	return p
}

// TestFolderOrderIsPerProfile is what #325 asked for: arranging the sidebar in
// one profile must not rearrange it in another.
func TestFolderOrderIsPerProfile(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Sent", "Work")
	if err := db.SetFolderPositions(ctx, []int64{ids["Work"], ids["Sent"], ids["INBOX"]}); err != nil {
		t.Fatalf("set positions: %v", err)
	}
	assertPaths(t, mustListFolders(t, db, accountID), "Work", "Sent", "INBOX")

	other := newLayoutProfile(t, db, "work", false)
	useProfile(t, db, other)
	// a profile that never arranged anything is back to discovery order rather
	// than inheriting the arrangement.
	assertPaths(t, mustListFolders(t, db, accountID), "INBOX", "Sent", "Work")

	if err := db.SetFolderPositions(ctx, []int64{ids["Sent"], ids["INBOX"], ids["Work"]}); err != nil {
		t.Fatalf("set positions in second profile: %v", err)
	}
	assertPaths(t, mustListFolders(t, db, accountID), "Sent", "INBOX", "Work")

	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	useProfile(t, db, *main)
	assertPaths(t, mustListFolders(t, db, accountID), "Work", "Sent", "INBOX")
}

// TestPinnedFoldersArePerProfile covers the other half: the Pinned group.
func TestPinnedFoldersArePerProfile(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	_, ids := newFolderTestAccount(t, db, "/", "INBOX", "Work", "Notes")
	if err := db.SetFolderPinned(ctx, ids["Work"], true); err != nil {
		t.Fatalf("pin Work: %v", err)
	}

	other := newLayoutProfile(t, db, "private", false)
	useProfile(t, db, other)
	pinned, err := db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	if len(pinned) != 0 {
		t.Fatalf("second profile has %d pinned folders, want none", len(pinned))
	}

	if err := db.SetFolderPinned(ctx, ids["Notes"], true); err != nil {
		t.Fatalf("pin Notes: %v", err)
	}
	pinned, err = db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	assertPaths(t, pinned, "Notes")

	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	useProfile(t, db, *main)
	pinned, err = db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned in main: %v", err)
	}
	assertPaths(t, pinned, "Work")
}

// TestAccountOrderIsPerProfile covers the account sections, which move with the
// folders because profiles already show different sets of accounts.
func TestAccountOrderIsPerProfile(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	var ids []int64
	for _, email := range []string{"one@example.com", "two@example.com"} {
		id, err := db.CreateAccount(ctx, &Account{Email: email})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}
		ids = append(ids, id)
	}
	if err := db.SetAccountPositions(ctx, []int64{ids[1], ids[0]}); err != nil {
		t.Fatalf("set account positions: %v", err)
	}

	accounts, err := db.ListAllAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if accounts[0].Email != "two@example.com" {
		t.Fatalf("main accounts[0] = %q, want two@example.com", accounts[0].Email)
	}

	other := newLayoutProfile(t, db, "work", false)
	useProfile(t, db, other)
	accounts, err = db.ListAllAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts in second profile: %v", err)
	}
	if accounts[0].Email != "one@example.com" {
		t.Errorf("second profile accounts[0] = %q, want creation order", accounts[0].Email)
	}
}

// TestSharedLayoutReadsMainAndKeepsItsOwn is the share checkbox: while it is
// on the profile follows main, and its own arrangement is still there when it
// goes off again.
func TestSharedLayoutReadsMainAndKeepsItsOwn(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Sent", "Work")
	if err := db.SetFolderPositions(ctx, []int64{ids["Work"], ids["Sent"], ids["INBOX"]}); err != nil {
		t.Fatalf("set main positions: %v", err)
	}

	other := newLayoutProfile(t, db, "work", false)
	useProfile(t, db, other)
	if err := db.SetFolderPositions(ctx, []int64{ids["Sent"], ids["Work"], ids["INBOX"]}); err != nil {
		t.Fatalf("set own positions: %v", err)
	}
	assertPaths(t, mustListFolders(t, db, accountID), "Sent", "Work", "INBOX")

	other.ShareLayout = true
	if err := db.UpdateProfile(ctx, other); err != nil {
		t.Fatalf("share layout: %v", err)
	}
	useProfile(t, db, other)
	assertPaths(t, mustListFolders(t, db, accountID), "Work", "Sent", "INBOX")

	other.ShareLayout = false
	if err := db.UpdateProfile(ctx, other); err != nil {
		t.Fatalf("unshare layout: %v", err)
	}
	useProfile(t, db, other)
	assertPaths(t, mustListFolders(t, db, accountID), "Sent", "Work", "INBOX")
}

// TestCopyProfileLayoutIsASnapshot: a new profile starting from main's layout
// gets a copy, not a link.
func TestCopyProfileLayoutIsASnapshot(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Sent", "Work")
	if err := db.SetFolderPositions(ctx, []int64{ids["Work"], ids["Sent"], ids["INBOX"]}); err != nil {
		t.Fatalf("set main positions: %v", err)
	}
	if err := db.SetFolderPinned(ctx, ids["Work"], true); err != nil {
		t.Fatalf("pin Work: %v", err)
	}

	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	other := newLayoutProfile(t, db, "work", false)
	if err := db.CopyProfileLayout(ctx, main.ID, other.ID); err != nil {
		t.Fatalf("copy layout: %v", err)
	}

	useProfile(t, db, other)
	assertPaths(t, mustListFolders(t, db, accountID), "Work", "Sent", "INBOX")
	pinned, err := db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	assertPaths(t, pinned, "Work")

	// rearranging the copy leaves main alone.
	if err := db.SetFolderPositions(ctx, []int64{ids["INBOX"], ids["Sent"], ids["Work"]}); err != nil {
		t.Fatalf("rearrange copy: %v", err)
	}
	useProfile(t, db, *main)
	assertPaths(t, mustListFolders(t, db, accountID), "Work", "Sent", "INBOX")
}

// TestDeletedFolderLeavesNoLayoutRow: the layout rows are keyed to folders, so
// deleting one must not leave a rank behind that a new folder could inherit.
func TestDeletedFolderLeavesNoLayoutRow(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	_, ids := newFolderTestAccount(t, db, "/", "INBOX", "Work")
	if err := db.SetFolderPinned(ctx, ids["Work"], true); err != nil {
		t.Fatalf("pin Work: %v", err)
	}
	if err := db.DeleteFolder(ctx, ids["Work"]); err != nil {
		t.Fatalf("delete folder: %v", err)
	}

	var rows int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT count(*) FROM profile_sidebar_layout WHERE folder_id = ?`, ids["Work"]).Scan(&rows); err != nil {
		t.Fatalf("count layout rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("deleted folder left %d layout rows, want 0", rows)
	}
}
