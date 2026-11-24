package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// The initial sync lists Inbox, then fetches Inbox's first body batch, and
// only then the next folder. Later folder lists are not queued while that
// batch is outstanding.
func TestIMAPInitialSyncInboxBodiesBeforeNextFolderStubs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		folders := []storage.Folder{
			{ID: 2, Name: "Archive", IMAPPath: "Archive"},
			{ID: 1, Name: "INBOX", IMAPPath: "INBOX"},
			{ID: 3, Name: "Nope", IMAPPath: "Nope", SyncExcluded: true},
		}

		var mu sync.Mutex
		var order []string
		inboxBodies := make(chan struct{})
		releaseBodies := make(chan struct{})
		var released bool

		s := newTestScheduler(t, 3)
		t.Cleanup(func() {
			mu.Lock()
			if !released {
				released = true
				close(releaseBodies)
			}
			mu.Unlock()
		})

		done := make(chan error, 1)
		enqueueSyncPlan(context.Background(), s, folders, syncPlan{done: done,
			step: func(_ context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
				step := initialSyncStep(folder, kind)
				mu.Lock()
				order = append(order, step)
				mu.Unlock()
				if step == "bodies:INBOX" {
					close(inboxBodies)
					<-releaseBodies
				}
				return nil
			}})

		recv(t, inboxBodies, 2*time.Second, "inbox bodies never started")

		// Archive stubs must still be waiting: bodies for Inbox are in flight.
		synctest.Wait()
		mu.Lock()
		got := append([]string(nil), order...)
		mu.Unlock()
		if want := []string{"stubs:INBOX", "bodies:INBOX"}; !slices.Equal(got, want) {
			t.Fatalf("while inbox bodies run, order %v, want %v", got, want)
		}

		mu.Lock()
		if !released {
			released = true
			close(releaseBodies)
		}
		mu.Unlock()

		if err := recv(t, done, 2*time.Second, "initial sync chain did not finish"); err != nil {
			t.Fatalf("initial sync chain: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if want := []string{"stubs:INBOX", "bodies:INBOX", "stubs:Archive", "bodies:Archive"}; !slices.Equal(order, want) {
			t.Fatalf("order %v, want %v", order, want)
		}
	})
}

func useFastSyncRetryBackoff(t *testing.T) {
	t.Helper()
	orig := syncRetryBackoff
	syncRetryBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() { syncRetryBackoff = orig })
}

func initialSyncFolders() []storage.Folder {
	return []storage.Folder{
		{ID: 2, Name: "Archive", IMAPPath: "Archive"},
		{ID: 1, Name: "INBOX", IMAPPath: "INBOX"},
	}
}

// runInitialSync runs the plan in a synctest bubble, so retries wait out the
// real syncRetryBackoff on the bubble's clock.
func runInitialSync(t *testing.T, step func(storage.Folder, syncsched.Kind) error) error {
	t.Helper()
	var err error
	synctest.Test(t, func(t *testing.T) {
		s := newTestScheduler(t, 3)
		done := make(chan error, 1)
		enqueueSyncPlan(context.Background(), s, initialSyncFolders(), syncPlan{done: done,
			step: func(_ context.Context, _ mailClient, f storage.Folder, k syncsched.Kind) error { return step(f, k) }})
		select {
		case err = <-done:
		case <-time.After(time.Minute):
			t.Fatal("initial sync chain did not finish")
		}
	})
	return err
}

// initialSyncStep names one plan task: "stubs:<path>" or "bodies:<path>".
func initialSyncStep(folder storage.Folder, kind syncsched.Kind) string {
	if kind == syncsched.KindListStubs {
		return "stubs:" + folder.IMAPPath
	}
	return "bodies:" + folder.IMAPPath
}

// A failed step is retried on the same folder and the next folder stays
// queued until a try succeeds. When the retries run out the folder is failed
// and the plan continues with the next folder. A body batch returning
// ErrAgain is not a failure: the generator runs again to its end, and the
// next folder starts after Inbox's first batch, alongside the rest.
func TestIMAPInitialSyncStepRetries(t *testing.T) {
	failed := errors.New("failed")
	// failUntil fails step on every try before the n-th.
	failUntil := func(step string, n int) func(string, int) error {
		return func(s string, try int) error {
			if s == step && try < n {
				return failed
			}
			return nil
		}
	}
	for _, c := range []struct {
		name string
		// fail is what try (from 1) of step returns; nil is a success.
		fail      func(step string, try int) error
		wantErr   bool
		wantTries map[string]int
		want      []string
		// unordered compares want after its first two steps (Inbox's list
		// and first batch) in any order: the rest run side by side.
		unordered bool
	}{
		{
			name:      "stubs retried before next folder",
			fail:      failUntil("stubs:INBOX", syncStepAttempts),
			wantTries: map[string]int{"stubs:INBOX": syncStepAttempts},
			want: []string{"stubs:INBOX", "stubs:INBOX", "stubs:INBOX",
				"bodies:INBOX", "stubs:Archive", "bodies:Archive"},
		},
		{
			name:    "continues after stub retries exhausted",
			fail:    failUntil("stubs:INBOX", syncStepAttempts+1),
			wantErr: true,
			want:    []string{"stubs:INBOX", "stubs:INBOX", "stubs:INBOX", "stubs:Archive", "bodies:Archive"},
		},
		{
			name:      "bodies retried then continues",
			fail:      failUntil("bodies:INBOX", syncStepAttempts),
			wantTries: map[string]int{"bodies:INBOX": syncStepAttempts},
			want: []string{"stubs:INBOX", "bodies:INBOX", "bodies:INBOX", "bodies:INBOX",
				"stubs:Archive", "bodies:Archive"},
		},
		{
			name:    "continues after body retries exhausted",
			fail:    failUntil("bodies:INBOX", syncStepAttempts+1),
			wantErr: true,
			want: []string{"stubs:INBOX", "bodies:INBOX", "bodies:INBOX", "bodies:INBOX",
				"stubs:Archive", "bodies:Archive"},
		},
		{
			name: "another batch neither advances nor counts as a failure",
			fail: func(s string, try int) error {
				if s == "bodies:INBOX" && try <= syncStepAttempts {
					return syncsched.ErrAgain
				}
				return nil
			},
			wantTries: map[string]int{"bodies:INBOX": syncStepAttempts + 1},
			want: []string{"stubs:INBOX", "bodies:INBOX", "bodies:INBOX", "bodies:INBOX", "bodies:INBOX",
				"stubs:Archive", "bodies:Archive"},
			unordered: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var order []string
			tries := map[string]int{}
			err := runInitialSync(t, func(folder storage.Folder, kind syncsched.Kind) error {
				step := initialSyncStep(folder, kind)
				mu.Lock()
				order = append(order, step)
				tries[step]++
				try := tries[step]
				mu.Unlock()
				return c.fail(step, try)
			})
			if t.Failed() {
				return
			}
			if (err != nil) != c.wantErr {
				t.Fatalf("initial sync chain err = %v, want error %v", err, c.wantErr)
			}
			mu.Lock()
			defer mu.Unlock()
			for step, n := range c.wantTries {
				if tries[step] != n {
					t.Fatalf("%s tries %d, want %d", step, tries[step], n)
				}
			}
			got, want := order, c.want
			if c.unordered && len(got) >= 2 {
				got = append(slices.Clone(got[:2]), slices.Sorted(slices.Values(got[2:]))...)
				want = append(slices.Clone(want[:2]), slices.Sorted(slices.Values(want[2:]))...)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("order %v, want %v", order, c.want)
			}
		})
	}
}

// A body batch that stores some ids and then fails is retried, and the
// retry asks the store again, so it fetches only the ids still incomplete.
// The folder then finishes and the plan moves on.
func TestIMAPInitialSyncBodyRetryFetchesOnlyIncomplete(t *testing.T) {
	useFastSyncRetryBackoff(t)
	ctx := context.Background()
	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"}
	if _, err := db.CreateFolder(ctx, &inbox); err != nil {
		t.Fatal(err)
	}
	archive := storage.Folder{AccountID: accountID, Name: "Archive", IMAPPath: "Archive", RemoteID: "Archive"}
	if _, err := db.CreateFolder(ctx, &archive); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"m1", "m2", "m3"} {
		if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{
			AccountID: accountID, FolderID: inbox.ID, RemoteID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}

	adapter := &partialBodyAdapter{}
	engine := psync.NewEngine(adapter, db, nil)
	var mu sync.Mutex
	var attempts [][]string

	s := newTestScheduler(t, 1)
	done := make(chan error, 1)
	enqueueSyncPlan(ctx, s, []storage.Folder{archive, inbox}, syncPlan{done: done,
		step: func(jobCtx context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
			if kind == syncsched.KindListStubs || folder.IMAPPath != "INBOX" {
				return nil
			}
			ids, err := db.RemoteIDsNeedingBodyNewest(jobCtx, folder.ID, 0, 0)
			if err != nil || len(ids) == 0 {
				return err
			}
			slices.Sort(ids)
			mu.Lock()
			attempts = append(attempts, append([]string(nil), ids...))
			mu.Unlock()
			if _, err := engine.FetchBodies(jobCtx, folder, ids); err != nil {
				return err
			}
			return syncsched.ErrAgain
		}})

	if err := recv(t, done, 2*time.Second, "initial sync chain did not finish"); err != nil {
		t.Fatalf("folder should complete after the incomplete retry: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 2 {
		t.Fatalf("body attempts %d, want 2", len(attempts))
	}
	if strings.Join(attempts[0], ",") != "m1,m2,m3" {
		t.Fatalf("first attempt %v, want [m1 m2 m3]", attempts[0])
	}
	if strings.Join(attempts[1], ",") != "m2,m3" {
		t.Fatalf("retry ids %v, want only incomplete [m2 m3]", attempts[1])
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.fetched) != 2 {
		t.Fatalf("fetch calls %v, want two", adapter.fetched)
	}
	slices.Sort(adapter.fetched[1])
	if strings.Join(adapter.fetched[1], ",") != "m2,m3" {
		t.Fatalf("second fetch %v, want [m2 m3]", adapter.fetched[1])
	}
	msgs, err := db.ListMessages(ctx, inbox.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	complete := map[string]bool{}
	for _, m := range msgs {
		complete[m.RemoteID] = m.BodyComplete
	}
	for _, id := range []string{"m1", "m2", "m3"} {
		if !complete[id] {
			t.Fatalf("%s body_complete=%v, want true", id, complete[id])
		}
	}
}

// partialBodyAdapter stores the first id of the first Fetch and fails the
// rest of that call. Later calls succeed.
type partialBodyAdapter struct {
	nopAdapter
	mu      sync.Mutex
	calls   int
	fetched [][]string
}

func (a *partialBodyAdapter) Fetch(_ context.Context, _ string, remoteIDs []string) ([]psync.Fetched, error) {
	a.mu.Lock()
	a.calls++
	call := a.calls
	a.fetched = append(a.fetched, append([]string(nil), remoteIDs...))
	a.mu.Unlock()
	if call == 1 {
		if len(remoteIDs) == 0 {
			return nil, errors.New("forced")
		}
		return []psync.Fetched{{
			RemoteID: remoteIDs[0], Subject: "body", Text: "body",
		}}, errors.New("forced")
	}
	out := make([]psync.Fetched, len(remoteIDs))
	for i, id := range remoteIDs {
		out[i] = psync.Fetched{RemoteID: id, Subject: "body", Text: "body"}
	}
	return out, nil
}

// After Inbox, the remaining folders list in parallel on the background slots.
func TestIMAPInitialSyncRunsOtherFoldersInParallelAfterInbox(t *testing.T) {
	folders := []storage.Folder{
		{ID: 1, Name: "INBOX", IMAPPath: "INBOX"},
		{ID: 2, Name: "Sent", IMAPPath: "Sent", Attributes: []string{`\Sent`}},
		{ID: 3, Name: "Archive", IMAPPath: "Archive"},
	}
	s := newTestScheduler(t, 3)

	bothStarted := make(chan struct{})
	var mu sync.Mutex
	inFlight := 0
	done := make(chan error, 1)
	enqueueSyncPlan(context.Background(), s, folders, syncPlan{done: done,
		step: func(_ context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
			if folder.IMAPPath == "INBOX" || kind != syncsched.KindListStubs {
				return nil
			}
			mu.Lock()
			inFlight++
			if inFlight == 2 {
				close(bothStarted)
			}
			mu.Unlock()
			select {
			case <-bothStarted:
				return nil
			case <-time.After(2 * time.Second):
				return errors.New("other folders did not list in parallel")
			}
		}})
	if err := recv(t, done, 5*time.Second, "initial sync chain did not finish"); err != nil {
		t.Fatal(err)
	}
}

// onInboxDone fires after Inbox stubs and its first body batch, before any
// other folder starts.
func TestIMAPInitialSyncCallsOnInboxDoneFirst(t *testing.T) {
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	s := newTestScheduler(t, 3)
	done := make(chan error, 1)
	enqueueSyncPlan(context.Background(), s, initialSyncFolders(), syncPlan{done: done,
		step: func(_ context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
			note(initialSyncStep(folder, kind))
			return nil
		},
		onInboxDone: func() { note("idle") }})
	if err := recv(t, done, 2*time.Second, "initial sync chain did not finish"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := "stubs:INBOX,bodies:INBOX,idle,stubs:Archive,bodies:Archive"
	if strings.Join(order, ",") != want {
		t.Fatalf("order %v, want %s", order, want)
	}
}

// With more Inbox bodies than one batch, onInboxDone (IDLE) and the other
// folders start after Inbox's first body batch, not after its whole body
// generator: with sync_message_limit=0 that generator can run for hours. The
// rest of Inbox's bodies still download, and the plan reports done only
// after them.
func TestIMAPInitialSyncStartsRestAfterFirstInboxBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		note := func(s string) {
			mu.Lock()
			order = append(order, s)
			mu.Unlock()
		}
		archiveListed := make(chan struct{})
		inboxBatches := 0
		s := newTestScheduler(t, 3)
		done := make(chan error, 1)
		enqueueSyncPlan(context.Background(), s, initialSyncFolders(), syncPlan{done: done,
			step: func(_ context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
				step := initialSyncStep(folder, kind)
				note(step)
				switch step {
				case "stubs:Archive":
					close(archiveListed)
				case "bodies:INBOX":
					mu.Lock()
					inboxBatches++
					n := inboxBatches
					mu.Unlock()
					if n == 1 {
						return syncsched.ErrAgain
					}
					// The last Inbox batch waits for Archive's list, which
					// must not wait for this generator to end.
					select {
					case <-archiveListed:
					case <-time.After(time.Minute):
						note("timeout")
					}
				}
				return nil
			},
			onInboxDone: func() { note("idle") }})
		if err := recv(t, done, 5*time.Minute, "initial sync chain did not finish"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if slices.Contains(order, "timeout") {
			t.Fatalf("Archive waited for the whole Inbox body generator: %v", order)
		}
		if want := []string{"stubs:INBOX", "bodies:INBOX", "idle"}; !slices.Equal(order[:min(3, len(order))], want) {
			t.Fatalf("order %v, want prefix %v", order, want)
		}
		if inboxBatches != 2 {
			t.Fatalf("Inbox body batches %d, want 2 (the generator must still run to its end)", inboxBatches)
		}
		if n := strings.Count(strings.Join(order, ","), "idle"); n != 1 {
			t.Fatalf("onInboxDone ran %d times, want 1: %v", n, order)
		}
	})
}

// A first Inbox body batch that fails is retried, and only the first batch
// that succeeds releases onInboxDone and the other folders, once.
func TestIMAPInitialSyncFailedFirstInboxBatchHoldsRest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		note := func(s string) {
			mu.Lock()
			order = append(order, s)
			mu.Unlock()
		}
		inboxBatches := 0
		s := newTestScheduler(t, 3)
		done := make(chan error, 1)
		enqueueSyncPlan(context.Background(), s, initialSyncFolders(), syncPlan{done: done,
			step: func(_ context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
				step := initialSyncStep(folder, kind)
				note(step)
				if step != "bodies:INBOX" {
					return nil
				}
				mu.Lock()
				inboxBatches++
				n := inboxBatches
				mu.Unlock()
				switch n {
				case 1:
					return errors.New("retryable")
				case 2:
					return syncsched.ErrAgain // succeeded, more to fetch
				}
				return nil
			},
			onInboxDone: func() { note("idle") }})
		if err := recv(t, done, time.Minute, "initial sync chain did not finish"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if want := []string{"stubs:INBOX", "bodies:INBOX", "bodies:INBOX", "idle"}; !slices.Equal(order[:min(4, len(order))], want) {
			t.Fatalf("order %v, want prefix %v", order, want)
		}
		if n := strings.Count(strings.Join(order, ","), "idle"); n != 1 {
			t.Fatalf("onInboxDone ran %d times, want 1: %v", n, order)
		}
		if inboxBatches != 3 {
			t.Fatalf("Inbox body batches %d, want 3", inboxBatches)
		}
	})
}
