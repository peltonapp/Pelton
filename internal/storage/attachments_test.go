package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFilenameKeepsTheLastPathSegment(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"a/b/report.pdf", "report.pdf"},
		{`a\b\report.pdf`, "report.pdf"},
		{`a/b\report.pdf`, "report.pdf"},
		{"/report.pdf", "report.pdf"},
		{"dir/", fallbackFilename},
		{"", fallbackFilename},
		{"../../etc/passwd", "passwd"},
		{"a/CON.txt", fallbackFilename},
	}
	for _, tt := range tests {
		if got := sanitizeFilename(tt.in); got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A staging tree left by a killed run is recovered on the next open: a dir
// whose message still exists goes back if its live path is free, the rest is
// dropped.
func TestRecoverCacheStagingOnOpen(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		deleteRow bool   // the staged delete committed
		liveFile  string // the live dir was written again since
		want      string // file found at the live path afterwards
	}{
		{name: "uncommitted delete is restored", want: "old.pdf"},
		{name: "committed delete is dropped", deleteRow: true},
		{name: "live dir wins", liveFile: "new.pdf", want: "new.pdf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, accountID, folder := newBodyTestFolder(t)
			id, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyComplete: true})
			if err != nil {
				t.Fatal(err)
			}
			acct, msg := accountSegment(accountID), messageSegment(id)
			root := filepath.Join(db.AttachmentsDir(), cacheStagingDirName+"-123")
			writeTestFile(t, filepath.Join(root, acct, msg, "old.pdf"))
			writeTestFile(t, filepath.Join(root, acct, "999", "orphan.pdf"))
			writeTestFile(t, filepath.Join(root, "junk"))
			live := filepath.Join(db.AttachmentsDir(), acct, msg)
			if tc.liveFile != "" {
				writeTestFile(t, filepath.Join(live, tc.liveFile))
			}
			if tc.deleteRow {
				if err := db.DeleteMessage(ctx, id); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()

			db, err = Open(db.Path())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			if err := db.RunMigrations(ctx); err != nil {
				t.Fatal(err)
			}
			var got []string
			entries, _ := os.ReadDir(live)
			for _, e := range entries {
				got = append(got, e.Name())
			}
			if strings.Join(got, ",") != tc.want {
				t.Errorf("live dir holds %v, want %q", got, tc.want)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Errorf("staging tree still there: %v", err)
			}
			if _, err := os.Stat(filepath.Join(db.AttachmentsDir(), acct, "999")); !os.IsNotExist(err) {
				t.Errorf("dir of a missing message was restored: %v", err)
			}
		})
	}
}

// A run that dies mid-change leaves its staging tree behind with the lock
// gone with the process. The very next start puts the attachments back; it
// does not wait for the tree to age.
func TestRecoverCacheStagingRightAfterCrash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	id, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(db.AttachmentsDir(), accountSegment(accountID), messageSegment(id))
	writeTestFile(t, filepath.Join(live, "old.pdf"))

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, tree, err := db.stageMessageDirs(accountID, []int64{id})
	if err != nil || tree == nil {
		t.Fatalf("stage: %v %v", tree, err)
	}
	// The crash: the transaction never commits and the process's lock dies.
	tree.lock.Close()
	tx.Rollback()
	db.Close()

	db, err = Open(db.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(live, "old.pdf")); err != nil {
		t.Fatalf("attachment not back after an immediate restart: %v", err)
	}
	if _, err := os.Stat(tree.path); !os.IsNotExist(err) {
		t.Errorf("staging tree still there: %v", err)
	}
}

// A staging tree whose owner still holds its lock belongs to a change in
// flight (in this process or another) and is left alone; once the lock is
// free the tree is recovered.
func TestRecoverCacheStagingSkipsLockedTrees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	id, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(db.AttachmentsDir(), accountSegment(accountID), messageSegment(id))
	writeTestFile(t, filepath.Join(live, "old.pdf"))
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, tree, err := db.stageMessageDirs(accountID, []int64{id})
	if err != nil || tree == nil {
		t.Fatalf("stage: %v %v", tree, err)
	}
	tx.Rollback()
	staged := filepath.Join(tree.path, accountSegment(accountID), messageSegment(id), "old.pdf")

	db.recoverCacheStaging(ctx)
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("locked staging tree touched: %v", err)
	}
	tree.keep()
	db.recoverCacheStaging(ctx)
	if _, err := os.Stat(filepath.Join(live, "old.pdf")); err != nil {
		t.Fatalf("released staging tree not recovered: %v", err)
	}
	if _, err := os.Stat(tree.path); !os.IsNotExist(err) {
		t.Errorf("released staging tree still there: %v", err)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), filePerm); err != nil {
		t.Fatal(err)
	}
}
