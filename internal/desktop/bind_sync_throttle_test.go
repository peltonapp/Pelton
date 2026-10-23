package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/sync/pool"
	"github.com/peltonapp/Pelton/internal/sync/throttle"
)

func TestIsSyncThrottleError(t *testing.T) {
	for _, tc := range []struct {
		err  string
		want bool
	}{
		{"dial tcp: connection refused", true},
		{"HTTP/1.1 429 Too Many Requests", true},
		{"jmap: status 429", true},
		{"server said: code=429", true},
		{"NO [LIMIT] too many open connections", true},
		{"imap: login: NO [UNAVAILABLE] Too many simultaneous connections. (Failure)", true},
		{"imap: login: NO Maximum number of connections from user+IP exceeded (mail_max_userip_connections=10)", true},
		{"rate limit exceeded", true},
		{"imap: NO [UNAVAILABLE] Try again later", true},
		{"imap: NO [INUSE] Mailbox in use", true},
		{"imap: login: NO [ALERT] Too many simultaneous connections. (Failure)", true},
		{"imap: BAD Request is throttled. Suggested Backoff Time: 40464 milliseconds", true},
		{"imap: NO suggested backoff time:120000 MILLISECONDS", true},
		{"unexpected EOF", false},
		{"imap: NO [UNAVAILABLEX] nope", false},
		{"sync: store message \"14290\": disk full", false},
		{"fetch uid 429 failed: NO bad message", false},
		{"message is 4290 bytes over quota", false},
	} {
		if got := isSyncThrottleError(errors.New(tc.err)); got != tc.want {
			t.Errorf("isSyncThrottleError(%q) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// Two throttle errors in a row halve the effective N and set the ceiling
// there. Successes do not take N above the ceiling until a probe is due;
// then one success raises N by one, never above what the user set.
func TestThrottleHalvesAndRecovers(t *testing.T) {
	p := pool.New(4)
	acc := &accountSessions{pool: p}
	app := &App{
		ctx:          context.Background(),
		log:          slog.New(slog.DiscardHandler),
		sessionPools: map[int64]*accountSessions{1: acc},
	}
	netErr := errors.New("dial tcp: connection refused")
	for i, step := range []struct {
		err   error
		after time.Duration // the throttle's clock moves this far first
		want  int
	}{
		{netErr, 0, 4},
		{netErr, 0, 2},
		{errors.New("NO mailbox does not exist"), 0, 2},
		{nil, 0, 2},
		{nil, 0, 2},
		{nil, 0, 2},
		{nil, 59 * time.Second, 2},
		{nil, time.Second, 3},
		{nil, 0, 3},
	} {
		advanceThrottleClock(acc, step.after)
		app.noteSyncPoolOutcome(1, step.err)
		if got := p.Effective(); got != step.want {
			t.Fatalf("step %d (%v): effective = %d, want %d", i, step.err, got, step.want)
		}
	}
}

// The pool's watchers (scheduler dispatch, session trim) take the
// scheduler and session locks. A resize runs them on no runtime lock, so
// they add no lock-order edges: the throttle, a settings save and
// ensureAccountSync all call SetEffective/SetConfigured unlocked.
func TestPoolResizeRunsOnNoRuntimeLock(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, id)
	var calls, locked int
	stop := rt.pool.Watch(func() {
		calls++
		if !rt.mu.TryLock() {
			locked++
			return
		}
		rt.mu.Unlock()
		if !a.syncsMu.TryLock() {
			locked++
			return
		}
		a.syncsMu.Unlock()
	})
	defer stop()
	netErr := errors.New("dial tcp: connection refused")
	a.noteSyncPoolOutcome(id, netErr)
	a.noteSyncPoolOutcome(id, netErr) // halves
	advanceThrottleClock(rt.shared, time.Hour)
	a.noteSyncPoolOutcome(id, nil) // a probe grows it
	setParallel(t, a, 2)
	a.reconfigureAccountPool(id)
	if _, err := a.ensureAccountSync(a.ctx, id); err != nil {
		t.Fatal(err)
	}
	if calls < 4 {
		t.Fatalf("pool watchers ran %d times, want every resize", calls)
	}
	if locked > 0 {
		t.Fatalf("%d of %d resizes ran under rt.mu or syncsMu", locked, calls)
	}
}

// A server refusing a login for too many connections is not refusing the
// password: the account must not be marked as having a wrong one.
func TestConnectionLimitIsNotALoginRefusal(t *testing.T) {
	err := fmt.Errorf("%w: NO Maximum number of connections from user+IP exceeded", pimap.ErrAuthFailed)
	if loginRefused(err) {
		t.Error("a connection limit counted as a refused password")
	}
	for _, busy := range []string{
		"imap: NO [UNAVAILABLE] Try again later",
		"imap: NO [INUSE] Mailbox in use",
		"imap: NO [LIMIT] Too many connections",
		"imap: NO Request is throttled. Suggested Backoff Time: 120000 milliseconds",
		"imap: NO [LIMIT] Maximum number of connections from user+IP exceeded (mail_max_userip_connections=10)",
	} {
		if loginRefused(fmt.Errorf("%w: %s", pimap.ErrAuthFailed, busy)) {
			t.Errorf("a busy server counted as a refused password: %s", busy)
		}
	}
	if !loginRefused(fmt.Errorf("%w: NO [AUTHENTICATIONFAILED] Invalid credentials", pimap.ErrAuthFailed)) {
		t.Error("a refused password was not recognised")
	}
}

// Dial and login failures are what a server limiting connections answers
// with, so the throttle hears of them even though no task ran.
func TestThrottleCountsFailedDials(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	setParallel(t, a, 4)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		return nil, errors.New("dial tcp 192.0.2.1:993: connect: connection refused")
	}
	account := mustAccount(t, a, id)
	for range syncThrottleFailThreshold {
		if err := a.runNow(a.ctx, account, syncsched.KindSource, 0, func(context.Context, mailClient) error { return nil }); err == nil {
			t.Fatal("runNow succeeded without a connection")
		}
	}
	if got := mustAccountSync(t, a, id).pool.Effective(); got != 2 {
		t.Fatalf("effective N after failed dials = %d, want 2", got)
	}
}

// One refused connection counts once. The dialer counts it, and the list
// that waited for the session reports the same error back: counted there
// too, a single "too many connections" halved N.
func TestRefusedDialCountsOnce(t *testing.T) {
	a := newHoldTestApp(t)
	id, fid := seedHoldAccount(t, a, "a@example.test")
	setParallel(t, a, 4)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		return nil, errors.New("imap: login: NO [UNAVAILABLE] Too many simultaneous connections. (Failure)")
	}
	account := mustAccount(t, a, id)
	folder, err := a.store.GetFolder(a.ctx, fid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.imapListPage(a.ctx, account, *folder, a.stubPager(id)); err == nil {
		t.Fatal("the list succeeded without a connection")
	}
	if got := mustAccountSync(t, a, id).pool.Effective(); got != 4 {
		t.Fatalf("effective N after one refused connection = %d, want 4", got)
	}
}

// The throttle sets N from the value it read under the same lock. A user's
// resize that lands between its decision and its write is not overwritten
// by a value worked out from before it: the user's N stands.
func TestThrottleDoesNotOverwriteAResizeInBetween(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, id)
	netErr := errors.New("dial tcp: connection refused")
	a.noteSyncPoolOutcome(id, netErr)

	var once sync.Once
	afterThrottleStep = func(throttle.Signal) {
		once.Do(func() {
			// The user saves 5 while the throttle is about to halve 4 to 2.
			setParallel(t, a, 5)
			saved := make(chan struct{})
			go func() {
				a.reconfigureAccountPool(id)
				close(saved)
			}()
			select {
			case <-saved:
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
	t.Cleanup(func() { afterThrottleStep = nil })
	a.noteSyncPoolOutcome(id, netErr)
	waitUntil(t, 2*time.Second, "the throttle overwrote the N the user set meanwhile", func() bool {
		return rt.pool.Effective() == 5
	})
}

// The throttle and the settings resize the account's shared pool under one
// account-wide lock, whichever runtime they came through. An outcome of a
// replaced runtime that decided to halve 4 to 2 before a new runtime
// applied the user's 5 does not overwrite the 5.
func TestStaleRuntimesThrottleDoesNotOverwriteANewSetting(t *testing.T) {
	old := stopAccountWorkerTimeout
	stopAccountWorkerTimeout = 20 * time.Millisecond
	t.Cleanup(func() { stopAccountWorkerTimeout = old })
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 4)
	oldRT := mustAccountSync(t, a, id)
	netErr := errors.New("dial tcp: connection refused")
	a.noteSyncPoolOutcome(id, netErr)

	var once sync.Once
	replaced := make(chan *accountSync, 1)
	afterThrottleStep = func(throttle.Signal) {
		once.Do(func() {
			// The old runtime decided 4 -> 2. It is replaced, and the user
			// saves 5 through the new one. The replacement's lookup applies
			// the setting under the resize lock this outcome holds, so it
			// runs beside it.
			a.stopAccountScheduler(id, false)
			go func() {
				rt, err := a.ensureAccountSync(a.ctx, id)
				if err != nil {
					t.Errorf("replacement: %v", err)
				}
				replaced <- rt
			}()
			setParallel(t, a, 5)
			saved := make(chan struct{})
			go func() {
				a.reconfigureAccountPool(id)
				close(saved)
			}()
			select {
			case <-saved:
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
	t.Cleanup(func() { afterThrottleStep = nil })
	a.noteSyncPoolOutcome(id, netErr)
	newRT := recv(t, replaced, 2*time.Second, "no replacement runtime")
	if newRT == nil || newRT == oldRT || newRT.pool != oldRT.pool {
		t.Fatal("no replacement on the same pool")
	}
	waitUntil(t, 2*time.Second, "the replaced runtime's throttle overwrote the N the user set", func() bool {
		return newRT.pool.Effective() == 5
	})
	time.Sleep(50 * time.Millisecond)
	if got := newRT.pool.Effective(); got != 5 {
		t.Fatalf("effective N = %d, want 5", got)
	}
}

// A fast-lane operation's outcome counts toward the throttle like a list's:
// two refusals halve N, and once a probe is due a success grows it.
func TestFastLaneOutcomesReachTheThrottle(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 4)
	account := mustAccount(t, a, id)
	rt := mustAccountSync(t, a, id)
	op := func(err error) {
		t.Helper()
		got := a.runNow(a.ctx, account, syncsched.KindMove, 0, func(context.Context, mailClient) error { return err })
		if !errors.Is(got, err) {
			t.Fatalf("runNow = %v, want %v", got, err)
		}
	}
	limitErr := errors.New("NO [LIMIT] too many open connections")
	op(limitErr)
	op(limitErr)
	if got := rt.pool.Effective(); got != 2 {
		t.Fatalf("effective N after two refused operations = %d, want 2", got)
	}
	advanceThrottleClock(rt.shared, time.Hour)
	op(nil)
	if got := rt.pool.Effective(); got != 3 {
		t.Fatalf("effective N after a good operation with a probe due = %d, want 3", got)
	}
}

// An operation that never ran, or whose caller gave up, says nothing of the
// server; and a unit run outside a task by withIMAPSession is counted by its
// caller (a list, a body fetch), not twice.
func TestFastLaneOutcomesSkipStopsAndSelfReportingUnits(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 4)
	account := mustAccount(t, a, id)
	rt := mustAccountSync(t, a, id)
	for _, err := range []error{syncsched.ErrDropped, syncsched.ErrDropped, context.Canceled, context.Canceled, errAccountSyncHeld, errAccountSyncHeld} {
		a.noteOpOutcome(id, err)
	}
	limitErr := errors.New("NO [LIMIT] too many open connections")
	for range 2 {
		_ = a.withIMAPSession(a.ctx, account, func(mailClient) error { return limitErr })
	}
	if got := rt.pool.Effective(); got != 4 {
		t.Fatalf("effective N = %d, want 4: nothing above is the operation's own outcome", got)
	}
}

// A runtime lookup that read the parallelism before a settings save and
// applies it after does not undo the save: the user's 5 stands, in the pool
// and in the setting the throttle and later saves go by.
func TestRuntimeLookupDoesNotOverwriteANewerSetting(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, id)

	read := make(chan struct{})
	var once sync.Once
	beforeAccountSyncLock = func() { once.Do(func() { close(read) }) }
	t.Cleanup(func() { beforeAccountSyncLock = nil })
	a.syncsMu.Lock()
	looked := make(chan error, 1)
	go func() {
		_, err := a.ensureAccountSync(a.ctx, id)
		looked <- err
	}()
	recv(t, read, 2*time.Second, "the lookup never read the parallelism")
	// The lookup read 4 and waits for syncsMu. The user saves 5.
	setParallel(t, a, 5)
	a.reconfigureAccountPool(id)
	a.syncsMu.Unlock()
	if err := recv(t, looked, 2*time.Second, "the lookup never returned"); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	rt.shared.resizeMu.Lock()
	userN := rt.shared.userN
	rt.shared.resizeMu.Unlock()
	if c, e := rt.pool.Configured(), rt.pool.Effective(); c != 5 || e != 5 || userN != 5 {
		t.Fatalf("after the lookup: configured=%d effective=%d userN=%d, want 5 5 5", c, e, userN)
	}
}
