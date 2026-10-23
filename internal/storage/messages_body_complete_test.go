package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

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

func mustMessage(t *testing.T, db *DB, id int64) *Message {
	t.Helper()
	m, err := db.GetMessage(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A list upsert ignores the caller's BodyComplete and leaves the body to
// fetch; a full upsert marks it complete.
func TestUpsertBodyComplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		upsert func(*DB, context.Context, *Message) (int64, error)
		in     bool
		want   bool
	}{
		{"ListMetaLeavesBodyIncomplete", (*DB).UpsertMessageListMeta, true, false},
		{"FullUpsertSetsBodyComplete", (*DB).InsertMessage, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, accountID, folder := newBodyTestFolder(t)
			id, err := tc.upsert(db, ctx, &Message{
				AccountID: accountID, FolderID: folder.ID, RemoteID: "1",
				Subject: "Hi", BodyPlain: "preview", BodyComplete: tc.in,
			})
			if err != nil {
				t.Fatal(err)
			}
			if m := mustMessage(t, db, id); m.BodyComplete != tc.want {
				t.Fatalf("body_complete = %v, want %v", m.BodyComplete, tc.want)
			}
			needs, err := db.MessageNeedsBody(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if needs != !tc.want {
				t.Fatalf("MessageNeedsBody = %v, want %v", needs, !tc.want)
			}
		})
	}
}

func TestListMetaRefreshKeepsBodyIncomplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
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
	m := mustMessage(t, db, id)
	if m.BodyComplete {
		t.Fatal("list meta refresh must not mark the body complete")
	}
	if m.Subject != "Hi again" {
		t.Fatalf("subject %q", m.Subject)
	}
}

func TestMessageNeedsBodyMissing(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	_, err := db.MessageNeedsBody(context.Background(), 404)
	if !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("err %v, want ErrMessageNotFound", err)
	}
}

func TestInsertLeavesCompleteMessageUntouched(t *testing.T) {
	t.Parallel()
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

	got := mustMessage(t, db, id)
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

// A body fill of an incomplete row that already has attachment files replaces
// them; a failed fill rolls the row and the files back.
func TestBodyFillAttachmentFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		fail bool
	}{
		{"RollbackRestoresAttachmentFiles", true},
		{"ReplacesPriorAttachmentFiles", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
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

			if tc.fail {
				db.deleteTxHook = func(context.Context) error { return errors.New("inject fail") }
			}
			_, err = db.InsertMessageWithAttachments(ctx, &Message{
				AccountID: accountID, FolderID: folder.ID, RemoteID: "partial",
				Subject: "Stub", BodyPlain: "full", Flags: FlagFlagged,
			}, []IncomingAttachment{{
				Filename: "note.txt", ContentType: "text/plain", Content: bytes.NewReader([]byte("new-bytes")),
			}})
			if tc.fail != (err != nil) {
				t.Fatalf("fill err %v, want failure %v", err, tc.fail)
			}

			wantBytes, wantComplete, wantPlain, wantFlags := "new-bytes", true, "full", FlagFlagged
			if tc.fail {
				wantBytes, wantComplete, wantPlain, wantFlags = "original-bytes", false, "original body", FlagSeen
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("attachment missing: %v", err)
			}
			if string(body) != wantBytes {
				t.Fatalf("attachment %q, want %s", body, wantBytes)
			}
			got := mustMessage(t, db, id)
			if got.BodyComplete != wantComplete || got.BodyPlain != wantPlain || got.Flags != wantFlags {
				t.Fatalf("row: %+v", got)
			}
			atts, err := db.ListAttachments(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if len(atts) != 1 || atts[0].Filename != "note.txt" {
				t.Fatalf("attachments: %+v", atts)
			}
		})
	}
}

func TestRemoteIDsNeedingBodyKeepsIncomplete(t *testing.T) {
	t.Parallel()
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
	if want := []string{"stub", "missing"}; !slices.Equal(got, want) {
		t.Fatalf("need %v, want %v", got, want)
	}
}

func TestRemoteIDsNeedingBodyNewest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"nouid", "old", "mid", "new", "deleted", "newest"} {
		mid, err := db.UpsertMessageListMeta(ctx, &Message{
			AccountID: accountID, FolderID: folder.ID, RemoteID: id, UID: uint32(i),
			Date: base.AddDate(0, 0, i),
		})
		if err != nil {
			t.Fatal(err)
		}
		if id == "deleted" {
			if err := db.MarkDeletePending(ctx, mid); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "done", UID: 10,
		Subject: "Kept", BodyPlain: "body", Date: base.AddDate(0, 0, 10),
	}, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		floorUID uint32
		limit    int
		want     []string
	}{
		{"all", 0, 0, []string{"newest", "new", "mid", "old", "nouid"}},
		{"limit counts incomplete rows", 0, 2, []string{"newest", "new"}},
		// below the floor only the row without a uid is left.
		{"floor", 3, 0, []string{"newest", "new", "nouid"}},
		{"floor and limit", 2, 3, []string{"newest", "new", "mid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.RemoteIDsNeedingBodyNewest(ctx, folder.ID, tc.floorUID, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMessageIDByRemoteID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, inbox := newBodyTestFolder(t)
	other := Folder{AccountID: accountID, Name: "Other", IMAPPath: "mb2", RemoteID: "mb2"}
	if _, err := db.CreateFolder(ctx, &other); err != nil {
		t.Fatal(err)
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

func TestMessageSizesByRemoteID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, inbox := newBodyTestFolder(t)
	other := Folder{AccountID: accountID, Name: "Other", IMAPPath: "mb2", RemoteID: "mb2"}
	if _, err := db.CreateFolder(ctx, &other); err != nil {
		t.Fatal(err)
	}
	// more ids than one IN chunk, so the chunking is covered.
	var ids []string
	want := map[string]int64{}
	for i := range 100 {
		id := fmt.Sprintf("E%d", i)
		ids = append(ids, id)
		want[id] = int64(i) * 1000
		if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: inbox.ID, RemoteID: id, SizeBytes: int64(i) * 1000}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: other.ID, RemoteID: "X", SizeBytes: 7}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		ids  []string
		want map[string]int64
	}{
		{"none", nil, map[string]int64{}},
		{"all", ids, want},
		{"missing and other folder left out", []string{"E5", "missing", "X"}, map[string]int64{"E5": 5000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.MessageSizesByRemoteID(ctx, inbox.ID, tc.ids)
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// The list stub carries the sender's name and date from the envelope. Filling
// its body from a header they could not be read from must not erase them,
// while a name the fill does carry replaces it.
func TestBodyFillKeepsStubFromNameWhenFillHasNone(t *testing.T) {
	t.Parallel()
	stubDate := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, tc := range []struct {
		name, fillName, want string
	}{
		{"fill without name", "", "Jane Doe"},
		{"fill with name", "Jane Q. Doe", "Jane Q. Doe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, accountID, folder := newBodyTestFolder(t)
			id, err := db.UpsertMessageListMeta(ctx, &Message{
				AccountID: accountID, FolderID: folder.ID, RemoteID: "1",
				FromAddress: "jane@x.org", FromName: "Jane Doe", Date: stubDate,
			})
			if err != nil {
				t.Fatal(err)
			}
			// the fill carries no Date header.
			if _, err := db.InsertMessageWithAttachments(ctx, &Message{
				AccountID: accountID, FolderID: folder.ID, RemoteID: "1",
				FromAddress: "jane@x.org", FromName: tc.fillName, BodyPlain: "body",
			}, nil); err != nil {
				t.Fatal(err)
			}
			m := mustMessage(t, db, id)
			if m.FromName != tc.want {
				t.Fatalf("FromName %q, want %q", m.FromName, tc.want)
			}
			if !m.Date.Equal(stubDate) {
				t.Fatalf("date %v, want stub date %v kept", m.Date, stubDate)
			}
		})
	}
}

// A body fill writes only into an incomplete stub. A stub deleted while its
// body was in flight (moved, archived) must not come back as a new row.
func TestFillMessageBody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		seed    func(*testing.T, *DB, int64, Folder)
		wantErr error
		want    string // body_plain of "1" afterwards, "" for no row
	}{
		{"fills a stub", func(t *testing.T, db *DB, accountID int64, folder Folder) {
			if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyPlain: "preview"}); err != nil {
				t.Fatal(err)
			}
		}, nil, "fetched"},
		{"no row", func(*testing.T, *DB, int64, Folder) {}, ErrMessageNotFound, ""},
		{"stub deleted", func(t *testing.T, db *DB, accountID int64, folder Folder) {
			id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyPlain: "preview"})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.DeleteCachedMessage(ctx, accountID, id); err != nil {
				t.Fatal(err)
			}
		}, ErrMessageNotFound, ""},
		{"complete row", func(t *testing.T, db *DB, accountID int64, folder Folder) {
			if _, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyPlain: "kept"}); err != nil {
				t.Fatal(err)
			}
		}, ErrMessageBodyComplete, "kept"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, accountID, folder := newBodyTestFolder(t)
			tc.seed(t, db, accountID, folder)
			_, err := db.FillMessageBody(ctx, BodyTarget{}, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyPlain: "fetched"}, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err %v, want %v", err, tc.wantErr)
			}
			id, err := db.MessageIDByRemoteID(ctx, folder.ID, "1")
			if tc.want == "" {
				if !errors.Is(err, ErrMessageNotFound) {
					t.Fatalf("row %d exists after the fill, err %v", id, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m := mustMessage(t, db, id); m.BodyPlain != tc.want || !m.BodyComplete {
				t.Fatalf("body %q complete %v, want %q complete", m.BodyPlain, m.BodyComplete, tc.want)
			}
		})
	}
}

// replaceMessageBody checks the row inside its transaction: a row filled or
// deleted after the caller's lookup is left alone.
func TestReplaceMessageBodyGuardsRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		change  func(*testing.T, *DB, int64, int64)
		wantErr error
		want    string
	}{
		{"filled meanwhile", func(t *testing.T, db *DB, _ int64, id int64) {
			if _, err := db.FillMessageBody(ctx, BodyTarget{}, &Message{FolderID: mustMessage(t, db, id).FolderID, RemoteID: "1", BodyPlain: "first"}, nil); err != nil {
				t.Fatal(err)
			}
		}, ErrMessageBodyComplete, "first"},
		{"deleted meanwhile", func(t *testing.T, db *DB, accountID, id int64) {
			if err := db.DeleteCachedMessage(ctx, accountID, id); err != nil {
				t.Fatal(err)
			}
		}, ErrMessageNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, accountID, folder := newBodyTestFolder(t)
			id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyPlain: "preview"})
			if err != nil {
				t.Fatal(err)
			}
			tc.change(t, db, accountID, id)
			_, err = db.replaceMessageBody(ctx, BodyTarget{}, id, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "1", BodyPlain: "second"}, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err %v, want %v", err, tc.wantErr)
			}
			m, err := db.GetMessage(ctx, id)
			if tc.want == "" {
				if !errors.Is(err, ErrMessageNotFound) {
					t.Fatalf("deleted row came back: %+v, err %v", m, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.BodyPlain != tc.want {
				t.Fatalf("body %q, want %q", m.BodyPlain, tc.want)
			}
		})
	}
}
