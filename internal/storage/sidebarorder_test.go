package storage

import (
	"context"
	"errors"
	"testing"
)

// pathsOf reduces a folder list to its imap paths, which is what the ordering
// assertions below compare.
func pathsOf(folders []Folder) []string {
	out := make([]string, 0, len(folders))
	for _, f := range folders {
		out = append(out, f.IMAPPath)
	}
	return out
}

func assertPaths(t *testing.T, got []Folder, want ...string) {
	t.Helper()
	paths := pathsOf(got)
	if len(paths) != len(want) {
		t.Fatalf("folders = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("folders = %v, want %v", paths, want)
		}
	}
}

// an install that has never reordered anything must keep discovery order, which
// is the whole point of position defaulting to 0 rather than to a rank.
func TestListFoldersKeepsDiscoveryOrderUntilReordered(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Sent", "Archive", "Work")
	assertPaths(t, mustListFolders(t, db, accountID), "INBOX", "Sent", "Archive", "Work")

	if err := db.SetFolderPositions(ctx, []int64{ids["Work"], ids["Archive"], ids["Sent"], ids["INBOX"]}); err != nil {
		t.Fatalf("set positions: %v", err)
	}
	assertPaths(t, mustListFolders(t, db, accountID), "Work", "Archive", "Sent", "INBOX")
}

// a folder discovered after a reorder has position 0, and must land at the end
// of the list rather than jumping ahead of every ranked folder.
func TestNewFolderSortsAfterReorderedOnes(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Sent")
	if err := db.SetFolderPositions(ctx, []int64{ids["Sent"], ids["INBOX"]}); err != nil {
		t.Fatalf("set positions: %v", err)
	}

	fresh := Folder{AccountID: accountID, Name: "Later", IMAPPath: "Later", Delimiter: "/"}
	if _, err := db.CreateFolder(ctx, &fresh); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	assertPaths(t, mustListFolders(t, db, accountID), "Sent", "INBOX", "Later")
}

func TestSetAccountPositions(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	var ids []int64
	for _, email := range []string{"one@example.com", "two@example.com", "three@example.com"} {
		id, err := db.CreateAccount(ctx, &Account{Email: email})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}
		ids = append(ids, id)
	}

	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if accounts[0].Email != "one@example.com" {
		t.Fatalf("without a reorder accounts[0] = %q, want one@example.com", accounts[0].Email)
	}

	if err := db.SetAccountPositions(ctx, []int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatalf("set account positions: %v", err)
	}
	accounts, err = db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	want := []string{"three@example.com", "one@example.com", "two@example.com"}
	for i, w := range want {
		if accounts[i].Email != w {
			t.Fatalf("accounts[%d] = %q, want %q", i, accounts[i].Email, w)
		}
	}
}

// pinning appends, so the group's order follows the order the user pinned in
// until they drag it. Pinning spans accounts: the group is global.
func TestPinFolderAppendsAcrossAccounts(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	_, first := newFolderTestAccount(t, db, "/", "INBOX", "Work")
	secondID, err := db.CreateAccount(ctx, &Account{Email: "b@example.com"})
	if err != nil {
		t.Fatalf("create second account: %v", err)
	}
	other := Folder{AccountID: secondID, Name: "Team", IMAPPath: "Team", Delimiter: "/"}
	if _, err := db.CreateFolder(ctx, &other); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	for _, id := range []int64{first["Work"], other.ID, first["INBOX"]} {
		if err := db.SetFolderPinned(ctx, id, true); err != nil {
			t.Fatalf("pin folder %d: %v", id, err)
		}
	}
	pinned, err := db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	assertPaths(t, pinned, "Work", "Team", "INBOX")

	if err := db.SetPinnedFolderPositions(ctx, []int64{other.ID, first["INBOX"], first["Work"]}); err != nil {
		t.Fatalf("reorder pinned: %v", err)
	}
	pinned, err = db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	assertPaths(t, pinned, "Team", "INBOX", "Work")
}

// pinning twice must not move a folder to the end of the group, and unpinning
// must take it out without disturbing the folders around it.
func TestPinFolderIsIdempotentAndUnpinnable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/", "INBOX", "Work", "Notes")
	for _, p := range []string{"INBOX", "Work", "Notes"} {
		if err := db.SetFolderPinned(ctx, ids[p], true); err != nil {
			t.Fatalf("pin %s: %v", p, err)
		}
	}
	if err := db.SetFolderPinned(ctx, ids["INBOX"], true); err != nil {
		t.Fatalf("re-pin INBOX: %v", err)
	}
	pinned, err := db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	assertPaths(t, pinned, "INBOX", "Work", "Notes")

	if err := db.SetFolderPinned(ctx, ids["Work"], false); err != nil {
		t.Fatalf("unpin Work: %v", err)
	}
	pinned, err = db.ListPinnedFolders(ctx)
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	assertPaths(t, pinned, "INBOX", "Notes")

	// unpinning is a mirror operation only: the folder is still in its tree.
	assertPaths(t, mustListFolders(t, db, accountID), "INBOX", "Work", "Notes")

	if err := db.SetFolderPinned(ctx, 9999, true); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("pin missing folder = %v, want ErrFolderNotFound", err)
	}
}

func mustListFolders(t *testing.T, db *DB, accountID int64) []Folder {
	t.Helper()
	folders, err := db.ListFolders(context.Background(), accountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	return folders
}
