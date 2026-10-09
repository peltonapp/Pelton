package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRemoteIDsNeedingBodyNewestUsesBodyIndex(t *testing.T) {
	db := newTestDB(t)
	query, args := remoteIDsNeedingBodyNewestQuery(1, 0)
	rows, err := db.sql.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "idx_messages_folder_body_date") {
		t.Fatalf("plan does not use idx_messages_folder_body_date:\n%s", joined)
	}
	if strings.Contains(joined, "TEMP B-TREE") {
		t.Fatalf("plan sorts in a temp b-tree:\n%s", joined)
	}
}

func TestMessageDatesByRemoteID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	f := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, &f); err != nil {
		t.Fatal(err)
	}
	d1 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	d2 := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	for _, m := range []Message{
		{AccountID: accountID, FolderID: f.ID, UID: 1, RemoteID: "e1", Date: d1},
		{AccountID: accountID, FolderID: f.ID, UID: 2, RemoteID: "e2", Date: d2},
	} {
		msg := m
		if _, err := db.InsertMessage(ctx, &msg); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.MessageDatesByRemoteID(ctx, f.ID, []string{"e2", "e1", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["e1"].Equal(d1) || !got["e2"].Equal(d2) {
		t.Fatalf("dates = %v", got)
	}
	empty, err := db.MessageDatesByRemoteID(ctx, f.ID, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("nil ids: %v, %v", empty, err)
	}
}
