package applock

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeClock is a clock the tests move by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newLocker() (*Locker, *fakeClock, *[]bool) {
	clock := &fakeClock{t: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)}
	l := New(clock.now)
	var changes []bool
	l.OnChange(func(locked bool) { changes = append(changes, locked) })
	return l, clock, &changes
}

func TestLockDoesNothingWhileOff(t *testing.T) {
	l, _, changes := newLocker()
	if l.Lock() {
		t.Fatal("Lock locked with the lock off")
	}
	if l.Locked() || len(*changes) != 0 {
		t.Fatalf("locked=%v changes=%v, want unlocked and no change", l.Locked(), *changes)
	}
}

func TestLockAndUnlock(t *testing.T) {
	l, _, changes := newLocker()
	l.Configure(true, 0)
	if !l.Lock() {
		t.Fatal("Lock did not lock")
	}
	if l.Lock() {
		t.Fatal("a second Lock reported a change")
	}
	if !l.Locked() {
		t.Fatal("not locked after Lock")
	}
	l.Unlock()
	if l.Locked() {
		t.Fatal("still locked after Unlock")
	}
	l.Unlock()
	if got := *changes; len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("changes = %v, want [true false]", got)
	}
}

func TestIdleLock(t *testing.T) {
	tests := []struct {
		name    string
		idle    time.Duration
		elapsed time.Duration
		touchAt time.Duration
		want    bool
	}{
		{name: "before the idle time", idle: 5 * time.Minute, elapsed: 4 * time.Minute, want: false},
		{name: "at the idle time", idle: 5 * time.Minute, elapsed: 5 * time.Minute, want: true},
		{name: "activity restarts the wait", idle: 5 * time.Minute, elapsed: 7 * time.Minute, touchAt: 3 * time.Minute, want: false},
		{name: "zero idle never locks by itself", idle: 0, elapsed: 24 * time.Hour, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, clock, _ := newLocker()
			l.Configure(true, tt.idle)
			if tt.touchAt > 0 {
				clock.advance(tt.touchAt)
				l.Touch()
				clock.advance(tt.elapsed - tt.touchAt)
			} else {
				clock.advance(tt.elapsed)
			}
			if got := l.Tick(); got != tt.want {
				t.Fatalf("Tick() = %v, want %v", got, tt.want)
			}
			if l.Locked() != tt.want {
				t.Fatalf("Locked() = %v, want %v", l.Locked(), tt.want)
			}
		})
	}
}

func TestEnablingStartsTheIdleClock(t *testing.T) {
	l, clock, _ := newLocker()
	clock.advance(time.Hour)
	l.Configure(true, 5*time.Minute)
	if l.Tick() {
		t.Fatal("locked right after the lock was turned on")
	}
}

func TestTouchWhileLockedDoesNotCount(t *testing.T) {
	l, clock, _ := newLocker()
	l.Configure(true, 5*time.Minute)
	l.Lock()
	clock.advance(10 * time.Minute)
	l.Touch()
	l.Unlock()
	clock.advance(4 * time.Minute)
	if l.Tick() {
		t.Fatal("locked again before the idle time after unlocking")
	}
}

func TestTurningOffUnlocks(t *testing.T) {
	l, _, changes := newLocker()
	l.Configure(true, 0)
	l.Lock()
	l.Fail()
	l.Configure(false, 0)
	if l.Locked() {
		t.Fatal("still locked after the lock was turned off")
	}
	if l.Failures() != 0 {
		t.Fatalf("failures = %d after turning off, want 0", l.Failures())
	}
	if got := *changes; len(got) != 2 || got[1] {
		t.Fatalf("changes = %v, want [true false]", got)
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{0, 0},
		{1, 0},
		{3, 0},
		{4, 5 * time.Second},
		{5, 10 * time.Second},
		{6, 20 * time.Second},
		{12, 15 * time.Minute},
		{1000, 15 * time.Minute},
	}
	for _, tt := range tests {
		if got := Backoff(tt.failures); got != tt.want {
			t.Errorf("Backoff(%d) = %v, want %v", tt.failures, got, tt.want)
		}
	}
}

func TestFailWaitAndUnlockReset(t *testing.T) {
	l, clock, _ := newLocker()
	l.Configure(true, 0)
	l.Lock()
	for range 3 {
		if wait := l.Fail(); wait != 0 {
			t.Fatalf("wait %v within the free attempts", wait)
		}
	}
	if wait := l.Fail(); wait != 5*time.Second {
		t.Fatalf("fourth failure wait = %v, want 5s", wait)
	}
	if got := l.Wait(); got != 5*time.Second {
		t.Fatalf("Wait() = %v, want 5s", got)
	}
	clock.advance(2 * time.Second)
	if got := l.Wait(); got != 3*time.Second {
		t.Fatalf("Wait() after 2s = %v, want 3s", got)
	}
	clock.advance(3 * time.Second)
	if got := l.Wait(); got != 0 {
		t.Fatalf("Wait() once elapsed = %v, want 0", got)
	}
	l.Unlock()
	if l.Failures() != 0 || l.Wait() != 0 {
		t.Fatalf("failures=%d wait=%v after unlock, want 0 and 0", l.Failures(), l.Wait())
	}
}

func TestRestoreFailuresKeepsTheWait(t *testing.T) {
	l, _, _ := newLocker()
	l.Configure(true, 0)
	l.RestoreFailures(5)
	if got := l.Wait(); got != 10*time.Second {
		t.Fatalf("Wait() after restoring 5 failures = %v, want 10s", got)
	}
	if wait := l.Fail(); wait != 20*time.Second {
		t.Fatalf("next failure wait = %v, want 20s", wait)
	}
}

func TestPassphraseHash(t *testing.T) {
	hash, err := HashPassphrase("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("hash contains the passphrase")
	}
	if !strings.HasPrefix(hash, "scrypt$32768$8$1$") {
		t.Fatalf("hash %q does not carry its parameters", hash)
	}
	again, err := HashPassphrase("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if again == hash {
		t.Fatal("two hashes of the same passphrase are equal; the salt is missing")
	}
	for _, tt := range []struct {
		pass string
		want bool
	}{
		{"correct horse", true},
		{"correct horse ", false},
		{"Correct horse", false},
		{"", false},
	} {
		got, err := VerifyPassphrase(tt.pass, hash)
		if err != nil {
			t.Fatalf("VerifyPassphrase(%q): %v", tt.pass, err)
		}
		if got != tt.want {
			t.Errorf("VerifyPassphrase(%q) = %v, want %v", tt.pass, got, tt.want)
		}
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	for _, bad := range []string{
		"",
		"plain",
		"bcrypt$1$2$3$4$5",
		"scrypt$x$8$1$c2FsdA$a2V5",
		"scrypt$32768$8$1$!!$a2V5",
		"scrypt$32768$8$1$c2FsdA$",
	} {
		if _, err := VerifyPassphrase("p", bad); !errors.Is(err, ErrBadHash) {
			t.Errorf("VerifyPassphrase(%q) err = %v, want ErrBadHash", bad, err)
		}
	}
}
