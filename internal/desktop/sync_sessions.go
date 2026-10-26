package desktop

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/sync/pool"
	"github.com/peltonapp/Pelton/internal/sync/throttle"
)

// sessionCheckAfter is how long a pooled session may sit unused before it is
// checked with a NOOP on reuse. A server or a NAT box may have dropped it
// silently, and the task would fail on its first command instead.
var sessionCheckAfter = 30 * time.Second

// accountSessions is an account's connection cap, logged-in sessions and
// the exclusion its schedulers share. It outlives the account's scheduler: a
// worker restart or a profile switch builds a new scheduler on the same
// sessions, so a task the old one left running still counts toward the cap,
// still holds its folder against the new scheduler's tasks (group) and
// still holds its folder-list claim (listing). Only account removal closes
// it.
type accountSessions struct {
	pool     *pool.Pool
	sessions *pool.Sessions[mailClient]
	// group is the running work of the account's schedulers, old and new.
	group syncsched.Group

	mu    sync.Mutex
	views map[*sessionView]struct{}

	// listMu guards listing: the folders with a list in flight; see
	// listFolderOnce. The value is true once another request was coalesced
	// into that list.
	listMu  sync.Mutex
	listing folderClaims

	// bodyClaims are the message ids the account's running body and
	// offline units fetch now, whichever runtime runs them: a folder's body
	// copies and its offline download take from it, so no id is fetched
	// twice.
	bodyClaims syncsched.Claims
	// backlogMu guards backlog: per folder, how many body batches were
	// missing when its last body unit planned, for the generator's run
	// (forgotten as it ends). It sizes the folder's body width (bodyWidth),
	// so a small folder does not open many sessions.
	// It is a leaf lock: the scheduler reads it under its own lock.
	backlogMu sync.Mutex
	backlog   map[int64]int
	// backlogRun is, per folder, the newest body run started (bodyRun) in
	// any of the account's runtimes; runSeq numbers them. A run's end
	// forgets the folder's estimate only while it is still the newest.
	backlogRun map[int64]uint64
	runSeq     uint64

	// resizeMu serializes the throttle's and the settings' resizes of the
	// pool, each from its read of N to its write, whichever runtime they
	// came through. It guards the fields below and is held while the pool's
	// watchers run, which never take it.
	resizeMu sync.Mutex
	// throttle is the adaptive throttle's state: what the account's server
	// is known to take (see package throttle). Memory only: a restart
	// learns it again.
	throttle throttle.State
	// userN is the parallelism the user last set for this account, as
	// applyUserSetting last applied it. A save that leaves it unchanged must not
	// undo a throttle.
	userN int
	// now is the throttle's clock; nil is the real one. Tests set it.
	now func() time.Time
}

// clock is the time the throttle goes by.
func (s *accountSessions) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// setBodyBacklog records that folder had batches body batches missing.
func (s *accountSessions) setBodyBacklog(folderID int64, batches int) {
	s.backlogMu.Lock()
	defer s.backlogMu.Unlock()
	if s.backlog == nil {
		s.backlog = make(map[int64]int)
	}
	s.backlog[folderID] = batches
}

// startBodyRun records a run of folder's body generator queued or joined
// and returns its number for endBodyRun.
func (s *accountSessions) startBodyRun(folderID int64) uint64 {
	s.backlogMu.Lock()
	defer s.backlogMu.Unlock()
	if s.backlogRun == nil {
		s.backlogRun = make(map[int64]uint64)
	}
	s.runSeq++
	s.backlogRun[folderID] = s.runSeq
	return s.runSeq
}

// endBodyRun is the end of run (startBodyRun): if no later run of folder
// started, in any runtime, it forgets the folder's estimate, so the next
// run's first unit runs alone and tells it again, and reports true. The
// check and the forget are one step under backlogMu, so a run started
// meanwhile never loses the estimate its first unit set.
func (s *accountSessions) endBodyRun(folderID int64, run uint64) bool {
	s.backlogMu.Lock()
	defer s.backlogMu.Unlock()
	if s.backlogRun[folderID] != run {
		return false
	}
	delete(s.backlogRun, folderID)
	delete(s.backlog, folderID)
	return true
}

// bodyBacklog is how many body batches folder had missing at its last body
// unit, 1 before the first: a folder's first unit runs alone and tells.
func (s *accountSessions) bodyBacklog(folderID int64) int {
	s.backlogMu.Lock()
	defer s.backlogMu.Unlock()
	if n, ok := s.backlog[folderID]; ok {
		return n
	}
	return 1
}

// throttleCeiling is the largest N the account's server is known to take, 0
// while none is known.
func (s *accountSessions) throttleCeiling() int {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	return s.throttle.Ceiling()
}

// throttleNextProbe is when the throttle may next try one more session,
// zero while no ceiling is known.
func (s *accountSessions) throttleNextProbe() time.Time {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	return s.throttle.NextProbe()
}

// resetThrottle forgets what the throttle learnt of the server and puts the
// account back at the N the user set: the server, or the login it is
// limited by, changed.
func (s *accountSessions) resetThrottle() {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	s.throttle.Reset()
	// The pool's watchers take scheduler and session locks, never resizeMu.
	s.pool.SetEffective(s.pool.Configured())
}

// accountSessionsFor returns the account's sessions, creating them on first
// use with a cap of n.
func (a *App) accountSessionsFor(accountID int64, n int) *accountSessions {
	a.sessionPoolsMu.Lock()
	defer a.sessionPoolsMu.Unlock()
	if s := a.sessionPools[accountID]; s != nil {
		return s
	}
	p := pool.New(n)
	s := &accountSessions{
		pool:     p,
		sessions: pool.NewSessions(p, a.imapDialer(accountID), 0),
		views:    make(map[*sessionView]struct{}),
		userN:    n,
	}
	if a.sessionPools == nil {
		a.sessionPools = make(map[int64]*accountSessions)
	}
	a.sessionPools[accountID] = s
	return s
}

// accountShared is the account's state shared by its runtimes, or nil when
// the account has no sessions yet: then no IMAP work runs.
func (a *App) accountShared(accountID int64) *accountSessions {
	a.sessionPoolsMu.Lock()
	defer a.sessionPoolsMu.Unlock()
	return a.sessionPools[accountID]
}

// closeAccountSessions closes and forgets a removed account's sessions.
func (a *App) closeAccountSessions(accountID int64) {
	a.sessionPoolsMu.Lock()
	s := a.sessionPools[accountID]
	delete(a.sessionPools, accountID)
	a.sessionPoolsMu.Unlock()
	if s != nil {
		s.sessions.Close()
	}
}

// view is the provider of one scheduler over the shared sessions.
func (s *accountSessions) view() *sessionView {
	v := &sessionView{acc: s}
	s.mu.Lock()
	s.views[v] = struct{}{}
	s.mu.Unlock()
	return v
}

// sessionView is one scheduler's share of the account's sessions. Its limit
// is the cap less what the shared sessions spend elsewhere (other schedulers'
// sessions, their dials still in flight, closes still running), so the
// account never has more sessions open than the cap, however many schedulers
// it went through.
type sessionView struct {
	acc *accountSessions
	// out is the sessions this view holds and pending its Gets still under
	// way (dialing, or checking a reused session); closed is set once its
	// scheduler stopped. All are guarded by acc.mu.
	out      int
	pending  int
	closed   bool
	watchers []func()
}

var _ syncsched.Provider[mailClient] = (*sessionView)(nil)

// Get hands out a session, checking one that sat idle past
// sessionCheckAfter with a NOOP first. A session that fails it is closed and
// the next one tried, down to a fresh dial. Its close holds a slot of the
// cap until it is done, so a dial refused for that (at N=1, say) waits for
// the close and tries again, instead of giving the task back for nothing.
func (v *sessionView) Get(ctx context.Context) (mailClient, error) {
	v.acc.mu.Lock()
	v.pending++
	v.acc.mu.Unlock()
	putBack := false
	for {
		c, err := v.acc.sessions.Get(ctx)
		if putBack && errors.Is(err, pool.ErrSessionsFull) {
			putBack = false
			if err = v.acc.sessions.WaitClosed(ctx); err == nil {
				continue
			}
		}
		if err != nil {
			// The shared sessions told the other schedulers already when a
			// dial failed (see pool.Sessions.Watch).
			v.acc.mu.Lock()
			v.pending--
			v.forgetIfDoneLocked()
			v.acc.mu.Unlock()
			return nil, err
		}
		if !stale(c) {
			v.acc.mu.Lock()
			v.pending--
			v.out++
			v.acc.mu.Unlock()
			return c, nil
		}
		markBroken(c)
		v.acc.sessions.Put(c, true)
		putBack = true
	}
}

// stale reports a reused session that does not answer a NOOP.
func stale(c mailClient) bool {
	p, ok := c.(*pooledIMAP)
	if !ok || p.idleSince.IsZero() || time.Since(p.idleSince) < sessionCheckAfter {
		return false
	}
	return p.Noop() != nil
}

// Put returns a session and wakes the other schedulers, whose limit it frees.
// The view lets go of it before the shared sessions take it back, so for a
// moment the session counts twice and the limit reads low, never high.
func (v *sessionView) Put(c mailClient, broken bool) {
	if p, ok := c.(*pooledIMAP); ok {
		p.idleSince = time.Now()
	}
	if broken {
		markBroken(c)
	}
	v.acc.mu.Lock()
	v.out--
	v.forgetIfDoneLocked()
	var wake []func()
	for o := range v.acc.views {
		if o != v {
			wake = append(wake, o.watchers...)
		}
	}
	v.acc.mu.Unlock()
	v.acc.sessions.Put(c, broken)
	for _, fn := range wake {
		fn()
	}
}

// Limit is the cap less the part of it this view cannot have: sessions
// checked out, dialing or closing for anyone else.
func (v *sessionView) Limit() int {
	v.acc.mu.Lock()
	own := v.out + v.pending
	v.acc.mu.Unlock()
	others := max(v.acc.sessions.Busy()-own, 0)
	return max(v.acc.pool.Effective()-others, 0)
}

// Watch runs fn after every change of the cap, after a closing session is
// gone, after a dial failed, and whenever another scheduler returns a
// session.
func (v *sessionView) Watch(fn func()) (stop func()) {
	v.acc.mu.Lock()
	v.watchers = append(v.watchers, fn)
	v.acc.mu.Unlock()
	return v.acc.sessions.Watch(fn)
}

// Close ends the view: its scheduler stopped. The idle sessions close; the
// shared sessions stay for the next scheduler of the account.
func (v *sessionView) Close() {
	v.acc.mu.Lock()
	v.closed = true
	v.watchers = nil
	v.forgetIfDoneLocked()
	v.acc.mu.Unlock()
	v.acc.sessions.CloseIdle()
}

// forgetIfDoneLocked drops a closed view once it holds no session and has
// no Get under way. acc.mu is held.
func (v *sessionView) forgetIfDoneLocked() {
	if v.closed && v.out == 0 && v.pending == 0 {
		delete(v.acc.views, v)
	}
}
