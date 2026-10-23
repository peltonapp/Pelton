package desktop

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// A session reused after sitting idle is checked with a NOOP first; one the
// server dropped is closed and replaced by a fresh dial.
func TestIdleSessionIsCheckedBeforeReuse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		noop      error
		wantDials int32
	}{
		{"answers", nil, 1},
		{"dropped", errors.New("connection reset"), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := sessionCheckAfter
			sessionCheckAfter = 10 * time.Millisecond
			t.Cleanup(func() { sessionCheckAfter = old })
			a := newHoldTestApp(t)
			id, _ := seedHoldAccount(t, a, "a@example.test")
			var dials atomic.Int32
			a.newIMAPClient = func(pimap.Config) (mailClient, error) {
				dials.Add(1)
				return &fakeIMAP{failNoop: tc.noop}, nil
			}
			account := mustAccount(t, a, id)
			run := func() {
				t.Helper()
				if err := a.runNow(a.ctx, account, syncsched.KindSource, 0, func(context.Context, mailClient) error { return nil }); err != nil {
					t.Fatalf("runNow: %v", err)
				}
			}
			run()
			time.Sleep(30 * time.Millisecond)
			run()
			if got := dials.Load(); got != tc.wantDials {
				t.Fatalf("%d dials, want %d", got, tc.wantDials)
			}
		})
	}
}

// A worker restart builds a new scheduler while a task of the old one may
// still hold a session. The new one counts it: at N=1 its task waits for
// that session instead of dialing a second connection.
func TestRestartKeepsTheConnectionCap(t *testing.T) {
	old := stopAccountWorkerTimeout
	stopAccountWorkerTimeout = 20 * time.Millisecond
	t.Cleanup(func() { stopAccountWorkerTimeout = old })
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	setParallel(t, a, 1)
	var dials atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		dials.Add(1)
		return &fakeIMAP{}, nil
	}
	account := mustAccount(t, a, id)
	release := make(chan struct{})
	recv(t, signalJob(mustAccountSync(t, a, id).sched, syncsched.KindBodies, 0, release), 2*time.Second, "old task never started")
	// The stop gives up waiting while the old task still runs.
	a.stopAccountScheduler(id, false)

	ran := make(chan error, 1)
	go func() {
		ran <- a.runNow(a.ctx, account, syncsched.KindSource, 0, func(context.Context, mailClient) error { return nil })
	}()
	noRecv(t, ran, 200*time.Millisecond, "the new scheduler ran beside the old task's session")
	close(release)
	if err := recv(t, ran, 2*time.Second, "the new task never got the freed session"); err != nil {
		t.Fatalf("runNow: %v", err)
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("%d connections, want the one session reused", got)
	}
}

// blockingClose is a session whose Close waits for release, like a LOGOUT
// to a slow server.
type blockingClose struct {
	*fakeIMAP
	release chan struct{}
}

func (b *blockingClose) Close() error {
	<-b.release
	return nil
}

// sharedSessions is an account's sessions with a cap of n over dial, with no
// App behind them.
func sharedSessions(t *testing.T, n int, dial pool.Dialer[mailClient]) *accountSessions {
	t.Helper()
	p := pool.New(n)
	s := &accountSessions{pool: p, sessions: pool.NewSessions(p, dial, 0), views: make(map[*sessionView]struct{})}
	t.Cleanup(s.sessions.Close)
	return s
}

// A session another scheduler is still dialing is not free: the new
// scheduler's limit leaves it out, so it does not ask for a session the pool
// must refuse and spin on the refusals until that dial ends.
func TestSessionViewCountsAnotherSchedulersDial(t *testing.T) {
	dialing, release := make(chan struct{}), make(chan struct{})
	s := sharedSessions(t, 1, func(context.Context) (mailClient, error) {
		close(dialing)
		<-release
		return &fakeIMAP{}, nil
	})
	old, next := s.view(), s.view()
	got := make(chan mailClient, 1)
	go func() {
		c, err := old.Get(context.Background())
		if err != nil {
			t.Errorf("old Get: %v", err)
		}
		got <- c
	}()
	recv(t, dialing, 2*time.Second, "dial never started")
	if n := next.Limit(); n != 0 {
		t.Fatalf("new scheduler's limit during the old one's dial = %d, want 0", n)
	}
	close(release)
	c := recv(t, got, 2*time.Second, "old Get never returned")
	if n := next.Limit(); n != 0 {
		t.Fatalf("limit while the old scheduler holds the session = %d, want 0", n)
	}
	old.Put(c, false)
	if n := next.Limit(); n != 1 {
		t.Fatalf("limit after the session came back = %d, want 1", n)
	}
}

// A session closing in the background still holds its slot. The other
// scheduler sees no free session until the close is done, and is told then.
func TestSessionViewWaitsForAClosingSession(t *testing.T) {
	sess := &blockingClose{fakeIMAP: &fakeIMAP{}, release: make(chan struct{})}
	s := sharedSessions(t, 1, func(context.Context) (mailClient, error) { return sess, nil })
	old, next := s.view(), s.view()
	woken := make(chan struct{}, 8)
	stop := next.Watch(func() { woken <- struct{}{} })
	defer stop()
	c, err := old.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	old.Put(c, true)
	for len(woken) > 0 {
		<-woken
	}
	if n := next.Limit(); n != 0 {
		t.Fatalf("limit while the broken session closes = %d, want 0", n)
	}
	close(sess.release)
	recv(t, woken, 2*time.Second, "no notification once the close was done")
	waitUntil(t, 2*time.Second, "limit back to 1", func() bool { return next.Limit() == 1 })
}

// hangingLogout is a session on a server that never answers LOGOUT: Logout
// returns only once the socket is closed.
type hangingLogout struct {
	*fakeIMAP
	logouts atomic.Int32
	closed  chan struct{}
	once    sync.Once
}

func (h *hangingLogout) Logout() error {
	h.logouts.Add(1)
	<-h.closed
	return errors.New("connection closed")
}

func (h *hangingLogout) Close() error {
	h.once.Do(func() { close(h.closed) })
	return nil
}

// A closing session holds its slot of the cap until Close returns, so Close
// must not wait on the server: a session put back broken is dropped without
// LOGOUT, and a healthy one gets a bounded LOGOUT.
func TestPooledSessionCloseDoesNotWaitOnTheServer(t *testing.T) {
	for _, broken := range []bool{true, false} {
		h := &hangingLogout{fakeIMAP: &fakeIMAP{}, closed: make(chan struct{})}
		p := &pooledIMAP{mailClient: h, untrack: func() { _ = h.Close() }, logoutWait: 50 * time.Millisecond}
		if broken {
			markBroken(p)
		}
		closed := make(chan struct{})
		go func() { _ = p.Close(); close(closed) }()
		recv(t, closed, time.Second, "Close waited on the server")
		select {
		case <-h.closed:
		default:
			t.Fatalf("broken=%v: socket left open", broken)
		}
		if want := map[bool]int32{true: 0, false: 1}[broken]; h.logouts.Load() != want {
			t.Fatalf("broken=%v: %d LOGOUTs, want %d", broken, h.logouts.Load(), want)
		}
	}
}

// At N=1 a reused session that fails its NOOP is closed, and its close
// holds the only slot for a moment. Get waits for that close and dials,
// instead of refusing (ErrSessionsFull) and handing the task back.
func TestStaleSessionAtOneSessionIsReplaced(t *testing.T) {
	old := sessionCheckAfter
	sessionCheckAfter = 10 * time.Millisecond
	t.Cleanup(func() { sessionCheckAfter = old })
	var dials atomic.Int32
	s := sharedSessions(t, 1, func(context.Context) (mailClient, error) {
		dials.Add(1)
		return &pooledIMAP{
			mailClient: &fakeIMAP{failNoop: errors.New("connection reset")},
			// A close that takes a moment, as a socket's does.
			untrack: func() { time.Sleep(50 * time.Millisecond) },
		}, nil
	})
	v := s.view()
	c, err := v.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v.Put(c, false)
	time.Sleep(30 * time.Millisecond)
	c, err = v.Get(context.Background())
	if err != nil {
		t.Fatalf("Get after a stale session: %v, want a fresh one", err)
	}
	v.Put(c, false)
	if n := dials.Load(); n != 2 {
		t.Fatalf("%d dials, want 2", n)
	}
}

// At N=1 a replacement scheduler waits while the old one dials the only
// session. When that dial fails the slot is free again and the replacement
// is told, so its queued task runs at once instead of waiting for some
// unrelated change of the account.
func TestReplacementRunsAfterTheOldSchedulersDialFails(t *testing.T) {
	var dials atomic.Int32
	dialing, fail := make(chan struct{}), make(chan struct{})
	s := sharedSessions(t, 1, func(context.Context) (mailClient, error) {
		if dials.Add(1) == 1 {
			close(dialing)
			<-fail
			return nil, errors.New("connection refused")
		}
		return &fakeIMAP{}, nil
	})
	old := s.view()
	oldErr := make(chan error, 1)
	go func() {
		_, err := old.Get(context.Background())
		oldErr <- err
	}()
	recv(t, dialing, 2*time.Second, "old dial never started")
	old.Close() // the old scheduler stopped; its dial is still in flight

	next := s.view()
	sched := syncsched.New(context.Background(), next, syncsched.Options{})
	t.Cleanup(func() { _ = sched.Abort(context.Background()) })
	ran := make(chan struct{})
	sched.Enqueue(syncsched.Task[mailClient]{Kind: syncsched.KindSource, Run: func(context.Context, mailClient) error {
		close(ran)
		return nil
	}})
	if n := next.Limit(); n != 0 {
		t.Fatalf("replacement's limit during the old dial = %d, want 0", n)
	}
	close(fail)
	if err := recv(t, oldErr, 2*time.Second, "old Get never returned"); err == nil {
		t.Fatal("old dial succeeded, want its error")
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatalf("replacement never ran its task after the old dial failed: limit=%d stats=%+v dials=%d", next.Limit(), sched.Stats(), dials.Load())
	}
	s.mu.Lock()
	_, kept := s.views[old]
	s.mu.Unlock()
	if kept {
		t.Fatal("a closed view whose last Get failed is still registered")
	}
}
