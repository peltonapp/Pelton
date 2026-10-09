package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestListMetaLeavesBodyIncomplete(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: "1",
		Subject: "Hi", BodyPlain: "preview", BodyComplete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyComplete {
		t.Fatal("list meta must leave body_complete=0")
	}
	needs, err := db.MessageNeedsBody(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !needs {
		t.Fatal("incomplete stub must need a body")
	}
}

func TestListMetaRefreshKeepsBodyIncomplete(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "1",
		Subject: "Hi", BodyPlain: "preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "1",
		Subject: "Hi again", BodyPlain: "newer preview", BodyComplete: true,
	}); err != nil {
		t.Fatal(err)
	}
	m, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyComplete {
		t.Fatal("list meta refresh must not mark the body complete")
	}
	if m.Subject != "Hi again" {
		t.Fatalf("subject %q", m.Subject)
	}
}

func TestFullUpsertSetsBodyComplete(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := db.InsertMessage(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "full",
		Subject: "Full", BodyPlain: "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !m.BodyComplete {
		t.Fatal("full upsert must set body_complete=1")
	}
	needs, err := db.MessageNeedsBody(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if needs {
		t.Fatal("complete message must not need a body")
	}
}

func TestMessageNeedsBodyMissing(t *testing.T) {
	db := newTestDB(t)
	_, err := db.MessageNeedsBody(context.Background(), 404)
	if !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("err %v, want ErrMessageNotFound", err)
	}
}

func TestBodyFillOfIncompleteStubPreservesUnsetFlags(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)

	id, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "stub-1",
		Subject: "Hi", BodyPlain: "preview", Flags: FlagSeen | FlagFlagged,
	})
	if err != nil {
		t.Fatal(err)
	}

	filledID, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "stub-1",
		Subject: "Hi", BodyPlain: "full body", BodyHTML: "<p>full</p>",
		Flags: 0,
	}, []IncomingAttachment{{
		Filename: "a.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("att")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if filledID != id {
		t.Fatalf("filled id %d, want %d", filledID, id)
	}

	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.BodyComplete || got.BodyPlain != "full body" || got.BodyHTML != "<p>full</p>" {
		t.Fatalf("filled row: %+v", got)
	}
	if got.Flags != FlagSeen|FlagFlagged {
		t.Fatalf("flags %#b, want seen|flagged kept when caller passed 0", got.Flags)
	}
	if !got.HasAttachments {
		t.Fatal("body fill must record attachments")
	}

	loadedID, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "stub-flags",
		Subject: "Other", Flags: FlagSeen,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE messages SET body_complete = 0 WHERE id = ?`, loadedID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "stub-flags",
		Subject: "Other", BodyPlain: "body", Flags: FlagFlagged,
	}, nil); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetMessage(ctx, loadedID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != FlagFlagged {
		t.Fatalf("flags %#b, want flagged from the body fill", got.Flags)
	}
	if !got.BodyComplete || got.BodyPlain != "body" {
		t.Fatalf("loaded fill: %+v", got)
	}
}

func TestInsertLeavesCompleteMessageUntouched(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)

	id, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "done",
		Subject: "Kept", BodyPlain: "original body", Flags: FlagSeen | FlagFlagged,
	}, []IncomingAttachment{{
		Filename: "keep.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("original-bytes")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(db.AttachmentsDir(), accountSegment(accountID), messageSegment(id), "keep.txt")

	_, err = db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "done",
		Subject: "Replaced", BodyPlain: "new body", Flags: 0,
	}, []IncomingAttachment{{
		Filename: "keep.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("new-bytes")),
	}})
	if !errors.Is(err, ErrMessageBodyComplete) {
		t.Fatalf("err %v, want ErrMessageBodyComplete", err)
	}

	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Kept" || got.BodyPlain != "original body" || !got.BodyComplete {
		t.Fatalf("complete row changed: %+v", got)
	}
	if got.Flags != FlagSeen|FlagFlagged {
		t.Fatalf("flags %#b, want seen|flagged", got.Flags)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "original-bytes" {
		t.Fatalf("attachment %q, want original-bytes", body)
	}
	atts, err := db.ListAttachments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].Filename != "keep.txt" {
		t.Fatalf("attachments %+v", atts)
	}
}

func TestBodyFillRollbackRestoresAttachmentFiles(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)

	id, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "partial",
		Subject: "Stub", BodyPlain: "original body", Flags: FlagSeen,
	}, []IncomingAttachment{{
		Filename: "note.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("original-bytes")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE messages SET body_complete = 0 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(db.AttachmentsDir(), accountSegment(accountID), messageSegment(id), "note.txt")

	db.deleteTxHook = func(context.Context) error { return errors.New("inject fail") }
	_, err = db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "partial",
		Subject: "Stub", BodyPlain: "new body", Flags: 0,
	}, []IncomingAttachment{{
		Filename: "note.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("new-bytes")),
	}})
	if err == nil {
		t.Fatal("expected injected failure")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("attachment missing after failed replace: %v", err)
	}
	if string(body) != "original-bytes" {
		t.Fatalf("attachment %q, want original-bytes", body)
	}
	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.BodyComplete || got.BodyPlain != "original body" || got.Flags != FlagSeen {
		t.Fatalf("row after rollback: %+v", got)
	}
	atts, err := db.ListAttachments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].Filename != "note.txt" {
		t.Fatalf("attachments after rollback: %+v", atts)
	}
}

func TestBodyFillReplacesPriorAttachmentFiles(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)

	id, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "partial",
		Subject: "Stub", BodyPlain: "preview", Flags: FlagSeen,
	}, []IncomingAttachment{{
		Filename: "note.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("original-bytes")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE messages SET body_complete = 0 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(db.AttachmentsDir(), accountSegment(accountID), messageSegment(id), "note.txt")

	if _, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "partial",
		Subject: "Stub", BodyPlain: "full", Flags: FlagSeen,
	}, []IncomingAttachment{{
		Filename: "note.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("new-bytes")),
	}}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new-bytes" {
		t.Fatalf("attachment %q, want new-bytes", body)
	}
	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.BodyComplete || got.BodyPlain != "full" {
		t.Fatalf("filled row: %+v", got)
	}
}

func TestRemoteIDsNeedingBodyKeepsIncomplete(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	if _, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "stub",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "done",
		Subject: "Kept", BodyPlain: "body",
	}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := db.RemoteIDsNeedingBody(ctx, folder.ID, []string{"done", "stub", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"stub", "missing"}
	if len(got) != len(want) {
		t.Fatalf("need %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("need %v, want %v", got, want)
		}
	}
}

func newBodyTestFolder(t *testing.T) (*DB, int64, Folder) {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	return db, accountID, folder
}

func TestRemoteIDsNeedingBodyNewest(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"old", "mid", "new", "newest"} {
		if _, err := db.UpsertMessageListMeta(ctx, &Message{
			AccountID: accountID, FolderID: folder.ID, RemoteID: id,
			Date: base.AddDate(0, 0, i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "done",
		Subject: "Kept", BodyPlain: "body", Date: base.AddDate(0, 0, 10),
	}, nil); err != nil {
		t.Fatal(err)
	}
	all, err := db.RemoteIDsNeedingBodyNewest(ctx, folder.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"newest", "new", "mid", "old"}; !slices.Equal(all, want) {
		t.Fatalf("limit 0: %v, want %v", all, want)
	}
	two, err := db.RemoteIDsNeedingBodyNewest(ctx, folder.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	// The window is the 2 newest rows (done, newest); "done" is complete, so
	// only "newest" remains. Older stubs must not slide into the window.
	if want := []string{"newest"}; !slices.Equal(two, want) {
		t.Fatalf("limit 2: %v, want %v", two, want)
	}
	three, err := db.RemoteIDsNeedingBodyNewest(ctx, folder.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"newest", "new"}; !slices.Equal(three, want) {
		t.Fatalf("limit 3: %v, want %v", three, want)
	}
}

func TestBodyFillWithoutDateKeepsStubDate(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)

	stubDate := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	id, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "stub-date",
		Subject: "Hi", Date: stubDate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "stub-date",
		Subject: "Hi", BodyPlain: "body without a Date header",
	}, nil); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Date.Equal(stubDate) {
		t.Fatalf("date %v, want stub date %v kept", got.Date, stubDate)
	}
}

func TestMessageIDByRemoteID(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	other := Folder{AccountID: accountID, Name: "Other", IMAPPath: "mb2", RemoteID: "mb2"}
	for _, f := range []*Folder{&inbox, &other} {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: inbox.ID, RemoteID: "E1"})
	if err != nil {
		t.Fatal(err)
	}

	if got, err := db.MessageIDByRemoteID(ctx, inbox.ID, "E1"); err != nil || got != id {
		t.Errorf("MessageIDByRemoteID(inbox, E1) = %d, %v, want %d", got, err, id)
	}
	// the same remote id in another folder is a different message.
	if _, err := db.MessageIDByRemoteID(ctx, other.ID, "E1"); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("lookup in another folder: err %v, want ErrMessageNotFound", err)
	}
	if _, err := db.MessageIDByRemoteID(ctx, inbox.ID, "missing"); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("lookup of a missing id: err %v, want ErrMessageNotFound", err)
	}
}

// The list stub carries the sender's name from the envelope. Filling its body
// from a header the name could not be read from must not erase it, while a
// name the fill does carry replaces it.
func TestBodyFillKeepsStubFromNameWhenFillHasNone(t *testing.T) {
	cases := []struct {
		name, fillName, want string
	}{
		{"fill without name", "", "Jane Doe"},
		{"fill with name", "Jane Q. Doe", "Jane Q. Doe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newTestDB(t)
			accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
			if err != nil {
				t.Fatal(err)
			}
			folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
			if _, err := db.CreateFolder(ctx, &folder); err != nil {
				t.Fatal(err)
			}
			id, err := db.UpsertMessageListMeta(ctx, &Message{
				AccountID: accountID, FolderID: folder.ID, RemoteID: "1",
				FromAddress: "jane@x.org", FromName: "Jane Doe",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.InsertMessageWithAttachments(ctx, &Message{
				AccountID: accountID, FolderID: folder.ID, RemoteID: "1",
				FromAddress: "jane@x.org", FromName: tc.fillName, BodyPlain: "body",
			}, nil); err != nil {
				t.Fatal(err)
			}
			m, err := db.GetMessage(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if m.FromName != tc.want {
				t.Fatalf("FromName %q, want %q", m.FromName, tc.want)
			}
		})
	}
}
