package storage

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

// listMetaRows reads the columns a list upsert writes or derives, and what
// the full-text index finds, for every row of a folder in remote id order.
func listMetaRows(t *testing.T, db *DB, folderID int64) []string {
	t.Helper()
	rows, err := db.sql.Query(`
SELECT m.remote_id, m.uid, m.subject, m.from_address, m.from_name, m.to_addresses,
       m.date, m.flags, m.body_plain, m.body_complete, m.has_attachments, m.size_bytes,
       m.search_state, m.text_rev,
       (SELECT count(*) FROM messages_fts WHERE messages_fts MATCH m.remote_id || '*' AND rowid = m.id)
  FROM messages m WHERE m.folder_id = ? ORDER BY m.remote_id`, folderID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var rid, subject, from, fromName, to, date, plain string
		var uid, flags, complete, atts, size, state, rev, fts int64
		if err := rows.Scan(&rid, &uid, &subject, &from, &fromName, &to, &date, &flags, &plain, &complete, &atts, &size, &state, &rev, &fts); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s uid=%d %q %s %q %s %s flags=%d plain=%q complete=%d atts=%d size=%d search=%d rev=%d fts=%d",
			rid, uid, subject, from, fromName, to, date, flags, plain, complete, atts, size, state, rev, fts))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A batch writes exactly what the same upserts one at a time write: new
// stubs, a refresh of an existing row (deduplicated by folder and remote
// id, its text revision bumped and its index entry marked stale), an
// unchanged refresh, and a remote id twice in one batch. The index
// triggers fire for every row.
func TestUpsertMessageListMetaBatchMatchesSingleUpserts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	date := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	seed := func(t *testing.T) (*DB, int64, Folder) {
		db, accountID, folder := newBodyTestFolder(t)
		full := &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "r1", Subject: "r1 old", FromAddress: "a@ex", Date: date, BodyPlain: "body"}
		if _, err := db.InsertMessage(ctx, full); err != nil {
			t.Fatal(err)
		}
		read := mustMessage(t, db, full.ID)
		if err := db.RecordSearchState(ctx, []IndexRead{read.IndexRead()}, SearchFull); err != nil {
			t.Fatal(err)
		}
		if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "r2", Subject: "r2 same", FromAddress: "b@ex", Date: date}); err != nil {
			t.Fatal(err)
		}
		return db, accountID, folder
	}
	batch := func(accountID int64, folder Folder) []Message {
		m := func(rid, subject string, uid uint32) Message {
			return Message{AccountID: accountID, FolderID: folder.ID, RemoteID: rid, UID: uid, Subject: subject,
				FromAddress: rid + "@ex", FromName: "N " + rid, ToAddresses: "to@ex", Date: date,
				Flags: FlagSeen, BodyPlain: "preview " + rid, HasAttachments: true, SizeBytes: 9}
		}
		r2 := m("r2", "r2 same", 0)
		r2.FromAddress, r2.FromName, r2.ToAddresses, r2.BodyPlain = "b@ex", "", "", ""
		return []Message{
			m("r3", "r3 new", 3),
			m("r1", "r1 new", 1), // complete row, text changed
			r2,                   // stub, nothing searchable changed
			m("r4", "r4 first", 4),
			m("r4", "r4 again", 4), // twice in one batch
		}
	}

	single, accountID, folder := seed(t)
	for _, m := range batch(accountID, folder) {
		if _, err := single.UpsertMessageListMeta(ctx, &m); err != nil {
			t.Fatal(err)
		}
	}
	batched, accountID, folder := seed(t)
	ms := batch(accountID, folder)
	b, err := batched.UpsertMessageListMetaBatch(ctx, ms, time.Minute)
	if err != nil || b.Err != nil || b.Done != len(ms) {
		t.Fatalf("batch: done %d of %d, err %v, row err %v", b.Done, len(ms), err, b.Err)
	}
	if b.IDs[3] != b.IDs[4] {
		t.Fatalf("r4 twice got ids %d and %d, want one row", b.IDs[3], b.IDs[4])
	}

	want, got := listMetaRows(t, single, folder.ID), listMetaRows(t, batched, folder.ID)
	if !slices.Equal(got, want) {
		t.Fatalf("batch rows:\n%v\nwant (single upserts):\n%v", got, want)
	}
	if len(got) != 4 {
		t.Fatalf("%d rows, want 4 (r1..r4): %v", len(got), got)
	}
}

// A message that fails is undone alone and ends the transaction: the ones
// before it commit, the ones after wait for the caller's next call.
func TestUpsertMessageListMetaBatchStopsAtAFailedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	ms := []Message{
		{AccountID: accountID, FolderID: folder.ID, RemoteID: "a", Subject: "a"},
		{AccountID: accountID, FolderID: folder.ID + 99, RemoteID: "bad", Subject: "bad"}, // no such folder
		{AccountID: accountID, FolderID: folder.ID, RemoteID: "c", Subject: "c"},
	}
	b, err := db.UpsertMessageListMetaBatch(ctx, ms, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if b.Done != 2 || b.Err == nil || b.IDs[0] == 0 || b.IDs[1] != 0 {
		t.Fatalf("done %d, ids %v, err %v; want 2 done, the second failed", b.Done, b.IDs, b.Err)
	}
	b, err = db.UpsertMessageListMetaBatch(ctx, ms[b.Done:], time.Minute)
	if err != nil || b.Err != nil || b.Done != 1 {
		t.Fatalf("rest: done %d, err %v, row err %v", b.Done, err, b.Err)
	}
	rows := listMetaRows(t, db, folder.ID)
	if len(rows) != 2 || rows[0][:2] != "a " || rows[1][:2] != "c " {
		t.Fatalf("rows %v, want a and c", rows)
	}
	var n int
	if err := db.sql.QueryRow(`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'bad'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("index holds the failed row %d times (err %v)", n, err)
	}
}

// Once its budget has passed a transaction takes no more messages, but it
// always takes one, so a caller sending the rest always makes progress.
func TestUpsertMessageListMetaBatchStopsAtItsBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	ms := make([]Message, 3)
	for i := range ms {
		ms[i] = Message{AccountID: accountID, FolderID: folder.ID, RemoteID: fmt.Sprint(i), Subject: "s"}
	}
	for txs := 1; len(ms) > 0; txs++ {
		b, err := db.UpsertMessageListMetaBatch(ctx, ms, 0)
		if err != nil || b.Err != nil || b.Done != 1 {
			t.Fatalf("tx %d: done %d, err %v, row err %v; want one message", txs, b.Done, err, b.Err)
		}
		ms = ms[b.Done:]
	}
	if rows := listMetaRows(t, db, folder.ID); len(rows) != 3 {
		t.Fatalf("rows %v, want 3", rows)
	}
}
