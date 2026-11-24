package storage

import (
	"context"
	"testing"
	"time"
)

// A list stub is threaded from its envelope alone; the body fill brings the
// References that put it in its conversation, and the split inbox kind and
// List-Id of the full headers.
func TestFillRethreadsAndClassifies(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	when := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	parent, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", UID: 1,
		MessageID: "<root@x>", Subject: "Plan", FromAddress: "a@x", Date: when, BodyComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	stubID, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "2", UID: 2,
		MessageID: "<later@x>", Subject: "Changed topic", FromAddress: "news@lists.x", Date: when.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	threadOf := func(id int64) int64 {
		return upInt(t, db.sql, `SELECT thread_id FROM messages WHERE id = ?`, id)
	}
	if threadOf(stubID) == threadOf(parent) {
		t.Fatal("the stub joined the thread before its References were known")
	}
	if _, err := db.FillMessageBody(ctx, BodyTarget{MessageID: stubID}, &Message{AccountID: accountID, FolderID: folder.ID,
		RemoteID: "2", UID: 2, MessageID: "<later@x>", Subject: "Changed topic", FromAddress: "news@lists.x",
		Date: when.Add(time.Hour), References: []string{"<root@x>"}, ListID: "<news.lists.x>", BodyPlain: "text"}, nil); err != nil {
		t.Fatal(err)
	}
	if threadOf(stubID) != threadOf(parent) {
		t.Error("the filled message did not join the conversation its References name")
	}
	var listID string
	var kind MailKind
	if err := db.sql.QueryRow(`SELECT list_id, mail_kind FROM messages WHERE id = ?`, stubID).Scan(&listID, &kind); err != nil {
		t.Fatal(err)
	}
	if listID != "<news.lists.x>" || kind&MailKindList == 0 {
		t.Errorf("list id %q, kind %d after the fill", listID, kind)
	}
}
