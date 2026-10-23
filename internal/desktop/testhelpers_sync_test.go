package desktop

// Shared fixtures and waits for the per-account sync runtime tests: the hold
// test app, its seeded accounts, scheduler jobs that signal, progress
// recording and select-with-timeout helpers.

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
	psync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// recv waits up to d for a value on ch and returns it. It fails the test with
// what when nothing arrives in time.
func recv[T any](t testing.TB, ch <-chan T, d time.Duration, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("timed out after %v: %s", d, what)
		var zero T
		return zero
	}
}

// noRecv fails the test with what when ch delivers (or is closed) within d.
func noRecv[T any](t testing.TB, ch <-chan T, d time.Duration, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(what)
	case <-time.After(d):
	}
}

// waitUntil polls cond every 5 ms until it holds, failing the test with what
// when it still does not after d.
func waitUntil(t testing.TB, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v: %s", d, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// signalJob enqueues a task of kind on folder that closes the returned
// channel when it starts and then waits for hold to close (a nil hold
// returns at once).
func signalJob(s *syncsched.Scheduler[mailClient], kind syncsched.Kind, folderID int64, hold <-chan struct{}) <-chan struct{} {
	started := make(chan struct{})
	s.Enqueue(imapTask{Kind: kind, FolderID: folderID, Run: func(context.Context, mailClient) error {
		close(started)
		if hold != nil {
			<-hold
		}
		return nil
	}})
	return started
}

// useFakeIMAP makes the app's sessions dial a fakeIMAP, unless the test
// already set a dialer. Every task needs a session now.
func useFakeIMAP(a *App) {
	if a.newIMAPClient == nil {
		a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &fakeIMAP{}, nil }
	}
}

// newTestScheduler is a scheduler over fakeIMAP sessions with a cap of n,
// stopped when the test ends.
func newTestScheduler(t testing.TB, n int) *syncsched.Scheduler[mailClient] {
	t.Helper()
	p := pool.New(n)
	s := syncsched.New(context.Background(), pool.NewSessions(p, func(context.Context) (mailClient, error) {
		return &fakeIMAP{}, nil
	}, 0), syncsched.Options{})
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

// setParallel stores n as the global sync parallelism setting.
func setParallel(t testing.TB, a *App, n int) {
	t.Helper()
	if err := a.store.SetInt(a.ctx, settingSyncMaxParallel, n); err != nil {
		t.Fatalf("set parallel: %v", err)
	}
}

// nopAdapter is a psync.Adapter on which every call succeeds and does
// nothing. Embed it and override the calls a test cares about.
type nopAdapter struct{}

func (nopAdapter) Addr() string { return "test:993" }
func (nopAdapter) ListMailboxes(context.Context) ([]psync.Mailbox, error) {
	return nil, nil
}
func (nopAdapter) ListMessages(context.Context, psync.RemoteMailbox) ([]psync.Header, string, string, error) {
	return nil, "", "", nil
}
func (nopAdapter) Fetch(context.Context, string, []string) ([]psync.Fetched, error) {
	return nil, nil
}
func (nopAdapter) SetFlags(context.Context, string, string, storage.Flag) error { return nil }
func (nopAdapter) Move(context.Context, string, []string, string) error         { return nil }
func (nopAdapter) Delete(context.Context, string, []string) error               { return nil }
func (nopAdapter) CreateMailbox(context.Context, string, string) (psync.Mailbox, error) {
	return psync.Mailbox{}, nil
}
func (nopAdapter) RenameMailbox(context.Context, string, string) error { return nil }
func (nopAdapter) DeleteMailbox(context.Context, string) error         { return nil }

// progressRecorder collects the progress events of one account. With cancel
// set it cancels the run when the first non-closing event arrives.
type progressRecorder struct {
	mu     sync.Mutex
	events []SyncProgressEvent
	cancel context.CancelFunc
}

// hook is an emitForTest hook recording accountID's progress events.
func (r *progressRecorder) hook(accountID int64) func(name string, payload any) {
	return func(name string, payload any) {
		e, ok := payload.(SyncProgressEvent)
		if !ok || name != EventSyncProgress || e.AccountID != accountID {
			return
		}
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
		if e.Folder != "" && r.cancel != nil {
			r.cancel()
		}
	}
}

// snapshot is a copy of the events recorded so far.
func (r *progressRecorder) snapshot() []SyncProgressEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SyncProgressEvent(nil), r.events...)
}

// closings counts the closing (folder-less) events.
func closings(events []SyncProgressEvent) int {
	n := 0
	for _, e := range events {
		if e.Folder == "" {
			n++
		}
	}
	return n
}

// assertClosedOnce fails the test unless events end with a closing event and
// hold exactly one.
func assertClosedOnce(t testing.TB, events []SyncProgressEvent) {
	t.Helper()
	if len(events) == 0 || events[len(events)-1].Folder != "" {
		t.Fatalf("last progress event of %+v, want a closing one", events)
	}
	if n := closings(events); n != 1 {
		t.Fatalf("closing events = %d, want 1", n)
	}
}

// newHoldTestApp is an App with a migrated store and a profile session, for
// tests that start, hold and stop account workers. Its background goroutines
// are joined before the store closes.
func newHoldTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
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

// mustAccountSync is the account's runtime, started if needed, failing the
// test if the account is held.
func mustAccountSync(t testing.TB, a *App, accountID int64) *accountSync {
	t.Helper()
	rt, err := a.ensureAccountSync(a.ctx, accountID)
	if err != nil {
		t.Fatalf("ensureAccountSync(%d): %v", accountID, err)
	}
	return rt
}

// tryScheduler is the account's scheduler, or nil while it is held.
func tryScheduler(a *App, accountID int64) *syncsched.Scheduler[mailClient] {
	rt, err := a.ensureAccountSync(a.ctx, accountID)
	if err != nil {
		return nil
	}
	return rt.sched
}

// trySync reports whether ensureAccountSync would start or return the
// account's runtime.
func trySync(a *App, accountID int64) error {
	_, err := a.ensureAccountSync(a.ctx, accountID)
	return err
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

// hasAccountState reports whether the account still has an accountStates entry.
func hasAccountState(a *App, accountID int64) bool {
	a.accountStatesMu.Lock()
	defer a.accountStatesMu.Unlock()
	_, ok := a.accountStates[accountID]
	return ok
}

// twoAccountRig is two seeded accounts (A and B) on one hold test app, each
// with an IMAP session parked open inside withIMAPSession until the test ends.
type twoAccountRig struct {
	a                *App
	idA, idB         int64
	folderA, folderB int64
	accA, accB       storage.Account

	mu      sync.Mutex
	clients map[string]*closeRecordingIMAP
}

// newTwoAccountRig seeds the two accounts and opens one session for each.
func newTwoAccountRig(t *testing.T) *twoAccountRig {
	t.Helper()
	r := &twoAccountRig{a: newHoldTestApp(t), clients: map[string]*closeRecordingIMAP{}}
	a := r.a
	r.idA, r.folderA = seedHoldAccount(t, a, "a@example.test")
	r.idB, r.folderB = seedHoldAccount(t, a, "b@example.test")
	for _, p := range []struct {
		id  int64
		acc *storage.Account
	}{{r.idA, &r.accA}, {r.idB, &r.accB}} {
		acc, err := a.store.GetAccount(a.ctx, p.id)
		if err != nil {
			t.Fatal(err)
		}
		*p.acc = *acc
	}
	a.newIMAPClient = func(cfg pimap.Config) (mailClient, error) {
		c := &closeRecordingIMAP{}
		r.mu.Lock()
		r.clients[cfg.Username] = c
		r.mu.Unlock()
		return c, nil
	}

	release := make(chan struct{})
	var sessions sync.WaitGroup
	opened := make(chan struct{}, 2)
	for _, acc := range []storage.Account{r.accA, r.accB} {
		sessions.Go(func() {
			_ = a.withIMAPSession(a.ctx, acc, func(mailClient) error {
				opened <- struct{}{}
				<-release
				return nil
			})
		})
	}
	t.Cleanup(func() { close(release); sessions.Wait() })
	for range 2 {
		recv(t, opened, 2*time.Second, "sessions never opened")
	}
	return r
}

// sessionClosed reports whether the account's parked session was closed.
// It fails the test if the account never opened one.
func (r *twoAccountRig) sessionClosed(t *testing.T, acc storage.Account) bool {
	t.Helper()
	r.mu.Lock()
	c := r.clients[acc.Email]
	r.mu.Unlock()
	if c == nil {
		t.Fatalf("%s never opened a session", acc.Email)
	}
	return c.closed.Load()
}

// parkedJob is a background job parked on an account's scheduler.
type parkedJob struct{ cancelled chan struct{} }

// park runs a background body job on the account's scheduler that waits until
// it is cancelled (closing cancelled) or the test ends.
func (r *twoAccountRig) park(t *testing.T, accountID int64) parkedJob {
	t.Helper()
	p := parkedJob{make(chan struct{})}
	running, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	tryScheduler(r.a, accountID).Enqueue(imapTask{
		Kind: syncsched.KindBodies,
		Run: func(ctx context.Context, _ mailClient) error {
			close(running)
			select {
			case <-ctx.Done():
				close(p.cancelled)
				return ctx.Err()
			case <-release:
				return nil
			}
		},
	})
	recv(t, running, 2*time.Second, "parked job never started")
	return p
}
