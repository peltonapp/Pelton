package storage

import (
	"context"
	"path/filepath"
	"testing"
)

// Every layer's head must apply on top of main's last migration (36) and on
// an empty database. The fixture is main's schema: openDBThrough(t, 36).
func TestMigrationsApplyFromMainAndFresh(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, 36)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate from v36: %v", err)
	}
	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open fresh: %v", err)
	}
	t.Cleanup(func() { fresh.Close() })
	if err := fresh.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate fresh: %v", err)
	}
	for name, d := range map[string]*DB{"from v36": db, "fresh": fresh} {
		for _, col := range []string{"remote_id", "state_token", "sync_floor_id", "sync_initialized"} {
			var n int
			if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('folders') WHERE name = ?`, col).Scan(&n); err != nil || n != 1 {
				t.Fatalf("%s: folders.%s missing after migrating (%v)", name, col, err)
			}
		}
		// sync-core adds 0037-0041 on top of main and nothing after them.
		applied, err := d.appliedMigrations(ctx)
		if err != nil {
			t.Fatalf("%s: applied migrations: %v", name, err)
		}
		for v := 37; v <= 41; v++ {
			if !applied[v] {
				t.Errorf("%s: migration %d not applied", name, v)
			}
		}
		for v := range applied {
			if v > 41 {
				t.Errorf("%s: unexpected migration %d", name, v)
			}
		}
	}
}
