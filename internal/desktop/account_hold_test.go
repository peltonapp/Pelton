package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// While an account is held, UI paths get a quiet no-op: opening a message
// shows the stub, and Sync does not report a failure.
func TestHeldAccountUIPathsAreQuiet(t *testing.T) {
	a := newHoldTestApp(t)
	id, folderID := seedHoldAccount(t, a, "quiet@example.test")
	var fetched atomic.Int32
	a.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error {
		fetched.Add(1)
		return nil
	}
	var dialed atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		dialed.Add(1)
		return &fakeIMAP{}, nil
	}
	stub := &storage.Message{AccountID: id, FolderID: folderID, RemoteID: "stub-1", Subject: "stub"}
	if _, err := a.store.UpsertMessageListMeta(a.ctx, stub); err != nil {
		t.Fatal(err)
	}

	release := a.holdAccountSync(id)
	if err := a.fetchMessageBodyOnDemand(stub); err != nil {
		t.Errorf("on-demand body while held = %v, want nil (stub shown)", err)
	}
	if err := a.SyncAccountNow(id); err != nil {
		t.Errorf("SyncAccountNow while held = %v, want nil", err)
	}
	if a.accountSync(id) != nil {
		t.Error("a held account got a scheduler")
	}
	if fetched.Load() != 0 || dialed.Load() != 0 {
		t.Errorf("held account ran sync work: %d fetches, %d connections", fetched.Load(), dialed.Load())
	}
	release()
	if err := a.fetchMessageBodyOnDemand(stub); err != nil {
		t.Fatalf("on-demand body after release: %v", err)
	}
	if fetched.Load() != 1 {
		t.Errorf("fetches after release = %d, want 1", fetched.Load())
	}
}

// Removing a mailbox is a hard cancel of that account only: its worker,
// sessions and scheduler stop, its state goes, and nothing brings them back.
// The other account keeps running.
func TestDeleteAccountHardCancelsOnlyThatAccount(t *testing.T) {
	a := newHoldTestApp(t)
	idA, folderA := seedHoldAccount(t, a, "gone@example.test")
	idB, _ := seedHoldAccount(t, a, "stays@example.test")
	accA, err := a.store.GetAccount(a.ctx, idA)
	if err != nil {
		t.Fatal(err)
	}
	accB, err := a.store.GetAccount(a.ctx, idB)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	clients := map[string]*closeRecordingIMAP{}
	a.newIMAPClient = func(cfg pimap.Config) (mailClient, error) {
		c := &closeRecordingIMAP{}
		mu.Lock()
		clients[cfg.Username] = c
		mu.Unlock()
		return c, nil
	}
	clientFor := func(user string) *closeRecordingIMAP {
		mu.Lock()
		defer mu.Unlock()
		return clients[user]
	}

	release := make(chan struct{})
	var sessions sync.WaitGroup
	opened := make(chan struct{}, 2)
	for _, acc := range []*storage.Account{accA, accB} {
		acc := *acc
		sessions.Go(func() {
			_ = a.withIMAPSession(a.ctx, acc, func(mailClient) error {
				opened <- struct{}{}
				<-release
				return nil
			})
		})
	}
	defer func() { close(release); sessions.Wait() }()
	for range 2 {
		select {
		case <-opened:
		case <-time.After(2 * time.Second):
			t.Fatal("sessions never opened")
		}
	}

	type parked struct{ running, cancelled, release chan struct{} }
	park := func(id int64) parked {
		p := parked{make(chan struct{}), make(chan struct{}), make(chan struct{})}
		tryScheduler(a, id).Enqueue(syncsched.Job{
			Priority: syncsched.PriorityBackgroundBody,
			Kind:     syncsched.JobFetchBodies,
			Run: func(ctx context.Context) error {
				close(p.running)
				select {
				case <-ctx.Done():
					close(p.cancelled)
					return ctx.Err()
				case <-p.release:
					return nil
				}
			},
		})
		select {
		case <-p.running:
		case <-time.After(2 * time.Second):
			t.Fatalf("account %d job never started", id)
		}
		return p
	}
	jobA := park(idA)
	jobB := park(idB)
	defer close(jobA.release)
	defer close(jobB.release)
	plantQuiescentWorker(a, idA)

	if err := a.DeleteAccount(idA); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	if c := clientFor(accA.Email); c == nil || !c.closed.Load() {
		t.Error("removed account's session was not closed")
	}
	select {
	case <-jobA.cancelled:
	case <-time.After(2 * time.Second):
		t.Error("removed account's job was not cancelled")
	}
	if a.accountSync(idA) != nil {
		t.Error("removed account's scheduler survived")
	}
	if workerFor(a, idA) != nil {
		t.Error("removed account's worker survived")
	}
	a.accountStatesMu.Lock()
	_, hasState := a.accountStates[idA]
	a.accountStatesMu.Unlock()
	if hasState {
		t.Error("removed account's state entry was kept")
	}

	if c := clientFor(accB.Email); c == nil || c.closed.Load() {
		t.Error("other account's session was closed")
	}
	select {
	case <-jobB.cancelled:
		t.Error("other account's job was cancelled")
	case <-time.After(100 * time.Millisecond):
	}
	if a.accountSync(idB) == nil {
		t.Error("other account's scheduler was removed")
	}

	// Nothing brings the removed account's runtime back.
	if err := trySync(a, idA); err == nil {
		t.Error("ensureAccountSync recreated a removed account's runtime")
	}
	stale := &storage.Message{AccountID: idA, FolderID: folderA, RemoteID: "x"}
	_ = a.fetchMessageBodyOnDemand(stale)
	if a.accountSync(idA) != nil {
		t.Error("a scheduler came back for the removed account")
	}
	a.accountStatesMu.Lock()
	_, hasState = a.accountStates[idA]
	a.accountStatesMu.Unlock()
	if hasState {
		t.Error("a state entry came back for the removed account")
	}
}

// tryScheduler is the account's scheduler, or nil while it is held.
func tryScheduler(a *App, accountID int64) *syncsched.Scheduler {
	s, err := a.ensureAccountScheduler(a.ctx, accountID)
	if err != nil {
		return nil
	}
	return s
}

// trySync reports whether ensureAccountSync would start or return the
// account's runtime.
func trySync(a *App, accountID int64) error {
	_, err := a.ensureAccountSync(a.ctx, accountID)
	return err
}

// newHoldTestApp is an App with a migrated store and a profile session, for
// tests that start, hold and stop account workers.
func newHoldTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	session, cancel := context.WithCancel(ctx)
	app := &App{
		ctx:         ctx,
		session:     session,
		sessionStop: cancel,
		store:       store,
		log:         slog.New(slog.DiscardHandler),
		workers:     make(map[int64]*accountWorker),
	}
	// Cancel the session and join workers before other cleanups (credentials,
	// store) so an idle loop cannot race the mock keyring on teardown.
	t.Cleanup(func() {
		cancel()
		app.joinAllAccountWorkers()
	})
	return app
}

// seedHoldAccount creates an account with a stored password, an INBOX holding
// one message and an address book.
func seedHoldAccount(t *testing.T, a *App, email string) (accountID, folderID int64) {
	t.Helper()
	ctx := a.ctx
	acc := &storage.Account{Email: email, IMAPHost: "imap.example.test", IMAPPort: 993}
	id, err := a.store.CreateAccount(ctx, acc)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := credentials.Store(id, credentials.Secret{
		Method: credentials.MethodPassword, Password: "secret",
	}); err != nil {
		t.Fatalf("store secret: %v", err)
	}
	// Stop the worker before deleting the secret: cleanups run LIFO, so this
	// runs before newHoldTestApp's join, and a test may have left a worker
	// still reading the keyring.
	t.Cleanup(func() {
		a.stopAccountWorker(id)
		_ = credentials.Delete(id)
	})

	folder := &storage.Folder{
		AccountID: id, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX",
	}
	fid, err := a.store.CreateFolder(ctx, folder)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if _, err := a.store.InsertMessage(ctx, &storage.Message{
		AccountID: id, FolderID: fid, UID: 1, Subject: "hello",
	}); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if _, err := a.store.CreateAddressBook(ctx, &storage.AddressBook{
		AccountID: id, Name: "Personal", URL: "https://dav.example",
		CollectionPath: "/books/personal/", Username: email,
	}); err != nil {
		t.Fatalf("create address book: %v", err)
	}
	return id, fid
}

// plantQuiescentWorker registers a worker for the account that has already
// exited, so stopAccountWorker finds one to stop without waiting.
func plantQuiescentWorker(a *App, accountID int64) *accountWorker {
	done := make(chan struct{})
	close(done)
	_, cancel := context.WithCancel(context.Background())
	w := &accountWorker{cancel: cancel, done: done}
	a.workersMu.Lock()
	a.workers[accountID] = w
	a.workersMu.Unlock()
	return w
}

// workerFor is the account's registered worker, or nil.
func workerFor(a *App, accountID int64) *accountWorker {
	a.workersMu.Lock()
	defer a.workersMu.Unlock()
	return a.workers[accountID]
}
