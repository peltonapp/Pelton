package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// mainSyncVersion is the last migration of the sync rework on main. Every
// migration after it adds to the rebuilt messages table.
const mainSyncVersion = 45

// A database at main's v45 with mail in it takes every later migration: the
// rows keep every value they had, each later column arrives with its default,
// and every index of both lines exists on the rebuilt table.
func TestRevampMigrationsOnMainV45(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, mainSyncVersion)
	if got := upInt(t, db.sql, `SELECT max(id) FROM schema_migrations`); got != mainSyncVersion {
		t.Fatalf("seed database at migration %d, want %d", got, mainSyncVersion)
	}
	account := upExec(t, db.sql, `INSERT INTO accounts (email, imap_host, imap_port, created_at) VALUES ('a@example.com', 'imap.example', 993, '')`)
	folder := upExec(t, db.sql, `INSERT INTO folders (account_id, name, imap_path, remote_id, uid_validity) VALUES (?, 'INBOX', 'INBOX', 'INBOX', 7)`, account)
	for i := 1; i <= 40; i++ {
		upExec(t, db.sql, `INSERT INTO messages (account_id, folder_id, uid, remote_id, message_id, subject, from_address, date, flags,
			body_plain, body_html, size_bytes, body_complete, search_state, has_attachments)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			account, folder, i, fmt.Sprint(i), fmt.Sprintf("<m%d@x>", i), fmt.Sprintf("Betreff %d", i), "s@example.com",
			fmt.Sprintf("2026-01-%02dT10:00:00Z", 1+i%28), i%8, fmt.Sprintf("Text %d", i), "", 100+i, i%3 != 0, i%3, i%5 == 0)
	}
	mainCols := tableColumns(t, db, "messages")
	before := messageSnapshot(t, db, mainCols)

	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("revamp migrations on v45: %v", err)
	}
	if after := messageSnapshot(t, db, mainCols); len(after) != len(before) {
		t.Fatalf("%d rows after the migrations, want %d", len(after), len(before))
	} else {
		for id, row := range before {
			if after[id] != row {
				t.Errorf("row %d changed:\nbefore %s\nafter  %s", id, row, after[id])
			}
		}
	}

	defaults := map[string]string{
		"thread_id": "0", "thread_subject": "", "thread_loose": "0", "mail_kind": "0",
		"pinned": "0", "pin_pending": "0", "list_id": "", "vault_id": "0",
	}
	have := map[string]bool{}
	for _, c := range tableColumns(t, db, "messages") {
		have[c] = true
	}
	for col, want := range defaults {
		if !have[col] {
			t.Errorf("messages has no column %s", col)
			continue
		}
		if n := upInt(t, db.sql, `SELECT count(*) FROM messages WHERE CAST(`+col+` AS TEXT) != ?`, want); n != 0 {
			t.Errorf("%d rows have %s other than its default %q", n, col, want)
		}
	}

	indexes := []string{
		// main's sync rework.
		"idx_messages_remote", "idx_messages_uid", "idx_messages_account", "idx_messages_message_id",
		"idx_messages_pending", "idx_messages_snooze", "idx_messages_list", "idx_messages_state",
		"idx_messages_search_pending", "idx_messages_needs_refetch", "idx_messages_smime_fp",
		"idx_messages_folder_body_date", "idx_messages_search_stale_stub",
		// the revamp (0058 replaced idx_messages_thread with idx_messages_thread_date).
		"idx_messages_thread_subject", "idx_messages_with_attachments", "idx_messages_sender",
		"idx_messages_split", "idx_messages_pinned", "idx_messages_newsletters", "idx_messages_people",
		"idx_messages_date", "idx_messages_thread_date", "idx_messages_listing", "idx_messages_copies",
		"idx_messages_vault",
	}
	for _, idx := range indexes {
		if n := upInt(t, db.sql, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = 'messages'`, idx); n != 1 {
			t.Errorf("index %s missing on messages", idx)
			continue
		}
		rows, err := db.sql.Query(`SELECT coalesce(name, '') FROM pragma_index_info(?)`, idx)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			if c != "" && !have[c] {
				t.Errorf("index %s names column %s, which messages lacks", idx, c)
			}
			n++
		}
		rows.Close()
		if n == 0 {
			t.Errorf("index %s has no columns", idx)
		}
	}

	// the list reads its covering index on the rebuilt table.
	plan := reindexPlanFor(t, db, `SELECT id, date, flags FROM messages INDEXED BY idx_messages_listing
		WHERE folder_id = ? AND pending_delete = 0 AND snooze_hidden = 0 ORDER BY pinned DESC, date DESC`, folder)
	if plan == "" {
		t.Error("idx_messages_listing cannot serve the list")
	}

	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })
	if err := fresh.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := schemaOf(t, db), schemaOf(t, fresh); got != want {
		t.Errorf("schema after upgrading v45 differs from a fresh install")
	}
	checkUpgradeHealth(t, db, "revamp on v45")

	// the stored mail still works with both lines' code.
	stub := Message{AccountID: account, FolderID: folder, RemoteID: "41", UID: 41, Subject: "neu", BodyPlain: "Vorschau"}
	id, err := db.UpsertMessageListMeta(ctx, &stub)
	if err != nil {
		t.Fatalf("stub after upgrade: %v", err)
	}
	full := Message{AccountID: account, FolderID: folder, RemoteID: "41", UID: 41, Subject: "neu", BodyPlain: "ganz"}
	if _, err := db.FillMessageBody(ctx, BodyTarget{MessageID: id}, &full, nil); err != nil {
		t.Fatalf("fill after upgrade: %v", err)
	}
	if got, err := db.GetMessage(ctx, id); err != nil || got.BodyPlain != "ganz" || !got.BodyComplete {
		t.Fatalf("filled row = %+v, %v", got, err)
	}
}

func tableColumns(t *testing.T, db *DB, table string) []string {
	t.Helper()
	rows, err := db.sql.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	return cols
}

// messageSnapshot renders every row over cols as text, by id.
func messageSnapshot(t *testing.T, db *DB, cols []string) map[int64]string {
	t.Helper()
	expr := "''"
	for _, c := range cols {
		expr += ` || '|' || quote([` + c + `])`
	}
	rows, err := db.sql.Query(`SELECT id, ` + expr + ` FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var s string
		if err := rows.Scan(&id, &s); err != nil {
			t.Fatal(err)
		}
		out[id] = s
	}
	return out
}

func reindexPlanFor(t *testing.T, db *DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.sql.Query(`EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	return plan
}
