package desktop

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// onDemandBodyAdapter serves a full body for every fetched id and records the
// ids.
type onDemandBodyAdapter struct {
	nopAdapter
	mu      sync.Mutex
	fetched []string
}

func (a *onDemandBodyAdapter) Fetch(_ context.Context, _ string, remoteIDs []string) ([]psync.Fetched, error) {
	a.mu.Lock()
	a.fetched = append(a.fetched, remoteIDs...)
	a.mu.Unlock()
	out := make([]psync.Fetched, len(remoteIDs))
	for i, id := range remoteIDs {
		out[i] = psync.Fetched{RemoteID: id, Subject: "Full subject", Text: "Full body text for reading pane"}
	}
	return out, nil
}

func (a *onDemandBodyAdapter) lastFetched() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.fetched...)
}

// fetchInto stores the adapter's bodies the way the on-demand job does.
func (a *onDemandBodyAdapter) fetchInto(db *storage.DB, ctx context.Context, f storage.Folder, remoteIDs []string) error {
	_, err := psync.NewEngine(a, db, nil).FetchBodies(ctx, f, remoteIDs)
	return err
}

const stubRemoteID = "uid42"

// stubApp is an app whose account has one body-less stub in INBOX, opened
// with a preview the way a list sync stores it.
type stubApp struct {
	app       *App
	db        *storage.DB
	accountID int64
	folder    storage.Folder
	msgID     int64
}

// newStubApp seeds the stub, lets configure set fields the scheduler may read
// (configure may be nil), then starts the account's scheduler.
func newStubApp(t *testing.T, configure func(*stubApp)) *stubApp {
	t.Helper()
	a := newHoldTestApp(t)
	ctx := a.ctx
	accountID, err := a.store.CreateAccount(ctx, &storage.Account{
		Email: "reader@example.com", IMAPHost: "imap.example.com", IMAPPort: 993,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"}
	if _, err := a.store.CreateFolder(ctx, folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	msgID, err := a.store.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID, FolderID: folder.ID, RemoteID: stubRemoteID,
		Subject: "Stub subject", BodyPlain: "preview snippet",
	})
	if err != nil {
		t.Fatalf("insert stub: %v", err)
	}
	s := &stubApp{app: a, db: a.store, accountID: accountID, folder: *folder, msgID: msgID}
	useFake(t, a, accountID, &fakeIMAP{}) // every task needs a pooled session
	if configure != nil {
		configure(s)
	}
	a.ensureAccountSync(ctx, accountID)
	return s
}

// Opening a stub shows its preview at once, fetches the body once in the
// background, and the next open shows the stored body without a fetch.
func TestGetMessageOnDemandBody(t *testing.T) {
	s := newStubApp(t, nil)
	adapter := &onDemandBodyAdapter{}
	fetchDone := make(chan struct{})
	s.app.onDemandFetchForTest = func(jobCtx context.Context, acc storage.Account, f storage.Folder, remoteIDs []string) error {
		defer close(fetchDone)
		if acc.ID != s.accountID || f.ID != s.folder.ID {
			t.Errorf("on-demand fetch for account %d folder %d, want %d/%d", acc.ID, f.ID, s.accountID, s.folder.ID)
		}
		if len(remoteIDs) != 1 || remoteIDs[0] != stubRemoteID {
			t.Errorf("on-demand remote ids %v, want [%s]", remoteIDs, stubRemoteID)
		}
		return adapter.fetchInto(s.db, jobCtx, f, remoteIDs)
	}

	detail, err := s.app.GetMessage(s.msgID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if detail.BodyPlain != "preview snippet" || detail.BodyComplete {
		t.Fatalf("first read BodyPlain=%q BodyComplete=%v, want the preview and false before the fetch", detail.BodyPlain, detail.BodyComplete)
	}
	recv(t, fetchDone, 3*time.Second, "background body fetch did not run")
	if got := adapter.lastFetched(); len(got) != 1 || got[0] != stubRemoteID {
		t.Fatalf("fetched ids %v, want [%s]", got, stubRemoteID)
	}
	if m := mustGetMessage(t, s.db, s.msgID); !m.BodyComplete {
		t.Fatal("body_complete should be set after on-demand fetch")
	}

	detail, err = s.app.GetMessage(s.msgID)
	if err != nil {
		t.Fatalf("second GetMessage: %v", err)
	}
	if detail.BodyPlain != "Full body text for reading pane" || !detail.BodyComplete {
		t.Fatalf("second read BodyPlain=%q BodyComplete=%v, want the full fetched body", detail.BodyPlain, detail.BodyComplete)
	}
	if got := adapter.lastFetched(); len(got) != 1 {
		t.Fatalf("second GetMessage triggered a fetch; fetched %v", got)
	}
}

func mustGetMessage(t *testing.T, db *storage.DB, id int64) *storage.Message {
	t.Helper()
	m, err := db.GetMessage(context.Background(), id)
	if err != nil {
		t.Fatalf("get message %d: %v", id, err)
	}
	return m
}

// bodyRetryHarness is a stub opened in the reading pane, with the protocol
// fetch replaced, fast retries, and every body event the app emits recorded.
type bodyRetryHarness struct {
	*stubApp

	mu       sync.Mutex
	events   []string
	attempts int
	got      chan struct{}
}

func newBodyRetryHarness(t *testing.T, fetch func(h *bodyRetryHarness, attempt int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error) *bodyRetryHarness {
	t.Helper()
	h := &bodyRetryHarness{got: make(chan struct{}, 16)}
	h.stubApp = newStubApp(t, func(s *stubApp) {
		s.app.bodyRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
		s.app.emitForTest = func(name string, _ any) {
			if name != EventMailUpdated && name != EventMailBodyFailed {
				return
			}
			h.mu.Lock()
			h.events = append(h.events, name)
			h.mu.Unlock()
			h.got <- struct{}{}
		}
		s.app.onDemandFetchForTest = func(jobCtx context.Context, _ storage.Account, f storage.Folder, remoteIDs []string) error {
			h.mu.Lock()
			h.attempts++
			n := h.attempts
			h.mu.Unlock()
			return fetch(h, n, jobCtx, f, remoteIDs)
		}
	})
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
	recv(t, h.got, 3*time.Second, "the background fetch never reported back, so the spinner would spin forever")
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.events...)
}

func (h *bodyRetryHarness) attemptCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attempts
}

// How a reading-pane fetch reports back depends on what the retries end with.
func TestGetMessageBodyFetchOutcome(t *testing.T) {
	authErr := errors.New("You have to authenticate first.")
	for _, tc := range []struct {
		name  string
		fetch func(h *bodyRetryHarness, adapter *onDemandBodyAdapter, attempt int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error
		event string
		// attempts is the number of protocol fetches (0: not checked);
		// -1 means the first plus one per retry delay.
		attempts int
		stored   bool
	}{{
		// A fetch that fails a few times and then works stores the body
		// without the reader ever seeing the failures.
		name: "RetriesFailedFetch",
		fetch: func(h *bodyRetryHarness, adapter *onDemandBodyAdapter, attempt int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error {
			if attempt < 3 {
				return authErr
			}
			return adapter.fetchInto(h.db, jobCtx, f, remoteIDs)
		},
		event: EventMailUpdated, attempts: 3, stored: true,
	}, {
		// Once every retry has failed the pane is told, so it can stop the
		// spinner.
		name: "ReportsFailure",
		fetch: func(*bodyRetryHarness, *onDemandBodyAdapter, int, context.Context, storage.Folder, []string) error {
			return authErr
		},
		event: EventMailBodyFailed, attempts: -1,
	}, {
		// A fetch that reports success without storing the body (the server
		// no longer has the message) is a failure too. Reporting it as an
		// update had the pane reload, find the stub again and start another
		// fetch, forever.
		name: "StoresNothingFails",
		fetch: func(*bodyRetryHarness, *onDemandBodyAdapter, int, context.Context, storage.Folder, []string) error {
			return nil
		},
		event: EventMailBodyFailed,
	}, {
		// A body stored by something else while a retry waits (the
		// background fill of the folder) ends the fetch without another
		// trip to the server.
		name: "StoredMeanwhileSkipsTheServer",
		fetch: func(h *bodyRetryHarness, adapter *onDemandBodyAdapter, _ int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error {
			if err := adapter.fetchInto(h.db, jobCtx, f, remoteIDs); err != nil {
				return err
			}
			return errors.New("connection reset after the fill stored the body")
		},
		event: EventMailUpdated, attempts: 1, stored: true,
	}, {
		// A message the server sends but that cannot be read fails the same
		// way every time: the pane is told at once, without retries.
		name: "UnreadableFailsAtOnce",
		fetch: func(*bodyRetryHarness, *onDemandBodyAdapter, int, context.Context, storage.Folder, []string) error {
			return &psync.MessageError{RemoteIDs: []string{stubRemoteID}, Err: errors.New("malformed MIME")}
		},
		event: EventMailBodyFailed, attempts: 1,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &onDemandBodyAdapter{}
			h := newBodyRetryHarness(t, func(h *bodyRetryHarness, attempt int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error {
				return tc.fetch(h, adapter, attempt, jobCtx, f, remoteIDs)
			})

			events := h.open(t)
			if len(events) != 1 || events[0] != tc.event {
				t.Fatalf("events %v, want one %s", events, tc.event)
			}
			want := tc.attempts
			if want == -1 {
				want = len(h.app.bodyRetryDelays) + 1
			}
			if got := h.attemptCount(); want != 0 && got != want {
				t.Errorf("%d protocol fetches, want %d", got, want)
			}
			if m := mustGetMessage(t, h.db, h.msgID); m.BodyComplete != tc.stored {
				t.Errorf("body stored = %v, want %v", m.BodyComplete, tc.stored)
			}
		})
	}
}

// Opening the message again while its fetch is still retrying joins that fetch
// instead of starting a second one.
func TestGetMessageDoesNotFetchTheSameBodyTwice(t *testing.T) {
	release := make(chan struct{})
	h := newBodyRetryHarness(t, func(_ *bodyRetryHarness, attempt int, _ context.Context, _ storage.Folder, _ []string) error {
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
	recv(t, h.got, 3*time.Second, "the fetch never reported back")
	noRecv(t, h.got, 100*time.Millisecond, "a second fetch of the same body ran and reported")
	if want, got := len(h.app.bodyRetryDelays)+1, h.attemptCount(); got != want {
		t.Fatalf("%d fetches, want %d from a single fetch with its retries", got, want)
	}
}

// A fast lane full of opened messages drops the oldest one. That is not a
// failed fetch: the reading pane asks again without spending one of its
// retries, so a fetch that then fails a few times still gets them all.
func TestGetMessageDroppedFetchIsNotAnAttempt(t *testing.T) {
	adapter := &onDemandBodyAdapter{}
	h := newBodyRetryHarness(t, func(h *bodyRetryHarness, attempt int, jobCtx context.Context, f storage.Folder, remoteIDs []string) error {
		if attempt <= len(h.app.bodyRetryDelays) {
			return errors.New("connection reset")
		}
		return adapter.fetchInto(h.db, jobCtx, f, remoteIDs)
	})
	setParallel(t, h.app, 1)
	h.app.reconfigureAccountPool(h.accountID)
	sched := mustAccountSync(t, h.app, h.accountID).sched
	hold := make(chan struct{})
	recv(t, signalJob(sched, syncsched.KindBodies, 0, hold), 2*time.Second, "blocker never started")
	if _, err := h.app.GetMessage(h.msgID); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "the open was never queued", func() bool { return sched.Stats().Fast == 1 })
	for i := range syncsched.FastCap {
		sched.Enqueue(imapTask{Kind: syncsched.KindOnDemandBody, Key: fmt.Sprint("other", i), Run: func(context.Context, mailClient) error { return nil }})
	}
	close(hold)
	recv(t, h.got, 5*time.Second, "the fetch never reported back")
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) != 1 || h.events[0] != EventMailUpdated {
		t.Fatalf("events %v, want mail:updated after the drop and %d failures", h.events, len(h.app.bodyRetryDelays))
	}
}

// A mailbox being removed keeps the stub; keeping it offline says so, not
// that the server lost the message.
func TestDownloadMessageOfflineOnAHeldAccount(t *testing.T) {
	s := newStubApp(t, nil)
	s.app.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error { return nil }
	release := s.app.holdAccountSync(s.accountID)
	defer release()
	if err := s.app.DownloadMessageOffline(s.msgID); !errors.Is(err, errAccountSyncHeld) {
		t.Fatalf("DownloadMessageOffline = %v, want %v", err, errAccountSyncHeld)
	}
}

// An open still queued when the message is moved or deleted ends without a
// trip to the server, and the pane, which follows the list, hears nothing.
func TestQueuedOpenOfAGoneMessageFetchesNothing(t *testing.T) {
	h := newBodyRetryHarness(t, func(*bodyRetryHarness, int, context.Context, storage.Folder, []string) error { return nil })
	setParallel(t, h.app, 1)
	h.app.reconfigureAccountPool(h.accountID)
	sched := mustAccountSync(t, h.app, h.accountID).sched
	hold := make(chan struct{})
	recv(t, signalJob(sched, syncsched.KindBodies, 0, hold), 2*time.Second, "blocker never started")
	if _, err := h.app.GetMessage(h.msgID); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "the open was never queued", func() bool { return sched.Stats().Fast == 1 })
	if err := h.db.DeleteMessage(h.app.ctx, h.msgID); err != nil {
		t.Fatal(err)
	}
	close(hold)
	waitUntil(t, 2*time.Second, "the open never ran", func() bool { st := sched.Stats(); return st.Fast+st.Running == 0 })
	noRecv(t, h.got, 100*time.Millisecond, "the pane was told about a message that left the list")
	if n := h.attemptCount(); n != 0 {
		t.Fatalf("%d fetches of a message that is gone, want none", n)
	}
}

// The pane's retries belong to the profile it was opened in. After a switch
// they stop: the next try would look the id up in the new profile's store
// and start sync work there for an account it does not have.
func TestPaneRetriesEndWithTheProfile(t *testing.T) {
	h := newBodyRetryHarness(t, func(*bodyRetryHarness, int, context.Context, storage.Folder, []string) error {
		return errors.New("connection reset")
	})
	h.app.bodyRetryDelays = []time.Duration{200 * time.Millisecond, 200 * time.Millisecond}
	if _, err := h.app.GetMessage(h.msgID); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "first fetch never ran", func() bool { return h.attemptCount() == 1 })
	h.app.sessionMu.Lock()
	h.app.sessionStop()
	h.app.session = nil
	h.app.sessionMu.Unlock()
	// The pane's fetch, retries included, is one bodyFetches entry: once it
	// is gone no retry is left to come. Retries that ignored the switch
	// would run both 200 ms delays first and be counted.
	waitBodyFetchDone(t, h.app, h.msgID)
	if n := h.attemptCount(); n != 1 {
		t.Fatalf("%d fetches, want the retries to stop with the profile", n)
	}
}
