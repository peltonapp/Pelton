package throttle

import (
	"testing"
	"time"
)

func TestSuggestedBackoff(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want time.Duration
	}{
		{"A3 BAD Request is throttled. Suggested Backoff Time: 40464 milliseconds", 40464 * time.Millisecond},
		{"imap: NO Request is throttled. Suggested Backoff Time: 299499 milliseconds", 299499 * time.Millisecond},
		{"suggested backoff time:120000 MILLISECONDS", 2 * time.Minute},
		{"Suggested  Backoff  Time :  5000", 5 * time.Second},
		{"SUGGESTED BACK-OFF TIME=750 ms", 750 * time.Millisecond},
		{"Suggested Backoff Time: 99999999999999999999 milliseconds", 24 * time.Hour},
		{"Suggested Backoff Time: 172800000 milliseconds", 24 * time.Hour},
		{"Request is throttled.", 0},
		{"Suggested Backoff Time: soon", 0},
		{"", 0},
	} {
		if got := SuggestedBackoff(tc.msg); got != tc.want {
			t.Errorf("SuggestedBackoff(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestNamedLimit(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want int
	}{
		{"NO [LIMIT] Maximum number of connections from user+IP exceeded (mail_max_userip_connections=10)", 10},
		{"Maximum number of connections from user+IP exceeded (MAIL_MAX_USERIP_CONNECTIONS = 3)", 3},
		{"mail_max_userip_connections: 2", 2},
		{"mail_max_userip_connections=0", 0},
		{"mail_max_userip_connections=99999999999999999999", 0},
		{"Maximum number of connections from user+IP exceeded", 0},
		{"mail_max_userip_connections=", 0},
	} {
		if got := NamedLimit(tc.msg); got != tc.want {
			t.Errorf("NamedLimit(%q) = %d, want %d", tc.msg, got, tc.want)
		}
	}
}

var (
	t0      = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	refused = Signal{Kind: Refused}
	ok      = Signal{Kind: OK}
)

// learn takes s from configured N 4 to a ceiling of 2 at t0.
func learn(t *testing.T, s *State) {
	t.Helper()
	s.Step(refused, 4, 4, t0)
	if got := s.Step(refused, 4, 4, t0); got != 2 {
		t.Fatalf("two refusals at 4: N %d, want 2", got)
	}
	if s.Ceiling() != 2 || !s.NextProbe().Equal(t0.Add(time.Minute)) {
		t.Fatalf("ceiling %d next probe %v", s.Ceiling(), s.NextProbe())
	}
}

func TestRefusalsFarApartAreNoStreak(t *testing.T) {
	var s State
	s.Step(refused, 4, 4, t0)
	if got := s.Step(refused, 4, 4, t0.Add(StreakWindow+time.Second)); got != 0 {
		t.Fatalf("refusals %v apart halved N to %d", StreakWindow+time.Second, got)
	}
	if got := s.Step(refused, 4, 4, t0.Add(StreakWindow+2*time.Second)); got != 2 {
		t.Fatalf("a streak of two: N %d, want 2", got)
	}
}

func TestFailedProbesBackOff(t *testing.T) {
	var s State
	learn(t, &s)
	now := t0
	for i, wait := range []time.Duration{time.Minute, 3 * time.Minute, 9 * time.Minute, 24 * time.Minute, time.Hour, time.Hour} {
		if got := s.Step(ok, 2, 4, now.Add(wait-time.Second)); got != 0 {
			t.Fatalf("probe %d came %v early: N %d", i, time.Second, got)
		}
		now = now.Add(wait)
		if got := s.Step(ok, 2, 4, now); got != 3 {
			t.Fatalf("probe %d due at %v: N %d, want 3", i, wait, got)
		}
		if got := s.Step(limitOnDial, 3, 4, now); got != 2 {
			t.Fatalf("probe %d refused: N %d, want back to 2", i, got)
		}
	}
}

func TestProbeSucceedsOnlyOnceConnected(t *testing.T) {
	var s State
	learn(t, &s)
	now := t0.Add(time.Minute)
	s.Step(ok, 2, 4, now)
	for range RecoverThreshold {
		s.Step(ok, 3, 4, now)
	}
	if s.Ceiling() != 2 || s.Probing() != 3 {
		t.Fatalf("successes on the open sessions confirmed the probe: ceiling %d probing %d", s.Ceiling(), s.Probing())
	}
	s.Connected(2)
	s.Connected(3)
	for range RecoverThreshold {
		s.Step(ok, 3, 4, now)
	}
	if s.Ceiling() != 3 || s.Probing() != 0 || !s.NextProbe().Equal(now.Add(time.Minute)) {
		t.Fatalf("ceiling %d probing %d next %v", s.Ceiling(), s.Probing(), s.NextProbe())
	}
}

func TestSuggestedBackoffHoldsEveryRaise(t *testing.T) {
	var s State
	s.Step(refused, 8, 8, t0)
	s.Step(refused, 8, 8, t0) // N 4, ceiling 4
	// At the ceiling, with the server asking for 10 min: N 2, ceiling 3.
	s.Step(refused, 4, 8, t0)
	if got := s.Step(Signal{Kind: Refused, Backoff: 10 * time.Minute}, 4, 8, t0); got != 2 || s.Ceiling() != 3 {
		t.Fatalf("N %d ceiling %d, want 2 3", got, s.Ceiling())
	}
	if !s.NextProbe().Equal(t0.Add(10 * time.Minute)) {
		t.Fatalf("next probe %v, want the suggested 10 min, not the 1 min schedule", s.NextProbe())
	}
	// Below the ceiling, the +1 recovery waits for the backoff too.
	for range 3 * RecoverThreshold {
		if got := s.Step(ok, 2, 8, t0.Add(9*time.Minute)); got != 0 {
			t.Fatalf("raised to %d inside the suggested backoff", got)
		}
	}
	if got := s.Step(ok, 2, 8, t0.Add(10*time.Minute)); got != 3 {
		t.Fatalf("after the backoff: N %d, want 3", got)
	}
}

func TestNamedLimitCapsTheCeiling(t *testing.T) {
	var s State
	if got := s.Step(Signal{Kind: Refused, Limit: 3}, 6, 6, t0); got != 3 {
		t.Fatalf("a refusal naming 3 at N 6: N %d, want 3", got)
	}
	if s.Ceiling() != 3 {
		t.Fatalf("ceiling %d, want 3", s.Ceiling())
	}
	for i := range 10 {
		if got := s.Step(ok, 3, 6, t0.Add(time.Duration(i)*time.Hour)); got != 0 {
			t.Fatalf("probed above the named limit: N %d", got)
		}
	}
	// A limit above the configured N is capped at it, and at least 1.
	var u State
	u.Step(Signal{Kind: Refused, Limit: 20}, 2, 2, t0)
	if u.Ceiling() != 2 {
		t.Fatalf("ceiling %d, want the configured 2", u.Ceiling())
	}
	s.Reset()
	if s.Ceiling() != 0 || !s.NextProbe().IsZero() {
		t.Fatal("Reset kept the ceiling")
	}
}

// limitOnDial is a connection-limit refusal of a new session's LOGIN: the
// only refusal that ends a probe.
var limitOnDial = Signal{Kind: Refused, Dial: true, ConnLimit: true}

// startProbe takes s from learn's ceiling of 2 to a probe of 3 at t0+1m.
func startProbe(t *testing.T, s *State) time.Time {
	t.Helper()
	learn(t, s)
	now := t0.Add(time.Minute)
	if got := s.Step(ok, 2, 4, now); got != 3 || s.Probing() != 3 {
		t.Fatalf("probe due: N %d probing %d, want 3 3", got, s.Probing())
	}
	return now
}

// A network error, or a limit refusal of a session already open, says
// nothing about whether the server takes one more connection. During a
// probe it counts as it does outside one: it neither ends the probe nor
// advances the backoff; only a streak of two halves N.
func TestProbeSurvivesErrorsThatAreNotALimitOnADial(t *testing.T) {
	for _, sig := range []Signal{
		refused,
		{Kind: Refused, Dial: true},
		{Kind: Refused, ConnLimit: true},
		{Kind: Refused, ConnLimit: true, Limit: 3},
	} {
		var s State
		now := startProbe(t, &s)
		due := s.NextProbe()
		if got := s.Step(sig, 3, 4, now); got != 0 || s.Probing() != 3 || s.Ceiling() != 2 {
			t.Fatalf("%+v during the probe: N %d probing %d ceiling %d, want 0 3 2", sig, got, s.Probing(), s.Ceiling())
		}
		want := due
		if sig.Backoff > 0 {
			want = later(due, now.Add(sig.Backoff))
		}
		if !s.NextProbe().Equal(want) {
			t.Fatalf("%+v moved the next probe to %v, want %v", sig, s.NextProbe(), want)
		}
		// The probe still confirms once a session at 3 logs in.
		now = now.Add(2 * time.Second)
		s.Connected(3)
		for range RecoverThreshold {
			s.Step(ok, 3, 4, now)
		}
		if s.Ceiling() != 3 || s.Probing() != 0 {
			t.Fatalf("%+v: after a login and successes, ceiling %d probing %d, want 3 0", sig, s.Ceiling(), s.Probing())
		}
	}
	// A streak of two network errors halves N as anywhere else; the probe
	// ends with it, the backoff stays where it was.
	var s State
	now := startProbe(t, &s)
	s.Step(refused, 3, 4, now)
	if got := s.Step(refused, 3, 4, now.Add(time.Second)); got != 1 {
		t.Fatalf("a streak of two during the probe: N %d, want 1", got)
	}
	if s.Probing() != 0 || s.Ceiling() != 2 || !s.NextProbe().Equal(now.Add(time.Second+ProbeBackoff[0])) {
		t.Fatalf("after the streak: probing %d ceiling %d next %v", s.Probing(), s.Ceiling(), s.NextProbe())
	}
}

// A connection-limit refusal of a new session at the raised N is the answer
// the probe waited for: N goes back to the ceiling, the backoff advances.
func TestProbeEndsOnALimitRefusalOfANewDial(t *testing.T) {
	var s State
	now := startProbe(t, &s)
	if got := s.Step(limitOnDial, 3, 4, now); got != 2 {
		t.Fatalf("limit on a dial: N %d, want back to 2", got)
	}
	if s.Probing() != 0 || s.Ceiling() != 2 || !s.NextProbe().Equal(now.Add(ProbeBackoff[1])) {
		t.Fatalf("probing %d ceiling %d next %v, want 0 2 %v", s.Probing(), s.Ceiling(), s.NextProbe(), now.Add(ProbeBackoff[1]))
	}
}

// With the open sessions covering the work, no session dials at the raised
// N: the probe confirms nothing and stays, however long, and whatever
// scattered errors the open sessions see. It never reschedules itself.
func TestUndialledProbeStaysPending(t *testing.T) {
	var s State
	now := startProbe(t, &s)
	due := s.NextProbe()
	for i := range 6 * 60 {
		now = now.Add(10 * time.Second)
		sig := ok
		if i%30 == 29 {
			sig = refused // one network error every 5 min
		}
		if got := s.Step(sig, 3, 4, now); got != 0 {
			t.Fatalf("step %d (%v): N %d, want 3 kept", i, sig.Kind, got)
		}
	}
	if s.Probing() != 3 || s.Ceiling() != 2 || !s.NextProbe().Equal(due) {
		t.Fatalf("after an hour: probing %d ceiling %d next %v, want 3 2 %v", s.Probing(), s.Ceiling(), s.NextProbe(), due)
	}
}

// The server asking to slow down (Exchange's "Request is throttled", a
// suggested backoff) ends a probe on any session, open or new: N goes back
// to the ceiling and the backoff advances, and the next probe waits for the
// later of that step and the suggested wait.
func TestServerThrottleEndsAProbeOnAnySession(t *testing.T) {
	for _, tc := range []struct {
		sig  Signal
		next time.Duration // after the probe's start
	}{
		{Signal{Kind: Refused, ConnLimit: true, Throttled: true}, ProbeBackoff[1]},
		// a rate limit or an HTTP 429: throttled, not a connection limit
		{Signal{Kind: Refused, Throttled: true}, ProbeBackoff[1]},
		{Signal{Kind: Refused, ConnLimit: true, Throttled: true, Backoff: 10 * time.Minute}, 10 * time.Minute},
		{Signal{Kind: Refused, ConnLimit: true, Throttled: true, Backoff: time.Second, Dial: true}, ProbeBackoff[1]},
	} {
		var s State
		now := startProbe(t, &s)
		if got := s.Step(tc.sig, 3, 4, now); got != 2 {
			t.Fatalf("%+v during the probe: N %d, want back to 2", tc.sig, got)
		}
		if s.Probing() != 0 || s.Ceiling() != 2 || !s.NextProbe().Equal(now.Add(tc.next)) {
			t.Fatalf("%+v: probing %d ceiling %d next %v, want 0 2 %v", tc.sig, s.Probing(), s.Ceiling(), s.NextProbe(), now.Add(tc.next))
		}
		// No raise, probe or +1, before the next probe is due.
		if got := s.Step(ok, 2, 4, now.Add(tc.next-time.Second)); got != 0 {
			t.Fatalf("%+v: raised to %d before the next probe", tc.sig, got)
		}
	}
}
