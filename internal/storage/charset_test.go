package storage

import (
	"context"
	"testing"
)

func TestMarkMangledMessagesFindsOnlyBrokenText(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	account, _ := db.EnsureLocalAccount(ctx)
	folder, _ := db.EnsureLocalFolder(ctx, account.ID, "Archive")

	fine := Message{AccountID: account.ID, FolderID: folder.ID, UID: 1, MessageID: "a@example.com",
		Subject: "Grüße", BodyPlain: "Alles gut"}
	brokenBody := Message{AccountID: account.ID, FolderID: folder.ID, UID: 2, MessageID: "b@example.com",
		Subject: "hi", BodyPlain: "caf\xe9"}
	brokenSubject := Message{AccountID: account.ID, FolderID: folder.ID, UID: 3, MessageID: "c@example.com",
		Subject: "Gr\xfc\xdfe", BodyPlain: "fine"}
	for _, m := range []*Message{&fine, &brokenBody, &brokenSubject} {
		if _, err := db.InsertMessage(ctx, m); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	found, err := db.MarkMangledMessages(ctx)
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if found != 2 {
		t.Errorf("marked %d messages, want 2", found)
	}

	marked, err := db.MessagesNeedingRefetch(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(marked) != 2 {
		t.Fatalf("listed %d messages, want 2", len(marked))
	}
	for _, m := range marked {
		if m.ID == fine.ID {
			t.Error("a message with valid text was marked for refetch")
		}
	}

	// a second pass has nothing left to find, so a repeated scan is free.
	if again, err := db.MarkMangledMessages(ctx); err != nil || again != 0 {
		t.Errorf("second scan found %d, %v, want 0 and no error", again, err)
	}
}

func TestRepairMessageTextClearsTheMark(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	account, _ := db.EnsureLocalAccount(ctx)
	folder, _ := db.EnsureLocalFolder(ctx, account.ID, "Archive")

	m := Message{AccountID: account.ID, FolderID: folder.ID, UID: 1, MessageID: "a@example.com",
		Subject: "Gr\xfc\xdfe", BodyPlain: "caf\xe9"}
	if _, err := db.InsertMessage(ctx, &m); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.MarkMangledMessages(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}

	if err := db.RepairMessageText(ctx, m.ID, "Grüße", "café", "", "windows-1252"); err != nil {
		t.Fatalf("repair: %v", err)
	}

	fixed, err := db.GetMessage(ctx, m.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fixed.Subject != "Grüße" || fixed.BodyPlain != "café" {
		t.Errorf("text = %q / %q, want the repaired text", fixed.Subject, fixed.BodyPlain)
	}
	if fixed.CharsetGuess != "windows-1252" {
		t.Errorf("CharsetGuess = %q, want windows-1252", fixed.CharsetGuess)
	}
	left, err := db.MessagesNeedingRefetch(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d messages still marked after the repair", len(left))
	}
}

// a message the server no longer has can never be repaired, so the mark comes
// off anyway rather than being retried on every sync forever.
func TestClearRefetchMark(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	account, _ := db.EnsureLocalAccount(ctx)
	folder, _ := db.EnsureLocalFolder(ctx, account.ID, "Archive")

	m := Message{AccountID: account.ID, FolderID: folder.ID, UID: 1, MessageID: "a@example.com",
		BodyPlain: "caf\xe9"}
	if _, err := db.InsertMessage(ctx, &m); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.MarkMangledMessages(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := db.ClearRefetchMark(ctx, m.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	left, _ := db.MessagesNeedingRefetch(ctx, folder.ID, 0)
	if len(left) != 0 {
		t.Errorf("%d messages still marked", len(left))
	}
}
