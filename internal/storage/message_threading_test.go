package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestThreadingMigrationPreservesLegacyMessages(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ensureMigrationsTable(ctx); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version <= 38 {
			if err := db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, query := range []string{
		`INSERT INTO accounts (id, email, imap_host, imap_port, created_at) VALUES (1, 'legacy@example.com', 'imap.example.com', 993, '')`,
		`INSERT INTO folders (id, account_id, name, imap_path) VALUES (1, 1, 'INBOX', 'INBOX')`,
		`INSERT INTO messages (id, account_id, folder_id, uid, subject, body_plain, body_html) VALUES (1, 1, 1, 42, 'legacy subject', 'legacy body', '<p>legacy body</p>')`,
	} {
		if _, err := db.sql.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	var value sql.NullString
	if err := db.sql.QueryRowContext(ctx, `SELECT [references] FROM messages LIMIT 1`).Scan(&value); err == nil || !strings.Contains(err.Error(), "no such column") {
		t.Fatalf("pre-migration [references] must be a missing column, got value=%+v err=%v", value, err)
	}
	for i := 0; i < 2; i++ {
		if err := db.RunMigrations(ctx); err != nil {
			t.Fatalf("migration run %d: %v", i+1, err)
		}
	}
	var irt, refs sql.NullString
	if err := db.sql.QueryRowContext(ctx, `SELECT in_reply_to, [references] FROM messages WHERE id = 1`).Scan(&irt, &refs); err != nil {
		t.Fatal(err)
	}
	if irt.Valid || refs.Valid {
		t.Fatalf("legacy ancestry must remain SQL NULL: in_reply_to=%+v references=%+v", irt, refs)
	}
	m, err := db.GetMessage(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "legacy subject" || m.BodyPlain != "legacy body" || m.BodyHTML != "<p>legacy body</p>" || m.AccountID != 1 || m.FolderID != 1 || m.UID != 42 {
		t.Fatalf("migration changed legacy content: %+v", m)
	}
	if m.InReplyTo != nil || m.References != nil {
		t.Fatalf("legacy ancestry must be unknown: IRT=%#v References=%#v", m.InReplyTo, m.References)
	}
}

func TestMessageThreadingRoundTrip(t *testing.T) {
	cases := []struct {
		name              string
		irt, refs         []string
		irtJSON, refsJSON sql.NullString
	}{
		{name: "unknown"},
		{name: "known empty", irt: []string{}, refs: []string{}, irtJSON: sql.NullString{String: `[]`, Valid: true}, refsJSON: sql.NullString{String: `[]`, Valid: true}},
		{name: "ordered duplicates", irt: []string{"Parent@Example.com", "Parent@Example.com", "other@example.com"}, refs: []string{"root@example.com", "Parent@Example.com", "root@example.com"}, irtJSON: sql.NullString{String: `["Parent@Example.com","Parent@Example.com","other@example.com"]`, Valid: true}, refsJSON: sql.NullString{String: `["root@example.com","Parent@Example.com","root@example.com"]`, Valid: true}},
		{name: "unknown IRT empty references", refs: []string{}, refsJSON: sql.NullString{String: `[]`, Valid: true}},
		{name: "empty IRT unknown references", irt: []string{}, irtJSON: sql.NullString{String: `[]`, Valid: true}},
	}
	for _, tc := range cases {
		for _, withAttachments := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/attachments=%t", tc.name, withAttachments), func(t *testing.T) {
				db, m := threadingTestMessage(t)
				ctx := context.Background()
				m.InReplyTo, m.References = tc.irt, tc.refs
				var err error
				if withAttachments {
					_, err = db.InsertMessageWithAttachments(ctx, m, []IncomingAttachment{{Filename: "note.txt", ContentType: "text/plain", Content: strings.NewReader("attachment body")}})
				} else {
					_, err = db.InsertMessage(ctx, m)
				}
				if err != nil {
					t.Fatal(err)
				}
				var irt, refs sql.NullString
				if err := db.sql.QueryRowContext(ctx, `SELECT in_reply_to, [references] FROM messages WHERE id = ?`, m.ID).Scan(&irt, &refs); err != nil {
					t.Fatal(err)
				}
				if irt != tc.irtJSON || refs != tc.refsJSON {
					t.Fatalf("SQL ancestry = (%+v, %+v), want (%+v, %+v)", irt, refs, tc.irtJSON, tc.refsJSON)
				}
				assertThreadingMessageReads(t, db, m)
				atts, err := db.ListAttachments(ctx, m.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !withAttachments {
					if len(atts) != 0 {
						t.Fatalf("unexpected attachments: %+v", atts)
					}
					return
				}
				if len(atts) != 1 || atts[0].Filename != "note.txt" {
					t.Fatalf("attachment missing after insert: %+v", atts)
				}
				file, err := db.OpenAttachment(atts[0].DiskPath)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				body, err := io.ReadAll(file)
				if err != nil || string(body) != "attachment body" {
					t.Fatalf("attachment content=%q err=%v", body, err)
				}
			})
		}
	}
}

func TestCorruptThreadingJSONKeepsMessageReadable(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []string
	}{
		{name: "SQL NULL"},
		{name: "malformed", value: `["parent@example.com",`},
		{name: "object", value: `{"id":"parent@example.com"}`},
		{name: "string", value: `"parent@example.com"`},
		{name: "number", value: `42`},
		{name: "boolean", value: `true`},
		{name: "JSON null", value: `null`},
		{name: "mixed array", value: `["parent@example.com",42]`},
		{name: "null element", value: `["parent@example.com",null]`},
		{name: "known empty", value: `[]`, want: []string{}},
	}
	for _, column := range []string{"in_reply_to", "[references]"} {
		for _, tc := range cases {
			t.Run(column+"/"+tc.name, func(t *testing.T) {
				db, m := threadingTestMessage(t)
				ctx := context.Background()
				m.InReplyTo = []string{"parent@example.com"}
				m.References = []string{"root@example.com"}
				if _, err := db.InsertMessage(ctx, m); err != nil {
					t.Fatal(err)
				}
				if _, err := db.sql.ExecContext(ctx, `UPDATE messages SET `+column+` = ? WHERE id = ?`, tc.value, m.ID); err != nil {
					t.Fatal(err)
				}
				if column == "in_reply_to" {
					m.InReplyTo = tc.want
				} else {
					m.References = tc.want
				}
				assertThreadingMessageReads(t, db, m)
			})
		}
	}
}

func threadingTestMessage(t *testing.T) (*DB, *Message) {
	t.Helper()
	db := newTestDB(t)
	ctx := context.Background()
	account, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := db.EnsureLocalFolder(ctx, account.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	return db, &Message{AccountID: account.ID, FolderID: folder.ID, UID: 42, Subject: "readable subject", BodyPlain: "readable body", BodyHTML: "<p>readable body</p>"}
}

func assertThreadingMessageReads(t *testing.T, db *DB, want *Message) {
	t.Helper()
	ctx := context.Background()
	reads := []struct {
		name string
		read func() ([]Message, error)
	}{
		{"GetMessage", func() ([]Message, error) {
			m, err := db.GetMessage(ctx, want.ID)
			if err != nil {
				return nil, err
			}
			return []Message{*m}, nil
		}},
		{"ListMessages", func() ([]Message, error) { return db.ListMessages(ctx, want.FolderID, 0) }},
		{"QueryMessages", func() ([]Message, error) {
			return db.QueryMessages(ctx, MessageQuery{FolderIDs: []int64{want.FolderID}})
		}},
		{"QueryMessages unified", func() ([]Message, error) {
			return db.QueryMessages(ctx, MessageQuery{FolderIDs: []int64{want.FolderID, -1}})
		}},
		{"ListMessagesForIndex", func() ([]Message, error) { return db.ListMessagesForIndex(ctx, 0, 0) }},
	}
	for _, read := range reads {
		t.Run(read.name, func(t *testing.T) {
			messages, err := read.read()
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 {
				t.Fatalf("got %d messages, want 1", len(messages))
			}
			got := messages[0]
			if !reflect.DeepEqual(got.InReplyTo, want.InReplyTo) || !reflect.DeepEqual(got.References, want.References) {
				t.Errorf("ancestry=(%#v, %#v), want (%#v, %#v)", got.InReplyTo, got.References, want.InReplyTo, want.References)
			}
			if got.ID != want.ID || got.AccountID != want.AccountID || got.FolderID != want.FolderID || got.UID != want.UID || got.Subject != want.Subject || got.BodyPlain != want.BodyPlain || got.BodyHTML != want.BodyHTML || got.HasAttachments != want.HasAttachments {
				t.Errorf("message content changed: %+v", got)
			}
		})
	}
}
