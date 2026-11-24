package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Upgrading main's schema copies the database aside before the messages
// rebuild, and the copy is the database as it was before the upgrade.
func TestUpgradeWritesPreUpgradeSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, mainVersion)
	seedMain(t, db)
	wantMessages := upInt(t, db.sql, `SELECT count(*) FROM messages`)
	path := db.PreUpgradeSnapshotPath()
	if err := os.WriteFile(path, []byte("an older copy"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	matches, _ := filepath.Glob(db.Path() + ".pre-upgrade*")
	if len(matches) != 1 {
		t.Errorf("pre-upgrade copies = %v, want exactly one", matches)
	}

	snap, err := Open(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	t.Cleanup(func() { snap.Close() })
	if n := upInt(t, snap.sql, `SELECT max(id) FROM schema_migrations`); n != mainVersion {
		t.Errorf("snapshot is at migration %d, want %d", n, mainVersion)
	}
	if n := upInt(t, snap.sql, `SELECT count(*) FROM messages`); n != wantMessages {
		t.Errorf("snapshot holds %d messages, want %d", n, wantMessages)
	}
	if n := upInt(t, snap.sql, `SELECT count(*) FROM pragma_table_info('messages') WHERE name = 'remote_id'`); n != 0 {
		t.Error("snapshot was taken after the rebuild")
	}
}

// A fresh install has nothing to back up.
func TestFreshInstallWritesNoSnapshot(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(db.PreUpgradeSnapshotPath()); !os.IsNotExist(err) {
		t.Errorf("fresh install left a pre-upgrade copy: %v", err)
	}
}

// When the copy cannot be written the rebuild does not run, and the error
// says the database could not be backed up.
func TestUpgradeStopsWhenSnapshotFails(t *testing.T) {
	db := openDBThrough(t, mainVersion)
	seedMain(t, db)
	// a non-empty directory in the copy's place cannot be removed or written.
	blocker := db.PreUpgradeSnapshotPath()
	if err := os.MkdirAll(filepath.Join(blocker, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := db.RunMigrations(context.Background())
	if err == nil || !strings.Contains(err.Error(), "could not back up the database") {
		t.Fatalf("err = %v, want a backup failure", err)
	}
	if n := upInt(t, db.sql, `SELECT max(id) FROM schema_migrations`); n != mainVersion {
		t.Errorf("schema at %d after the failed backup, want %d", n, mainVersion)
	}
}

// The disk check counts the copy: room for the rebuild alone is not enough.
func TestUpgradeSpaceCheckCountsTheSnapshot(t *testing.T) {
	db := openDBThrough(t, mainVersion)
	seedMain(t, db)
	used, _, err := db.usedBytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	old := diskFree
	t.Cleanup(func() { diskFree = old })
	diskFree = func(string) (uint64, bool) { return uint64(2*used) + 1, true }

	err = db.RunMigrations(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not enough free disk space") {
		t.Fatalf("err = %v, want a disk-space refusal", err)
	}
	if _, err := os.Stat(db.PreUpgradeSnapshotPath()); !os.IsNotExist(err) {
		t.Error("a copy was written despite the refusal")
	}
}

// The copy stays for 30 days after it was written, then a start that has no
// rebuild to run deletes it.
func TestPreUpgradeSnapshotExpires(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, mainVersion)
	seedMain(t, db)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	path := db.PreUpgradeSnapshotPath()
	written := time.Now()
	old := snapshotNow
	t.Cleanup(func() { snapshotNow = old })

	snapshotNow = func() time.Time { return written.Add(29 * 24 * time.Hour) }
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("copy gone before 30 days: %v", err)
	}

	snapshotNow = func() time.Time { return written.Add(31 * 24 * time.Hour) }
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("copy still there after 30 days: %v", err)
	}
}
