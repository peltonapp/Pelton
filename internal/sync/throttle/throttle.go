// Package throttle decides an account's effective connection count from what
// the server says about its sessions. It is pure: no I/O, no clock of its
// own (the caller passes the time), no locking (the caller serializes).
//
// The rules:
//
//   - Refusals. FailThreshold throttle errors, each less than StreakWindow
//     after the one before, halve effective N. A success between them does
//     not end the streak: at a connection limit the sessions already open
//     keep working while every new one is refused.
//   - Ceiling. The first halving records a ceiling: the new effective N, the
//     largest N taken to work. Recovery never goes above it until a probe is
//     due. Two refusals at the ceiling itself lower it by one (never below the
//     halved N): the level that worked no longer does.
//   - Below the ceiling (a halving at a lower N, after transient errors) N
//     grows back by +1 after RecoverThreshold successes in a row, up to the
//     ceiling.
//   - Probes. At the ceiling, once the probe is due, the next success raises
//     N by one. The probe succeeds when a session at the raised N logs in and
//     RecoverThreshold successes follow, outside any suggested backoff: the
//     ceiling rises by one and the backoff starts over at its first step.
//     A probe ends on a connection-limit refusal of a new session
//     (Signal.Dial and Signal.ConnLimit), or on the server asking to slow
//     down on any session (Signal.Throttled, a suggested backoff): N goes
//     back to the ceiling and the backoff advances one step (ProbeBackoff,
//     the last step repeating). A network error, or any other refusal of
//     work on a session already open, says nothing about one more
//     connection: it counts as it does outside a
//     probe (a streak halves N, which ends the probe without advancing the
//     backoff). While the open sessions cover the work no session dials, and
//     the probe waits at the raised N, however long, until one does.
//   - Server hints. A suggested backoff (Exchange Online's "Suggested
//     Backoff Time") holds every raise until it has passed. A connection
//     limit the server names (Dovecot's mail_max_userip_connections) caps the
//     ceiling and every probe at it; refusals at or below it still count as
//     usual, since other clients of the same user and address share it.
//
// The state lives in memory only. Reset forgets it.
package throttle

import "time"

const (
	// FailThreshold is how many throttle errors in a row halve N.
	FailThreshold = 2
	// RecoverThreshold is how many successes in a row grow N by one below
	// the ceiling, and confirm a probe.
	RecoverThreshold = 3
)

// StreakWindow is the longest gap between two refusals that still makes
// them a streak. A rare error of a long, healthy session does not add up
// with one an hour later.
const StreakWindow = time.Minute

// ProbeBackoff is the wait before each probe after the last one failed (the
// first entry after a halving or a probe that succeeded). The last entry
// repeats.
var ProbeBackoff = []time.Duration{
	1 * time.Minute,
	3 * time.Minute,
	9 * time.Minute,
	24 * time.Minute,
	60 * time.Minute,
}

// Kind is what an outcome says about the server's capacity.
type Kind int

const (
	// Neutral is an outcome that says nothing about capacity: an error of
	// the request itself (no such mailbox), or work that never ran.
	Neutral Kind = iota
	// OK is a server operation that worked.
	OK
	// Refused is a connection or rate-limit refusal, or a network error.
	// Signal.Dial and Signal.ConnLimit say which.
	Refused
)

// Signal is one outcome as the throttle sees it.
type Signal struct {
	Kind Kind
	// Backoff is the wait the server suggested, 0 when it named none.
	Backoff time.Duration
	// Limit is the connection limit the server named, 0 when it named none.
	Limit int
	// Dial is set for the outcome of a new session's connect or LOGIN, not
	// of work on a session already open.
	Dial bool
	// ConnLimit is set for a refusal that names the server's connection
	// limit (a limit text, [LIMIT], [UNAVAILABLE], [INUSE], Exchange's
	// throttling), not a network error.
	ConnLimit bool
	// Throttled is set when the server asks to slow down: Exchange's
	// "Request is throttled" or a suggested backoff.
	Throttled bool
}

// State is one account's throttle.
type State struct {
	fails     int
	lastFail  time.Time
	successes int

	// ceiling is the largest N known to work, 0 while none is known.
	ceiling int
	// limit is the connection limit the server named, 0 while none.
	limit int

	// step indexes ProbeBackoff; nextProbe is when a probe is due.
	step      int
	nextProbe time.Time
	// notBefore holds every raise until the server's suggested backoff ends.
	notBefore time.Time

	// probe is the N being probed, 0 when none is; connected is set once a
	// session at that N logged in.
	probe     int
	connected bool
}

// Reset forgets everything learnt: the ceiling, the backoff, the server's
// hints and any streak.
func (s *State) Reset() { *s = State{} }

// Ceiling is the largest N known to work, 0 while none is known.
func (s *State) Ceiling() int { return s.ceiling }

// Probing is the N being probed, 0 when no probe runs.
func (s *State) Probing() int { return s.probe }

// NextProbe is when the next probe may start: the later of the backoff and
// the server's suggested wait. It is zero when no ceiling is known.
func (s *State) NextProbe() time.Time {
	if s.ceiling == 0 {
		return time.Time{}
	}
	return later(s.nextProbe, s.notBefore)
}

// Connected notes a session that logged in while open sessions (this one
// included) were open. A probe whose raised N was reached is connected.
func (s *State) Connected(open int) {
	if s.probe > 0 && open >= s.probe {
		s.connected = true
	}
}

// Step counts one outcome at effective N eff, configured N configured and
// time now. It returns the new effective N when it changes, else 0.
func (s *State) Step(sig Signal, eff, configured int, now time.Time) int {
	if s.probe > 0 && eff != s.probe {
		// N changed under the probe (a settings save): it has nothing left
		// to prove.
		s.probe, s.connected = 0, false
	}
	if sig.Backoff > 0 {
		s.notBefore = later(s.notBefore, now.Add(sig.Backoff))
	}
	if sig.Limit > 0 {
		s.limit = sig.Limit
		if c := s.capFor(configured); s.ceiling == 0 || s.ceiling > c {
			s.ceiling = c
			s.schedule(now)
		}
		if eff > s.capFor(configured) {
			// Above the limit the server named: the refusal is explained, go
			// down to it at once.
			s.probe, s.connected, s.fails = 0, false, 0
			return s.ceiling
		}
	}
	switch sig.Kind {
	case Refused:
		return s.refused(sig, eff, now)
	case OK:
		return s.succeeded(eff, configured, now)
	}
	return 0
}

func (s *State) refused(sig Signal, eff int, now time.Time) int {
	s.successes = 0
	if s.probe > 0 && (sig.Dial && sig.ConnLimit || sig.Throttled || sig.Backoff > 0) {
		// The server refused one more connection, or asked to slow down:
		// the probe failed. Back to the ceiling, and wait longer.
		s.probe, s.connected, s.fails = 0, false, 0
		s.step = min(s.step+1, len(ProbeBackoff)-1)
		s.schedule(now)
		return changed(s.ceiling, eff)
	}
	if s.fails > 0 && now.Sub(s.lastFail) > StreakWindow {
		s.fails = 0
	}
	s.fails++
	s.lastFail = now
	if s.fails < FailThreshold {
		return 0
	}
	s.fails = 0
	// A halving during a probe ends it: N is no longer the one probed.
	s.probe, s.connected = 0, false
	next := max(eff/2, 1)
	switch {
	case s.ceiling == 0:
		s.ceiling = next
		s.step = 0
		s.schedule(now)
	case eff >= s.ceiling:
		// Refused at the level that worked: it no longer does.
		s.ceiling = max(next, eff-1)
		s.schedule(now)
	}
	return changed(next, eff)
}

func (s *State) succeeded(eff, configured int, now time.Time) int {
	s.successes++
	if s.probe > 0 {
		if s.connected && s.successes >= RecoverThreshold && !now.Before(s.notBefore) {
			s.ceiling = s.probe
			s.probe, s.connected, s.successes = 0, false, 0
			s.step = 0
			s.schedule(now)
		}
		return 0
	}
	ceiling := s.ceiling
	if ceiling == 0 {
		ceiling = configured
	}
	if now.Before(s.notBefore) {
		return 0
	}
	if eff < min(ceiling, configured) {
		if s.successes < RecoverThreshold {
			return 0
		}
		s.successes = 0
		return eff + 1
	}
	if s.ceiling == 0 || eff >= s.capFor(configured) || now.Before(s.nextProbe) {
		return 0
	}
	s.probe, s.connected, s.successes = eff+1, false, 0
	return eff + 1
}

// capFor is the highest N worth probing: the configured N, or the server's
// named limit when that is lower.
func (s *State) capFor(configured int) int {
	c := configured
	if s.limit > 0 {
		c = min(c, s.limit)
	}
	return max(c, 1)
}

func (s *State) schedule(now time.Time) {
	s.nextProbe = now.Add(ProbeBackoff[s.step])
}

func changed(next, eff int) int {
	if next == eff {
		return 0
	}
	return next
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
