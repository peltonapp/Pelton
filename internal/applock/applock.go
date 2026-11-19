// Package applock is the state behind Pelton's app lock: whether the lock is
// on, whether the app is locked right now, when it was last used, and how long
// a wrong passphrase has to wait before the next try. It knows nothing about
// windows, settings or how the user proves who they are; the desktop layer
// asks it and acts on its answers.
//
// The lock is a screen lock for someone sitting at an unlocked computer. It
// does not encrypt anything.
package applock

import (
	"sync"
	"time"
)

// freeAttempts is how many wrong passphrases may follow each other without a
// wait, so a typo or two never slows anyone down.
const freeAttempts = 3

// firstBackoff and maxBackoff bound the wait after a wrong passphrase. The
// wait doubles from firstBackoff with every further miss and stops growing at
// maxBackoff.
const (
	firstBackoff = 5 * time.Second
	maxBackoff   = 15 * time.Minute
)

// Backoff returns how long the next passphrase attempt has to wait after the
// given number of wrong ones in a row.
func Backoff(failures int) time.Duration {
	if failures <= freeAttempts {
		return 0
	}
	wait := firstBackoff
	for i := freeAttempts + 1; i < failures; i++ {
		wait *= 2
		if wait >= maxBackoff {
			return maxBackoff
		}
	}
	return wait
}

// Locker holds the lock state. Every method is safe for concurrent use. The
// change callback runs after the state has changed, outside the lock, on the
// goroutine that caused the change.
type Locker struct {
	mu       sync.Mutex
	now      func() time.Time
	enabled  bool
	locked   bool
	idle     time.Duration
	last     time.Time
	failures int
	retryAt  time.Time
	onChange func(locked bool)
}

// New returns a Locker that is off and unlocked. now is the clock it reads;
// nil means time.Now.
func New(now func() time.Time) *Locker {
	if now == nil {
		now = time.Now
	}
	return &Locker{now: now, last: now()}
}

// OnChange sets the function called whenever the app becomes locked or
// unlocked.
func (l *Locker) OnChange(fn func(locked bool)) {
	l.mu.Lock()
	l.onChange = fn
	l.mu.Unlock()
}

// Configure turns the lock on or off and sets the idle time after which it
// locks by itself; 0 means it only locks on demand. Turning the lock off also
// unlocks, and turning it on counts as activity, so the idle time starts then.
func (l *Locker) Configure(enabled bool, idle time.Duration) {
	l.mu.Lock()
	wasLocked := l.locked
	if enabled && !l.enabled {
		l.last = l.now()
	}
	l.enabled = enabled
	l.idle = max(idle, 0)
	if !enabled {
		l.locked = false
		l.failures = 0
		l.retryAt = time.Time{}
	}
	changed := wasLocked != l.locked
	fn := l.onChange
	l.mu.Unlock()
	if changed && fn != nil {
		fn(false)
	}
}

// Enabled reports whether the lock is on.
func (l *Locker) Enabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// Locked reports whether the app is locked right now.
func (l *Locker) Locked() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.locked
}

// Lock locks the app. It does nothing while the lock is off or when the app
// is already locked, and reports whether it locked.
func (l *Locker) Lock() bool {
	l.mu.Lock()
	if !l.enabled || l.locked {
		l.mu.Unlock()
		return false
	}
	l.locked = true
	fn := l.onChange
	l.mu.Unlock()
	if fn != nil {
		fn(true)
	}
	return true
}

// Touch records that the app was used. Activity while locked does not count:
// the lock screen itself is not use of the app.
func (l *Locker) Touch() {
	l.mu.Lock()
	if !l.locked {
		l.last = l.now()
	}
	l.mu.Unlock()
}

// Tick locks the app when it has gone unused for the idle time, and reports
// whether it did. Call it periodically.
func (l *Locker) Tick() bool {
	l.mu.Lock()
	due := l.enabled && !l.locked && l.idle > 0 && l.now().Sub(l.last) >= l.idle
	l.mu.Unlock()
	if !due {
		return false
	}
	return l.Lock()
}

// Unlock unlocks the app after the user has proven who they are. It clears
// the wrong passphrase count and counts as activity.
func (l *Locker) Unlock() {
	l.mu.Lock()
	wasLocked := l.locked
	l.locked = false
	l.failures = 0
	l.retryAt = time.Time{}
	l.last = l.now()
	fn := l.onChange
	l.mu.Unlock()
	if wasLocked && fn != nil {
		fn(false)
	}
}

// Wait returns how long until the next passphrase attempt is allowed; 0 means
// now.
func (l *Locker) Wait() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return max(l.retryAt.Sub(l.now()), 0)
}

// Fail records a wrong passphrase and returns how long the next attempt has
// to wait.
func (l *Locker) Fail() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures++
	wait := Backoff(l.failures)
	l.retryAt = l.now().Add(wait)
	return wait
}

// Succeed clears the wrong passphrase count without unlocking, for a
// passphrase checked to change a setting rather than to unlock.
func (l *Locker) Succeed() {
	l.mu.Lock()
	l.failures = 0
	l.retryAt = time.Time{}
	l.mu.Unlock()
}

// Failures returns the number of wrong passphrases in a row.
func (l *Locker) Failures() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.failures
}

// RestoreFailures sets the wrong passphrase count remembered from an earlier
// run, so quitting and relaunching does not skip the wait.
func (l *Locker) RestoreFailures(n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = max(n, 0)
	l.retryAt = l.now().Add(Backoff(l.failures))
}
