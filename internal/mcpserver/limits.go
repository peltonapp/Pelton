package mcpserver

import (
	"fmt"
	"sync"
	"time"
)

// ScriptLimits caps what an agent can do with the script tools.
type ScriptLimits struct {
	// MaxScriptBytes caps one script an agent sends: code, blocks, settings
	// and trigger together.
	MaxScriptBytes int
	// CreatesPerHour caps the scripts an agent creates in any hour.
	CreatesPerHour int
	// TestRunsPerMinute caps test runs in any minute.
	TestRunsPerMinute int
}

// DefaultScriptLimits are the caps the app uses.
func DefaultScriptLimits() ScriptLimits {
	return ScriptLimits{MaxScriptBytes: 64 << 10, CreatesPerHour: 10, TestRunsPerMinute: 20}
}

// ScriptLimiter enforces ScriptLimits over sliding windows. It is safe for
// concurrent use.
type ScriptLimiter struct {
	limits ScriptLimits
	now    func() time.Time

	mu      sync.Mutex
	creates []time.Time
	tests   []time.Time
}

// NewScriptLimiter returns a limiter with nothing used yet.
func NewScriptLimiter(l ScriptLimits) *ScriptLimiter {
	return &ScriptLimiter{limits: l, now: time.Now}
}

func (l *ScriptLimiter) checkSize(d ScriptDraft) error {
	if n := d.size(); n > l.limits.MaxScriptBytes {
		return fmt.Errorf("the script is %d bytes; the limit for scripts sent by an assistant is %d bytes", n, l.limits.MaxScriptBytes)
	}
	return nil
}

// within keeps the times inside window.
func within(times []time.Time, now time.Time, window time.Duration) []time.Time {
	keep := times[:0]
	for _, t := range times {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	return keep
}

// reserveCreate counts a create against the hourly cap, or refuses it. The
// returned func gives the slot back, for a create that then failed.
func (l *ScriptLimiter) reserveCreate() (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.creates = within(l.creates, now, time.Hour)
	if len(l.creates) >= l.limits.CreatesPerHour {
		return nil, fmt.Errorf("an assistant may create at most %d scripts an hour; try again later", l.limits.CreatesPerHour)
	}
	l.creates = append(l.creates, now)
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		for i, t := range l.creates {
			if t.Equal(now) {
				l.creates = append(l.creates[:i], l.creates[i+1:]...)
				return
			}
		}
	}, nil
}

// takeTestRun counts a test run against the per-minute cap, or refuses it.
func (l *ScriptLimiter) takeTestRun() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.tests = within(l.tests, now, time.Minute)
	if len(l.tests) >= l.limits.TestRunsPerMinute {
		return fmt.Errorf("an assistant may run at most %d tests a minute; wait a moment", l.limits.TestRunsPerMinute)
	}
	l.tests = append(l.tests, now)
	return nil
}
