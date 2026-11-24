package pool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Conn is one logged-in server session the pool can keep open between jobs.
// Close logs out (best effort) and drops the connection. Sessions always
// calls Close on its own goroutine, never on the caller's and never with a
// lock held, so a slow LOGOUT delays nothing; until Close returns the
// session still counts toward the cap.
type Conn interface {
	Close() error
}

// Dialer opens and logs in a new session.
type Dialer[C Conn] func(ctx context.Context) (C, error)

// DefaultIdleTimeout is how long an unused session stays open. It is well
// under the 30 minutes after which RFC 3501 lets a server drop an idle client.
const DefaultIdleTimeout = 2 * time.Minute

// ErrSessionsFull means Get was called with every session of the cap
// already checked out, usually because the cap shrank just before. It has a
// SessionsFull method, so a scheduler can hand the work back unstarted.
var ErrSessionsFull error = fullError{}

type fullError struct{}

func (fullError) Error() string      { return "pool: all sessions are checked out" }
func (fullError) SessionsFull() bool { return true }

var (
	// ErrSessionsClosed means the account's sessions were closed for good.
	ErrSessionsClosed = errors.New("pool: sessions closed")
)

// Sessions keeps up to the pool's effective N logged-in sessions of one
// account and hands them to jobs one at a time. At a constant N, open
// sessions (checked out, dialing, idle and closing) never exceed it: an idle
// session is reused before a new one is dialed, a dial waits until closing
// sessions are gone, idle extras close when N shrinks, and a returned
// session closes instead of idling when it would be over the cap. After a
// shrink, sessions already checked out stay open until their jobs return
// them, so the new N is reached again as those jobs end; no Get succeeds
// above it meanwhile.
//
// Sessions does not queue callers: Get never waits and returns
// ErrSessionsFull at the cap. The scheduler (desktop/syncsched) runs at
// most Limit workers and backs off after ErrSessionsFull until Watch fires.
type Sessions[C Conn] struct {
	pool        *Pool
	dial        Dialer[C]
	idleTimeout time.Duration

	// mu guards the fields below. The cap (pool.Effective) is read with mu
	// held; the order is always mu, then the Pool's lock.
	mu       sync.Mutex
	idle     []idleConn[C] // oldest first
	out      int           // checked out or dialing
	closing  int           // handed to a closer goroutine, Close not yet returned
	maxOpen  int           // highest open count seen, for tests and logs
	closed   bool
	reaper   *time.Timer
	unwatch  func()
	watchers map[int]func() // told when a closing session is gone
	nextW    int
	// closers counts close goroutines still running (Close or the watchers
	// after it); drained, when a WaitClosed waits, is closed at zero.
	closers int
	drained chan struct{}
}

type idleConn[C Conn] struct {
	c     C
	since time.Time
}

// NewSessions returns the session cache for p. idleTimeout <= 0 uses
// DefaultIdleTimeout.
func NewSessions[C Conn](p *Pool, dial Dialer[C], idleTimeout time.Duration) *Sessions[C] {
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	s := &Sessions[C]{pool: p, dial: dial, idleTimeout: idleTimeout}
	s.unwatch = p.Watch(s.trim)
	return s
}

// Get returns an idle session, newest first, or dials a new one. It never
// waits for a session: with every one of the cap checked out it returns
// ErrSessionsFull.
func (s *Sessions[C]) Get(ctx context.Context) (C, error) {
	var zero C
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return zero, ErrSessionsClosed
	}
	// The cap is read under s.mu, so a shrink that already returned is seen,
	// and it is checked before an idle session is reused too.
	if s.out >= s.pool.Effective() {
		s.mu.Unlock()
		return zero, ErrSessionsFull
	}
	if n := len(s.idle); n > 0 {
		c := s.idle[n-1].c
		s.idle = s.idle[:n-1]
		s.out++
		s.mu.Unlock()
		return c, nil
	}
	// A session still logging out counts: dialing now could put one more
	// connection on the server than the cap.
	if s.out+s.closing >= s.pool.Effective() {
		s.mu.Unlock()
		return zero, ErrSessionsFull
	}
	s.out++
	s.noteOpenLocked()
	s.mu.Unlock()

	c, err := s.dial(ctx)
	if err != nil {
		// The failed dial frees its slot. A caller that read the cap as full
		// while it was in flight (another scheduler on these sessions) is
		// told, as after a close, or it would wait for an unrelated change.
		s.mu.Lock()
		s.out--
		fns := s.watchersLocked()
		s.mu.Unlock()
		for _, fn := range fns {
			fn()
		}
		return zero, err
	}
	return c, nil
}

// watchersLocked is a copy of the watchers, to call after s.mu is released.
func (s *Sessions[C]) watchersLocked() []func() {
	fns := make([]func(), 0, len(s.watchers))
	for _, fn := range s.watchers {
		fns = append(fns, fn)
	}
	return fns
}

// Put returns a session from Get. A broken session (connection error, or a
// job that ended mid-command) is closed and the next Get dials again. A good
// one idles until the timeout, unless keeping it would exceed the cap.
func (s *Sessions[C]) Put(c C, broken bool) {
	s.mu.Lock()
	s.out--
	keep := !broken && !s.closed && s.out+len(s.idle) < s.pool.Effective()
	if keep {
		s.idle = append(s.idle, idleConn[C]{c: c, since: time.Now()})
		s.armReaperLocked()
	} else {
		s.closeLocked([]idleConn[C]{{c: c}})
	}
	s.mu.Unlock()
}

// Limit is the connection cap now: the pool's effective N.
func (s *Sessions[C]) Limit() int { return s.pool.Effective() }

// Watch runs fn after every change of the cap (see Pool.Watch), after a
// closing session is gone and after a dial failed, since a refused Get may
// dial then. fn runs on no
// lock of the Sessions.
func (s *Sessions[C]) Watch(fn func()) (stop func()) {
	stopPool := s.pool.Watch(fn)
	s.mu.Lock()
	if s.watchers == nil {
		s.watchers = make(map[int]func())
	}
	id := s.nextW
	s.nextW++
	s.watchers[id] = fn
	s.mu.Unlock()
	return func() {
		stopPool()
		s.mu.Lock()
		delete(s.watchers, id)
		s.mu.Unlock()
	}
}

// Open is the number of sessions checked out, dialing, idle or closing.
func (s *Sessions[C]) Open() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out + len(s.idle) + s.closing
}

// Busy is the number of sessions checked out, still dialing or still
// closing: the part of the cap that no Get can have now. A provider shared by several schedulers
// computes each one's limit from it, Effective() - (Busy() - own), where own
// counts the caller's sessions including its dials in flight, so a dial of
// another scheduler is not seen as a free session.
func (s *Sessions[C]) Busy() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out + s.closing
}

// Idle is the number of open sessions no job holds.
func (s *Sessions[C]) Idle() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.idle)
}

// MaxOpen is the highest Open count seen so far.
func (s *Sessions[C]) MaxOpen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxOpen
}

// CloseIdle closes every idle session, in the background. Checked-out ones
// are left alone.
func (s *Sessions[C]) CloseIdle() {
	s.mu.Lock()
	s.closeLocked(s.idle)
	s.idle = nil
	s.mu.Unlock()
}

// Close closes the idle sessions, in the background, and makes later Puts
// close theirs. Get fails afterwards. Sessions still checked out are not
// interrupted.
func (s *Sessions[C]) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.closeLocked(s.idle)
	s.idle = nil
	if s.reaper != nil {
		s.reaper.Stop()
	}
	unwatch := s.unwatch
	s.mu.Unlock()
	if unwatch != nil {
		unwatch()
	}
}

// trim closes idle sessions over the cap and those idle past the timeout.
// It runs on SetEffective's goroutine (a Pool watcher), so it only hands
// them to a closer goroutine and never waits for a LOGOUT.
func (s *Sessions[C]) trim() {
	now := time.Now()
	s.mu.Lock()
	limit := s.pool.Effective()
	var drop []idleConn[C]
	for len(s.idle) > 0 && (s.out+len(s.idle) > limit || now.Sub(s.idle[0].since) >= s.idleTimeout) {
		drop = append(drop, s.idle[0])
		s.idle = s.idle[1:]
	}
	if len(s.idle) > 0 {
		s.armReaperLocked()
	}
	s.closeLocked(drop)
	s.mu.Unlock()
}

// armReaperLocked schedules trim for when the oldest idle session expires.
func (s *Sessions[C]) armReaperLocked() {
	if s.closed || len(s.idle) == 0 {
		return
	}
	wait := max(s.idleTimeout-time.Since(s.idle[0].since), 0)
	if s.reaper == nil {
		s.reaper = time.AfterFunc(wait, s.trim)
		return
	}
	s.reaper.Reset(wait)
}

// noteOpenLocked records the open count (as Open counts it) after a dial
// was reserved, the only place it grows.
func (s *Sessions[C]) noteOpenLocked() {
	if n := s.out + len(s.idle) + s.closing; n > s.maxOpen {
		s.maxOpen = n
	}
}

// closeLocked closes each conn on a goroutine of its own, so one hung
// LOGOUT holds only its own slot. A conn counts toward the cap (closing)
// until its Close returns; then the watchers are told.
func (s *Sessions[C]) closeLocked(conns []idleConn[C]) {
	s.closing += len(conns)
	s.closers += len(conns)
	for _, ic := range conns {
		go s.closeOne(ic.c)
	}
}

// closeOne closes c, frees its slot and tells the watchers.
func (s *Sessions[C]) closeOne(c C) {
	_ = c.Close()
	s.mu.Lock()
	s.closing--
	fns := s.watchersLocked()
	s.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	s.mu.Lock()
	s.closers--
	if s.closers == 0 && s.drained != nil {
		close(s.drained)
		s.drained = nil
	}
	s.mu.Unlock()
}

// WaitClosed waits until no session is closing: every Close started has
// returned and the watchers it tells have run. It gives up when ctx ends and
// returns ctx.Err(), so it is a bounded wait (tests, app quit). Closes
// started while it waits are waited for too.
func (s *Sessions[C]) WaitClosed(ctx context.Context) error {
	s.mu.Lock()
	if s.closers == 0 {
		s.mu.Unlock()
		return nil
	}
	if s.drained == nil {
		s.drained = make(chan struct{})
	}
	drained := s.drained
	s.mu.Unlock()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
