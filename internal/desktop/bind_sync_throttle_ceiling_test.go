package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// limitServer is a fake IMAP server that takes at most limit connections of
// the account at once and refuses a LOGIN above it with refusal.
type limitServer struct {
	mu      sync.Mutex
	limit   int
	live    int
	refusal string
}

func (s *limitServer) login() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live >= s.limit {
		return errors.New(s.refusal)
	}
	s.live++
	return nil
}

func (s *limitServer) setLimit(n int) {
	s.mu.Lock()
	s.limit = n
	s.mu.Unlock()
}

// limitConn is one session of a limitServer. The pool only closes it.
type limitConn struct {
	mailClient
	srv *limitServer
}

func (c *limitConn) Close() error {
	c.srv.mu.Lock()
	c.srv.live--
	c.srv.mu.Unlock()
	return nil
}

// ceilingHarness drives one account's throttle on a fake clock: a pool of
// sessions over a limitServer, dialed the way imapDialer dials (a refusal is
// counted where it happens, a login is reported), and a steady load that
// wants every session the cap allows, one round per simulated second.
type ceilingHarness struct {
	t     *testing.T
	app   *App
	acc   *accountSessions
	srv   *limitServer
	start time.Time
	now   time.Time
	// refused is when each refused LOGIN happened, since start.
	refused []time.Duration
	// raises is each time effective N changed: when, and to what.
	changes []nChange
}

type nChange struct {
	at time.Duration
	n  int
}

const accID int64 = 1

func newCeilingHarness(t *testing.T, configured, limit int, refusal string) *ceilingHarness {
	t.Helper()
	h := &ceilingHarness{t: t, srv: &limitServer{limit: limit, refusal: refusal}}
	h.start = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	h.now = h.start
	p := pool.New(configured)
	h.acc = &accountSessions{pool: p, userN: configured, now: func() time.Time { return h.now }}
	h.app = &App{
		ctx:          context.Background(),
		log:          slog.New(slog.DiscardHandler),
		sessionPools: map[int64]*accountSessions{accID: h.acc},
	}
	h.acc.sessions = pool.NewSessions(p, func(context.Context) (mailClient, error) {
		if err := h.srv.login(); err != nil {
			h.refused = append(h.refused, h.now.Sub(h.start))
			h.app.noteSyncPoolDial(accID, err)
			return nil, throttleCounted{err}
		}
		h.app.noteSyncPoolLogin(accID)
		return &limitConn{srv: h.srv}, nil
	}, 100*time.Hour)
	last := configured
	p.Watch(func() {
		if n := p.Effective(); n != last {
			last = n
			h.changes = append(h.changes, nChange{h.now.Sub(h.start), n})
		}
	})
	t.Cleanup(h.acc.sessions.Close)
	return h
}

// round takes every session the cap allows, runs one good task on each and
// returns them, then lets a second pass.
func (h *ceilingHarness) round() {
	var got []mailClient
	for len(got) < h.acc.pool.Effective() {
		c, err := h.acc.sessions.Get(context.Background())
		if err != nil {
			break
		}
		got = append(got, c)
	}
	for range got {
		h.app.noteSyncPoolOutcome(accID, nil)
	}
	for _, c := range got {
		h.acc.sessions.Put(c, false)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.acc.sessions.WaitClosed(ctx); err != nil {
		h.t.Fatal("sessions never closed")
	}
	h.now = h.now.Add(time.Second)
}

func (h *ceilingHarness) run(d time.Duration) {
	for end := h.now.Add(d); h.now.Before(end); {
		h.round()
	}
}

// bursts groups refusals less than 10 s apart and returns when each group
// started and how many refusals it had.
func bursts(refused []time.Duration) (starts []time.Duration, sizes []int) {
	for i, r := range refused {
		if i == 0 || r-refused[i-1] >= 10*time.Second {
			starts = append(starts, r)
			sizes = append(sizes, 0)
		}
		sizes[len(sizes)-1]++
	}
	return starts, sizes
}

func near(got, want, tol time.Duration) bool {
	d := got - want
	return d >= -tol && d <= tol
}

const tooMany = "imap: login: NO [ALERT] Too many simultaneous connections. (Failure)"

// A server that takes one connection is learnt once. After that, a second
// connection is tried only when a probe is due, 1, 3, 9, 24 and 60 minutes
// after the last failed one and every 60 minutes from then on, and each
// probe costs one refused LOGIN. Before the ceiling, the throttle doubled N
// back after every three good tasks and was refused again within seconds:
// about a hundred refused LOGINs in a few minutes, which gets an address
// banned by fail2ban.
func TestThrottleProbesALimitedServerRarely(t *testing.T) {
	h := newCeilingHarness(t, 4, 1, tooMany)
	h.run(2*time.Hour + 40*time.Minute)
	starts, sizes := bursts(h.refused)
	t.Logf("refused LOGINs: %d, bursts at %v sizes %v", len(h.refused), starts, sizes)
	if len(starts) == 0 || starts[0] > 5*time.Second {
		t.Fatalf("the limit was never hit at start: bursts %v", starts)
	}
	wantGaps := []time.Duration{1 * time.Minute, 3 * time.Minute, 9 * time.Minute, 24 * time.Minute, 60 * time.Minute, 60 * time.Minute}
	if len(starts) != len(wantGaps)+1 {
		t.Fatalf("got %d bursts of refusals at %v, want %d", len(starts), starts, len(wantGaps)+1)
	}
	for i, gap := range wantGaps {
		if got := starts[i+1] - starts[i]; !near(got, gap, 5*time.Second) {
			t.Errorf("probe %d came %v after the last refusal, want %v", i+1, got, gap)
		}
		if sizes[i+1] != 1 {
			t.Errorf("probe %d cost %d refused LOGINs, want 1", i+1, sizes[i+1])
		}
	}
	if got := h.acc.pool.Effective(); got != 1 {
		t.Fatalf("effective N = %d, want 1", got)
	}
}

// When the server lets more in, the next probe succeeds, and N climbs by one
// a minute after each success up to the configured N, with no refusal.
func TestThrottleProbesClimbWhenTheServerAllowsMore(t *testing.T) {
	h := newCeilingHarness(t, 3, 1, tooMany)
	h.run(20 * time.Minute)
	refusedBefore := len(h.refused)
	if got := h.acc.pool.Effective(); got != 1 {
		t.Fatalf("effective N after 20 min at a limit of 1 = %d, want 1", got)
	}
	raisedAt := h.now.Sub(h.start)
	h.srv.setLimit(3)
	h.changes = nil
	h.run(40 * time.Minute)
	if len(h.refused) != refusedBefore {
		t.Fatalf("%d refused LOGINs after the server raised its limit", len(h.refused)-refusedBefore)
	}
	if len(h.changes) != 2 || h.changes[0].n != 2 || h.changes[1].n != 3 {
		t.Fatalf("N changes after the raise = %v, want 1 -> 2 -> 3", h.changes)
	}
	// The next probe was due when the backoff in force at the raise ran
	// out; the one after a minute after the first succeeded.
	if h.changes[0].at < raisedAt {
		t.Fatalf("the first probe came before the raise: %v", h.changes)
	}
	if gap := h.changes[1].at - h.changes[0].at; !near(gap, time.Minute, 5*time.Second) {
		t.Fatalf("second probe %v after the first, want about 1 min: %v", gap, h.changes)
	}
	if got := h.acc.throttleCeiling(); got != 3 {
		t.Fatalf("ceiling = %d, want 3", got)
	}
}

// A new N from the settings starts over: the ceiling and the probe backoff
// are forgotten, and the server is learnt again from the new N.
func TestThrottleSettingsChangeResetsCeilingAndBackoff(t *testing.T) {
	h := newCeilingHarness(t, 4, 1, tooMany)
	h.run(30 * time.Minute) // probes at +1, +4 and +13 min failed: backoff at 24 min
	if c := h.acc.throttleCeiling(); c != 1 {
		t.Fatalf("ceiling = %d, want 1", c)
	}
	if next := h.acc.throttleNextProbe().Sub(h.now); next < 5*time.Minute {
		t.Fatalf("next probe in %v, want the backoff to have grown", next)
	}
	// A save of the same N keeps what was learnt.
	h.acc.applyUserSetting(func() int { return 4 })
	if c := h.acc.throttleCeiling(); c != 1 || h.acc.pool.Effective() != 1 {
		t.Fatalf("a save of the same N reset the throttle: ceiling %d, N %d", c, h.acc.pool.Effective())
	}
	h.acc.applyUserSetting(func() int { return 3 })
	if c, n := h.acc.throttleCeiling(), h.acc.throttleNextProbe(); c != 0 || !n.IsZero() {
		t.Fatalf("after a new N: ceiling %d, next probe %v, want none", c, n)
	}
	if got := h.acc.pool.Effective(); got != 3 {
		t.Fatalf("effective N after a new N = %d, want 3", got)
	}
	h.refused = nil
	h.run(3 * time.Minute)
	starts, _ := bursts(h.refused)
	if len(starts) != 2 || !near(starts[1]-starts[0], time.Minute, 5*time.Second) {
		t.Fatalf("after the reset, refusals at %v, want the start and a probe 1 min later", starts)
	}
}

// Errors below the ceiling (the network, not the server's limit) are
// recovered from by +1 steps after each streak of successes, up to the
// ceiling and no further until a probe is due.
func TestThrottleRecoversBelowTheCeilingByOne(t *testing.T) {
	h := newCeilingHarness(t, 4, 10, tooMany)
	netErr := errors.New("dial tcp 192.0.2.1:993: i/o timeout")
	h.app.noteSyncPoolOutcome(accID, netErr)
	h.app.noteSyncPoolOutcome(accID, netErr)
	if n, c := h.acc.pool.Effective(), h.acc.throttleCeiling(); n != 2 || c != 2 {
		t.Fatalf("after two errors: N %d ceiling %d, want 2 2", n, c)
	}
	h.run(5 * time.Minute) // probes 3 and 4 succeed
	if n, c := h.acc.pool.Effective(), h.acc.throttleCeiling(); n != 4 || c != 4 {
		t.Fatalf("after probes: N %d ceiling %d, want 4 4", n, c)
	}
	// Errors at the ceiling: it worked, now it does not. N halves, the
	// ceiling drops by one.
	h.app.noteSyncPoolOutcome(accID, netErr)
	h.app.noteSyncPoolOutcome(accID, netErr)
	if n, c := h.acc.pool.Effective(), h.acc.throttleCeiling(); n != 2 || c != 3 {
		t.Fatalf("errors at the ceiling: N %d ceiling %d, want 2 3", n, c)
	}
	// Errors below it halve N and leave the ceiling.
	h.app.noteSyncPoolOutcome(accID, netErr)
	h.app.noteSyncPoolOutcome(accID, netErr)
	if n, c := h.acc.pool.Effective(), h.acc.throttleCeiling(); n != 1 || c != 3 {
		t.Fatalf("errors below the ceiling: N %d ceiling %d, want 1 3", n, c)
	}
	var steps []int
	for range 4 * syncThrottleRecoverThreshold {
		h.app.noteSyncPoolOutcome(accID, nil)
		if n := h.acc.pool.Effective(); len(steps) == 0 || steps[len(steps)-1] != n {
			steps = append(steps, n)
		}
	}
	if fmt.Sprint(steps) != "[1 2 3]" {
		t.Fatalf("N after successes below the ceiling went %v, want [1 2 3]", steps)
	}
}

// Exchange Online names the wait it wants. No probe comes before it is over,
// even when the backoff schedule would allow one.
func TestThrottleHonoursTheSuggestedBackoff(t *testing.T) {
	h := newCeilingHarness(t, 2, 1, "imap: login: NO Request is throttled. Suggested Backoff Time: 300000 milliseconds")
	h.run(20 * time.Minute)
	starts, _ := bursts(h.refused)
	if len(starts) < 2 {
		t.Fatalf("refusals at %v, want a probe", starts)
	}
	if gap := starts[1] - starts[0]; gap < 5*time.Minute {
		t.Fatalf("first probe %v after the refusal, want at least the suggested 5 min", gap)
	}
}

// Dovecot names its limit. The ceiling is set to it at once, and no probe
// goes above it however long the account runs.
func TestThrottleTakesDovecotsNamedLimit(t *testing.T) {
	h := newCeilingHarness(t, 5, 2,
		"imap: login: NO [LIMIT] Maximum number of connections from user+IP exceeded (mail_max_userip_connections=2)")
	h.run(3 * time.Hour)
	if n, c := h.acc.pool.Effective(), h.acc.throttleCeiling(); n != 2 || c != 2 {
		t.Fatalf("N %d ceiling %d, want 2 2", n, c)
	}
	if len(h.refused) != 1 {
		t.Fatalf("%d refused LOGINs, want the one that named the limit", len(h.refused))
	}
}

// A refusal at or below Dovecot's limit (another client of the same user
// and address holds connections) halves N as usual.
func TestThrottleHalvesBelowDovecotsLimit(t *testing.T) {
	h := newCeilingHarness(t, 4, 4, "")
	limitErr := errors.New("imap: login: NO [LIMIT] Maximum number of connections from user+IP exceeded (mail_max_userip_connections=4)")
	h.app.noteSyncPoolOutcome(accID, limitErr)
	h.app.noteSyncPoolOutcome(accID, limitErr)
	if n := h.acc.pool.Effective(); n != 2 {
		t.Fatalf("N after two refusals at the named limit = %d, want 2", n)
	}
}

// A probe succeeds only once a session at the raised N logged in, and the
// real dialer is what tells the throttle of a login. Without it the
// successes on the session already open would raise the ceiling to an N
// the server never took.
func TestThrottleProbeNeedsALoginThroughTheDialer(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 2)
	rt := mustAccountSync(t, a, id)
	a.noteSyncPoolOutcome(id, errors.New(tooMany))
	a.noteSyncPoolOutcome(id, errors.New(tooMany))
	if n, c := rt.pool.Effective(), rt.shared.throttleCeiling(); n != 1 || c != 1 {
		t.Fatalf("after two refusals: N %d ceiling %d, want 1 1", n, c)
	}
	advanceThrottleClock(rt.shared, time.Hour)
	a.noteSyncPoolOutcome(id, nil)
	if n := rt.pool.Effective(); n != 2 {
		t.Fatalf("no probe: N %d, want 2", n)
	}
	for range syncThrottleRecoverThreshold {
		a.noteSyncPoolOutcome(id, nil)
	}
	if c := rt.shared.throttleCeiling(); c != 1 {
		t.Fatalf("ceiling %d before a second session logged in, want 1", c)
	}
	var held []mailClient
	for range 2 {
		c, err := rt.sessions.Get(a.ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	for range syncThrottleRecoverThreshold {
		a.noteSyncPoolOutcome(id, nil)
	}
	for _, c := range held {
		rt.sessions.Put(c, false)
	}
	if c := rt.shared.throttleCeiling(); c != 2 {
		t.Fatalf("ceiling %d after two sessions logged in and worked, want 2", c)
	}
}

// Dials started before a shrink are refused after it. Those sessions were
// above the new N, so their refusals are no news: counted, N 8 went to 4 and
// on to 2 and 1 on the refusals of the same burst of dials.
func TestThrottleIgnoresRefusalsOfDialsAboveN(t *testing.T) {
	p := pool.New(8)
	acc := &accountSessions{pool: p, userN: 8}
	app := &App{
		ctx:          context.Background(),
		log:          slog.New(slog.DiscardHandler),
		sessionPools: map[int64]*accountSessions{accID: acc},
	}
	srv := &limitServer{limit: 1, refusal: tooMany}
	turn := make(chan struct{})
	first := true
	acc.sessions = pool.NewSessions(p, func(context.Context) (mailClient, error) {
		if first {
			first = false
			_ = srv.login()
			return &limitConn{srv: srv}, nil
		}
		<-turn
		err := srv.login()
		app.noteSyncPoolDial(accID, err)
		return nil, throttleCounted{err}
	}, time.Hour)
	t.Cleanup(acc.sessions.Close)
	held, err := acc.sessions.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer acc.sessions.Put(held, false)
	done := make(chan error)
	for range 7 {
		go func() {
			_, err := acc.sessions.Get(context.Background())
			done <- err
		}()
	}
	waitUntil(t, 2*time.Second, "the dials never started", func() bool { return acc.sessions.Open() == 8 })
	for range 7 {
		turn <- struct{}{}
		if err := <-done; err == nil {
			t.Fatal("a dial over the limit succeeded")
		}
	}
	// Two refusals within N 8 halve it to 4; the next two dials are above 4;
	// two more refusals within 4 (ceiling 4 itself) halve it to 2, ceiling 3;
	// the last one, at 2, starts a new streak.
	if n, c := p.Effective(), acc.throttleCeiling(); n != 2 || c != 3 {
		t.Fatalf("N %d ceiling %d, want 2 3", n, c)
	}
}

// advanceThrottleClock moves the account throttle's clock d ahead of where
// it is and stops it there.
func advanceThrottleClock(s *accountSessions, d time.Duration) {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	at := s.clock().Add(d)
	s.now = func() time.Time { return at }
}

// What the throttle learnt is about one server and one login. An edit that
// keeps them keeps it; a new host or user name forgets it.
func TestThrottleEndpointChangeResetsTheCeiling(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, id)
	a.noteSyncPoolOutcome(id, errors.New(tooMany))
	a.noteSyncPoolOutcome(id, errors.New(tooMany))
	if c := rt.shared.throttleCeiling(); c != 2 {
		t.Fatalf("ceiling %d, want 2", c)
	}
	acc := mustAccount(t, a, id)
	req := UpdateAccountRequest{
		ID: id, DisplayName: "renamed", Username: acc.Username,
		IMAPHost: acc.IMAPHost, IMAPPort: acc.IMAPPort, IMAPTLS: acc.IMAPTLS,
		SMTPHost: acc.SMTPHost, SMTPPort: acc.SMTPPort, SMTPTLS: acc.SMTPTLS,
		syncMaxParallelOmitted: true,
	}
	if _, err := a.UpdateAccount(req); err != nil {
		t.Fatal(err)
	}
	if c, n := rt.shared.throttleCeiling(), rt.pool.Effective(); c != 2 || n != 2 {
		t.Fatalf("a rename reset the throttle: ceiling %d, N %d", c, n)
	}
	req.IMAPHost = "imap2.example.test"
	if _, err := a.UpdateAccount(req); err != nil {
		t.Fatal(err)
	}
	if c, n := rt.shared.throttleCeiling(), rt.pool.Effective(); c != 0 || n != 4 {
		t.Fatalf("after a new host: ceiling %d, N %d, want 0 and 4", c, n)
	}
}

// Only a connection-limit refusal of a new dial ends a probe. A network
// error, or a limit refusal of work on a session already open, counts as it
// does outside a probe: N stays at the probed level and the backoff stays.
// The errors are 2 min apart, more than the 1 min streak window.
// Before, any refusal ended the probe, and network blips pushed the backoff
// to 60 min.
func TestThrottleProbeEndsOnlyOnALimitRefusalOfADial(t *testing.T) {
	h := newCeilingHarness(t, 2, 1, tooMany)
	h.app.noteSyncPoolOutcome(accID, errors.New(tooMany))
	h.app.noteSyncPoolOutcome(accID, errors.New(tooMany))
	if n, c := h.acc.pool.Effective(), h.acc.throttleCeiling(); n != 1 || c != 1 {
		t.Fatalf("after two refusals: N %d ceiling %d, want 1 1", n, c)
	}
	h.now = h.now.Add(time.Minute)
	h.app.noteSyncPoolOutcome(accID, nil)
	if n := h.acc.pool.Effective(); n != 2 {
		t.Fatalf("no probe: N %d, want 2", n)
	}
	due := h.acc.throttleNextProbe()
	netErr := errors.New("read tcp 192.0.2.1:993: connection reset by peer")
	for _, err := range []error{netErr, errors.New(tooMany)} {
		h.now = h.now.Add(2 * time.Minute)
		h.app.noteSyncPoolOutcome(accID, err)
		if n, next := h.acc.pool.Effective(), h.acc.throttleNextProbe(); n != 2 || !next.Equal(due) {
			t.Fatalf("%v on an open session ended the probe: N %d next %v, want 2 %v", err, n, next, due)
		}
	}
	h.now = h.now.Add(2 * time.Minute)
	h.app.noteSyncPoolDial(accID, netErr)
	if n := h.acc.pool.Effective(); n != 2 {
		t.Fatalf("a network error of a dial ended the probe: N %d", n)
	}
	h.now = h.now.Add(2 * time.Minute)
	h.app.noteSyncPoolDial(accID, errors.New(tooMany))
	if n, next := h.acc.pool.Effective(), h.acc.throttleNextProbe(); n != 1 || !next.Equal(h.now.Add(3*time.Minute)) {
		t.Fatalf("a limit refusal of a dial: N %d next %v, want 1 %v", n, next, h.now.Add(3*time.Minute))
	}
}

// The server asking to slow down on a session already open ends a probe:
// N goes back to the ceiling and the backoff advances. A network error there
// does not.
func TestThrottleProbeEndsWhenTheServerThrottlesAnOpenSession(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		ends bool
	}{
		{"imap: NO Request is throttled.", true},
		{"imap: NO [ALERT] Rate limit exceeded, try again later", true},
		{"imap: NO Too many requests", true},
		{"jmap: unexpected HTTP status 429 Too Many Requests", true},
		{"read tcp 192.0.2.1:993: connection reset by peer", false},
	} {
		h := newCeilingHarness(t, 2, 1, tooMany)
		h.app.noteSyncPoolOutcome(accID, errors.New(tooMany))
		h.app.noteSyncPoolOutcome(accID, errors.New(tooMany))
		h.now = h.now.Add(time.Minute)
		h.app.noteSyncPoolOutcome(accID, nil)
		if n := h.acc.pool.Effective(); n != 2 {
			t.Fatalf("no probe: N %d, want 2", n)
		}
		due := h.acc.throttleNextProbe()
		h.app.noteSyncPoolOutcome(accID, errors.New(tc.msg))
		n, next := h.acc.pool.Effective(), h.acc.throttleNextProbe()
		switch {
		case tc.ends && (n != 1 || !next.Equal(h.now.Add(3*time.Minute))):
			t.Errorf("%q during the probe: N %d next %v, want 1 %v", tc.msg, n, next, h.now.Add(3*time.Minute))
		case !tc.ends && (n != 2 || !next.Equal(due)):
			t.Errorf("%q during the probe: N %d next %v, want 2 %v", tc.msg, n, next, due)
		}
	}
}
