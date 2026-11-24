package storage

import (
	"context"
	"path/filepath"
	"testing"
)

// TestScriptMigrationsApplyInOrder checks the scripting migrations 0104 to
// 0108 on a fresh database and on one that stopped after 0106 with a script
// and a run in it, which keep their rows and gain the later columns.
func TestScriptMigrationsApplyInOrder(t *testing.T) {
	ctx := context.Background()
	check := func(t *testing.T, db *DB) {
		t.Helper()
		var placements, detail string
		if err := db.sql.QueryRowContext(ctx, `SELECT placements FROM scripts LIMIT 1`).Scan(&placements); err != nil {
			t.Fatal(err)
		}
		if err := db.sql.QueryRowContext(ctx, `SELECT detail FROM script_runs LIMIT 1`).Scan(&detail); err != nil {
			t.Fatal(err)
		}
		if placements != "[]" || detail != "{}" {
			t.Fatalf("placements %q, detail %q", placements, detail)
		}
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO script_libraries (name, created_at, updated_at) VALUES ('util', '', '')`); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(t *testing.T, db *DB) {
		t.Helper()
		for _, q := range []string{
			`INSERT INTO scripts (id, name, kind, trigger, created_at, updated_at) VALUES (1, 'old', 'lua', '{"type":"manual"}', '', '')`,
			`INSERT INTO script_runs (script_id, trigger, started_at, status) VALUES (1, 'manual', '', 'ok')`,
			`INSERT INTO script_kv (script_id, key, value) VALUES (1, 'k', '1')`,
		} {
			if _, err := db.sql.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("fresh", func(t *testing.T) {
		db, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.RunMigrations(ctx); err != nil {
			t.Fatal(err)
		}
		seed(t, db)
		check(t, db)
	})

	t.Run("after 0106", func(t *testing.T) {
		db, err := Open(filepath.Join(t.TempDir(), "partial.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.ensureMigrationsTable(ctx); err != nil {
			t.Fatal(err)
		}
		migrations, err := loadMigrations()
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range migrations {
			if m.version <= 106 {
				if err := db.applyMigration(ctx, m); err != nil {
					t.Fatal(err)
				}
			}
		}
		seed(t, db)
		for range 2 {
			if err := db.RunMigrations(ctx); err != nil {
				t.Fatal(err)
			}
		}
		var kv string
		if err := db.sql.QueryRowContext(ctx, `SELECT value FROM script_kv WHERE script_id = 1`).Scan(&kv); err != nil || kv != "1" {
			t.Fatalf("stored value %q: %v", kv, err)
		}
		check(t, db)
	})
}
