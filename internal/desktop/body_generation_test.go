package desktop

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/peltonapp/Pelton/internal/credentials"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// R9-1 regression tests: a body fetched under one UIDVALIDITY must never
// fill the row a local reset listed for the reused uid while the FETCH was in
// flight. SELECT reports the old generation each time, so only the storage
// target check can catch it.

// generationRaceIMAP answers SELECT with UIDVALIDITY 111 and runs before
// inside FETCH, then returns the old generation's message for uid 7.
type generationRaceIMAP struct {
	fakeIMAP
	before func()
}

func (c *generationRaceIMAP) Select(path string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: path, UIDValidity: 111}, nil
}

func (c *generationRaceIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.before()
	return fn(7, &pimap.Message{UID: 7, Subject: "old generation", Text: "OLD-GENERATION BODY"}, nil)
}

// generationApp is an account with an INBOX at UIDVALIDITY 111 holding a
// stub for uid 7.
func generationApp(t *testing.T) (*App, *storage.DB, context.Context, int64, storage.Folder, int64) {
	t.Helper()
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX", UIDValidity: 111}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFolderUIDValidity(ctx, folder.ID, 111); err != nil {
		t.Fatal(err)
	}
	oldID, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: accountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "old stub"})
	if err != nil {
		t.Fatal(err)
	}
	return a, db, ctx, accountID, folder, oldID
}

// replaceGeneration is a listing that saw UIDVALIDITY 222: the old stub
// goes and uid 7 is listed again as another message.
func replaceGeneration(ctx context.Context, db *storage.DB, folder storage.Folder, oldID int64, gen uint32, uid int) (int64, error) {
	if err := db.DeleteMessage(ctx, oldID); err != nil {
		return 0, err
	}
	if err := db.SetFolderUIDValidity(ctx, folder.ID, gen); err != nil {
		return 0, err
	}
	return db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: uint32(uid), RemoteID: strconv.Itoa(uid), Subject: "NEW GENERATION STUB"})
}

func assertStillStub(t *testing.T, db *storage.DB, ctx context.Context, id int64) {
	t.Helper()
	m, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyComplete || m.Subject != "NEW GENERATION STUB" {
		t.Fatalf("old-generation body filled replacement row %d: subject=%q body=%d bytes", id, m.Subject, len(m.BodyPlain))
	}
}

// The engine on the app's real IMAP adapter: a reset during FETCH drops
// the body and is no error.
func TestBodyFillAfterLocalGenerationResetIsDropped(t *testing.T) {
	a, db, ctx, accountID, folder, oldID := generationApp(t)
	var newID int64
	c := &generationRaceIMAP{before: func() {
		var err error
		if newID, err = replaceGeneration(ctx, db, folder, oldID, 222, 7); err != nil {
			t.Error(err)
		}
	}}
	engine := a.newSyncEngine(pimap.NewAdapter(c), accountID)
	engine.OnStored, engine.OnProgress = nil, nil
	if _, err := engine.FetchBodies(context.Background(), folder, []string{"7"}); err != nil {
		t.Fatalf("a dropped body failed the fetch: %v", err)
	}
	assertStillStub(t, db, ctx, newID)
}

// The actual open path: GetMessage on the old stub starts the pane's body
// fetch, which must not fill the replacement.
func TestOpenAfterLocalGenerationResetDoesNotFillReplacement(t *testing.T) {
	a, db, ctx, accountID, folder, oldID := generationApp(t)
	if err := credentials.Store(accountID, credentials.Secret{Method: credentials.MethodPassword, Password: "mock"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentials.Delete(accountID) })
	replaced := make(chan int64, 1)
	c := &generationRaceIMAP{before: func() {
		newID, err := replaceGeneration(ctx, db, folder, oldID, 222, 7)
		if err != nil {
			t.Error(err)
			return
		}
		replaced <- newID
	}}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return c, nil }
	a.bodyRetryDelays = fastBodyRetries
	if _, err := a.GetMessage(oldID); err != nil {
		t.Fatal(err)
	}
	var newID int64
	select {
	case newID = <-replaced:
	case <-time.After(5 * time.Second):
		t.Fatal("the open never fetched")
	}
	waitBodyFetchDone(t, a, oldID)
	assertStillStub(t, db, ctx, newID)
}

// fastBodyRetries are as many pane retries as production makes, without
// its 1 s, 3 s and 8 s pauses: the dropped body is retried once and the
// retry finds the old row gone.
var fastBodyRetries = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

// waitBodyFetchDone waits for the pane's background body fetch of id.
func waitBodyFetchDone(t *testing.T, a *App, id int64) {
	t.Helper()
	waitUntil(t, 5*time.Second, "the pane's body fetch never ended", func() bool {
		_, running := a.bodyFetches.Load(id)
		return !running
	})
}

// The chunked open path: a large message whose folder is reset between its
// chunks does not fill the replacement with the old parts.
func TestLargeOpenAfterLocalGenerationResetDoesNotFillReplacement(t *testing.T) {
	const size = 12 << 20
	c := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(size)}}
	a, _, folder, _ := pagingInbox(t, c)
	// pagingIMAP's SELECT reports UIDVALIDITY 1.
	if err := a.store.SetFolderUIDValidity(a.ctx, folder.ID, 1); err != nil {
		t.Fatal(err)
	}
	folder.UIDValidity = 1
	seedStub(t, a, folder, 1, size)
	oldID, err := a.store.MessageIDByRemoteID(a.ctx, folder.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	replaced := make(chan int64, 1)
	var once sync.Once
	c.onPartial = func(off int64) {
		if off == 0 {
			return
		}
		once.Do(func() {
			newID, err := replaceGeneration(a.ctx, a.store, folder, oldID, 2, 1)
			if err != nil {
				t.Error(err)
			}
			replaced <- newID
		})
	}
	a.bodyRetryDelays = fastBodyRetries
	if _, err := a.GetMessage(oldID); err != nil {
		t.Fatal(err)
	}
	var newID int64
	select {
	case newID = <-replaced:
	case <-time.After(5 * time.Second):
		t.Fatalf("no second chunk: %v", c.opsSnapshot())
	}
	waitBodyFetchDone(t, a, oldID)
	assertStillStub(t, a.store, a.ctx, newID)
}

// An explicit download of a message that had no row stores nothing, and
// reports no row, when the folder was reset and the uid listed again while
// it was fetched.
func TestOfflineStoreAfterLocalGenerationResetIsDropped(t *testing.T) {
	a, db, ctx, _, folder, oldID := generationApp(t)
	newID, err := replaceGeneration(ctx, db, folder, oldID, 222, 7)
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.storeOffline(folder, psync.Fetched{RemoteID: "7", LegacyUID: 7, Subject: "old generation", Text: "OLD-GENERATION BODY"}, true)
	if err == nil || id != 0 {
		t.Fatalf("storeOffline = %d, %v; want the body dropped with an error", id, err)
	}
	assertStillStub(t, db, ctx, newID)
}
