package desktop

import (
	"sync/atomic"
	"testing"
	"time"

	pimap "github.com/peltonapp/Pelton/internal/imap"
)

// selectRecordingIMAP counts SELECTs, which only the initial sync stub and body
// jobs issue in the worker's initial sync.
type selectRecordingIMAP struct {
	fakeIMAP
	selects *atomic.Int32
}

func (c *selectRecordingIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	c.selects.Add(1)
	return &pimap.Mailbox{Name: mailbox}, nil
}

// waitAtomic waits up to d for n to reach at least want.
func waitAtomic(n *atomic.Int32, want int32, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for n.Load() < want {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

// A holder that starts the worker again (a removal that failed) does so while
// it still holds the account and lets go afterwards. Every sync call that
// worker makes must get through the hold, not only its first: here the hold
// outlives the worker's whole start, the worst interleaving, and the initial
// sync must still run.
func TestWorkerStartedUnderHoldRunsInitialSync(t *testing.T) {
	t.Run("imap", func(t *testing.T) {
		a := newHoldTestApp(t)
		var selects atomic.Int32
		a.newIMAPClient = func(pimap.Config) (mailClient, error) {
			return &selectRecordingIMAP{selects: &selects}, nil
		}
		id, _ := seedHoldAccount(t, a, "held-imap@example.test")

		release := a.holdAccountSync(id)
		a.startHeldAccountWorker(id)
		ran := waitAtomic(&selects, 1, 2*time.Second)
		release()
		if !ran {
			t.Fatal("the worker started under the hold never ran its IMAP initial sync")
		}
	})
}

// A worker started by anything but the holder (profile start, backup restore)
// while the account is held must not run: an IMAP one writes folder rows
// before its first scheduler call, in the middle of the teardown.
func TestOtherWorkerStartDuringHoldDoesNothing(t *testing.T) {
	a := newHoldTestApp(t)
	var dials atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		dials.Add(1)
		return &fakeIMAP{}, nil
	}
	id, _ := seedHoldAccount(t, a, "alien@example.test")

	release := a.holdAccountSync(id)
	defer release()
	a.startAccountWorker(id)
	time.Sleep(100 * time.Millisecond)
	if n := dials.Load(); n != 0 {
		t.Errorf("a worker started during the hold opened %d IMAP sessions", n)
	}
	if workerFor(a, id) != nil {
		t.Error("a worker was registered during the hold")
	}
}
