package storage

import (
	"context"
	"testing"
	"time"
)

func TestMessageTrustRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, accountID := newFolderTestDB(t)
	inbox := &Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	insert := func(uid uint32, from, replyTo string, at time.Time) int64 {
		id, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: inbox.ID, UID: uid,
			FromAddress: from, FromName: "Anna", ReplyTo: replyTo, Date: at})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := insert(1, "Anna <Anna@Example.com>", "", base)
	second := insert(2, "anna@example.com", "", base.Add(24*time.Hour))
	latest := insert(3, "Anna <anna@example.com>", "other@else.test", base.Add(48*time.Hour))
	insert(4, "hanna@example.com", "", base.Add(time.Hour))

	want := MessageTrust{MessageID: first, AuthState: "trusted", ServID: "mx.example.org", DKIM: "pass",
		DKIMDomains: []string{"example.com", "esp.test"}, DMARC: "pass", Signers: []string{"example.com"}, ReadReceiptTo: "r@example.com"}
	if err := db.SetMessageTrust(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMessageTrust(ctx, first)
	if err != nil || got.AuthState != "trusted" || len(got.DKIMDomains) != 2 || got.ReadReceiptTo != "r@example.com" {
		t.Fatalf("GetMessageTrust = %+v, %v", got, err)
	}
	if none, err := db.GetMessageTrust(ctx, second); err != nil || none.AuthState != "" {
		t.Fatalf("a message without a record = %+v, %v", none, err)
	}

	history, err := db.SenderTrustHistory(ctx, "ANNA@example.com", []int64{inbox.ID}, base.Add(48*time.Hour), latest, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].ID != second || history[1].ID != first {
		t.Fatalf("history = %+v, want the two earlier mails from anna, newest first", history)
	}
	if history[1].Trust.AuthState != "trusted" || len(history[1].Trust.DKIMDomains) != 2 {
		t.Errorf("history record = %+v", history[1].Trust)
	}

	if err := db.DeleteMessage(ctx, first); err != nil {
		t.Fatal(err)
	}
	if gone, _ := db.GetMessageTrust(ctx, first); gone.AuthState != "" {
		t.Errorf("the record outlived its message: %+v", gone)
	}
}

func TestSenderMarks(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if err := db.SetSenderMark(ctx, " Anna@Example.com ", SenderKnown); err != nil {
		t.Fatal(err)
	}
	m, err := db.GetSenderMark(ctx, "anna@example.com")
	if err != nil || m.Mark != SenderKnown || m.At.IsZero() {
		t.Fatalf("mark = %+v, %v", m, err)
	}
	if err := db.SetSenderMark(ctx, "anna@example.com", SenderImpostor); err != nil {
		t.Fatal(err)
	}
	if m, _ = db.GetSenderMark(ctx, "anna@example.com"); m.Mark != SenderImpostor {
		t.Errorf("mark = %+v, want it replaced", m)
	}
	if err := db.SetSenderMark(ctx, "anna@example.com", "bogus"); err == nil {
		t.Error("an unknown mark was stored")
	}
	if err := db.SetSenderMark(ctx, "anna@example.com", ""); err != nil {
		t.Fatal(err)
	}
	if m, _ = db.GetSenderMark(ctx, "anna@example.com"); m.Mark != "" {
		t.Errorf("mark = %+v, want it cleared", m)
	}
}

func TestSentLines(t *testing.T) {
	ctx := context.Background()
	db, accountID := newFolderTestDB(t)
	sent := &Folder{AccountID: accountID, Name: "Sent", IMAPPath: "Sent"}
	if _, err := db.CreateFolder(ctx, sent); err != nil {
		t.Fatal(err)
	}
	for i, to := range []string{"Anna <anna@example.com>", "bob@example.com", "hanna@example.com"} {
		if _, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: sent.ID, UID: uint32(i + 1),
			ToAddresses: to, Date: time.Date(2025, 1, i+1, 0, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := db.SentLines(ctx, []int64{sent.ID}, "ANNA@example.com", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Errorf("lines = %+v, want anna's and the longer hanna address for the caller to tell apart", lines)
	}
	all, err := db.SentLines(ctx, []int64{sent.ID}, "", 2)
	if err != nil || len(all) != 2 || all[0].To != "hanna@example.com" {
		t.Errorf("all = %+v, %v; want the newest two", all, err)
	}
}
