package desktop

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
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
	idling   int
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
	c.mu.Lock()
	c.idling++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.idling--
		c.mu.Unlock()
	}()
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	return false, nil
}

// parked reports whether a session is waiting in IDLE now.
func (c *countingIdle) parked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.idling > 0
}

func (c *countingIdle) snapshot() (open, maxOpen, connects int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.open, c.maxOpen, c.connects
}

// idlePoolFixture is a hold test app at the given parallel setting with one
// account, its INBOX and a started scheduler. Every session is client.
func idlePoolFixture(t *testing.T, parallel int) (*App, *storage.Account, *countingIdle) {
	t.Helper()
	a := newHoldTestApp(t)
	setParallel(t, a, parallel)
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{
		Email: "idle@example.com", IMAPHost: "imap.example.com", IMAPPort: 993,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := a.store.CreateFolder(a.ctx, &storage.Folder{
		AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX",
	}); err != nil {
		t.Fatalf("create inbox: %v", err)
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	client := &countingIdle{entered: make(chan struct{})}
	useFake(t, a, accountID, &client.fakeIMAP)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	a.ensureAccountSync(a.ctx, account.ID)
	return a, account, client
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

// runIdle starts the account's push watcher (IMAP IDLE on the scheduler).
// Cleanup stops the account's scheduler.
func runIdle(t *testing.T, a *App, account *storage.Account) {
	t.Helper()
	a.protocolFor(*account).watch(a.ctx, *account)
	t.Cleanup(func() { a.stopAccountScheduler(account.ID, false) })
}

// At N=1 the only session works the normal lane; IDLE parks on that same
// session once the lane is empty, without a second login.
func TestIdleAtN1ParksOnTheSessionTheQueueUsed(t *testing.T) {
	a, account, client := idlePoolFixture(t, 1)
	rt := a.accountSync(account.ID)
	hold := make(chan struct{})
	recv(t, signalJob(rt.sched, syncsched.KindListStubs, 7, hold), 2*time.Second, "task never started")
	runIdle(t, a, account)

	time.Sleep(100 * time.Millisecond)
	if client.parked() {
		t.Fatal("IDLE parked while the only session runs a task")
	}
	close(hold)
	recv(t, client.entered, 2*time.Second, "IDLE never parked after the queue drained")
	if open, maxOpen, connects := client.snapshot(); open != 1 || maxOpen != 1 || connects != 1 {
		t.Fatalf("open=%d max=%d connects=%d, want one session reused by IDLE", open, maxOpen, connects)
	}

	// Normal work takes the session back from IDLE, and IDLE returns after.
	ran := signalJob(rt.sched, syncsched.KindBodies, 7, nil)
	recv(t, ran, 2*time.Second, "queued task never got the session IDLE held")
	waitUntil(t, 2*time.Second, "IDLE never came back", client.parked)
	if _, maxOpen, connects := client.snapshot(); maxOpen != 1 || connects != 1 {
		t.Fatalf("max=%d connects=%d, want the one session throughout", maxOpen, connects)
	}
}

// At N=2 IDLE keeps one session and normal work runs on the other; a third
// task waits, so the account never has more than two sessions.
func TestIdleAtN2HoldsOneSessionWhileNormalRuns(t *testing.T) {
	a, account, client := idlePoolFixture(t, 2)
	rt := a.accountSync(account.ID)
	runIdle(t, a, account)
	recv(t, client.entered, 2*time.Second, "IDLE never parked")

	hold := make(chan struct{})
	recv(t, signalJob(rt.sched, syncsched.KindListStubs, 7, hold), 2*time.Second, "normal task did not run beside IDLE")
	second := signalJob(rt.sched, syncsched.KindListStubs, 8, nil)
	noRecv(t, second, 100*time.Millisecond, "a second normal task took IDLE's session")
	if !client.parked() {
		t.Fatal("IDLE left its session for normal work at N=2")
	}
	close(hold)
	recv(t, second, 2*time.Second, "second task never ran")
	if _, maxOpen, _ := client.snapshot(); maxOpen != 2 {
		t.Fatalf("max sessions %d, want 2", maxOpen)
	}
}

// With every session busy, IDLE's session serves the fast lane, then parks
// again. No third session is opened at N=2.
func TestIdleServesFastLaneWhenSessionsBusy(t *testing.T) {
	a, account, client := idlePoolFixture(t, 2)
	rt := a.accountSync(account.ID)
	runIdle(t, a, account)
	recv(t, client.entered, 2*time.Second, "IDLE never parked")
	hold := make(chan struct{})
	defer close(hold)
	recv(t, signalJob(rt.sched, syncsched.KindListStubs, 7, hold), 2*time.Second, "normal task never started")

	if err := a.runNow(a.ctx, *account, syncsched.KindFlags, 0, func(context.Context, mailClient) error { return nil }); err != nil {
		t.Fatalf("fast task: %v", err)
	}
	waitUntil(t, 2*time.Second, "IDLE did not park again after the fast task", client.parked)
	if _, maxOpen, connects := client.snapshot(); maxOpen != 2 || connects != 2 {
		t.Fatalf("max=%d connects=%d, want IDLE's session reused for the fast task", maxOpen, connects)
	}
}

// Mail that reached Inbox while IDLE was away raises no IDLE event, so IDLE
// first queues a new-mail check when its last one is stale. Only an Inbox with
// a cursor gets it (a cheap delta). The check draws no progress line. After a
// fast task IDLE parks again on the same session without a new login.
func TestIdleEntryChecksInboxBeforeWaiting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cursor string
		want   []string
	}{
		// login, the check's own SELECT, IDLE's SELECT, the wait; then after
		// the fast task only IDLE's SELECT and the wait.
		{"cursor", opsIdleCursor, []string{"login", "select INBOX", "select INBOX", "idle", "select INBOX", "idle"}},
		{"no cursor", "", []string{"login", "select INBOX", "idle", "select INBOX", "idle"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, account, _ := idlePoolFixture(t, 1)
			client := &opsIdle{countingIdle: countingIdle{entered: make(chan struct{})}, idling: make(chan struct{}, 4)}
			a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
			setInboxCursor(t, a, account.ID, tc.cursor)
			rec := &progressRecorder{}
			a.emitForTest = rec.hook(account.ID)
			runIdle(t, a, account)

			waitIdle := func(what string) {
				t.Helper()
				select {
				case <-client.idling:
				case <-time.After(2 * time.Second):
					t.Fatalf("IDLE never parked %s; commands %q", what, client.snapshotOps())
				}
			}
			waitIdle("the first time")
			if ev := rec.snapshot(); len(ev) != 0 {
				t.Fatalf("IDLE entry check emitted progress %+v, want none", ev)
			}
			if err := a.runNow(a.ctx, *account, syncsched.KindSource, 0, func(context.Context, mailClient) error { return nil }); err != nil {
				t.Fatal(err)
			}
			waitIdle("after the fast task")
			if got := client.snapshotOps(); !slices.Equal(got, tc.want) {
				t.Fatalf("session commands = %q, want %q", got, tc.want)
			}
		})
	}
}

// IDLE parked at N=1 when the throttle raises N to 2: queued work starts on
// a second session and IDLE stays parked.
func TestIdleKeepsParkingWhenNGrows(t *testing.T) {
	a, account, client := idlePoolFixture(t, 2)
	rt := a.accountSync(account.ID)
	rt.pool.SetEffective(1)
	runIdle(t, a, account)
	recv(t, client.entered, 2*time.Second, "IDLE never parked")
	rt.pool.SetEffective(2)

	recv(t, signalJob(rt.sched, syncsched.KindListStubs, 7, nil), 2*time.Second, "normal task never started after N grew")
	if !client.parked() {
		t.Fatal("IDLE left its session although N grew")
	}
}

// pushIMAP is an Inbox holding one new message that IDLE reports once.
type pushIMAP struct {
	recordingOneMailIMAP
	pushed atomic.Bool
}

func (c *pushIMAP) SupportsIdle() bool { return true }

func (c *pushIMAP) IdleUntil(ctx context.Context) (bool, error) {
	if !c.pushed.Swap(true) {
		return true, nil
	}
	<-ctx.Done()
	return false, nil
}

// A push queues only the new stubs' listing in the fast lane; the bodies go
// to the normal lane, behind the folder's listing.
func TestNewMailPushListsFastBodiesNormal(t *testing.T) {
	a, account, _ := idlePoolFixture(t, 2)
	client := &pushIMAP{recordingOneMailIMAP: recordingOneMailIMAP{fetched: make(chan []imap.UID, 4)}}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	rt := a.accountSync(account.ID)
	inbox, err := a.findInboxFolder(account.ID)
	if err != nil {
		t.Fatal(err)
	}

	task := a.newMailTask(*account, &imapWatch{})
	if !task.Kind.Fast() || task.Kind != syncsched.KindNewMail {
		t.Fatalf("new-mail task kind %v, want the fast new-mail kind", task.Kind)
	}
	// A running list of Inbox holds Inbox bodies, so they stay queued.
	release := holdFolderList(t, rt, inbox.ID)
	if err := task.Run(a.ctx, client); err != nil {
		t.Fatalf("new-mail task: %v", err)
	}
	select {
	case uids := <-client.fetched:
		t.Fatalf("new-mail task fetched bodies %v in the fast lane", uids)
	default:
	}
	if st := rt.sched.Stats(); st.Normal != 1 || st.Fast != 0 {
		t.Fatalf("stats %+v, want the bodies queued in the normal lane", st)
	}
	release()
	if uids := recv(t, client.fetched, 2*time.Second, "bodies never fetched"); len(uids) != 1 || uids[0] != 5 {
		t.Fatalf("fetched %v, want [5]", uids)
	}
}

// End to end: an IDLE push leads to the new message's body.
func TestNewMailPushFetchesTheNewBody(t *testing.T) {
	a, account, _ := idlePoolFixture(t, 2)
	client := &pushIMAP{recordingOneMailIMAP: recordingOneMailIMAP{fetched: make(chan []imap.UID, 4)}}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	runIdle(t, a, account)
	if uids := recv(t, client.fetched, 2*time.Second, "push never led to a body fetch"); len(uids) != 1 || uids[0] != 5 {
		t.Fatalf("fetched %v after the push, want [5]", uids)
	}
}

// The account never has more than N sessions open, with IDLE on and fast and
// normal work arriving at once, at N=1, 2 and 3.
func TestSessionsNeverExceedNWithIdleAndLoad(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			a, account, client := idlePoolFixture(t, n)
			runIdle(t, a, account)
			recv(t, client.entered, 2*time.Second, "IDLE never parked")
			var wg sync.WaitGroup
			for i := range 24 {
				wg.Go(func() {
					work := func(context.Context, mailClient) error {
						time.Sleep(time.Duration(i%4) * time.Millisecond)
						return nil
					}
					var err error
					if i%2 == 0 {
						err = a.runNow(a.ctx, *account, syncsched.KindFlags, 0, work)
					} else {
						err = a.runPooled(a.ctx, *account, poolTask{Kind: syncsched.KindBodies, FolderID: int64(i), Key: fmt.Sprint("b", i), Run: work})
					}
					if err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			if _, maxOpen, _ := client.snapshot(); maxOpen > n {
				t.Fatalf("max sessions open %d, N %d", maxOpen, n)
			}
		})
	}
}

// The scheduler builds the new-mail task between tasks, on its own behalf,
// so building it reads nothing from the store: Inbox is looked up when the
// task runs. (An App without a store would panic on any query.)
func TestNewMailTaskIsBuiltWithoutTheStore(t *testing.T) {
	w := &imapWatch{}
	w.setInbox(7)
	task := (&App{}).newMailTask(storage.Account{ID: 1}, w)
	if task.Kind != syncsched.KindNewMail || task.FolderID != 7 {
		t.Fatalf("task %v on folder %d, want the new-mail check of Inbox 7", task.Kind, task.FolderID)
	}
}

// lateMailIdle reports new mail in the same moment the scheduler wakes it.
type lateMailIdle struct{ fakeIMAP }

func (*lateMailIdle) SupportsIdle() bool { return true }
func (*lateMailIdle) IdleUntil(ctx context.Context) (bool, error) {
	<-ctx.Done()
	return true, nil
}

// IDLE woken for other work still reports mail the server announced
// meanwhile, so the new-mail check runs instead of waiting for the next
// push or timed sync.
func TestParkReportsMailThatCameWithTheWake(t *testing.T) {
	a, account, _ := idlePoolFixture(t, 2)
	w := &imapWatch{}
	w.checked() // fresh: park instead of re-checking at once
	wake := make(chan struct{})
	got := make(chan bool, 1)
	go func() {
		changed, err := a.parkIMAPIdle(a.ctx, *account, &lateMailIdle{}, wake, w)
		if err != nil {
			t.Error(err)
		}
		got <- changed
	}()
	time.Sleep(20 * time.Millisecond)
	close(wake)
	if !recv(t, got, 2*time.Second, "park never returned") {
		t.Fatal("the mail IDLE saw while being woken was dropped")
	}
}

// A stale check reports a change once, not on every park. At N=2 a running
// list of Inbox blocks the new-mail task it queued; IDLE then waits in IDLE
// instead of reporting the same stale check again and again, and the check
// runs once the list ends.
func TestIdleStaleCheckFiresOnceWhileInboxIsListed(t *testing.T) {
	a, account, _ := idlePoolFixture(t, 2)
	client := &opsIdle{countingIdle: countingIdle{entered: make(chan struct{})}, idling: make(chan struct{}, 8)}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	setInboxCursor(t, a, account.ID, opsIdleCursor)
	inbox, err := a.findInboxFolder(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt := a.accountSync(account.ID)
	hold := make(chan struct{})
	recv(t, signalJob(rt.sched, syncsched.KindListStubs, inbox.ID, hold), 2*time.Second, "Inbox list never started")

	var parks, checks atomic.Int64
	push := a.imapPush(*account)
	park, newMail := push.Park, push.NewMail
	push.Park = func(ctx context.Context, c mailClient, wake <-chan struct{}) (bool, error) {
		parks.Add(1)
		return park(ctx, c, wake)
	}
	push.NewMail = func() imapTask {
		task := newMail()
		run := task.Run
		task.Run = func(ctx context.Context, c mailClient) error {
			checks.Add(1)
			return run(ctx, c)
		}
		return task
	}
	rt.sched.EnablePush(push)
	t.Cleanup(func() { a.stopAccountScheduler(account.ID, false) })

	select {
	case <-client.idling:
	case <-time.After(2 * time.Second):
		t.Fatalf("IDLE never waited in IDLE while Inbox was listed: %d parks", parks.Load())
	}
	time.Sleep(100 * time.Millisecond)
	if n := parks.Load(); n > 2 {
		t.Fatalf("%d parks while Inbox was listed, want at most 2 (one stale check, one IDLE)", n)
	}
	if n := checks.Load(); n != 0 {
		t.Fatalf("new-mail check ran %d times beside the Inbox list", n)
	}

	close(hold)
	waitUntil(t, 2*time.Second, "new-mail check never ran after the list ended", func() bool { return checks.Load() == 1 })
	select {
	case <-client.idling:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE never parked again after the check")
	}
	if n := checks.Load(); n != 1 {
		t.Fatalf("new-mail check ran %d times, want once", n)
	}
	if n := parks.Load(); n > 3 {
		t.Fatalf("%d parks in all, want at most 3", n)
	}
}
