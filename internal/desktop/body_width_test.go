package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

// wideIMAP is a mailbox shared by every session of a test. FETCH waits
// delay, so the batches of one folder overlap, and it records the FETCHes in
// flight and how often each uid was served. hook, when set, runs first with
// the call's number (from 1) and may fail, panic or block it.
type wideIMAP struct {
	fakeIMAP
	validity atomic.Uint32
	delay    time.Duration
	hook     func(call int, uids []imap.UID) error

	mu             sync.Mutex
	calls          int
	inFlight, peak int
	served         map[imap.UID]int
}

func newWideIMAP(delay time.Duration) *wideIMAP {
	c := &wideIMAP{delay: delay, served: map[imap.UID]int{}}
	c.validity.Store(1)
	return c
}

func (c *wideIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: mailbox, UIDValidity: c.validity.Load(), UIDNext: 1000, NumMessages: 40}, nil
}

func (c *wideIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.inFlight++
	c.peak = max(c.peak, c.inFlight)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
	}()
	if c.hook != nil {
		if err := c.hook(call, uids); err != nil {
			return err
		}
	}
	time.Sleep(c.delay)
	for _, uid := range uids {
		c.mu.Lock()
		c.served[uid]++
		c.mu.Unlock()
		if err := fn(uid, &pimap.Message{UID: uid, Subject: "s", Text: fmt.Sprintf("body bodyword%d", uid)}, nil); err != nil {
			return err
		}
	}
	return nil
}

// meet makes the FETCHes after the first wait until two of them are in
// flight (or 5 s passed), so a test of concurrent batches does not depend
// on how the units' store reads happen to interleave.
func meet() func(call int, uids []imap.UID) error {
	var arrived atomic.Int32
	return func(call int, _ []imap.UID) error {
		if call < 2 || call > 3 {
			return nil
		}
		arrived.Add(1)
		for end := time.Now().Add(5 * time.Second); arrived.Load() < 2 && time.Now().Before(end); {
			time.Sleep(time.Millisecond)
		}
		return nil
	}
}

func (c *wideIMAP) stats() (calls, peak int, served map[imap.UID]int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.peak, maps.Clone(c.served)
}

// wideInbox is an account at parallelism par with an INBOX of n stubs
// (uids 1..n, 1 KB each) whose sessions all use client, and an unlimited
// sync window.
func wideInbox(t *testing.T, client mailClient, n, par int) (*App, storage.Account, storage.Folder, *accountSync) {
	t.Helper()
	a, account, inbox := reconcileInbox(t, client)
	inbox.ID = mustFolderID(t, a, account.ID, "INBOX")
	setParallel(t, a, par)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 0); err != nil {
		t.Fatal(err)
	}
	for uid := 1; uid <= n; uid++ {
		seedStub(t, a, inbox, uid, 1<<10)
	}
	return a, account, inbox, mustAccountSync(t, a, account.ID)
}

// bodiesOnly is a plan step whose lists do nothing and whose body units are
// the real generator's.
func bodiesOnly(a *App, account storage.Account) syncStep {
	return func(ctx context.Context, c mailClient, folder storage.Folder, kind syncsched.Kind) error {
		if kind != syncsched.KindBodies {
			return nil
		}
		return a.imapBodyBatch(withTaskSession(ctx, account.ID, c), account, folder)
	}
}

// runWidePlan runs a plan over inbox on rt with the body width, and
// returns how often onFolderDone ran and its result.
func runWidePlan(t *testing.T, a *App, account storage.Account, inbox storage.Folder, rt *accountSync) (int32, error) {
	t.Helper()
	var folderDone atomic.Int32
	done := make(chan error, 2)
	enqueueSyncPlan(a.ctx, rt.sched, []storage.Folder{inbox}, syncPlan{
		step: bodiesOnly(a, account), bodyWidth: rt.bodyWidth, bodyRun: rt.bodyRun,
		onFolderDone: func() { folderDone.Add(1) }, done: done,
	})
	err := recv(t, done, 20*time.Second, "the plan never ended")
	noRecv(t, done, 50*time.Millisecond, "a second plan result")
	return folderDone.Load(), err
}

func countFinished(t *testing.T, folderID int64) *atomic.Int32 {
	var n atomic.Int32
	onBodiesFinished = func(id int64) {
		if id == folderID {
			n.Add(1)
		}
	}
	t.Cleanup(func() { onBodiesFinished = nil })
	return &n
}

func missingBodies(t *testing.T, a *App, folderID int64) int {
	t.Helper()
	need, err := a.store.RemoteIDsNeedingBodyNewest(a.ctx, folderID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(need)
}

// One folder with 40 missing bodies at N=4: its body generator fetches on
// several sessions at once, never one uid twice, and the plan reports the
// folder done once, after one repair pass.
func TestPlanFetchesOneFolderOnSeveralSessions(t *testing.T) {
	client := newWideIMAP(30 * time.Millisecond)
	client.hook = meet()
	a, account, inbox, rt := wideInbox(t, client, 40, 4)
	finished := countFinished(t, inbox.ID)
	folderDone, err := runWidePlan(t, a, account, inbox, rt)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	_, peak, served := client.stats()
	if peak < 2 {
		t.Fatalf("at most %d FETCH at once, want the folder on several sessions", peak)
	}
	for uid := imap.UID(1); uid <= 40; uid++ {
		if served[uid] != 1 {
			t.Fatalf("uid %d fetched %d times, want once (served %v)", uid, served[uid], served)
		}
	}
	if folderDone != 1 || finished.Load() != 1 {
		t.Fatalf("folder done %d, repair passes %d; want 1 and 1", folderDone, finished.Load())
	}
	if n := missingBodies(t, a, inbox.ID); n != 0 {
		t.Fatalf("%d bodies missing", n)
	}
	if h := rt.bodyClaims().Held(inbox.ID); h != 0 {
		t.Fatalf("%d ids still claimed", h)
	}
}

// The width follows the backlog: a folder with 12 missing bodies (3
// batches) at N=10 runs at most 3 units at once, not 10.
func TestBodyWidthFollowsTheBacklog(t *testing.T) {
	client := newWideIMAP(30 * time.Millisecond)
	a, account, inbox, rt := wideInbox(t, client, 12, 10)
	var units, peak atomic.Int32
	step := func(ctx context.Context, c mailClient, folder storage.Folder, kind syncsched.Kind) error {
		if kind != syncsched.KindBodies {
			return nil
		}
		n := units.Add(1)
		defer units.Add(-1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		return a.imapBodyBatch(withTaskSession(ctx, account.ID, c), account, folder)
	}
	done := make(chan error, 1)
	enqueueSyncPlan(a.ctx, rt.sched, []storage.Folder{inbox}, syncPlan{step: step, bodyWidth: rt.bodyWidth, done: done})
	if err := recv(t, done, 20*time.Second, "the plan never ended"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if p := peak.Load(); p > 3 {
		t.Fatalf("%d body units at once for 3 batches, want at most 3", p)
	}
	if n := missingBodies(t, a, inbox.ID); n != 0 {
		t.Fatalf("%d bodies missing", n)
	}
}

// A unit whose session breaks mid-batch frees its claims: another unit
// fetches those ids, and the plan still ends well.
func TestBrokenBodyUnitReleasesItsClaims(t *testing.T) {
	client := newWideIMAP(20 * time.Millisecond)
	var failed []imap.UID
	client.hook = func(call int, uids []imap.UID) error {
		if call == 2 {
			failed = slices.Clone(uids)
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	a, account, inbox, rt := wideInbox(t, client, 40, 4)
	if _, err := runWidePlan(t, a, account, inbox, rt); err != nil {
		t.Fatalf("plan: %v", err)
	}
	_, _, served := client.stats()
	if len(failed) == 0 {
		t.Fatal("no batch failed")
	}
	for _, uid := range failed {
		if served[uid] != 1 {
			t.Fatalf("uid %d of the failed batch served %d times, want once by another unit", uid, served[uid])
		}
	}
	if n := missingBodies(t, a, inbox.ID); n != 0 {
		t.Fatalf("%d bodies missing", n)
	}
	if h := rt.bodyClaims().Held(inbox.ID); h != 0 {
		t.Fatalf("%d ids still claimed", h)
	}
}

// A unit that panics mid-batch frees its claims (the scheduler recovers it
// and the deferred release runs).
func TestPanickedBodyUnitReleasesItsClaims(t *testing.T) {
	client := newWideIMAP(20 * time.Millisecond)
	client.hook = func(call int, uids []imap.UID) error {
		if call == 2 {
			panic("fetch exploded")
		}
		return nil
	}
	a, account, inbox, rt := wideInbox(t, client, 40, 4)
	_, err := runWidePlan(t, a, account, inbox, rt)
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("plan: %v, want the panic as the folder's failure", err)
	}
	if h := rt.bodyClaims().Held(inbox.ID); h != 0 {
		t.Fatalf("%d ids still claimed after the panic", h)
	}
}

// Ids "load older" adds while a unit runs are kept when the unit prunes the
// ids it found filled: the prune removes only those.
func TestPruneBodyExtraKeepsIdsAddedMeanwhile(t *testing.T) {
	rt := &accountSync{}
	rt.addBodyWant(1, []string{"30", "20"}, false)
	read := rt.bodyWantOf(1).extra                       // a unit reads the extra ids...
	rt.addBodyWant(1, []string{"10"}, false)             // ...another one adds some...
	rt.pruneBodyExtra(1, idsNotIn(read, []string{"20"})) // ...and the first finds 30 filled
	if got := rt.bodyWantOf(1).extra; !slices.Equal(got, []string{"20", "10"}) {
		t.Fatalf("extra %v, want [20 10]", got)
	}
}

// A copy that finds the folder's UIDVALIDITY changed on the server ends the
// generator: the units in flight finish what they selected before, no FETCH
// goes out after the change, and Done comes once, with nil.
func TestGenerationChangeEndsEveryCopy(t *testing.T) {
	client := newWideIMAP(0)
	client.validity.Store(111)
	gate := make(chan struct{})
	var blocked, afterFlip atomic.Int32
	var flipped atomic.Bool
	client.hook = func(call int, uids []imap.UID) error {
		if flipped.Load() {
			afterFlip.Add(1)
		}
		if call > 1 {
			blocked.Add(1)
			<-gate
		}
		return nil
	}
	a, account, inbox, rt := wideInbox(t, client, 40, 3)
	if err := a.store.SetFolderUIDValidity(a.ctx, inbox.ID, 111); err != nil {
		t.Fatal(err)
	}
	var folderDone atomic.Int32
	done := make(chan error, 2)
	enqueueSyncPlan(a.ctx, rt.sched, []storage.Folder{inbox}, syncPlan{
		step: bodiesOnly(a, account), bodyWidth: rt.bodyWidth,
		onFolderDone: func() { folderDone.Add(1) }, done: done,
	})
	waitUntil(t, 10*time.Second, "three units never ran at once", func() bool { return blocked.Load() == 3 })
	client.validity.Store(222)
	flipped.Store(true)
	close(gate)
	if err := recv(t, done, 10*time.Second, "the plan never ended"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	noRecv(t, done, 50*time.Millisecond, "a second plan result")
	if n := afterFlip.Load(); n != 0 {
		t.Fatalf("%d FETCH after the change, want none", n)
	}
	_, _, served := client.stats()
	if got, want := missingBodies(t, a, inbox.ID), 40-len(served); got != want || want == 0 {
		t.Fatalf("%d bodies missing, want %d (all but the ones served before the change)", got, want)
	}
	if folderDone.Load() != 1 {
		t.Fatalf("folder done %d times, want once", folderDone.Load())
	}
}

// Concurrent units of one folder write their batches and feed the search
// index at once (run with -race): every body lands, and the index finds
// every one of them.
func TestConcurrentBodyBatchesIndexEveryBody(t *testing.T) {
	// Opened first, so it closes after the app's background work ended.
	idx, err := openSearchIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	client := newWideIMAP(10 * time.Millisecond)
	client.hook = meet()
	a, account, inbox, rt := wideInbox(t, client, 40, 4)
	a.index = idx
	if _, err := runWidePlan(t, a, account, inbox, rt); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, peak, _ := client.stats(); peak < 2 {
		t.Fatalf("at most %d FETCH at once, want concurrent batches", peak)
	}
	// After the passes the batches started (they hold searchMu in turn).
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	for uid := 1; uid <= 40; uid++ {
		id, err := a.store.MessageIDByRemoteID(a.ctx, inbox.ID, strconv.Itoa(uid))
		if err != nil {
			t.Fatal(err)
		}
		res, err := a.index.Search(search.Query{Text: fmt.Sprintf("bodyword%d", uid), Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(res.Hits, func(h search.Hit) bool { return h.ID == id }) {
			t.Fatalf("uid %d: its body is not in the index (hits %v)", uid, res.Hits)
		}
	}
}

// Two units that both find nothing missing run the folder's repair pass
// once: the second ends while the first repairs.
func TestRepairPassRunsOnceAcrossCopies(t *testing.T) {
	client := newWideIMAP(0)
	a, account, inbox, _ := wideInbox(t, client, 0, 3)
	release := make(chan struct{})
	var calls atomic.Int32
	onBodiesFinished = func(int64) {
		if calls.Add(1) == 1 {
			<-release
		}
	}
	t.Cleanup(func() { onBodiesFinished = nil })
	first := make(chan error, 1)
	go func() { first <- a.imapBodyBatch(a.ctx, account, inbox) }()
	waitUntil(t, 5*time.Second, "the first repair never started", func() bool { return calls.Load() == 1 })
	if err := a.imapBodyBatch(a.ctx, account, inbox); err != nil {
		t.Fatalf("second unit: %v", err)
	}
	close(release)
	if err := recv(t, first, 5*time.Second, "the first unit never ended"); err != nil {
		t.Fatalf("first unit: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("repair pass ran %d times, want once", n)
	}
}

// The repair pass runs once per run of the generator: a copy that comes
// back after another one repaired (it ended with ErrAgain, so the entry
// reopened) and finds nothing missing ends without a second pass. New work
// makes it due again: a new run queued or joined, or bodies missing again.
func TestRepairPassRunsOncePerRun(t *testing.T) {
	client := newWideIMAP(0)
	a, account, inbox, rt := wideInbox(t, client, 0, 3)
	finished := countFinished(t, inbox.ID)
	unit := func(want int32, what string) {
		t.Helper()
		if err := a.imapBodyBatch(a.ctx, account, inbox); err != nil && !errors.Is(err, syncsched.ErrAgain) {
			t.Fatalf("%s: %v", what, err)
		}
		if got := finished.Load(); got != want {
			t.Fatalf("%s: %d repair passes, want %d", what, got, want)
		}
	}
	rt.bodyRun(inbox.ID)
	unit(1, "first unit of the run")
	unit(1, "a copy back after the repair")
	rt.bodyRun(inbox.ID)
	unit(2, "a new run")
	seedStub(t, a, inbox, 1, 1<<10)
	unit(2, "a unit with a body to fetch")
	unit(3, "the unit after it")
}

// The width estimate belongs to one run of the generator: a run that ended
// with batches still missing (it failed) leaves none behind, so the next
// run's first unit runs alone again and tells the width.
func TestBodyWidthEstimateEndsWithItsRun(t *testing.T) {
	client := newWideIMAP(5 * time.Millisecond)
	var failing atomic.Bool
	failing.Store(true)
	client.hook = func(call int, _ []imap.UID) error {
		if failing.Load() && call > 1 {
			return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "fetch failed"}
		}
		return nil
	}
	a, account, inbox, rt := wideInbox(t, client, 40, 4)
	if _, err := runWidePlan(t, a, account, inbox, rt); err == nil {
		t.Fatal("first run ended well, want it to fail with batches missing")
	}
	if missingBodies(t, a, inbox.ID) == 0 {
		t.Fatal("nothing missing after the failed run")
	}
	failing.Store(false)
	// Units that start while the run's first one has not planned yet.
	var beside atomic.Int32
	var first, firstRunning atomic.Bool
	first.Store(true)
	step := func(ctx context.Context, c mailClient, folder storage.Folder, kind syncsched.Kind) error {
		if kind != syncsched.KindBodies {
			return nil
		}
		if first.CompareAndSwap(true, false) {
			firstRunning.Store(true)
			// Give the scheduler time to start more copies beside it.
			time.Sleep(50 * time.Millisecond)
			firstRunning.Store(false)
		} else if firstRunning.Load() {
			beside.Add(1)
		}
		return a.imapBodyBatch(withTaskSession(ctx, account.ID, c), account, folder)
	}
	done := make(chan error, 1)
	enqueueSyncPlan(a.ctx, rt.sched, []storage.Folder{inbox}, syncPlan{step: step, bodyWidth: rt.bodyWidth, bodyRun: rt.bodyRun, done: done})
	if err := recv(t, done, 20*time.Second, "the second run never ended"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := beside.Load(); n != 0 {
		t.Fatalf("%d units beside the second run's first one, want it alone", n)
	}
}

// A repair pass that panics lets the folder's pass go: the next unit runs
// it.
func TestPanickedRepairPassIsDueAgain(t *testing.T) {
	client := newWideIMAP(0)
	a, account, inbox, rt := wideInbox(t, client, 0, 3)
	var calls atomic.Int32
	onBodiesFinished = func(int64) {
		if calls.Add(1) == 1 {
			panic("repair exploded")
		}
	}
	t.Cleanup(func() { onBodiesFinished = nil })
	rt.bodyRun(inbox.ID)
	func() {
		defer func() { _ = recover() }()
		_ = a.imapBodyBatch(a.ctx, account, inbox)
	}()
	if err := a.imapBodyBatch(a.ctx, account, inbox); err != nil {
		t.Fatalf("unit after the panic: %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("repair pass started %d times, want again after the panic", n)
	}
}
