package storage

import (
	"context"
	"strings"
	"testing"
)

func TestCheckUniqueVersions(t *testing.T) {
	tests := []struct {
		name    string
		in      []migration
		wantErr bool
	}{
		{name: "empty", in: nil},
		{name: "distinct", in: []migration{{version: 1, name: "0001_a.sql"}, {version: 2, name: "0002_b.sql"}}},
		{name: "duplicate", in: []migration{{version: 35, name: "0035_a.sql"}, {version: 35, name: "0035_b.sql"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkUniqueVersions(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkUniqueVersions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestEmbeddedMigrationsHaveUniqueVersions guards against two branches each
// adding the same migration number and the merge keeping both files.
func TestEmbeddedMigrationsHaveUniqueVersions(t *testing.T) {
	if _, err := loadMigrations(); err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
}

// schema_migrations records each file's name. A database that applied
// another branch's file under one of our numbers is refused; rows from before
// names were recorded are accepted and named.
func TestMigrationNames(t *testing.T) {
	tests := []struct {
		name    string
		setup   string // run on a v36 database
		wantErr bool
	}{
		{name: "table without the column", setup: `ALTER TABLE schema_migrations DROP COLUMN name`},
		{name: "rows without names", setup: `UPDATE schema_migrations SET name = NULL`},
		{name: "same names"},
		{name: "other branch's 0037", wantErr: true,
			setup: `INSERT INTO schema_migrations (id, applied_at, name) VALUES (37, 'x', '0037_spam_origins.sql')`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openDBThrough(t, 36)
			if tt.setup != "" {
				upExec(t, db.sql, tt.setup)
			}
			err := db.RunMigrations(ctx)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "0037_spam_origins.sql") || !strings.Contains(err.Error(), "0037_remote_ids.sql") {
					t.Fatalf("err = %v, want a clash naming both files", err)
				}
				if n := upInt(t, db.sql, `SELECT count(*) FROM pragma_table_info('folders') WHERE name = 'remote_id'`); n != 0 {
					t.Error("migrations ran past the clash")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			migrations, err := loadMigrations()
			if err != nil {
				t.Fatal(err)
			}
			applied, err := db.appliedMigrations(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range migrations {
				if applied[m.version] != m.name {
					t.Errorf("migration %d recorded as %q, want %q", m.version, applied[m.version], m.name)
				}
			}
		})
	}
}

// A main/nightly database that ran #521's 0037_spam_origins.sql before names
// were recorded has row 37 with no name. It must be refused, not taken for our
// 0037, and the refusal must leave the database as it was.
func TestMigrationNamesUnnamedAboveLastUnnamed(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, 36)
	upExec(t, db.sql, `UPDATE schema_migrations SET name = NULL`)
	upExec(t, db.sql, `CREATE TABLE spam_origins (id INTEGER PRIMARY KEY, address TEXT NOT NULL)`)
	upExec(t, db.sql, `INSERT INTO schema_migrations (id, applied_at, name) VALUES (37, 'x', NULL)`)
	before := schemaOf(t, db)

	err := db.RunMigrations(ctx)
	if err == nil || !strings.Contains(err.Error(), "migration 37 without a name") {
		t.Fatalf("err = %v, want a refusal of the unnamed 37", err)
	}
	if schemaOf(t, db) != before {
		t.Error("schema changed by a refused start")
	}
	if n := upInt(t, db.sql, `SELECT count(*) FROM schema_migrations WHERE name IS NOT NULL`); n != 0 {
		t.Errorf("%d migrations were named before the refusal", n)
	}
	if n := upInt(t, db.sql, `SELECT count(*) FROM schema_migrations WHERE id > 37`); n != 0 {
		t.Errorf("%d migrations applied past the refusal", n)
	}
}

// An old feat/jmap dev database ran our 0037… before names were recorded; the
// schema shows it, so the rows are trusted and named.
func TestMigrationNamesUnnamedOursByFingerprint(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, 41)
	upExec(t, db.sql, `UPDATE schema_migrations SET name = NULL`)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if n := upInt(t, db.sql, `SELECT count(*) FROM schema_migrations WHERE name IS NULL`); n != 0 {
		t.Errorf("%d migrations left unnamed", n)
	}
}

// Only a first-line marker makes a migration a rebuild; the marker quoted in
// a comment further down does not.
func TestIsRebuildMatchesFirstLineOnly(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want bool
	}{
		{"-- pelton:foreign-keys-off\nDROP TABLE x;", true},
		{"-- pelton:foreign-keys-off\r\nDROP TABLE x;", true},
		{"ALTER TABLE x ADD y;\n-- unlike 0037 this needs no -- pelton:foreign-keys-off\n", false},
		{"", false},
	} {
		if got := isRebuild(migration{sql: tc.sql}); got != tc.want {
			t.Errorf("isRebuild(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

// A dev database that ran an earlier draft of 0037 (no search_state, or
// search_state without text_rev) has the row named like ours. It must be
// refused with the advice to recreate it, and the refusal must leave it as
// it was.
func TestMigrationNamesEarlierDraftRefused(t *testing.T) {
	drafts := map[string][]string{
		"no search_state": {`DROP INDEX idx_messages_search_pending`, `ALTER TABLE messages DROP COLUMN search_state`, `ALTER TABLE messages DROP COLUMN text_rev`},
		"no text_rev":     {`ALTER TABLE messages DROP COLUMN text_rev`},
	}
	for draft, drop := range drafts {
		for _, unnamed := range []bool{false, true} {
			t.Run(draft+"/"+map[bool]string{false: "named", true: "unnamed"}[unnamed], func(t *testing.T) {
				testEarlierDraftRefused(t, drop, unnamed)
			})
		}
	}
}

func testEarlierDraftRefused(t *testing.T, drop []string, unnamed bool) {
	ctx := context.Background()
	db := openDBThrough(t, 41)
	for _, q := range drop {
		upExec(t, db.sql, q)
	}
	want := "earlier draft of migration 37"
	if unnamed {
		upExec(t, db.sql, `UPDATE schema_migrations SET name = NULL`)
		want = "migration 37 without a name"
	}
	before := schemaOf(t, db)

	err := db.RunMigrations(ctx)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if !unnamed && !strings.Contains(err.Error(), "delete it") {
		t.Errorf("err = %v, want the advice to recreate the database", err)
	}
	if schemaOf(t, db) != before {
		t.Error("schema changed by a refused start")
	}
}
