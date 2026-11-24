package storage

import (
	"context"
	"errors"
	"testing"
)

// genFolder is a body-test folder that recorded UIDVALIDITY 111, with a stub
// for uid 7 listed under it.
func genFolder(t *testing.T) (*DB, Folder, int64) {
	t.Helper()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	if err := db.SetFolderUIDValidity(ctx, folder.ID, 111); err != nil {
		t.Fatal(err)
	}
	folder.UIDValidity = 111
	id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "old stub"})
	if err != nil {
		t.Fatal(err)
	}
	return db, folder, id
}

// resetGeneration does what a listing does when the folder's UIDVALIDITY
// moves to 222: the old rows go, and uid 7 is listed again as a different
// message. It returns the replacement row.
func resetGeneration(t *testing.T, db *DB, folder Folder, oldID int64) int64 {
	t.Helper()
	ctx := context.Background()
	if err := db.DeleteMessage(ctx, oldID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFolderUIDValidity(ctx, folder.ID, 222); err != nil {
		t.Fatal(err)
	}
	newID, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "NEW GENERATION STUB"})
	if err != nil {
		t.Fatal(err)
	}
	return newID
}

func oldBody(folder Folder) *Message {
	return &Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "old generation", BodyPlain: "OLD-GENERATION BODY"}
}

// R9-1: a body fetched under UIDVALIDITY 111 must not fill uid 7's row once
// the folder was reset to 222 and uid 7 names another message. Fill and
// insert both refuse with ErrBodyTargetChanged and write nothing.
func TestBodyFillRefusesReplacedGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		write func(db *DB, want BodyTarget, m *Message) (int64, error)
	}{
		{"fill", func(db *DB, want BodyTarget, m *Message) (int64, error) { return db.FillMessageBody(ctx, want, m, nil) }},
		{"insert", func(db *DB, want BodyTarget, m *Message) (int64, error) {
			return db.InsertFetchedMessage(ctx, want, m, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, folder, oldID := genFolder(t)
			newID := resetGeneration(t, db, folder, oldID)
			_, err := tc.write(db, BodyTarget{UIDValidity: 111}, oldBody(folder))
			if !errors.Is(err, ErrBodyTargetChanged) {
				t.Fatalf("err = %v, want ErrBodyTargetChanged", err)
			}
			if m := mustMessage(t, db, newID); m.BodyComplete || m.BodyPlain != "" || m.Subject != "NEW GENERATION STUB" {
				t.Fatalf("replacement row written: %+v", m)
			}
		})
	}
}

// An insert of a message with no row at all under a replaced generation
// must not create one either.
func TestBodyInsertRefusesReplacedGenerationWithoutRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, oldID := genFolder(t)
	if err := db.DeleteMessage(ctx, oldID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFolderUIDValidity(ctx, folder.ID, 222); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertFetchedMessage(ctx, BodyTarget{UIDValidity: 111}, oldBody(folder), nil); !errors.Is(err, ErrBodyTargetChanged) {
		t.Fatalf("err = %v, want ErrBodyTargetChanged", err)
	}
	if _, err := db.MessageIDByRemoteID(ctx, folder.ID, "7"); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("old-generation message inserted: %v", err)
	}
}

// A body pinned to a row does not fill a different row that now holds its
// remote id, even in the same generation.
func TestBodyFillRefusesReplacedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, oldID := genFolder(t)
	if err := db.DeleteMessage(ctx, oldID); err != nil {
		t.Fatal(err)
	}
	newID, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "relisted"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FillMessageBody(ctx, BodyTarget{UIDValidity: 111, MessageID: oldID}, oldBody(folder), nil); !errors.Is(err, ErrBodyTargetChanged) {
		t.Fatalf("err = %v, want ErrBodyTargetChanged", err)
	}
	if m := mustMessage(t, db, newID); m.BodyComplete {
		t.Fatalf("replacement row filled: %+v", m)
	}
}

// The target is checked in the transaction that writes, not by a read
// before it: a reset that lands after the row was looked up, but before the
// write, still refuses the body.
func TestBodyFillChecksTargetInWriteTransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		target func(oldID int64) BodyTarget
		change func(t *testing.T, db *DB, folder Folder, oldID int64)
	}{
		{"generation", func(int64) BodyTarget { return BodyTarget{UIDValidity: 111} },
			func(t *testing.T, db *DB, folder Folder, _ int64) {
				if err := db.SetFolderUIDValidity(ctx, folder.ID, 222); err != nil {
					t.Fatal(err)
				}
			}},
		{"row", func(oldID int64) BodyTarget { return BodyTarget{MessageID: oldID} },
			func(t *testing.T, db *DB, folder Folder, oldID int64) {
				if err := db.DeleteMessage(ctx, oldID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "relisted"}); err != nil {
					t.Fatal(err)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, folder, oldID := genFolder(t)
			db.TestingSetBodyFillLookupHook(func() { tc.change(t, db, folder, oldID) })
			_, err := db.FillMessageBody(ctx, tc.target(oldID), oldBody(folder), nil)
			db.TestingSetBodyFillLookupHook(nil)
			if !errors.Is(err, ErrBodyTargetChanged) && !errors.Is(err, ErrMessageNotFound) {
				t.Fatalf("err = %v, want the write refused", err)
			}
			id, err := db.MessageIDByRemoteID(ctx, folder.ID, "7")
			if err != nil {
				t.Fatal(err)
			}
			if m := mustMessage(t, db, id); m.BodyComplete {
				t.Fatalf("row %d filled after its target changed: %+v", id, m)
			}
		})
	}
}

// The unchanged target still fills: same generation, same row, and an
// unchecked target (zero) as before.
func TestBodyFillMatchingTargetFills(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, want := range []func(int64) BodyTarget{
		func(id int64) BodyTarget { return BodyTarget{UIDValidity: 111, MessageID: id} },
		func(int64) BodyTarget { return BodyTarget{} },
	} {
		db, folder, id := genFolder(t)
		got, err := db.FillMessageBody(ctx, want(id), oldBody(folder), nil)
		if err != nil || got != id {
			t.Fatalf("fill = %d, %v; want %d", got, err, id)
		}
		if m := mustMessage(t, db, id); !m.BodyComplete {
			t.Fatalf("not filled: %+v", m)
		}
	}
}

func TestMessageIDsByRemoteID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, id := genFolder(t)
	got, err := db.MessageIDsByRemoteID(ctx, folder.ID, []string{"7", "8"})
	if err != nil || len(got) != 1 || got["7"] != id {
		t.Fatalf("ids = %v, %v; want 7:%d", got, err, id)
	}
}
