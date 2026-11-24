package desktop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/throttle"
)

// lateHandled reports each time an account's late body outcome was handled.
func lateHandled(t *testing.T) <-chan int64 {
	ch := make(chan int64, 64)
	onLateBodiesHandled = func(id int64) { ch <- id }
	t.Cleanup(func() { onLateBodiesHandled = nil })
	return ch
}

// lateAccount is an account with Inbox only, its scheduler stopped at the
// end of the test.
func lateAccount(t *testing.T) (*App, storage.Account) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	t.Cleanup(func() { a.stopAccountScheduler(id, false) })
	return a, mustAccount(t, a, id)
}

// bodyProgress emits a body progress event of folder, as the sync engine
// does for a fetched batch: it opens the account's progress line again.
func bodyProgress(a *App, account storage.Account, folder storage.Folder) {
	a.emitSyncProgress(account.ID, account.Email, "", a.accountTally(account.ID).record(psync.FolderProgress{Folder: folder, Done: 1, Total: 2}))
}

// blockedBodies is a plan step whose body batches report progress, wait for
// release and then fail with err (nil: succeed).
func blockedBodies(a *App, account storage.Account, release <-chan struct{}, err error) syncStep {
	started := make(chan struct{})
	close(started)
	return gatedBodies(a, account, started, release, err)
}

// gatedBodies is blockedBodies whose batches wait for start first, so their
// progress comes after the pass's closing event.
func gatedBodies(a *App, account storage.Account, start, release <-chan struct{}, err error) syncStep {
	return func(ctx context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
		if kind != syncsched.KindBodies {
			return nil
		}
		select {
		case <-start:
		case <-ctx.Done():
			return ctx.Err()
		}
		bodyProgress(a, account, folder)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return err
	}
}

func syncFailing(t *testing.T, a *App) bool {
	t.Helper()
	states, err := a.store.AccountSyncStates(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return len(states) > 0 && states[0].Failing()
}

// A pass's bodies that fail after a newer pass recorded its outcome do not
// overwrite it: the failure is stale, the newer outcome stands.
func TestLateBodyFailureDoesNotOverwriteNewerOutcome(t *testing.T) {
	handled := lateHandled(t)
	a, account := lateAccount(t)
	release := make(chan struct{})
	step := blockedBodies(a, account, release, errors.New("NO server broke"))
	pass := withPassOutcome(a.ctx) // as syncAccountCtx runs it
	if err := a.runSyncPlan(pass, account, syncsched.ClassAuto, step, nil, false); err != nil {
		t.Fatalf("pass: %v", err)
	}
	a.notePassOutcome(pass, account.ID, nil) // the pass's own outcome (syncAccountCtx)
	a.noteSyncOutcome(account.ID, nil)       // a newer pass's
	close(release)
	recv(t, handled, 10*time.Second, "the late body outcome was never handled")
	if syncFailing(t, a) {
		t.Fatal("an old pass's late body failure overwrote a newer pass's OK")
	}
}

// With only the pass's own outcome recorded, its late body failure is still
// recorded, as when the pass waited for its bodies.
func TestLateBodyFailureAfterOwnOutcomeIsRecorded(t *testing.T) {
	handled := lateHandled(t)
	a, account := lateAccount(t)
	release := make(chan struct{})
	step := blockedBodies(a, account, release, errors.New("NO server broke"))
	pass := withPassOutcome(a.ctx) // as syncAccountCtx runs it
	if err := a.runSyncPlan(pass, account, syncsched.ClassAuto, step, nil, false); err != nil {
		t.Fatalf("pass: %v", err)
	}
	a.notePassOutcome(pass, account.ID, nil) // the pass's own outcome (syncAccountCtx)
	close(release)
	recv(t, handled, 10*time.Second, "the late body outcome was never handled")
	if !syncFailing(t, a) {
		t.Fatal("the pass's late body failure was not recorded")
	}
}

// A pass whose own outcome was not counted (a cancel says nothing about the
// account) leaves no slack for it: one newer pass's OK is then a newer
// outcome, and the old pass's late body failure must not overwrite it.
func TestLateBodyFailureDoesNotOverwriteNewerOutcomeWhenOwnUncounted(t *testing.T) {
	handled := lateHandled(t)
	a, account := lateAccount(t)
	release := make(chan struct{})
	step := blockedBodies(a, account, release, errors.New("NO server broke"))
	pass := withPassOutcome(a.ctx) // as syncAccountCtx runs it
	if err := a.runSyncPlan(pass, account, syncsched.ClassAuto, step, nil, false); err != nil {
		t.Fatalf("pass: %v", err)
	}
	a.notePassOutcome(pass, account.ID, context.Canceled) // the pass's own: not counted
	a.noteSyncOutcome(account.ID, nil)                    // a newer pass's
	close(release)
	recv(t, handled, 10*time.Second, "the late body outcome was never handled")
	if syncFailing(t, a) {
		t.Fatal("an old pass's late body failure overwrote a newer pass's OK")
	}
}

// Passes whose bodies joined one generator share its end: the generator's
// failure is handled once, with one throttle signal and one closing event,
// however many passes queued bodies meanwhile.
func TestLateBodyOutcomeOfJoinedPassesIsAppliedOnce(t *testing.T) {
	handled := lateHandled(t)
	a, account := lateAccount(t)
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(account.ID)
	var steps atomic.Int32
	afterThrottleStep = func(throttle.Signal) { steps.Add(1) }
	t.Cleanup(func() { afterThrottleStep = nil })
	release := make(chan struct{})
	step := blockedBodies(a, account, release, errors.New("NO server broke"))
	const passes = 5
	for i := range passes {
		pass := withPassOutcome(a.ctx)
		if err := a.runSyncPlan(pass, account, syncsched.ClassAuto, step, nil, false); err != nil {
			t.Fatalf("pass %d: %v", i+1, err)
		}
		a.notePassOutcome(pass, account.ID, nil)
	}
	steps.Store(0)
	closed := closings(rec.snapshot())
	close(release)
	recv(t, handled, 10*time.Second, "the late body outcome was never handled")
	noRecv(t, handled, 300*time.Millisecond, "the late body outcome was handled again")
	if !syncFailing(t, a) {
		t.Fatal("the generator's failure was not recorded")
	}
	if n := steps.Load(); n != 1 {
		t.Fatalf("the late failure stepped the throttle %d times, want 1", n)
	}
	if n := closings(rec.snapshot()) - closed; n != 1 {
		t.Fatalf("the bodies' end closed the line %d times, want 1", n)
	}
}

// Bodies that end without reporting progress leave the line as the pass
// closed it: their end sends no second closing event.
func TestBodiesEndDoesNotCloseAClosedLineAgain(t *testing.T) {
	handled := lateHandled(t)
	a, account := lateAccount(t)
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(account.ID)
	release := make(chan struct{})
	step := func(ctx context.Context, _ mailClient, _ storage.Folder, kind syncsched.Kind) error {
		if kind == syncsched.KindBodies {
			<-release
		}
		return nil
	}
	pass := withPassOutcome(a.ctx) // as syncAccountCtx runs it
	if err := a.runSyncPlan(pass, account, syncsched.ClassAuto, step, nil, false); err != nil {
		t.Fatalf("pass: %v", err)
	}
	close(release)
	recv(t, handled, 10*time.Second, "the late body outcome was never handled")
	assertClosedOnce(t, rec.snapshot())
}

// The late end of a removed account's bodies (the scheduler stopped under
// the removal's hold) sends no progress event: the account is going away.
func TestLateBodiesOfRemovedAccountEmitNothing(t *testing.T) {
	handled := lateHandled(t)
	a, account := lateAccount(t)
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(account.ID)
	start := make(chan struct{})
	step := gatedBodies(a, account, start, nil, nil)
	pass := withPassOutcome(a.ctx) // as syncAccountCtx runs it
	if err := a.runSyncPlan(pass, account, syncsched.ClassAuto, step, nil, false); err != nil {
		t.Fatalf("pass: %v", err)
	}
	close(start)
	waitUntil(t, 5*time.Second, "the bodies never reported progress", func() bool {
		evs := rec.snapshot()
		return len(evs) > 0 && evs[len(evs)-1].Phase == SyncPhaseBodies
	})
	before := len(rec.snapshot())
	release := a.holdAccountSync(account.ID)
	defer release()
	a.stopAccountScheduler(account.ID, true)
	recv(t, handled, 10*time.Second, "the late body outcome was never handled")
	for _, e := range rec.snapshot()[before:] {
		if e.Phase != SyncPhaseVerify {
			t.Fatalf("a removed account's late bodies emitted %+v", e)
		}
	}
	if syncFailing(t, a) {
		t.Fatal("a removed account's stopped bodies were recorded as a failure")
	}
}

// A scheduler stopped outside a removal (settings, pause) still closes the
// line the bodies left open.
func TestLateBodiesOfStoppedSchedulerCloseTheLine(t *testing.T) {
	handled := lateHandled(t)
	a, account := lateAccount(t)
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(account.ID)
	// A backlog that never ends: a soft stop lets the running batch end and
	// drops the generator's next one.
	start, release := make(chan struct{}), make(chan struct{})
	step := gatedBodies(a, account, start, release, syncsched.ErrAgain)
	pass := withPassOutcome(a.ctx) // as syncAccountCtx runs it
	if err := a.runSyncPlan(pass, account, syncsched.ClassAuto, step, nil, false); err != nil {
		t.Fatalf("pass: %v", err)
	}
	close(start)
	waitUntil(t, 5*time.Second, "the bodies never reported progress", func() bool {
		evs := rec.snapshot()
		return len(evs) > 0 && evs[len(evs)-1].Phase == SyncPhaseBodies
	})
	close(release)
	a.stopAccountScheduler(account.ID, false)
	recv(t, handled, 10*time.Second, "the late body outcome was never handled")
	evs := rec.snapshot()
	if last := evs[len(evs)-1]; last.Folder != "" {
		t.Fatalf("the line ends with %+v, want a closing event", last)
	}
}
