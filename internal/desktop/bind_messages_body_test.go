package desktop

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// onDemandBodyAdapter records Fetch calls for on-demand body tests.
type onDemandBodyAdapter struct {
	mu      sync.Mutex
	fetched []string
}

func (a *onDemandBodyAdapter) Addr() string { return "test:993" }

func (a *onDemandBodyAdapter) ListMailboxes(context.Context) ([]psync.Mailbox, error) {
	return nil, nil
}

func (a *onDemandBodyAdapter) ListMessages(context.Context, psync.RemoteMailbox) ([]psync.Header, string, string, error) {
	return nil, "", "", nil
}

func (a *onDemandBodyAdapter) Fetch(_ context.Context, _ string, remoteIDs []string) ([]psync.Fetched, error) {
	a.mu.Lock()
	a.fetched = append(a.fetched, remoteIDs...)
	a.mu.Unlock()
	out := make([]psync.Fetched, len(remoteIDs))
	for i, id := range remoteIDs {
		out[i] = psync.Fetched{
			RemoteID: id,
			Subject:  "Full subject",
			Text:     "Full body text for reading pane",
		}
	}
	return out, nil
}

func (a *onDemandBodyAdapter) SetFlags(context.Context, string, string, storage.Flag) error {
	return nil
}

func (a *onDemandBodyAdapter) Move(context.Context, string, []string, string) error { return nil }

func (a *onDemandBodyAdapter) Delete(context.Context, string, []string) error { return nil }

func (a *onDemandBodyAdapter) CreateMailbox(context.Context, string, string) (psync.Mailbox, error) {
	return psync.Mailbox{}, nil
}

func (a *onDemandBodyAdapter) RenameMailbox(context.Context, string, string) error { return nil }

func (a *onDemandBodyAdapter) DeleteMailbox(context.Context, string) error { return nil }

func (a *onDemandBodyAdapter) fetchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.fetched)
}

func (a *onDemandBodyAdapter) lastFetched() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.fetched...)
}

func TestGetMessageOnDemandBody(t *testing.T) {
	ctx, stopBackground := testContext(t)
	defer stopBackground()

	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	accountID, err := db.CreateAccount(ctx, &storage.Account{
		Email:    "reader@example.com",
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"}
	if _, err := db.CreateFolder(ctx, folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	const remoteID = "uid42"
	msgID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID,
		FolderID:  folder.ID,
		RemoteID:  remoteID,
		Subject:   "Stub subject",
		BodyPlain: "preview snippet",
	})
	if err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	adapter := &onDemandBodyAdapter{}
	app := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}
	app.ensureAccountScheduler(ctx, accountID)
	account, err := db.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}

	app.onDemandFetchForTest = func(jobCtx context.Context, acc storage.Account, f storage.Folder, remoteIDs []string) error {
		if acc.ID != account.ID || f.ID != folder.ID {
			t.Fatalf("on-demand fetch account/folder mismatch")
		}
		if len(remoteIDs) != 1 || remoteIDs[0] != remoteID {
			t.Fatalf("on-demand remote ids %v, want [%s]", remoteIDs, remoteID)
		}
		engine := psync.NewEngine(adapter, db, nil)
		_, err := engine.FetchBodies(jobCtx, f, remoteIDs)
		return err
	}
	t.Cleanup(func() { app.onDemandFetchForTest = nil })

	fetchDone := make(chan struct{})
	origOnDemand := app.onDemandFetchForTest
	app.onDemandFetchForTest = func(jobCtx context.Context, acc storage.Account, f storage.Folder, remoteIDs []string) error {
		err := origOnDemand(jobCtx, acc, f, remoteIDs)
		close(fetchDone)
		return err
	}

	detail, err := app.GetMessage(msgID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if detail.BodyPlain != "preview snippet" {
		t.Fatalf("BodyPlain=%q, want preview before fetch completes", detail.BodyPlain)
	}
	if detail.BodyComplete {
		t.Fatal("BodyComplete should be false before fetch")
	}

	select {
	case <-fetchDone:
	case <-time.After(3 * time.Second):
		t.Fatal("background body fetch did not run")
	}
	if adapter.fetchCount() != 1 {
		t.Fatalf("Fetch calls %d, want 1", adapter.fetchCount())
	}
	if got := adapter.lastFetched(); len(got) != 1 || got[0] != remoteID {
		t.Fatalf("fetched ids %v, want [%s]", got, remoteID)
	}

	m, err := db.GetMessage(ctx, msgID)
	if err != nil {
		t.Fatalf("reload message: %v", err)
	}
	if !m.BodyComplete {
		t.Fatal("body_complete should be set after on-demand fetch")
	}

	detail2, err := app.GetMessage(msgID)
	if err != nil {
		t.Fatalf("second GetMessage: %v", err)
	}
	if detail2.BodyPlain != "Full body text for reading pane" {
		t.Fatalf("second read BodyPlain=%q, want full fetched body", detail2.BodyPlain)
	}
	if !detail2.BodyComplete {
		t.Fatal("BodyComplete should be true after fetch")
	}
	if adapter.fetchCount() != 1 {
		t.Fatalf("second GetMessage triggered fetch; count=%d want 1", adapter.fetchCount())
	}
}

func TestGetMessageStubReturnsPreviewBeforeBodyFetch(t *testing.T) {
	ctx, stopBackground := testContext(t)
	defer stopBackground()

	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	accountID, err := db.CreateAccount(ctx, &storage.Account{
		Email: "reader@example.com",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "MbInbox"}
	if _, err := db.CreateFolder(ctx, folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	const remoteID = "e1"
	msgID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID,
		FolderID:  folder.ID,
		RemoteID:  remoteID,
		Subject:   "Stub subject",
		BodyPlain: "preview snippet",
	})
	if err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	adapter := &onDemandBodyAdapter{}
	app := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}
	app.ensureAccountScheduler(ctx, accountID)

	fetchDone := make(chan struct{})
	app.onDemandFetchForTest = func(jobCtx context.Context, acc storage.Account, f storage.Folder, remoteIDs []string) error {
		engine := psync.NewEngine(adapter, db, nil)
		_, err := engine.FetchBodies(jobCtx, f, remoteIDs)
		close(fetchDone)
		return err
	}
	t.Cleanup(func() { app.onDemandFetchForTest = nil })

	detail, err := app.GetMessage(msgID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if detail.BodyPlain != "preview snippet" {
		t.Fatalf("BodyPlain=%q, want preview before fetch completes", detail.BodyPlain)
	}

	select {
	case <-fetchDone:
	case <-time.After(3 * time.Second):
		t.Fatal("background body fetch did not run")
	}

	m, err := db.GetMessage(ctx, msgID)
	if err != nil {
		t.Fatalf("reload message: %v", err)
	}
	if !m.BodyComplete {
		t.Fatal("expected body_complete after background fetch")
	}
}

// bodyRetryHarness is a stub opened in the reading pane, with the protocol
// fetch replaced and every event the app emits recorded.
type bodyRetryHarness struct {
	app   *App
	db    *storage.DB
	msgID int64

	mu     sync.Mutex
	events []string
	got    chan struct{}
}

func newBodyRetryHarness(t *testing.T, fetch func(attempt int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error) *bodyRetryHarness {
	t.Helper()
	ctx, stopBackground := testContext(t)
	t.Cleanup(stopBackground)

	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "reader@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "MbInbox"}
	if _, err := db.CreateFolder(ctx, folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	msgID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: "e1", Subject: "Stub", BodyPlain: "preview",
	})
	if err != nil {
		t.Fatalf("insert stub: %v", err)
	}

	h := &bodyRetryHarness{db: db, msgID: msgID, got: make(chan struct{}, 16)}
	h.app = &App{
		ctx:             ctx,
		store:           db,
		log:             slog.New(slog.DiscardHandler),
		bodyRetryDelays: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
		emitForTest: func(name string, payload any) {
			if name != EventMailUpdated && name != EventMailBodyFailed {
				return
			}
			h.mu.Lock()
			h.events = append(h.events, name)
			h.mu.Unlock()
			h.got <- struct{}{}
		},
	}
	h.app.ensureAccountScheduler(ctx, accountID)
	var attempts int
	var attemptsMu sync.Mutex
	h.app.onDemandFetchForTest = func(jobCtx context.Context, _ storage.Account, f storage.Folder, remoteIDs []string) error {
		attemptsMu.Lock()
		attempts++
		n := attempts
		attemptsMu.Unlock()
		return fetch(n, jobCtx, f, remoteIDs)
	}
	return h
}

// open reads the message the way the reading pane does and waits for the
// background fetch to report back.
func (h *bodyRetryHarness) open(t *testing.T) []string {
	t.Helper()
	detail, err := h.app.GetMessage(h.msgID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if detail.BodyComplete {
		t.Fatal("a stub should open with the body spinner on")
	}
	select {
	case <-h.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the background fetch never reported back, so the spinner would spin forever")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.events...)
}

// A fetch that fails a few times and then works stores the body without the
// reader ever seeing the failures.
func TestGetMessageRetriesFailedBodyFetch(t *testing.T) {
	adapter := &onDemandBodyAdapter{}
	var h *bodyRetryHarness
	h = newBodyRetryHarness(t, func(attempt int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error {
		if attempt < 3 {
			return errors.New("You have to authenticate first.")
		}
		_, err := psync.NewEngine(adapter, h.db, nil).FetchBodies(jobCtx, f, remoteIDs)
		return err
	})

	events := h.open(t)
	if len(events) != 1 || events[0] != EventMailUpdated {
		t.Fatalf("events %v, want one %s", events, EventMailUpdated)
	}
	m, err := h.db.GetMessage(h.app.ctx, h.msgID)
	if err != nil {
		t.Fatal(err)
	}
	if !m.BodyComplete {
		t.Fatal("body not stored after the retry that worked")
	}
}

// Once every retry has failed the pane is told, so it can stop the spinner.
func TestGetMessageReportsBodyFetchFailure(t *testing.T) {
	var attempts int
	h := newBodyRetryHarness(t, func(attempt int, _ context.Context, _ storage.Folder, _ []string) error {
		attempts = attempt
		return errors.New("You have to authenticate first.")
	})

	events := h.open(t)
	if len(events) != 1 || events[0] != EventMailBodyFailed {
		t.Fatalf("events %v, want one %s", events, EventMailBodyFailed)
	}
	if want := len(h.app.bodyRetryDelays) + 1; attempts != want {
		t.Fatalf("%d attempts, want %d (the first and one per retry delay)", attempts, want)
	}
}

// A fetch that reports success without storing the body (the server no longer
// has the message) is a failure too. Reporting it as an update had the pane
// reload, find the stub again and start another fetch, forever.
func TestGetMessageBodyFetchThatStoresNothingFails(t *testing.T) {
	h := newBodyRetryHarness(t, func(int, context.Context, storage.Folder, []string) error {
		return nil
	})

	events := h.open(t)
	if len(events) != 1 || events[0] != EventMailBodyFailed {
		t.Fatalf("events %v, want one %s", events, EventMailBodyFailed)
	}
}

// Opening the message again while its fetch is still retrying joins that fetch
// instead of starting a second one.
func TestGetMessageDoesNotFetchTheSameBodyTwice(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	h := newBodyRetryHarness(t, func(attempt int, _ context.Context, _ storage.Folder, _ []string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		if attempt == 1 {
			<-release
		}
		return errors.New("offline")
	})

	if _, err := h.app.GetMessage(h.msgID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.GetMessage(h.msgID); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case <-h.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the fetch never reported back")
	}
	select {
	case <-h.got:
		t.Fatal("a second fetch of the same body ran and reported")
	case <-time.After(100 * time.Millisecond):
	}
	mu.Lock()
	defer mu.Unlock()
	if want := len(h.app.bodyRetryDelays) + 1; calls != want {
		t.Fatalf("%d fetches, want %d from a single fetch with its retries", calls, want)
	}
}

// A body stored by something else while a retry waits (the background fill of
// the folder) ends the fetch without another trip to the server.
func TestGetMessageBodyStoredMeanwhileSkipsTheServer(t *testing.T) {
	adapter := &onDemandBodyAdapter{}
	var mu sync.Mutex
	calls := 0
	var h *bodyRetryHarness
	h = newBodyRetryHarness(t, func(_ int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		if _, err := psync.NewEngine(adapter, h.db, nil).FetchBodies(jobCtx, f, remoteIDs); err != nil {
			return err
		}
		return errors.New("connection reset after the fill stored the body")
	})

	events := h.open(t)
	if len(events) != 1 || events[0] != EventMailUpdated {
		t.Fatalf("events %v, want one %s", events, EventMailUpdated)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("%d server fetches, want 1: the retry should find the body already stored", calls)
	}
}
