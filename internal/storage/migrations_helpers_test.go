package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func openDBThrough(t *testing.T, maxVersion int) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ensureMigrationsTable(context.Background()); err != nil {
		t.Fatalf("migrations table: %v", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	ctx := context.Background()
	for _, m := range migrations {
		if m.version > maxVersion {
			break
		}
		if err := db.applyMigration(ctx, m); err != nil {
			t.Fatalf("apply migration %d: %v", m.version, err)
		}
	}
	if err := db.UseActiveProfile(ctx); err != nil {
		t.Fatalf("use active profile: %v", err)
	}
	return db
}
