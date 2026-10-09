package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// The initial sync lists Inbox stubs, then Inbox bodies, and only then the next
// folder. Later folder stubs must not be queued while an earlier folder's
// bodies are still outstanding.
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

		s := syncsched.New(1)
		s.Start(context.Background(), pool.New(3), 0)
		t.Cleanup(func() {
			mu.Lock()
			if !released {
				released = true
				close(releaseBodies)
			}
			mu.Unlock()
			s.Stop()
		})

		done := make(chan error, 1)
		enqueueIMAPInitialSync(s, folders, func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
			name := "bodies"
			if kind == syncsched.JobListStubs {
				name = "stubs"
			}
			step := name + ":" + folder.IMAPPath
			mu.Lock()
			order = append(order, step)
			mu.Unlock()

			if folder.IMAPPath == "INBOX" && kind == syncsched.JobListStubs {
				return []string{"m1"}, nil
			}
			if folder.IMAPPath == "INBOX" && kind == syncsched.JobFetchBodies {
				ids := syncsched.RemoteIDs(ctx)
				if len(ids) != 1 || ids[0] != "m1" {
					t.Errorf("inbox bodies remote ids %v, want [m1]", ids)
				}
				close(inboxBodies)
				<-releaseBodies
			}
			if kind == syncsched.JobListStubs {
				return []string{"x"}, nil
			}
			return nil, nil
		}, nil, done)

		select {
		case <-inboxBodies:
		case <-time.After(2 * time.Second):
			t.Fatal("inbox bodies never started")
		}

		// Archive stubs must still be waiting: bodies for Inbox are in flight.
		synctest.Wait()
		mu.Lock()
		got := append([]string(nil), order...)
		mu.Unlock()
		if len(got) != 2 || got[0] != "stubs:INBOX" || got[1] != "bodies:INBOX" {
			t.Fatalf("while inbox bodies run, order %v, want [stubs:INBOX bodies:INBOX]", got)
		}

		mu.Lock()
		if !released {
			released = true
			close(releaseBodies)
		}
		mu.Unlock()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("initial sync chain: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("initial sync chain did not finish")
		}

		mu.Lock()
		defer mu.Unlock()
		want := []string{"stubs:INBOX", "bodies:INBOX", "stubs:Archive", "bodies:Archive"}
		if len(order) != len(want) {
			t.Fatalf("order %v, want %v", order, want)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("order %v, want %v", order, want)
			}
		}
	})
}

// The pause check follows the pool's effective size read at call time, so a
// throttle to 1 mid-run turns a live soft-pause into a hold. At N>=2 the live
// hold is ignored; live work uses the reserved slot.
func TestEnginePauseCheckReadsEffectiveNAtCallTime(t *testing.T) {
	n := 3
	s := syncsched.New(3)
	check := bindEnginePauseCheck(func() int { return n }, s)
	s.RequestSoftPause()
	if check() {
		t.Fatal("effective N>=2 must not follow a live soft-pause")
	}
	n = 1
	if !check() {
		t.Fatal("effective N=1 must follow a live soft-pause")
	}
	s.ClearSoftPause()
	if check() {
		t.Fatal("no soft-pause requested, check must be false")
	}
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

// runInitialSync runs the chain in a synctest bubble, so retries wait out the
// real syncRetryBackoff on the bubble's clock.
func runInitialSync(t *testing.T, run func(context.Context, storage.Folder, syncsched.JobKind) ([]string, error)) error {
	t.Helper()
	var err error
	synctest.Test(t, func(t *testing.T) {
		s := syncsched.New(1)
		s.Start(context.Background(), pool.New(3), 0)
		t.Cleanup(s.Stop)
		done := make(chan error, 1)
		enqueueIMAPInitialSync(s, initialSyncFolders(), run, nil, done)
		select {
		case err = <-done:
		case <-time.After(time.Minute):
			t.Fatal("initial sync chain did not finish")
		}
	})
	return err
}

// A list failure is retried on the same folder. The next folder stays queued
// until a try succeeds.
func TestIMAPInitialSyncRetriesStubsBeforeNextFolder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	inboxTries := 0
	err := runInitialSync(t, func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		step := "bodies:" + folder.IMAPPath
		if kind == syncsched.JobListStubs {
			step = "stubs:" + folder.IMAPPath
		}
		mu.Lock()
		order = append(order, step)
		tries := 0
		if folder.IMAPPath == "INBOX" && kind == syncsched.JobListStubs {
			inboxTries++
			tries = inboxTries
		}
		mu.Unlock()
		if folder.IMAPPath == "INBOX" && kind == syncsched.JobListStubs && tries < syncStepAttempts {
			return nil, errors.New("list failed")
		}
		if kind == syncsched.JobListStubs {
			return []string{"m1"}, nil
		}
		if folder.IMAPPath == "INBOX" {
			ids := syncsched.RemoteIDs(ctx)
			if len(ids) != 1 || ids[0] != "m1" {
				t.Errorf("inbox bodies remote ids %v, want [m1]", ids)
			}
		}
		return nil, nil
	})
	if t.Failed() {
		return
	}
	if err != nil {
		t.Fatalf("initial sync chain: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if inboxTries != syncStepAttempts {
		t.Fatalf("inbox stub tries %d, want %d", inboxTries, syncStepAttempts)
	}
	want := []string{
		"stubs:INBOX", "stubs:INBOX", "stubs:INBOX",
		"bodies:INBOX", "stubs:Archive", "bodies:Archive",
	}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// After the stub retries are used up, the folder is failed and the initial sync
// continues with the next folder instead of ending the chain.
func TestIMAPInitialSyncContinuesAfterStubRetriesExhausted(t *testing.T) {
	var mu sync.Mutex
	var order []string
	err := runInitialSync(t, func(_ context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		step := "bodies:" + folder.IMAPPath
		if kind == syncsched.JobListStubs {
			step = "stubs:" + folder.IMAPPath
		}
		mu.Lock()
		order = append(order, step)
		mu.Unlock()
		if folder.IMAPPath == "INBOX" && kind == syncsched.JobListStubs {
			return nil, errors.New("list failed")
		}
		if kind == syncsched.JobListStubs {
			return []string{"a1"}, nil
		}
		return nil, nil
	})
	if t.Failed() {
		return
	}
	if err == nil {
		t.Fatal("exhausted inbox stubs should fail the chain result")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"stubs:INBOX", "stubs:INBOX", "stubs:INBOX", "stubs:Archive", "bodies:Archive"}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// Body failures retry on the same ids before the next folder starts. When the
// retries run out, that folder is failed and the chain continues.
func TestIMAPInitialSyncRetriesBodiesThenContinues(t *testing.T) {
	var mu sync.Mutex
	var order []string
	bodyTries := 0
	err := runInitialSync(t, func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		step := "bodies:" + folder.IMAPPath
		if kind == syncsched.JobListStubs {
			step = "stubs:" + folder.IMAPPath
		}
		mu.Lock()
		order = append(order, step)
		tries := 0
		if folder.IMAPPath == "INBOX" && kind == syncsched.JobFetchBodies {
			bodyTries++
			tries = bodyTries
		}
		mu.Unlock()
		if kind == syncsched.JobListStubs {
			return []string{"m1"}, nil
		}
		if folder.IMAPPath == "INBOX" {
			ids := syncsched.RemoteIDs(ctx)
			if len(ids) != 1 || ids[0] != "m1" {
				t.Errorf("retry remote ids %v, want [m1]", ids)
			}
			if tries < syncStepAttempts {
				return nil, errors.New("fetch failed")
			}
		}
		return nil, nil
	})
	if t.Failed() {
		return
	}
	if err != nil {
		t.Fatalf("a later success should clear the folder: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"stubs:INBOX",
		"bodies:INBOX", "bodies:INBOX", "bodies:INBOX",
		"stubs:Archive", "bodies:Archive",
	}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

func TestIMAPInitialSyncContinuesAfterBodyRetriesExhausted(t *testing.T) {
	var mu sync.Mutex
	var order []string
	err := runInitialSync(t, func(_ context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		step := "bodies:" + folder.IMAPPath
		if kind == syncsched.JobListStubs {
			step = "stubs:" + folder.IMAPPath
		}
		mu.Lock()
		order = append(order, step)
		mu.Unlock()
		if kind == syncsched.JobListStubs {
			return []string{"m1"}, nil
		}
		if folder.IMAPPath == "INBOX" {
			return nil, errors.New("fetch failed")
		}
		return nil, nil
	})
	if t.Failed() {
		return
	}
	if err == nil {
		t.Fatal("exhausted inbox bodies should fail the chain result")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"stubs:INBOX",
		"bodies:INBOX", "bodies:INBOX", "bodies:INBOX",
		"stubs:Archive", "bodies:Archive",
	}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// Soft-pause is not a failed attempt. The body job is requeued and the next
// folder does not start until the resumed job finishes.
func TestIMAPInitialSyncSoftPauseDoesNotAdvanceOrRetryAsFailure(t *testing.T) {
	var mu sync.Mutex
	var order []string
	bodyTries := 0
	err := runInitialSync(t, func(_ context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		step := "bodies:" + folder.IMAPPath
		if kind == syncsched.JobListStubs {
			step = "stubs:" + folder.IMAPPath
		}
		mu.Lock()
		order = append(order, step)
		tries := 0
		if folder.IMAPPath == "INBOX" && kind == syncsched.JobFetchBodies {
			bodyTries++
			tries = bodyTries
		}
		mu.Unlock()
		if kind == syncsched.JobListStubs {
			return []string{"m1"}, nil
		}
		if folder.IMAPPath == "INBOX" && tries == 1 {
			return nil, psync.ErrSoftPaused
		}
		return nil, nil
	})
	if t.Failed() {
		return
	}
	if err != nil {
		t.Fatalf("initial sync chain: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if bodyTries != 2 {
		t.Fatalf("inbox body runs %d, want 2 (pause, then resume)", bodyTries)
	}
	want := []string{
		"stubs:INBOX", "bodies:INBOX", "bodies:INBOX",
		"stubs:Archive", "bodies:Archive",
	}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// A body batch that stores some ids and then fails must retry only the ids
// that are still incomplete. The folder then finishes and the initial sync moves on.
func TestIMAPInitialSyncBodyRetryFetchesOnlyIncomplete(t *testing.T) {
	useFastSyncRetryBackoff(t)
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
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

	s := syncsched.New(accountID)
	s.Start(ctx, pool.New(1), 0)
	t.Cleanup(s.Stop)
	done := make(chan error, 1)
	enqueueIMAPInitialSync(s, []storage.Folder{archive, inbox}, func(jobCtx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		if kind == syncsched.JobListStubs {
			if folder.IMAPPath == "INBOX" {
				return []string{"m1", "m2", "m3"}, nil
			}
			return []string{"a1"}, nil
		}
		if folder.IMAPPath != "INBOX" {
			return nil, nil
		}
		ids := syncsched.RemoteIDs(jobCtx)
		mu.Lock()
		attempts = append(attempts, append([]string(nil), ids...))
		mu.Unlock()
		_, ferr := engine.FetchBodies(jobCtx, folder, ids)
		if ferr == nil {
			return nil, nil
		}
		still, serr := db.RemoteIDsNeedingBody(jobCtx, folder.ID, ids)
		if serr != nil {
			return nil, ferr
		}
		return still, ferr
	}, nil, done)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("folder should complete after the incomplete retry: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("initial sync chain did not finish")
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
	mu      sync.Mutex
	calls   int
	fetched [][]string
}

func (a *partialBodyAdapter) Addr() string { return "imap.example.com:993" }

func (a *partialBodyAdapter) ListMailboxes(context.Context) ([]psync.Mailbox, error) {
	return nil, nil
}

func (a *partialBodyAdapter) ListMessages(context.Context, psync.RemoteMailbox) ([]psync.Header, string, string, error) {
	return nil, "", "", nil
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

func (a *partialBodyAdapter) SetFlags(context.Context, string, string, storage.Flag) error {
	return nil
}

func (a *partialBodyAdapter) Move(context.Context, string, []string, string) error { return nil }

func (a *partialBodyAdapter) Delete(context.Context, string, []string) error { return nil }

func (a *partialBodyAdapter) CreateMailbox(context.Context, string, string) (psync.Mailbox, error) {
	return psync.Mailbox{}, nil
}

func (a *partialBodyAdapter) RenameMailbox(context.Context, string, string) error { return nil }

func (a *partialBodyAdapter) DeleteMailbox(context.Context, string) error { return nil }

// After Inbox, the remaining folders list in parallel on the background slots.
func TestIMAPInitialSyncRunsOtherFoldersInParallelAfterInbox(t *testing.T) {
	folders := []storage.Folder{
		{ID: 1, Name: "INBOX", IMAPPath: "INBOX"},
		{ID: 2, Name: "Sent", IMAPPath: "Sent", Attributes: []string{`\Sent`}},
		{ID: 3, Name: "Archive", IMAPPath: "Archive"},
	}
	s := syncsched.New(1)
	s.Start(context.Background(), pool.New(3), 0)
	t.Cleanup(s.Stop)

	bothStarted := make(chan struct{})
	var mu sync.Mutex
	inFlight := 0
	done := make(chan error, 1)
	enqueueIMAPInitialSync(s, folders, func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		if folder.IMAPPath == "INBOX" || kind != syncsched.JobListStubs {
			return nil, nil
		}
		mu.Lock()
		inFlight++
		if inFlight == 2 {
			close(bothStarted)
		}
		mu.Unlock()
		select {
		case <-bothStarted:
			return nil, nil
		case <-time.After(2 * time.Second):
			return nil, errors.New("other folders did not list in parallel")
		}
	}, nil, done)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// onInboxDone fires after Inbox stubs and bodies, before any other folder starts.
func TestIMAPInitialSyncCallsOnInboxDoneFirst(t *testing.T) {
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	s := syncsched.New(1)
	s.Start(context.Background(), pool.New(3), 0)
	t.Cleanup(s.Stop)
	done := make(chan error, 1)
	enqueueIMAPInitialSync(s, initialSyncFolders(), func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		name := "bodies:"
		if kind == syncsched.JobListStubs {
			name = "stubs:"
		}
		note(name + folder.IMAPPath)
		return []string{"x"}, nil
	}, func() { note("idle") }, done)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := "stubs:INBOX,bodies:INBOX,idle,stubs:Archive,bodies:Archive"
	if strings.Join(order, ",") != want {
		t.Fatalf("order %v, want %s", order, want)
	}
}
