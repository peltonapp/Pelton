package sync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// genTestFolder is a sync test folder that recorded UIDVALIDITY 111, with a
// stub for uid 7 listed under it.
func genTestFolder(t *testing.T) (*storage.DB, storage.Folder, int64) {
	t.Helper()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	if err := db.SetFolderUIDValidity(ctx, folder.ID, 111); err != nil {
		t.Fatal(err)
	}
	folder.UIDValidity = 111
	id, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "old stub"})
	if err != nil {
		t.Fatal(err)
	}
	return db, folder, id
}

// localReset is a listing that saw UIDVALIDITY 222 while a body of
// generation 111 was in flight: the old rows go and uid 7 is listed again as
// a different message. It returns the replacement row.
func localReset(t *testing.T, db *storage.DB, folder storage.Folder, oldID int64) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := db.PurgeFolderMessages(ctx, folder.AccountID, folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFolderUIDValidity(ctx, folder.ID, 222); err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "NEW GENERATION STUB"})
	if err != nil {
		t.Fatal(err)
	}
	if id == oldID {
		t.Fatal("fixture: replacement reused the old row")
	}
	return id
}

func requireStub(t *testing.T, db *storage.DB, id int64) {
	t.Helper()
	m, err := db.GetMessage(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyComplete || m.Subject != "NEW GENERATION STUB" {
		t.Fatalf("old-generation body filled replacement row %d: subject=%q body=%q", id, m.Subject, m.BodyPlain)
	}
}

// R9-1: FetchBodies started under 111 drops the body when the folder is
// reset to 222 while the FETCH is in flight. The replacement stub stays a
// stub for the next body sync, and the drop is no batch failure.
func TestFetchBodiesDropsBodyOfReplacedGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, oldID := genTestFolder(t)
	var newID int64
	adapter := &fakeAdapter{onFetch: func(string) { newID = localReset(t, db, folder, oldID) }}
	res, err := NewEngine(adapter, db, nil).FetchBodies(ctx, folder, []string{"7"})
	if err != nil {
		t.Fatalf("a dropped body failed the batch: %v", err)
	}
	if res.New != 0 {
		t.Fatalf("new = %d %v, want none", res.New, res.NewIDs)
	}
	requireStub(t, db, newID)
}

// The chunked path: the last chunk of a message from 111 lands after the
// reset and is dropped, with no error.
func TestFetchBodyChunkDropsBodyOfReplacedGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, oldID := genTestFolder(t)
	src := bytes.Repeat([]byte("abcdefgh"), 512)
	a := &chunkAdapter{fakeAdapter: &fakeAdapter{}, src: map[string][]byte{"7": src}}
	engine := NewEngine(a, db, nil)
	p := NewPartialBody("7")
	if done, _, err := engine.FetchBodyChunk(ctx, folder, p, 4096); err != nil || done {
		t.Fatalf("first chunk: done=%v err=%v", done, err)
	}
	newID := localReset(t, db, folder, oldID)
	done, res, err := engine.FetchBodyChunk(ctx, folder, p, 4096)
	if err != nil || !done || res.New != 0 {
		t.Fatalf("last chunk: done=%v new=%d err=%v, want dropped", done, res.New, err)
	}
	requireStub(t, db, newID)
}

// The chunked path also pins the row the first chunk was for: a row
// re-listed under the same generation does not take the parts.
func TestFetchBodyChunkDropsBodyOfReplacedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, oldID := genTestFolder(t)
	src := bytes.Repeat([]byte("abcdefgh"), 512)
	engine := NewEngine(&chunkAdapter{fakeAdapter: &fakeAdapter{}, src: map[string][]byte{"7": src}}, db, nil)
	p := NewPartialBody("7")
	if _, _, err := engine.FetchBodyChunk(ctx, folder, p, 4096); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMessage(ctx, oldID); err != nil {
		t.Fatal(err)
	}
	newID, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "NEW GENERATION STUB"})
	if err != nil {
		t.Fatal(err)
	}
	if done, _, err := engine.FetchBodyChunk(ctx, folder, p, 4096); err != nil || !done {
		t.Fatalf("last chunk: done=%v err=%v", done, err)
	}
	requireStub(t, db, newID)
}

// StoreFetched (an explicit download) reports the drop as
// ErrBodyTargetChanged, so the caller does not take the replacement row for
// the stored message.
func TestStoreFetchedRefusesReplacedGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, oldID := genTestFolder(t)
	newID := localReset(t, db, folder, oldID)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	id, err := StoreFetched(ctx, db, log, folder, Fetched{RemoteID: "7", LegacyUID: 7, Subject: "old generation", Text: "OLD-GENERATION BODY"}, true)
	if !errors.Is(err, storage.ErrBodyTargetChanged) || id != 0 {
		t.Fatalf("StoreFetched = %d, %v; want ErrBodyTargetChanged", id, err)
	}
	requireStub(t, db, newID)
}
