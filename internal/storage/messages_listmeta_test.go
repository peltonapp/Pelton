package storage

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestUpsertMessageListMetaThenBodyFill(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	stubID, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID:   accountID,
		FolderID:    folder.ID,
		RemoteID:    "E1",
		Subject:     "Hello",
		FromAddress: "ada@ex",
		FromName:    "Ada",
		ToAddresses: "bob@ex",
		Date:        time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		Flags:       FlagSeen,
		BodyPlain:   "preview text",
		SizeBytes:   42,
	})
	if err != nil {
		t.Fatalf("upsert stub: %v", err)
	}

	got, err := db.GetMessage(ctx, stubID)
	if err != nil {
		t.Fatalf("get stub: %v", err)
	}
	if got.Subject != "Hello" || got.BodyPlain != "preview text" || got.BodyHTML != "" {
		t.Fatalf("stub row: %+v", got)
	}
	if got.BodyComplete {
		t.Fatal("list meta stub must leave body_complete=0")
	}

	bodyID, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID:   accountID,
		FolderID:    folder.ID,
		RemoteID:    "E1",
		Subject:     "Hello full",
		FromAddress: "ada@ex",
		FromName:    "Ada",
		ToAddresses: "bob@ex",
		Date:        time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		Flags:       FlagSeen,
		BodyPlain:   "full body",
		BodyHTML:    "<p>full</p>",
		SizeBytes:   100,
	}, []IncomingAttachment{{
		Filename:    "a.txt",
		ContentType: "text/plain",
		Content:     bytes.NewReader([]byte("att")),
	}})
	if err != nil {
		t.Fatalf("body fill: %v", err)
	}
	if bodyID != stubID {
		t.Fatalf("body fill id %d, want stub id %d", bodyID, stubID)
	}

	filled, err := db.GetMessage(ctx, stubID)
	if err != nil {
		t.Fatalf("get filled: %v", err)
	}
	if filled.Subject != "Hello full" || filled.BodyPlain != "full body" || filled.BodyHTML != "<p>full</p>" {
		t.Fatalf("filled row: %+v", filled)
	}
	if !filled.BodyComplete {
		t.Fatal("body fill must set body_complete=1")
	}
	if !filled.HasAttachments {
		t.Fatal("expected has_attachments after body fill")
	}

	list, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len %d, want 1 (no duplicate row)", len(list))
	}
}

func TestUpsertMessageListMetaPreservesFilledBody(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	id, err := db.InsertMessage(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "E1",
		Subject: "Old", BodyPlain: "body", BodyHTML: "<b>x</b>",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := db.UpsertMessageListMeta(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "E1",
		Subject: "New subj", BodyPlain: "should not overwrite",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Subject != "New subj" {
		t.Fatalf("subject: %q", got.Subject)
	}
	if got.BodyPlain != "body" || got.BodyHTML != "<b>x</b>" {
		t.Fatalf("body overwritten: plain=%q html=%q", got.BodyPlain, got.BodyHTML)
	}
	if !got.BodyComplete {
		t.Fatal("list meta refresh must keep a filled body complete")
	}
}

// Two syncs of one folder can both find a message missing and both insert its
// stub. The second must update the row the first wrote, not fail on the unique
// (folder_id, remote_id) index: a failed stub holds the folder's sync cursor.
func TestUpsertMessageListMetaConcurrentInsertsSameMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	const ids, writers = 200, 4
	var wg sync.WaitGroup
	errs := make(chan error, ids*writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ids; i++ {
				if _, err := db.UpsertMessageListMeta(ctx, &Message{
					AccountID: accountID, FolderID: folder.ID,
					RemoteID: fmt.Sprintf("E%d", i), Subject: "s",
				}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent upsert of the same stub failed: %v", err)
	}
	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != ids {
		t.Fatalf("rows = %d, want %d", len(msgs), ids)
	}
}

// Refreshing a stub's list columns from a server snapshot must not overwrite a
// local flag change that is still waiting to be pushed.
func TestUpsertMessageListMetaKeepsPendingFlags(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E1", Subject: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkFlagsPending(ctx, id, FlagSeen); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E1", Subject: "s2"}); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != FlagSeen {
		t.Fatalf("flags = %v, want the pending local \\Seen kept", got.Flags)
	}
	if got.Subject != "s2" {
		t.Fatalf("subject = %q, want the refreshed list column", got.Subject)
	}

	// Without a pending change the server's flags are adopted as before.
	if ok, err := db.ResolvePendingFlags(ctx, id, FlagSeen, FlagSeen); err != nil || !ok {
		t.Fatalf("resolve pending: ok=%v err=%v", ok, err)
	}
	if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E1", Subject: "s3", Flags: FlagFlagged}); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != FlagFlagged {
		t.Fatalf("flags = %v, want the server flags when nothing is pending", got.Flags)
	}
}

// Adopting server flags is skipped when the row gained a pending local change
// after the sync read its snapshot.
func TestAdoptServerFlagsSkipsPendingRow(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E1", Subject: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkFlagsPending(ctx, id, FlagSeen); err != nil {
		t.Fatal(err)
	}
	applied, err := db.AdoptServerFlags(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("adopt reported applied on a row with a pending change")
	}
	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != FlagSeen {
		t.Fatalf("flags = %v, want the pending \\Seen kept", got.Flags)
	}
	if ok, err := db.ResolvePendingFlags(ctx, id, FlagSeen, FlagSeen); err != nil || !ok {
		t.Fatalf("resolve pending: ok=%v err=%v", ok, err)
	}
	applied, err = db.AdoptServerFlags(ctx, id, FlagFlagged)
	if err != nil || !applied {
		t.Fatalf("adopt on a clean row: applied=%v err=%v", applied, err)
	}
}

// ResolvePendingFlags writes a push's result only over the row the sync read:
// a flag change made since then, or a resolved marker, wins.
func TestResolvePendingFlagsOnlyOverTheSnapshot(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		change    func(db *DB, id int64) error
		wantOK    bool
		wantFlags Flag
		wantPend  bool
	}{
		{"unchanged row", func(*DB, int64) error { return nil }, true, FlagSeen | FlagFlagged, false},
		{"flags changed since", func(db *DB, id int64) error { return db.MarkFlagsPending(ctx, id, 0) }, false, 0, true},
		{"no longer pending", func(db *DB, id int64) error {
			_, err := db.sql.ExecContext(ctx, `UPDATE messages SET pending_flags = 0 WHERE id = ?`, id)
			return err
		}, false, FlagSeen, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
			if err != nil {
				t.Fatal(err)
			}
			folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
			if _, err := db.CreateFolder(ctx, &folder); err != nil {
				t.Fatal(err)
			}
			id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E1", Subject: "s"})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.MarkFlagsPending(ctx, id, FlagSeen); err != nil {
				t.Fatal(err)
			}
			if err := tc.change(db, id); err != nil {
				t.Fatal(err)
			}
			ok, err := db.ResolvePendingFlags(ctx, id, FlagSeen, FlagSeen|FlagFlagged)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.wantOK {
				t.Fatalf("applied = %v, want %v", ok, tc.wantOK)
			}
			states, err := db.ListMessageStates(ctx, folder.ID)
			if err != nil || len(states) != 1 {
				t.Fatalf("states = %+v (%v)", states, err)
			}
			if states[0].Flags != tc.wantFlags || states[0].PendingFlags != tc.wantPend {
				t.Fatalf("row flags=%v pending=%v, want %v and %v", states[0].Flags, states[0].PendingFlags, tc.wantFlags, tc.wantPend)
			}
		})
	}
}

// A body fill (an offline download, or sync fetching a stub's body) carries the
// server's flags. A local change that is not on the server yet must survive
// it, or the next push sends the server's old flags back and the change is
// lost.
func TestBodyFillKeepsPendingFlags(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	accountID, err := db.CreateAccount(ctx, &Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "mb1", RemoteID: "mb1"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E1", Subject: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkFlagsPending(ctx, id, FlagSeen); err != nil {
		t.Fatal(err)
	}
	fill := &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E1", Subject: "s", BodyPlain: "body", Flags: FlagFlagged}
	if _, err := db.InsertMessageWithAttachments(ctx, fill, nil); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != FlagSeen {
		t.Fatalf("flags = %v, want the pending local \\Seen kept over the server's flags", got.Flags)
	}
	if !got.BodyComplete || got.BodyPlain != "body" {
		t.Fatalf("body not filled: complete=%v plain=%q", got.BodyComplete, got.BodyPlain)
	}

	// With nothing pending, the fill adopts the server's flags as before.
	id2, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E2", Subject: "s"})
	if err != nil {
		t.Fatal(err)
	}
	fill2 := &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "E2", Subject: "s", BodyPlain: "body", Flags: FlagFlagged}
	if _, err := db.InsertMessageWithAttachments(ctx, fill2, nil); err != nil {
		t.Fatal(err)
	}
	got2, err := db.GetMessage(ctx, id2)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Flags != FlagFlagged {
		t.Fatalf("flags = %v, want the server's flags when nothing is pending", got2.Flags)
	}
}
