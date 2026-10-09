package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// countingIdle is one IMAP connection whose open count the IDLE pool tests
// watch. Login and Logout move the count; a second overlapping Login means
// IDLE opened a session the pool was not tracking.
type countingIdle struct {
	fakeIMAP
	mu       sync.Mutex
	open     int
	maxOpen  int
	connects int
	entered  chan struct{}
	once     sync.Once
}

func (c *countingIdle) Login() error {
	c.mu.Lock()
	c.connects++
	c.open++
	if c.open > c.maxOpen {
		c.maxOpen = c.open
	}
	c.mu.Unlock()
	return nil
}

func (c *countingIdle) Logout() error {
	c.mu.Lock()
	if c.open > 0 {
		c.open--
	}
	c.mu.Unlock()
	return c.fakeIMAP.Logout()
}

func (c *countingIdle) SupportsIdle() bool { return true }

func (c *countingIdle) IdleUntil(ctx context.Context) (bool, error) {
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	return false, nil
}

func (c *countingIdle) snapshot() (open, maxOpen, connects int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.open, c.maxOpen, c.connects
}

func idlePoolFixture(t *testing.T, parallel int) (*App, *storage.Account, *countingIdle, context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		cancel()
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		cancel()
		t.Fatalf("migrate: %v", err)
	}
	if err := db.SetInt(ctx, settingSyncMaxParallel, parallel); err != nil {
		cancel()
		t.Fatalf("set parallel: %v", err)
	}
	accountID, err := db.CreateAccount(ctx, &storage.Account{
		Email: "idle@example.com", IMAPHost: "imap.example.com", IMAPPort: 993,
	})
	if err != nil {
		cancel()
		t.Fatalf("create account: %v", err)
	}
	if _, err := db.CreateFolder(ctx, &storage.Folder{
		AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX",
	}); err != nil {
		cancel()
		t.Fatalf("create inbox: %v", err)
	}
	account, err := db.GetAccount(ctx, accountID)
	if err != nil {
		cancel()
		t.Fatalf("get account: %v", err)
	}
	a := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}
	client := &countingIdle{entered: make(chan struct{})}
	useFake(t, a, accountID, &client.fakeIMAP)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	a.ensureAccountScheduler(ctx, account.ID)
	t.Cleanup(cancel)
	return a, account, client, ctx, cancel
}

// At N=1 IDLE must not connect while background holds the only slot, must
// hold that slot itself once it connects, and must log out before a queued
// background job can take it.
func TestIdleAtN1ReusesTheOnlyPoolSlot(t *testing.T) {
	a, account, client, ctx, cancel := idlePoolFixture(t, 1)
	rt := a.accountSync(account.ID)
	if err := rt.pool.Acquire(ctx, pool.Background); err != nil {
		t.Fatalf("hold background slot: %v", err)
	}
	held := true
	releaseBG := func() {
		if held {
			held = false
			rt.pool.Release(pool.Background)
		}
	}
	defer releaseBG()

	idleDone := make(chan struct{})
	go func() {
		defer close(idleDone)
		_ = a.idleSession(ctx, *account)
	}()
	t.Cleanup(func() {
		cancel()
		<-idleDone
		a.stopAccountScheduler(account.ID)
	})

	time.Sleep(200 * time.Millisecond)
	if _, _, connects := client.snapshot(); connects != 0 {
		t.Fatalf("IDLE connected %d times while the only slot was already held", connects)
	}

	releaseBG()
	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE never parked after the background slot was released")
	}
	if open, maxOpen, _ := client.snapshot(); open != 1 || maxOpen != 1 {
		t.Fatalf("while idling open=%d max=%d, want 1 and 1", open, maxOpen)
	}

	liveCtx, liveCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	err := rt.pool.Acquire(liveCtx, pool.Live)
	liveCancel()
	if err == nil {
		rt.pool.Release(pool.Live)
		t.Fatal("a second live session was checked out while IDLE held N=1")
	}

	hold := make(chan struct{})
	gotSlot := make(chan struct{})
	rt.sched.Enqueue(syncsched.Job{
		Priority: syncsched.PriorityBackgroundStubs,
		Kind:     syncsched.JobListStubs,
		Run: func(context.Context) error {
			close(gotSlot)
			<-hold
			return nil
		},
	})
	select {
	case <-gotSlot:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never received the slot IDLE was holding")
	}
	if open, maxOpen, _ := client.snapshot(); open != 0 || maxOpen != 1 {
		t.Fatalf("during background open=%d max=%d, want 0 and 1", open, maxOpen)
	}
	close(hold)
}

// At N=2 IDLE holds the live slot and background still runs on the other
// slot. The two together fill the pool.
func TestIdleAtN2HoldsLiveSlotWhileBackgroundRuns(t *testing.T) {
	a, account, client, ctx, cancel := idlePoolFixture(t, 2)
	rt := a.accountSync(account.ID)

	idleDone := make(chan struct{})
	go func() {
		defer close(idleDone)
		_ = a.idleSession(ctx, *account)
	}()
	t.Cleanup(func() {
		cancel()
		<-idleDone
		a.stopAccountScheduler(account.ID)
	})

	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE never parked")
	}

	done := make(chan struct{})
	var openDuring int
	var liveAcquired bool
	rt.sched.Enqueue(syncsched.Job{
		Priority: syncsched.PriorityBackgroundStubs,
		Kind:     syncsched.JobListStubs,
		Run: func(context.Context) error {
			openDuring, _, _ = client.snapshot()
			liveCtx, liveCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			err := rt.pool.Acquire(liveCtx, pool.Live)
			liveCancel()
			if err == nil {
				liveAcquired = true
				rt.pool.Release(pool.Live)
			}
			close(done)
			return nil
		},
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background job did not run beside IDLE")
	}
	if openDuring != 1 {
		t.Fatalf("IDLE session open count during background = %d, want 1", openDuring)
	}
	if liveAcquired {
		t.Fatal("live acquire succeeded while IDLE and background already filled N=2")
	}
	if _, maxOpen, _ := client.snapshot(); maxOpen != 1 {
		t.Fatalf("max concurrent IMAP sessions %d, want 1 (background job did not open its own)", maxOpen)
	}
}

// At N=1 a running background chunk must not make IDLE wait out the whole
// campaign. IDLE soft-pauses that chunk, then parks while later background
// work is still queued.
func TestIdleAtN1SoftPausesBackgroundAndParks(t *testing.T) {
	a, account, client, ctx, cancel := idlePoolFixture(t, 1)
	rt := a.accountSync(account.ID)

	chunkStarted := make(chan struct{})
	sawPause := make(chan struct{})
	rt.sched.Enqueue(syncsched.Job{
		Priority: syncsched.PriorityBackgroundBody,
		Kind:     syncsched.JobFetchBodies,
		Run: func(jobCtx context.Context) error {
			select {
			case <-chunkStarted:
			default:
				close(chunkStarted)
			}
			for {
				if rt.sched.SoftPauseRequested() {
					select {
					case <-sawPause:
					default:
						close(sawPause)
					}
					return psync.ErrSoftPaused
				}
				select {
				case <-jobCtx.Done():
					return jobCtx.Err()
				case <-time.After(15 * time.Millisecond):
				}
			}
		},
	})
	select {
	case <-chunkStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("background chunk never started")
	}

	laterStarted := make(chan struct{})
	rt.sched.Enqueue(syncsched.Job{
		Priority: syncsched.PriorityBackgroundBody,
		Kind:     syncsched.JobFetchBodies,
		Run: func(context.Context) error {
			close(laterStarted)
			return nil
		},
	})

	idleDone := make(chan struct{})
	go func() {
		defer close(idleDone)
		_ = a.idleSession(ctx, *account)
	}()
	t.Cleanup(func() {
		cancel()
		<-idleDone
		a.stopAccountScheduler(account.ID)
	})

	select {
	case <-sawPause:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE did not soft-pause the in-flight background chunk")
	}
	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE did not park while background work was still queued")
	}
	select {
	case <-laterStarted:
		t.Fatal("later background job ran before IDLE parked")
	default:
	}
	if !rt.sched.SoftPauseRequested() {
		t.Fatal("soft-pause should hold the queue while IDLE has the slot")
	}
	if !rt.sched.BackgroundBusy() {
		t.Fatal("background campaign should still be queued when IDLE parks")
	}
}

// opsIdle records the order of the session commands IDLE's tests care about:
// login, every SELECT and each IDLE wait.
type opsIdle struct {
	countingIdle
	opsMu  sync.Mutex
	ops    []string
	idling chan struct{}
}

func (c *opsIdle) record(op string) {
	c.opsMu.Lock()
	c.ops = append(c.ops, op)
	c.opsMu.Unlock()
}

func (c *opsIdle) Login() error {
	c.record("login")
	return c.countingIdle.Login()
}

// Select reports an empty CONDSTORE mailbox that matches opsIdleCursor, so a
// delta from that cursor finds nothing changed.
func (c *opsIdle) Select(mailbox string) (*pimap.Mailbox, error) {
	c.record("select " + mailbox)
	return &pimap.Mailbox{Name: mailbox, UIDValidity: 1, HighestModSeq: 1, UIDNext: 1}, nil
}

// opsIdleCursor is the Inbox cursor that opsIdle's mailbox matches.
const opsIdleCursor = "v1:1:1:1:0"

func (c *opsIdle) IdleUntil(ctx context.Context) (bool, error) {
	c.record("idle")
	c.idling <- struct{}{}
	<-ctx.Done()
	return false, nil
}

func (c *opsIdle) snapshotOps() []string {
	c.opsMu.Lock()
	defer c.opsMu.Unlock()
	return append([]string(nil), c.ops...)
}

// Mail that reached Inbox while IDLE had given its session away raises no
// IDLE event once IDLE is back, so every IDLE entry first syncs Inbox on the
// session it just selected. Only an Inbox with a stored cursor gets that
// resync: it is a cheap delta. Without one it would be a full flags fetch, and
// IDLE's own events and the timed sync cover that Inbox.
func TestIdleEntrySyncsInboxBeforeWaiting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cursor string
		entry  []string
	}{
		// login, the IDLE SELECT, the Inbox sync's own SELECT on the same
		// session, and only then the wait.
		{"cursor", opsIdleCursor, []string{"login", "select INBOX", "select INBOX", "idle"}},
		{"no cursor", "", []string{"login", "select INBOX", "idle"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, account, _, ctx, cancel := idlePoolFixture(t, 1)
			client := &opsIdle{countingIdle: countingIdle{entered: make(chan struct{})}, idling: make(chan struct{}, 4)}
			a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
			setInboxCursor(t, a, account.ID, tc.cursor)
			rt := a.accountSync(account.ID)

			idleDone := make(chan struct{})
			go func() {
				defer close(idleDone)
				_ = a.idleSession(ctx, *account)
			}()
			t.Cleanup(func() {
				cancel()
				<-idleDone
				a.stopAccountScheduler(account.ID)
			})

			waitIdle := func(what string) {
				t.Helper()
				select {
				case <-client.idling:
				case <-time.After(2 * time.Second):
					t.Fatalf("IDLE never parked %s; commands %q", what, client.snapshotOps())
				}
			}
			waitIdle("the first time")
			// A live job needs the only slot, so IDLE yields and comes back after it.
			liveRan := make(chan struct{})
			rt.sched.Enqueue(syncsched.Job{
				Priority: syncsched.PriorityLive,
				Kind:     syncsched.JobManualSync,
				Run: func(context.Context) error {
					close(liveRan)
					return nil
				},
			})
			select {
			case <-liveRan:
			case <-time.After(2 * time.Second):
				t.Fatal("live job never ran")
			}
			waitIdle("after the live job")

			want := append(append([]string(nil), tc.entry...), tc.entry...)
			if got := client.snapshotOps(); !slices.Equal(got, want) {
				t.Fatalf("session commands = %q, want %q", got, want)
			}
		})
	}
}

// setInboxCursor stores cursor as the account's Inbox delta cursor.
func setInboxCursor(t *testing.T, a *App, accountID int64, cursor string) {
	t.Helper()
	inbox, err := a.findInboxFolder(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetFolderStateToken(a.ctx, inbox.ID, cursor); err != nil {
		t.Fatal(err)
	}
}

// The progress line is shared with whatever sync is running. IDLE's entry
// resync is not a counted run, so it must neither draw nor close that line.
func TestIdleEntryResyncEmitsNoProgress(t *testing.T) {
	a, account, _, ctx, cancel := idlePoolFixture(t, 1)
	client := &opsIdle{countingIdle: countingIdle{entered: make(chan struct{})}, idling: make(chan struct{}, 4)}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	setInboxCursor(t, a, account.ID, opsIdleCursor)
	var mu sync.Mutex
	var events []SyncProgressEvent
	a.syncProgressEmitForTest = func(e SyncProgressEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	idleDone := make(chan struct{})
	go func() {
		defer close(idleDone)
		_ = a.idleSession(ctx, *account)
	}()
	t.Cleanup(func() {
		cancel()
		<-idleDone
		a.stopAccountScheduler(account.ID)
	})
	select {
	case <-client.idling:
	case <-time.After(2 * time.Second):
		t.Fatalf("IDLE never parked; commands %q", client.snapshotOps())
	}
	if ops := client.snapshotOps(); len(ops) < 3 || ops[2] != "select INBOX" {
		t.Fatalf("session commands = %q, want the entry resync", ops)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 0 {
		t.Fatalf("IDLE entry resync emitted progress %+v, want none", events)
	}
}

// At effective N=1 IDLE would trade the only session with every background
// chunk of the first pass, so it starts only after the whole pass. At N>=2 it
// starts as soon as Inbox is in.
func TestIdleStartsAfterInboxOnlyAtNTwo(t *testing.T) {
	for _, tc := range []struct {
		parallel int
		want     bool
	}{{1, false}, {2, true}} {
		a, account, _, _, _ := idlePoolFixture(t, tc.parallel)
		t.Cleanup(func() { a.stopAccountScheduler(account.ID) })
		started := false
		a.idleAfterInbox(account.ID, func() { started = true })()
		if started != tc.want {
			t.Fatalf("N=%d: IDLE started after Inbox = %v, want %v", tc.parallel, started, tc.want)
		}
	}
}

// IDLE that checked out at effective N=1 holds a soft-pause. If the throttle
// then raises N to 2, background work must still make IDLE yield that pause,
// or the queue stays held while IDLE parks.
func TestIdleYieldsPauseAfterNRisesWhileIdling(t *testing.T) {
	a, account, client, ctx, cancel := idlePoolFixture(t, 2)
	rt := a.accountSync(account.ID)
	rt.pool.SetEffective(1)

	idleDone := make(chan struct{})
	go func() {
		defer close(idleDone)
		_ = a.idleSession(ctx, *account)
	}()
	t.Cleanup(func() {
		cancel()
		<-idleDone
		a.stopAccountScheduler(account.ID)
	})

	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE never parked")
	}
	if !rt.sched.SoftPauseRequested() {
		t.Fatal("IDLE at N=1 should hold a soft-pause")
	}
	rt.pool.SetEffective(2)

	started := make(chan struct{})
	rt.sched.Enqueue(syncsched.Job{
		Priority: syncsched.PriorityBackgroundStubs,
		Kind:     syncsched.JobListStubs,
		Run: func(context.Context) error {
			close(started)
			return nil
		},
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never started while IDLE held the soft-pause it took at N=1")
	}
}
