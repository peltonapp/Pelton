package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Upgrade of a populated main database (v36) through 0037-0042. Timings on
// 150k messages are measured by hand, not here.

type upgradeFixture struct {
	account, folder int64 // gmail INBOX
	uid0, maxUID    int64 // message ids with uid 0 and uint32 max
	seq             int64 // sqlite_sequence(messages) before the upgrade
}

func upExec(t *testing.T, ex execer, q string, args ...any) int64 {
	t.Helper()
	res, err := ex.ExecContext(context.Background(), q, args...)
	if err != nil {
		t.Fatalf("exec %.60q: %v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func upInt(t *testing.T, ex execer, q string, args ...any) int64 {
	t.Helper()
	var n sql.NullInt64
	if err := ex.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("query %.60q: %v", q, err)
	}
	return n.Int64
}

// seedV36 fills a v36 database with the shapes main writes: several accounts
// and delimiters, non-ASCII and case-only folder names, uid 0 and uint32 max,
// pending/snoozed/flagged rows, empty bodies, attachments and the tables that
// point at messages. The newest messages are deleted so sqlite_sequence sits
// above max(id), as it does after any expunge.
func seedV36(t *testing.T, db *DB) upgradeFixture {
	t.Helper()
	tx, err := db.sql.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	const now = "2026-01-01T00:00:00Z"
	var fx upgradeFixture
	account := func(email string, local int) int64 {
		id := upExec(t, tx, `INSERT INTO accounts (email, display_name, imap_host, imap_port, smtp_host, smtp_port, created_at, username, is_local)
			VALUES (?, 'Zażółć', 'imap.example', 993, 'smtp.example', 465, ?, ?, ?)`, email, now, email, local)
		upExec(t, tx, `INSERT OR IGNORE INTO profile_accounts (profile_id, account_id) VALUES (1, ?)`, id)
		return id
	}
	folder := func(account int64, path, delim string, parent any, attrs string) int64 {
		return upExec(t, tx, `INSERT INTO folders (account_id, name, imap_path, delimiter, parent_id, attributes, uid_validity, last_seen_uid)
			VALUES (?, ?, ?, ?, ?, ?, 7, 900)`, account, path, path, delim, parent, attrs)
	}
	msg := func(account, folder, uid int64, subject, body string, i int64) int64 {
		var certs []byte
		if i%13 == 0 {
			certs = []byte{0x30, 0x82, 0x01, 0x00}
		}
		snooze := map[bool]string{true: "2030-01-01T00:00:00Z"}[i%9 == 4]
		return upExec(t, tx, `INSERT INTO messages (account_id, folder_id, uid, message_id, subject, from_address, from_name, to_addresses, date, flags,
			body_plain, body_html, size_bytes, pending_flags, pending_delete, flag_color, snooze_until, snooze_hidden, offline, needs_refetch, smime_certs, auth_spf)
			VALUES (?, ?, ?, ?, ?, ?, 'Nadawca', 'a@b.pl', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pass')`,
			account, folder, uid, fmt.Sprintf("<m%d@%d>", i, account), subject, fmt.Sprintf("s%d@sender.pl", uid),
			fmt.Sprintf("2020-01-%02dT10:00:00Z", 1+i%28), i%16, body, "<p>"+body+"</p>", len(body),
			boolToInt(i%9 == 1), boolToInt(i%9 == 2), i%8, snooze, boolToInt(snooze != ""), boolToInt(i%9 == 5), boolToInt(i%9 == 6), append([]byte{}, certs...))
	}

	gmail := account("jan@gmail.com", 0)
	dove := account("jan@dovecot.pl", 0)
	local := account("local@pelton", 1)
	inbox := folder(gmail, "INBOX", "/", nil, "")
	root := folder(gmail, "[Gmail]", "/", nil, `\Noselect`)
	folders := []struct{ account, id int64 }{
		{gmail, inbox},
		{gmail, folder(gmail, "[Gmail]/All Mail", "/", root, `\All`)},
		{gmail, folder(gmail, "Archive", "/", nil, `\Archive`)},
		{gmail, folder(gmail, "archive", "/", nil, "")}, // case-only twin
		{gmail, folder(gmail, "&AVsA8wFCAQc-", "/", nil, "")},
		{dove, folder(dove, "INBOX.Sent", ".", nil, `\Sent`)},
		{dove, folder(dove, "[Gmail]", ".", nil, "")}, // same path, other account
		{local, folder(local, "Import 2019", "/", folder(local, "", "", nil, ""), "")},
	}
	words := []string{"faktura", "zamówienie", "łódź", "Счёт", "日本語", "meeting"}
	for _, f := range folders {
		for i := int64(1); i <= 12; i++ {
			subject := fmt.Sprintf("%s %d", words[i%int64(len(words))], i)
			body := subject + " treść " + words[(i*5)%int64(len(words))]
			if i%11 == 0 {
				body = ""
			}
			id := msg(f.account, f.id, i*3, subject, body, i)
			if i%4 == 0 {
				upExec(t, tx, `INSERT INTO attachments (message_id, filename, content_type, size_bytes, disk_path) VALUES (?, 'f.pdf', 'application/pdf', 9, ?)`,
					id, fmt.Sprintf("attachments/%d/%d/f.pdf", f.account, id))
			}
			if i%6 == 0 {
				upExec(t, tx, `INSERT INTO message_pgp (message_id, raw) VALUES (?, x'00')`, id)
				upExec(t, tx, `INSERT INTO agent_actions (tool, message_id, summary, created_at) VALUES ('archive', ?, 'x', ?)`, id, now)
			}
		}
	}
	// main's window floor: INBOX caches uid 18 and up.
	upExec(t, tx, `UPDATE folders SET sync_floor_uid = 18 WHERE id = ?`, inbox)
	fx.account, fx.folder = gmail, inbox
	fx.uid0 = msg(local, folders[7].id, 0, "szkic", "szkic", 100)
	fx.maxUID = msg(gmail, inbox, 4294967295, "max uid", "max", 101)
	for i := int64(0); i < 4; i++ {
		upExec(t, tx, `INSERT INTO attachments (message_id, filename, disk_path) VALUES (?, 'x', 'x')`, msg(gmail, inbox, 100000+i, "gone", "gone", i))
	}
	upExec(t, tx, `DELETE FROM messages WHERE uid BETWEEN 100000 AND 100003`)
	fx.seq = upInt(t, tx, `SELECT seq FROM sqlite_sequence WHERE name = 'messages'`)
	upExec(t, tx, `INSERT OR REPLACE INTO settings (key, profile_id, value, updated_at) VALUES ('search_indexed_max_id', 1, ?, ?)`, fx.seq, now)
	upExec(t, tx, `INSERT INTO views (name, query_text, account_id, created_at, updated_at, profile_id) VALUES ('Faktury', 'faktura', ?, ?, ?, 1)`, gmail, now, now)
	book := upExec(t, tx, `INSERT INTO address_books (account_id, name, url, collection_path, created_at) VALUES (?, 'K', 'https://dav/', '/c/', ?)`, gmail, now)
	contact := upExec(t, tx, `INSERT INTO contacts (book_id, path, etag, uid, full_name, card, updated_at) VALUES (?, '/c/1.vcf', 'e', 'u', 'Józef', 'BEGIN:VCARD', ?)`, book, now)
	upExec(t, tx, `INSERT INTO contact_emails (contact_id, email, label, position) VALUES (?, 'jozef@cma.pl', 'work', 0)`, contact)
	upExec(t, tx, `INSERT INTO outbox (account_id, envelope_from, recipients, raw_message, state, next_attempt_at, created_at) VALUES (?, 'a', 'b', x'00', 'queued', ?, ?)`, gmail, now, now)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return fx
}

// fingerprint hashes every user table over the columns it had at v36, so data
// copied by a rebuild or kept by ADD COLUMN compares equal.
func fingerprint(t *testing.T, db *DB, cols map[string][]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for table, cs := range cols {
		q := fmt.Sprintf(`SELECT quote(%s) FROM %q ORDER BY rowid`, strings.Join(cs, `)||'|'||quote(`), table)
		rows, err := db.sql.Query(q)
		if err != nil {
			t.Fatalf("fingerprint %s: %v", table, err)
		}
		h, n := sha256.New(), 0
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(h, s)
			n++
		}
		rows.Close()
		out[table] = fmt.Sprintf("%d rows %x", n, h.Sum(nil)[:8])
	}
	return out
}

func userColumns(t *testing.T, db *DB) map[string][]string {
	t.Helper()
	cols := map[string][]string{}
	rows, err := db.sql.Query(`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%' AND m.name NOT LIKE 'messages_fts%' AND m.name != 'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			t.Fatal(err)
		}
		cols[table] = append(cols[table], col)
	}
	return cols
}

func checkUpgradeHealth(t *testing.T, db *DB, label string) {
	t.Helper()
	var ic string
	if err := db.sql.QueryRow(`PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		t.Errorf("%s: integrity_check %q %v", label, ic, err)
	}
	if n := upInt(t, db.sql, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("%s: %d foreign key violations", label, n)
	}
	if _, err := db.sql.Exec(`INSERT INTO messages_fts(messages_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		t.Errorf("%s: fts integrity-check: %v", label, err)
	}
	if a, b := upInt(t, db.sql, `SELECT count(*) FROM messages_fts_docsize`), upInt(t, db.sql, `SELECT count(*) FROM messages`); a != b {
		t.Errorf("%s: fts has %d docs for %d messages", label, a, b)
	}
}

func ftsMatches(t *testing.T, db *DB, q string) string {
	t.Helper()
	var ids sql.NullString
	if err := db.sql.QueryRow(`SELECT group_concat(rowid) FROM (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ? ORDER BY rowid)`, q).Scan(&ids); err != nil {
		t.Fatalf("fts %q: %v", q, err)
	}
	return ids.String
}

func schemaOf(t *testing.T, db *DB) string {
	t.Helper()
	var s sql.NullString
	if err := db.sql.QueryRow(`SELECT group_concat(type || ' ' || name || ': ' || coalesce(sql, ''), char(10))
		FROM (SELECT * FROM sqlite_master ORDER BY type, name)`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(s.String), " ")
}

// Each check is a count that must equal want right after its migration.
var upgradeChecks = map[int][]struct {
	query string
	want  int64
}{
	37: {
		{`SELECT count(*) FROM folders WHERE remote_id != imap_path OR state_token != '' OR sync_initialized != 0`, 0},
		{`SELECT count(*) FROM folders WHERE sync_floor_id != CASE WHEN sync_floor_uid > 0 THEN CAST(sync_floor_uid AS TEXT) ELSE '' END`, 0},
		{`SELECT count(*) FROM folders WHERE sync_floor_id = '18'`, 1},
		{`SELECT count(*) FROM messages WHERE remote_id != CAST(uid AS TEXT)`, 0},
		{`SELECT count(*) FROM sqlite_master WHERE sql LIKE '%messages_new%'`, 0},
		{`SELECT count(*) FROM sqlite_sequence WHERE name IN ('messages', 'messages_new')`, 1},
		{`SELECT count(*) FROM sqlite_master WHERE type = 'trigger' AND tbl_name = 'messages'`, 3},
		{`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'messages' AND name LIKE 'idx_%'`, 11},
		{`SELECT count(*) FROM messages WHERE body_complete != 1`, 0},
		{`SELECT count(*) FROM messages WHERE search_state != 2`, 0},
	},
	39: {{`SELECT count(*) FROM accounts WHERE sync_max_parallel IS NOT NULL`, 0}},
	40: {{`SELECT count(*) FROM sqlite_master WHERE name = 'idx_messages_folder_body_date'`, 1}},
	41: {{`SELECT count(*) FROM folders WHERE last_full_sync_at IS NOT NULL`, 0}},
	42: {{`SELECT count(*) FROM sqlite_master WHERE name = 'idx_messages_search_stale_stub'`, 1}},
}

func TestMigrationUpgradeFromMain(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, 36)
	fx := seedV36(t, db)
	checkUpgradeHealth(t, db, "v36")
	cols := userColumns(t, db)
	before := fingerprint(t, db, cols)
	fts := map[string]string{}
	for _, q := range []string{"faktura", "łódź", "Счёт", "日本語", "szkic"} {
		fts[q] = ftsMatches(t, db, q)
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version <= 36 {
			continue
		}
		if err := db.applyMigration(ctx, m); err != nil {
			t.Fatalf("apply %d: %v", m.version, err)
		}
		label := fmt.Sprintf("after %04d", m.version)
		checkUpgradeHealth(t, db, label)
		after := fingerprint(t, db, cols)
		for table, fp := range before {
			if after[table] != fp {
				t.Errorf("%s: %s changed: %s -> %s", label, table, fp, after[table])
			}
		}
		for q, want := range fts {
			if got := ftsMatches(t, db, q); got != want {
				t.Errorf("%s: fts %q = %s, want %s", label, q, got, want)
			}
		}
		for _, c := range upgradeChecks[m.version] {
			if got := upInt(t, db.sql, c.query); got != c.want {
				t.Errorf("%s: %s = %d, want %d", label, c.query, got, c.want)
			}
		}
		// The rebuild must keep the AUTOINCREMENT high-water mark, or ids of
		// deleted mail come back and the search watermark skips new mail.
		if seq := upInt(t, db.sql, `SELECT seq FROM sqlite_sequence WHERE name = 'messages'`); seq != fx.seq {
			t.Errorf("%s: sqlite_sequence(messages) %d, want %d", label, seq, fx.seq)
		}
	}

	if upInt(t, db.sql, `SELECT count(*) FROM messages WHERE (id = ? AND remote_id = '4294967295') OR (id = ? AND remote_id = '0')`, fx.maxUID, fx.uid0) != 2 {
		t.Errorf("uid 0 / uint32 max rows lost their remote_id")
	}
	id, err := db.InsertMessage(ctx, &Message{AccountID: fx.account, FolderID: fx.folder, UID: 5000, RemoteID: "5000",
		Subject: "kwarcytowy", BodyPlain: "po migracji", BodyComplete: true})
	if err != nil {
		t.Fatalf("insert after upgrade: %v", err)
	}
	if id <= fx.seq {
		t.Errorf("new message id %d reuses a deleted id (high-water %d)", id, fx.seq)
	}
	upExec(t, db.sql, `UPDATE messages SET subject = 'grafitowy', flags = 3 WHERE id = ?`, id)
	if got := ftsMatches(t, db, "grafitowy"); got != fmt.Sprint(id) || ftsMatches(t, db, "kwarcytowy") != "" {
		t.Errorf("fts after subject update: %q", got)
	}
	upExec(t, db.sql, `UPDATE messages SET flags = 1, body_complete = 0, pending_flags = 1`)
	if err := db.DeleteMessage(ctx, fx.maxUID); err != nil {
		t.Fatal(err)
	}
	checkUpgradeHealth(t, db, "after use")

	// The narrowed update trigger must list every FTS column.
	trigger := regexp.MustCompile(`(?is)AFTER UPDATE OF (.*?) ON messages`).FindStringSubmatch(schemaOf(t, db))
	if trigger == nil {
		t.Fatal("messages_fts_au: no UPDATE OF column list")
	}
	listed := map[string]bool{}
	for c := range strings.SplitSeq(trigger[1], ",") {
		listed[strings.TrimSpace(c)] = true
	}
	rows, err := db.sql.Query(`SELECT name FROM pragma_table_info('messages_fts')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c string
		rows.Scan(&c)
		if !listed[c] || !listed["id"] {
			t.Errorf("messages_fts_au does not fire on %s or id: %s", c, trigger[1])
		}
	}
	rows.Close()

	// Same schema as a fresh install; RunMigrations is then a no-op.
	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })
	if err := fresh.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	schema := schemaOf(t, db)
	if schema != schemaOf(t, fresh) {
		t.Errorf("upgraded schema differs from fresh:\n%s\n---\n%s", schema, schemaOf(t, fresh))
	}
	applied := upInt(t, db.sql, `SELECT count(*) FROM schema_migrations`)
	for range 2 {
		if err := db.RunMigrations(ctx); err != nil {
			t.Fatalf("RunMigrations on upgraded db: %v", err)
		}
	}
	if schemaOf(t, db) != schema || upInt(t, db.sql, `SELECT count(*) FROM schema_migrations`) != applied {
		t.Error("RunMigrations on an upgraded db was not a no-op")
	}
}

// Every message deleted before the upgrade: messages has no rows to copy but
// the high-water mark must still survive the rebuild.
func TestMigrationUpgradeEmptyMessagesKeepsHighWater(t *testing.T) {
	db := openDBThrough(t, 36)
	a := upExec(t, db.sql, `INSERT INTO accounts (email, imap_host, imap_port, created_at) VALUES ('a@b', 'h', 993, 'x')`)
	f := upExec(t, db.sql, `INSERT INTO folders (account_id, name, imap_path) VALUES (?, 'INBOX', 'INBOX')`, a)
	for uid := 1; uid <= 4; uid++ {
		upExec(t, db.sql, `INSERT INTO messages (account_id, folder_id, uid) VALUES (?, ?, ?)`, a, f, uid)
	}
	upExec(t, db.sql, `DELETE FROM messages`)
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id := upExec(t, db.sql, `INSERT INTO messages (account_id, folder_id, uid, remote_id) VALUES (?, ?, 1, '1')`, a, f); id != 5 {
		t.Errorf("next id %d, want 5", id)
	}
}

// Orphans written with foreign keys off before the upgrade do not block a
// foreign-keys-off migration; a violation the migration itself makes does.
func TestMigrationForeignKeysOffChecksOnlyNewViolations(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, 36)
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	upExec(t, conn, `PRAGMA foreign_keys = OFF`)
	upExec(t, conn, `INSERT INTO contact_emails (contact_id, email) VALUES (424242, 'orphan@x')`)
	upExec(t, conn, `INSERT INTO attachments (message_id, filename, disk_path) VALUES (777, 'a', 'b')`)
	upExec(t, conn, `PRAGMA foreign_keys = ON`)
	conn.Close()
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("upgrade with pre-existing orphans: %v", err)
	}
	bad := migration{version: 9999, name: "9999_bad.sql", sql: "-- pelton:foreign-keys-off\n" +
		"INSERT INTO attachments (message_id, filename, disk_path) VALUES (888, 'c', 'd');"}
	if err := db.applyMigration(ctx, bad); err == nil || !strings.Contains(err.Error(), "foreign key violation") {
		t.Errorf("migration adding an orphan: err = %v", err)
	}
	if n := upInt(t, db.sql, `SELECT count(*) FROM attachments WHERE message_id = 888`); n != 0 {
		t.Errorf("failed migration was not rolled back")
	}
	if on := upInt(t, db.sql, `PRAGMA foreign_keys`); on != 1 {
		t.Errorf("foreign_keys = %d after failed migration", on)
	}
}

// The rebuild leaves the old messages table as free pages and a WAL as large
// as the new one; RunMigrations gives both back.
func TestMigrationUpgradeReclaimsSpace(t *testing.T) {
	for _, tc := range []struct {
		name     string
		minFree  int64
		disk     func(used int64) uint64 // free disk after the rebuild; nil: real
		wantFree bool
	}{
		{"vacuum", 0, nil, false},
		{"below threshold", 1 << 40, nil, true},
		{"disk below twice the database", 0, func(used int64) uint64 { return uint64(2*used) - 1 }, true},
		{"disk at twice the database", 0, func(used int64) uint64 { return uint64(2 * used) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := reclaimMinFree
			reclaimMinFree = tc.minFree
			t.Cleanup(func() { reclaimMinFree = old })
			db := openDBThrough(t, 36)
			seedV36(t, db)
			if tc.disk != nil {
				oldDisk := diskFree
				t.Cleanup(func() { diskFree = oldDisk })
				rebuilt := false
				diskFree = func(string) (uint64, bool) {
					if !rebuilt { // the check before 0037
						rebuilt = true
						return 1 << 50, true
					}
					used, _, err := db.usedBytes(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					return tc.disk(used), true
				}
			}
			if err := db.RunMigrations(context.Background()); err != nil {
				t.Fatal(err)
			}
			if free := upInt(t, db.sql, `PRAGMA freelist_count`); (free > 0) != tc.wantFree {
				t.Errorf("freelist_count = %d, want free pages: %v", free, tc.wantFree)
			}
			if fi, err := os.Stat(db.Path() + "-wal"); err == nil && fi.Size() != 0 {
				t.Errorf("wal is %d bytes after the upgrade", fi.Size())
			}
			checkUpgradeHealth(t, db, "after reclaim")
		})
	}
}

// A disk without room for the rebuild stops the upgrade before 0037 with a
// readable error and leaves the database at 36.
func TestMigrationUpgradeRefusesFullDisk(t *testing.T) {
	db := openDBThrough(t, 36)
	seedV36(t, db)
	old := diskFree
	t.Cleanup(func() { diskFree = old })
	diskFree = func(string) (uint64, bool) { return 4096, true }
	err := db.RunMigrations(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not enough free disk space") || !strings.Contains(err.Error(), "0037_remote_ids.sql") {
		t.Fatalf("err = %v, want a readable disk-space error", err)
	}
	if n := upInt(t, db.sql, `SELECT max(id) FROM schema_migrations`); n != 36 {
		t.Errorf("schema at %d after refusal, want 36", n)
	}
	diskFree = func(string) (uint64, bool) { return 0, false } // unknown: just run
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// If foreign keys cannot be turned back on, the connection is dropped rather
// than pooled. With a pool of one, the next query would otherwise get it.
func TestMigrationForeignKeysOffDiscardsConnOnFailure(t *testing.T) {
	ctx := context.Background()
	db := openDBThrough(t, 36)
	db.sql.SetMaxOpenConns(1)
	old := enableForeignKeys
	enableForeignKeys = func(*sql.Conn) error { return errors.New("injected") }
	t.Cleanup(func() { enableForeignKeys = old })
	m := migration{version: 9999, name: "9999_noop.sql", sql: "-- pelton:foreign-keys-off\nSELECT 1;"}
	if err := db.applyMigration(ctx, m); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("err = %v, want the re-enable failure", err)
	}
	if on := upInt(t, db.sql, `PRAGMA foreign_keys`); on != 1 {
		t.Errorf("pooled connection has foreign_keys = %d", on)
	}
}
