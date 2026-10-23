package pool

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// fakeServer counts the connections open against it and the peak.
type fakeServer struct {
	open, peak, dials atomic.Int32
	failDial          atomic.Bool
	// limit, when set, fails the test the moment open exceeds it.
	limit func() int32
	t     *testing.T
	// closeDelay is how long a LOGOUT takes; the server counts the
	// connection open until it ends.
	closeDelay time.Duration
}

type fakeConn struct {
	id     int32
	srv    *fakeServer
	closed atomic.Bool
}

func (c *fakeConn) Close() error {
	if c.closed.Swap(true) {
		c.srv.t.Errorf("conn %d closed twice", c.id)
		return nil
	}
	if c.srv.closeDelay > 0 {
		time.Sleep(c.srv.closeDelay)
	}
	c.srv.open.Add(-1)
	return nil
}

func (s *fakeServer) dial(ctx context.Context) (*fakeConn, error) {
	if s.failDial.Load() {
		return nil, errors.New("dial refused")
	}
	n := s.open.Add(1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if s.limit != nil && n > s.limit() {
		s.t.Errorf("open connections = %d, over the cap %d", n, s.limit())
	}
	return &fakeConn{id: s.dials.Add(1), srv: s}, nil
}

func newTestSessions(t *testing.T, n int) (*Pool, *Sessions[*fakeConn], *fakeServer) {
	t.Helper()
	srv := &fakeServer{t: t}
	p := New(n)
	s := NewSessions(p, srv.dial, time.Minute)
	t.Cleanup(s.Close)
	return p, s, srv
}

// waitClosed waits for the background closes, failing the test after 5 s.
func waitClosed[C Conn](t *testing.T, s *Sessions[C]) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.WaitClosed(ctx); err != nil {
		t.Fatalf("WaitClosed: %v", err)
	}
}

func closingNow[C Conn](s *Sessions[C]) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

func mustGet(t *testing.T, s *Sessions[*fakeConn]) *fakeConn {
	t.Helper()
	c, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return c
}

func TestSessionsReuseIdleSession(t *testing.T) {
	_, s, srv := newTestSessions(t, 3)
	c1 := mustGet(t, s)
	s.Put(c1, false)
	c2 := mustGet(t, s)
	if c1 != c2 {
		t.Fatal("Get dialed a new session while one was idle")
	}
	if got := srv.dials.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1", got)
	}
	s.Put(c2, false)
	if c2.closed.Load() {
		t.Fatal("a good session was closed on Put")
	}
}

func TestSessionsPutBrokenClosesAndRedials(t *testing.T) {
	_, s, srv := newTestSessions(t, 2)
	c1 := mustGet(t, s)
	s.Put(c1, true)
	waitClosed(t, s)
	if !c1.closed.Load() {
		t.Fatal("broken session left open")
	}
	c2 := mustGet(t, s)
	if c2 == c1 || srv.dials.Load() != 2 {
		t.Fatalf("after a broken session Get must dial again (dials %d)", srv.dials.Load())
	}
	s.Put(c2, false)
}

func TestSessionsIdleTimeoutCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := &fakeServer{t: t}
		s := NewSessions(New(3), srv.dial, time.Minute)
		defer s.Close()
		a, b := mustGet(t, s), mustGet(t, s)
		s.Put(a, false)
		time.Sleep(30 * time.Second)
		s.Put(b, false)
		time.Sleep(31 * time.Second)
		synctest.Wait()
		if !a.closed.Load() || b.closed.Load() {
			t.Fatalf("after 61s: a closed %v (want true), b closed %v (want false)", a.closed.Load(), b.closed.Load())
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if !b.closed.Load() || s.Open() != 0 {
			t.Fatalf("b closed %v, open %d; want every idle session gone", b.closed.Load(), s.Open())
		}
	})
}

func TestSessionsReuseResetsIdleClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := &fakeServer{t: t}
		s := NewSessions(New(1), srv.dial, time.Minute)
		defer s.Close()
		c := mustGet(t, s)
		for range 5 {
			s.Put(c, false)
			time.Sleep(50 * time.Second)
			if got := mustGet(t, s); got != c {
				t.Fatal("session was replaced while in steady use")
			}
		}
		s.Put(c, false)
		if srv.dials.Load() != 1 {
			t.Fatalf("dials = %d, want 1", srv.dials.Load())
		}
	})
}

func TestSessionsShrinkClosesIdleExtras(t *testing.T) {
	tests := []struct {
		name     string
		n, to    int
		held     int // sessions still checked out when N shrinks
		wantIdle int
	}{
		{"3 idle to 1", 3, 1, 0, 1},
		{"3 idle to 2", 3, 2, 0, 2},
		{"1 held 2 idle to 1", 3, 1, 1, 0},
		{"1 held 2 idle to 2", 3, 2, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s, srv := newTestSessions(t, tt.n)
			var conns []*fakeConn
			for range tt.n {
				conns = append(conns, mustGet(t, s))
			}
			for _, c := range conns[tt.held:] {
				s.Put(c, false)
			}
			p.SetEffective(tt.to)
			waitClosed(t, s)
			if got := s.Idle(); got != tt.wantIdle {
				t.Fatalf("idle = %d, want %d", got, tt.wantIdle)
			}
			if got := int(srv.open.Load()); got != tt.held+tt.wantIdle {
				t.Fatalf("open = %d, want %d", got, tt.held+tt.wantIdle)
			}
			for _, c := range conns[:tt.held] {
				s.Put(c, false)
			}
			waitClosed(t, s)
			if got := s.Open(); got > tt.to {
				t.Fatalf("open after returns = %d, over the new cap %d", got, tt.to)
			}
		})
	}
}

func TestSessionsPutOverCapCloses(t *testing.T) {
	p, s, _ := newTestSessions(t, 2)
	a, b := mustGet(t, s), mustGet(t, s)
	p.SetEffective(1)
	s.Put(a, false)
	waitClosed(t, s)
	if !a.closed.Load() {
		t.Fatal("a session over the shrunk cap went idle")
	}
	s.Put(b, false)
	if b.closed.Load() || s.Open() != 1 {
		t.Fatalf("the session within the cap must idle (closed %v, open %d)", b.closed.Load(), s.Open())
	}
}

func TestSessionsGetFull(t *testing.T) {
	_, s, srv := newTestSessions(t, 1)
	c := mustGet(t, s)
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrSessionsFull) {
		t.Fatalf("second Get at N=1: %v, want ErrSessionsFull", err)
	}
	if srv.open.Load() != 1 {
		t.Fatalf("open = %d after a refused Get", srv.open.Load())
	}
	s.Put(c, false)
}

func TestSessionsDialErrorFreesReservation(t *testing.T) {
	_, s, srv := newTestSessions(t, 1)
	srv.failDial.Store(true)
	if _, err := s.Get(context.Background()); err == nil {
		t.Fatal("Get succeeded with a failing dialer")
	}
	srv.failDial.Store(false)
	s.Put(mustGet(t, s), false)
}

// A failed dial frees its slot, so the watchers are told: another caller
// that saw the cap full while the dial was in flight may dial now.
func TestSessionsDialErrorTellsTheWatchers(t *testing.T) {
	_, s, srv := newTestSessions(t, 1)
	woken := make(chan struct{}, 4)
	stop := s.Watch(func() { woken <- struct{}{} })
	defer stop()
	srv.failDial.Store(true)
	if _, err := s.Get(context.Background()); err == nil {
		t.Fatal("Get succeeded with a failing dialer")
	}
	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("no notification after a failed dial")
	}
	if b := s.Busy(); b != 0 {
		t.Fatalf("Busy after a failed dial = %d, want 0", b)
	}
}

func TestSessionsGetCancelledContext(t *testing.T) {
	_, s, srv := newTestSessions(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Get(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get: %v, want context.Canceled", err)
	}
	if srv.dials.Load() != 0 {
		t.Fatal("dialed under a cancelled context")
	}
}

func TestSessionsClose(t *testing.T) {
	_, s, _ := newTestSessions(t, 2)
	held, idle := mustGet(t, s), mustGet(t, s)
	s.Put(idle, false)
	s.Close()
	waitClosed(t, s)
	if !idle.closed.Load() {
		t.Fatal("Close left an idle session open")
	}
	if held.closed.Load() {
		t.Fatal("Close interrupted a checked-out session")
	}
	s.Put(held, false)
	waitClosed(t, s)
	if !held.closed.Load() {
		t.Fatal("Put after Close must close the session")
	}
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrSessionsClosed) {
		t.Fatalf("Get after Close: %v", err)
	}
	s.Close() // idempotent
}

func TestSessionsCloseIdleKeepsCheckedOut(t *testing.T) {
	_, s, _ := newTestSessions(t, 2)
	held, idle := mustGet(t, s), mustGet(t, s)
	s.Put(idle, false)
	s.CloseIdle()
	waitClosed(t, s)
	if !idle.closed.Load() || held.closed.Load() {
		t.Fatal("CloseIdle must close idle sessions only")
	}
	s.Put(held, false)
}

// TestSessionsNeverExceedN drives many slot holders against one account's
// session cache at once. Every holder takes one of N slots, then a session,
// as a scheduler worker does. The fake
// server fails the test the moment it sees more than N connections.
func TestSessionsNeverExceedN(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// A LOGOUT takes a fraction of a millisecond more than a
				// whole number of them, so a close never ends at the same
				// instant a worker (sleeping whole milliseconds) wakes: a
				// refused Get is checked against a closing count that cannot
				// change before the check.
				srv := &fakeServer{t: t, limit: func() int32 { return int32(n) }, closeDelay: 300*time.Millisecond + 333*time.Microsecond}
				p := New(n)
				s := NewSessions(p, srv.dial, 10*time.Second)
				defer s.Close()
				// slots stands in for the scheduler's worker count.
				slots := make(chan struct{}, p.Effective())
				var wg sync.WaitGroup
				for w := range 12 {
					rng := rand.New(rand.NewPCG(uint64(n), uint64(w)))
					wg.Go(func() {
						for range 40 {
							slots <- struct{}{}
							c, err := s.Get(context.Background())
							if errors.Is(err, ErrSessionsFull) {
								// With a slot held only a session still
								// logging out may fill the cap; the
								// scheduler backs off and asks again.
								if closing := closingNow(s); closing == 0 {
									t.Errorf("Get refused with a slot held and nothing closing")
								}
								<-slots
								time.Sleep(100 * time.Millisecond)
								continue
							}
							if err != nil {
								t.Errorf("Get with a slot held: %v", err)
								<-slots
								return
							}
							time.Sleep(time.Duration(rng.IntN(5000)) * time.Millisecond)
							s.Put(c, rng.IntN(8) == 0)
							<-slots
							time.Sleep(time.Duration(rng.IntN(15000)) * time.Millisecond)
						}
					})
				}
				wg.Wait()
				s.Close()
				waitClosed(t, s) // the bubble must not end with a LOGOUT running
				if peak := srv.peak.Load(); peak > int32(n) {
					t.Fatalf("peak open = %d, N = %d", peak, n)
				}
				if s.MaxOpen() > n {
					t.Fatalf("MaxOpen = %d, N = %d", s.MaxOpen(), n)
				}
				if srv.dials.Load() >= 12*40 {
					t.Fatalf("dials = %d: sessions were not reused", srv.dials.Load())
				}
			})
		})
	}
}

// TestSessionsThrottleShrinkConverges lowers N under load. Sessions already
// checked out finish, but the open count drops to the new N once they are
// returned, and no new session is dialed above it.
func TestSessionsThrottleShrinkConverges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var capN atomic.Int32
		capN.Store(3)
		srv := &fakeServer{t: t}
		p := New(3)
		s := NewSessions(p, srv.dial, time.Hour)
		defer s.Close()
		var dialsAfterShrink atomic.Int32
		var shrunk atomic.Bool
		dial := srv.dial
		s.dial = func(ctx context.Context) (*fakeConn, error) {
			c, err := dial(ctx)
			if err == nil && shrunk.Load() {
				dialsAfterShrink.Add(1)
				if o := srv.open.Load(); o > capN.Load() {
					t.Errorf("dialed to %d open after shrink to %d", o, capN.Load())
				}
			}
			return c, err
		}
		// Holders take a slot while fewer than the current N are held, as
		// scheduler workers do; holders from before a shrink keep theirs.
		var mu sync.Mutex
		held := 0
		take := func() bool {
			mu.Lock()
			defer mu.Unlock()
			if held >= p.Effective() {
				return false
			}
			held++
			return true
		}
		var wg sync.WaitGroup
		for w := range 6 {
			wg.Go(func() {
				for i := range 30 {
					for !take() {
						time.Sleep(100 * time.Millisecond)
					}
					c, err := s.Get(context.Background())
					switch {
					case errors.Is(err, ErrSessionsFull):
						// N shrank between take and Get; the scheduler requeues.
					case err != nil:
						t.Error(err)
					default:
						time.Sleep(time.Duration(1+(w+i)%4) * time.Second)
						s.Put(c, false)
					}
					mu.Lock()
					held--
					mu.Unlock()
				}
			})
		}
		time.Sleep(20 * time.Second)
		capN.Store(1)
		shrunk.Store(true)
		p.SetEffective(1)
		time.Sleep(10 * time.Second) // holders from before the shrink are done
		if o := srv.open.Load(); o > 1 {
			t.Fatalf("open = %d after shrink to 1", o)
		}
		wg.Wait()
		if o := s.Open(); o > 1 {
			t.Fatalf("open = %d after load ended, want <= 1", o)
		}
	})
}

// TestSessionsGetChecksCapBeforeReuse: an idle session over a cap that just
// shrank (trim has not run yet) is not handed out while N are checked out.
func TestSessionsGetChecksCapBeforeReuse(t *testing.T) {
	p, s, _ := newTestSessions(t, 2)
	a, b := mustGet(t, s), mustGet(t, s)
	s.Put(b, false)
	// The cap drops to 1; the resize watchers (trim) have not run yet.
	p.mu.Lock()
	p.effective = 1
	p.mu.Unlock()
	if c, err := s.Get(context.Background()); !errors.Is(err, ErrSessionsFull) {
		t.Fatalf("Get with 1 of 1 out handed out %v (err %v), want ErrSessionsFull", c, err)
	}
	s.Put(a, false)
}

// TestSessionsShrinkUnderConcurrentLoad shrinks N while Get and Put run on
// many goroutines. A Get that starts after the cap dropped succeeds only
// while fewer than the new N are held, counting holders from before. The
// shrink's watchers (trim) are held back for a moment, as when SetEffective
// has stored the cap but not yet run them: Gets in that window still find
// idle sessions above the new cap and must not take them.
func TestSessionsShrinkUnderConcurrentLoad(t *testing.T) {
	// Each round is one more chance for a Get to slip past the shrink. 40 is
	// a meaningful default; PELTON_STRESS=1 runs the 200 that hunt for it.
	rounds := 40
	if os.Getenv("PELTON_STRESS") != "" {
		rounds = 200
	}
	for round := range rounds {
		p, s, _ := newTestSessions(t, 3)
		var held, after atomic.Int32
		var stop atomic.Bool
		var wg sync.WaitGroup
		for w := range 8 {
			wg.Go(func() {
				for i := 0; !stop.Load(); i++ {
					post := after.Load() == 1
					c, err := s.Get(context.Background())
					if errors.Is(err, ErrSessionsFull) {
						runtime.Gosched()
						continue
					}
					if err != nil {
						t.Error(err)
						return
					}
					if n := held.Add(1); post && n > 1 {
						t.Errorf("round %d: %d sessions held after a Get that started after the shrink to 1", round, n)
					}
					runtime.Gosched()
					held.Add(-1)
					s.Put(c, (w+i)%7 == 0)
				}
			})
		}
		time.Sleep(time.Millisecond)
		p.mu.Lock()
		p.effective = 1
		p.mu.Unlock()
		after.Store(1)
		time.Sleep(2 * time.Millisecond)
		p.notifyResize() // trim, late
		time.Sleep(time.Millisecond)
		stop.Store(true)
		wg.Wait()
		// Closes run on their own goroutines and count as open until done.
		wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.WaitClosed(wctx)
		cancel()
		if err != nil {
			t.Fatalf("round %d: sessions still closing: %v", round, err)
		}
		if o := s.Open(); o > 1 {
			t.Fatalf("round %d: open = %d after the load ended, want <= 1", round, o)
		}
		s.Close()
		if t.Failed() {
			return
		}
	}
}

// TestSessionsBusyCountsDials: a dial in progress counts as busy, so a view
// over shared sessions does not see it as a free session.
func TestSessionsBusyCountsDials(t *testing.T) {
	srv := &fakeServer{t: t}
	p := New(2)
	release := make(chan struct{})
	dialing := make(chan struct{})
	s := NewSessions(p, func(ctx context.Context) (*fakeConn, error) {
		close(dialing)
		<-release
		return srv.dial(ctx)
	}, time.Minute)
	defer s.Close()
	got := make(chan *fakeConn)
	go func() {
		c, _ := s.Get(context.Background())
		got <- c
	}()
	<-dialing
	if b := s.Busy(); b != 1 {
		t.Fatalf("Busy = %d during a dial, want 1", b)
	}
	close(release)
	c := <-got
	if b := s.Busy(); b != 1 {
		t.Fatalf("Busy = %d with one checked out, want 1", b)
	}
	s.Put(c, false)
	if b := s.Busy(); b != 0 {
		t.Fatalf("Busy = %d with the session idle, want 0", b)
	}
}

// slowConn's Close blocks until release closes, like a LOGOUT to a server
// that does not answer.
type slowConn struct {
	release <-chan struct{}
	closed  atomic.Bool
}

func (c *slowConn) Close() error {
	<-c.release
	c.closed.Store(true)
	return nil
}

// TestSessionsCloseOffTheCallersGoroutine: shrinking N, a broken Put and
// CloseIdle return at once while Close blocks. Until the closes return the
// sessions count toward the cap, so no dial goes past it; when they return,
// Watch fires and a Get dials.
func TestSessionsCloseOffTheCallersGoroutine(t *testing.T) {
	release := make(chan struct{})
	p := New(3)
	s := NewSessions(p, func(context.Context) (*slowConn, error) {
		return &slowConn{release: release}, nil
	}, time.Minute)
	defer s.Close()
	var conns []*slowConn
	for range 3 {
		c, err := s.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	s.Put(conns[0], false)
	s.Put(conns[1], false)
	var freed atomic.Int32
	defer s.Watch(func() { freed.Add(1) })()
	returned := make(chan struct{})
	go func() {
		p.SetEffective(1) // trims the two idle sessions
		s.Put(conns[2], true)
		s.CloseIdle()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("SetEffective / Put / CloseIdle waited for a blocked Close")
	}
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrSessionsFull) {
		t.Fatalf("Get while 3 sessions are still closing: %v, want ErrSessionsFull", err)
	}
	if b := s.Busy(); b != 3 {
		t.Fatalf("Busy = %d while 3 sessions close, want 3", b)
	}
	close(release)
	waitClosed(t, s)
	if freed.Load() == 0 {
		t.Fatal("Watch was not told that the closed sessions freed the cap")
	}
	for i, c := range conns {
		if !c.closed.Load() {
			t.Fatalf("conn %d never closed", i)
		}
	}
	c, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get after the closes: %v", err)
	}
	s.Put(c, false)
}

// TestSessionsHungCloseHoldsOnlyItsSlot: one session whose LOGOUT hangs
// holds its own slot of the cap only. The others close at once, Watch fires
// for them, and a Get may dial into their slots.
func TestSessionsHungCloseHoldsOnlyItsSlot(t *testing.T) {
	hung := make(chan struct{})
	defer close(hung)
	quick := make(chan struct{})
	close(quick)
	releases := []chan struct{}{hung, quick, quick}
	p := New(3)
	s := NewSessions(p, func(context.Context) (*slowConn, error) {
		c := &slowConn{release: releases[0]}
		if len(releases) > 1 {
			releases = releases[1:]
		}
		return c, nil
	}, time.Minute)
	var conns []*slowConn
	for range 3 {
		c, err := s.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	freed := make(chan struct{}, 3)
	defer s.Watch(func() { freed <- struct{}{} })()
	for _, c := range conns { // the hung one is oldest, so first in the batch
		s.Put(c, false)
	}
	s.CloseIdle()
	for range 2 {
		select {
		case <-freed:
		case <-time.After(time.Second):
			t.Fatal("a hung LOGOUT held back the other closes or their notifications")
		}
	}
	if b := s.Busy(); b != 1 {
		t.Fatalf("Busy = %d with one LOGOUT hung, want 1", b)
	}
	for range 2 {
		if _, err := s.Get(context.Background()); err != nil {
			t.Fatalf("Get beside one hung close: %v", err)
		}
	}
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrSessionsFull) {
		t.Fatalf("third Get while the hung session still closes: %v, want ErrSessionsFull", err)
	}
}

// TestSessionsWaitClosedIsBounded: WaitClosed gives up when its context
// ends while a LOGOUT hangs, and returns once the close is done.
func TestSessionsWaitClosedIsBounded(t *testing.T) {
	release := make(chan struct{})
	s := NewSessions(New(1), func(context.Context) (*slowConn, error) {
		return &slowConn{release: release}, nil
	}, time.Minute)
	c, err := s.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Put(c, true)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.WaitClosed(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitClosed with a hung LOGOUT: %v, want DeadlineExceeded", err)
	}
	close(release)
	waitClosed(t, s)
	if !c.closed.Load() {
		t.Fatal("WaitClosed returned before the session closed")
	}
}

// TestSessionsWaitClosedBesideBrokenPuts runs WaitClosed while other
// goroutines start closes (Put broken); -race must see no data race.
func TestSessionsWaitClosedBesideBrokenPuts(t *testing.T) {
	_, s, _ := newTestSessions(t, 3)
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			for range 200 {
				c, err := s.Get(context.Background())
				if err != nil {
					runtime.Gosched()
					continue
				}
				s.Put(c, true)
			}
		})
	}
	wg.Go(func() {
		for range 200 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := s.WaitClosed(ctx); err != nil {
				t.Errorf("WaitClosed: %v", err)
			}
			cancel()
		}
	})
	wg.Wait()
	waitClosed(t, s)
	if b := s.Busy(); b != 0 {
		t.Fatalf("Busy = %d after every close, want 0", b)
	}
}

// TestSessionsMaxOpenCountsClosing: a session still logging out counts in
// MaxOpen as it does in Open.
func TestSessionsMaxOpenCountsClosing(t *testing.T) {
	release := make(chan struct{})
	s := NewSessions(New(3), func(context.Context) (*slowConn, error) {
		return &slowConn{release: release}, nil
	}, time.Minute)
	defer func() {
		close(release)
		waitClosed(t, s)
	}()
	a, err := s.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Put(a, true) // a closes in the background
	if _, err := s.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o, m := s.Open(), s.MaxOpen(); o != 3 || m != 3 {
		t.Fatalf("Open = %d, MaxOpen = %d with 2 out and 1 closing, want 3 and 3", o, m)
	}
}
