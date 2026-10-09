package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newFolderTestAccount creates an account and the folders named by their imap
// paths, all sharing one delimiter.
func newFolderTestAccount(t *testing.T, db *DB, delim string, paths ...string) (int64, map[string]int64) {
	t.Helper()
	ctx := context.Background()

	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	ids := make(map[string]int64, len(paths))
	for _, p := range paths {
		f := Folder{AccountID: accountID, Name: p, IMAPPath: p, Delimiter: delim}
		if _, err := db.CreateFolder(ctx, &f); err != nil {
			t.Fatalf("create folder %q: %v", p, err)
		}
		ids[p] = f.ID
	}
	return accountID, ids
}

func TestRenameFolderSubtree(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/",
		"Work", "Work/2026", "Work/2026/Q1",
		// a sibling whose name starts with the renamed folder's name. matching on
		// the bare name instead of the name plus delimiter would drag it along.
		"Workshop", "Workshop/Notes",
		"Personal",
	)

	n, err := db.RenameFolderSubtree(ctx, accountID, "Work/", "Archive/Work/")
	if err != nil {
		t.Fatalf("rename subtree: %v", err)
	}
	if n != 2 {
		t.Fatalf("rewrote %d rows, want 2", n)
	}

	want := map[string]string{
		"Work":           "Work", // the folder itself is the caller's job
		"Work/2026":      "Archive/Work/2026",
		"Work/2026/Q1":   "Archive/Work/2026/Q1",
		"Workshop":       "Workshop",
		"Workshop/Notes": "Workshop/Notes",
		"Personal":       "Personal",
	}
	folders, err := db.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	byID := make(map[int64]Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	for original, expected := range want {
		f := byID[ids[original]]
		if f.IMAPPath != expected {
			t.Errorf("%q became %q, want %q", original, f.IMAPPath, expected)
		}
		// an imap remote id is the path: left behind, sync and pushes would
		// address a mailbox that no longer exists.
		if f.RemoteID != expected {
			t.Errorf("%q remote id %q, want %q", original, f.RemoteID, expected)
		}
	}
}

// substr counts characters, not bytes: a non-ASCII parent must not shift the
// cut and mangle the children's paths and remote ids.
func TestRenameFolderSubtreeNonASCII(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/", "Ważne", "Ważne/Łódź", "Ważne/Łódź/Żółw")

	if _, err := db.RenameFolderSubtree(ctx, accountID, "Ważne/", "Pilne/"); err != nil {
		t.Fatalf("rename subtree: %v", err)
	}
	for original, want := range map[string]string{
		"Ważne/Łódź":      "Pilne/Łódź",
		"Ważne/Łódź/Żółw": "Pilne/Łódź/Żółw",
	} {
		f, err := db.GetFolder(ctx, ids[original])
		if err != nil {
			t.Fatalf("get folder: %v", err)
		}
		if f.IMAPPath != want || f.RemoteID != want {
			t.Errorf("%q became path %q remote %q, want %q", original, f.IMAPPath, f.RemoteID, want)
		}
	}
}

// a remote id that is not the path is stable across renames, so only the path
// moves.
func TestRenameKeepsOpaqueRemoteID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	ids := map[string]int64{}
	for path, rid := range map[string]string{"Work": "mb-1", "Work/2026": "mb-2"} {
		f := Folder{AccountID: accountID, Name: path, IMAPPath: path, Delimiter: "/", RemoteID: rid}
		if _, err := db.CreateFolder(ctx, &f); err != nil {
			t.Fatalf("create folder %q: %v", path, err)
		}
		ids[path] = f.ID
	}

	if _, err := db.RenameFolderSubtree(ctx, accountID, "Work/", "Jobs/"); err != nil {
		t.Fatalf("rename subtree: %v", err)
	}
	if err := db.RenameFolder(ctx, ids["Work"], "Jobs", "Jobs"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	for path, want := range map[string]string{"Work": "mb-1", "Work/2026": "mb-2"} {
		f, err := db.GetFolder(ctx, ids[path])
		if err != nil {
			t.Fatalf("get folder: %v", err)
		}
		if f.RemoteID != want {
			t.Errorf("%q remote id %q, want %q", path, f.RemoteID, want)
		}
	}
}

// a folder whose name contains a LIKE wildcard must match only itself.
func TestRenameFolderSubtreeEscapesWildcards(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	accountID, ids := newFolderTestAccount(t, db, "/",
		"100%", "100%/Done",
		// "_" matches any single character in LIKE, so an unescaped "100_" prefix
		// would also catch this one.
		"1005", "1005/Keep",
	)

	if _, err := db.RenameFolderSubtree(ctx, accountID, "100%/", "Full/"); err != nil {
		t.Fatalf("rename subtree: %v", err)
	}

	folders, err := db.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	byID := make(map[int64]Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	if got := byID[ids["100%/Done"]].IMAPPath; got != "Full/Done" {
		t.Errorf("100%%/Done became %q, want Full/Done", got)
	}
	if got := byID[ids["1005/Keep"]].IMAPPath; got != "1005/Keep" {
		t.Errorf("1005/Keep became %q, want it untouched", got)
	}
}

func TestFolderDescendants(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	accountID, _ := newFolderTestAccount(t, db, ".",
		"INBOX.Work", "INBOX.Work.2026", "INBOX.Work.2026.Q1",
		"INBOX.Workshop",
		"INBOX.Personal",
	)

	got, err := db.FolderDescendants(ctx, accountID, "INBOX.Work.")
	if err != nil {
		t.Fatalf("descendants: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d descendants, want 2: %v", len(got), paths(got))
	}
	// shallowest first, so a caller tearing the subtree down can walk it in
	// reverse and never delete a parent before its children.
	if got[0].IMAPPath != "INBOX.Work.2026" || got[1].IMAPPath != "INBOX.Work.2026.Q1" {
		t.Errorf("descendants = %v, want the subtree shallowest first", paths(got))
	}
}

func TestRenameFolder(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, ids := newFolderTestAccount(t, db, "/", "Projects")

	if err := db.RenameFolder(ctx, ids["Projects"], "Work", "Work"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	f, err := db.GetFolder(ctx, ids["Projects"])
	if err != nil {
		t.Fatalf("get folder: %v", err)
	}
	if f.Name != "Work" || f.IMAPPath != "Work" || f.RemoteID != "Work" {
		t.Errorf("folder = %q at %q (remote %q), want Work at Work", f.Name, f.IMAPPath, f.RemoteID)
	}

	if err := db.RenameFolder(ctx, 9999, "Nope", "Nope"); err == nil {
		t.Error("renaming a missing folder should error")
	}
}

func paths(folders []Folder) []string {
	out := make([]string, 0, len(folders))
	for _, f := range folders {
		out = append(out, f.IMAPPath)
	}
	return out
}

func TestFolderFullSyncAt(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	f := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, &f); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetFolder(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastFullSyncAt.IsZero() {
		t.Fatalf("new folder LastFullSyncAt = %v, want zero (never)", got.LastFullSyncAt)
	}
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := db.SetFolderFullSyncAt(ctx, f.ID, at); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetFolder(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastFullSyncAt.Equal(at) {
		t.Fatalf("LastFullSyncAt = %v, want %v", got.LastFullSyncAt, at)
	}
	list, err := db.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].LastFullSyncAt.Equal(at) {
		t.Fatalf("ListFolders LastFullSyncAt = %v", list)
	}
	if err := db.SetFolderFullSyncAt(ctx, f.ID+999, at); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("missing folder err = %v, want ErrFolderNotFound", err)
	}
}
