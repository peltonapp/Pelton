package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// Send is off-pool and pauses nothing: a pending outbox message must not
// soft-pause any account's background sync.
func TestPendingOutboxPausesNoAccount(t *testing.T) {
	a := newHoldTestApp(t)
	idA, _ := seedHoldAccount(t, a, "a@example.test")
	idB, _ := seedHoldAccount(t, a, "b@example.test")
	a.queue = outbox.NewQueue(a.store)
	if _, err := a.store.InsertOutbox(a.ctx, storage.OutboxRow{AccountID: idA, EnvelopeFrom: "a@example.test", Recipients: "x@y.test", Raw: []byte("raw"), State: "queued"}); err != nil {
		t.Fatal(err)
	}
	if !a.queue.Pending(a.ctx) {
		t.Fatal("fixture: outbox should be pending")
	}
	for _, id := range []int64{idA, idB} {
		a.ensureAccountScheduler(a.ctx, id)
		rt := a.accountSync(id)
		rt.pool.SetConfigured(1) // N=1 is where a pause would show most
		if rt.sched.SoftPauseRequested() {
			t.Fatalf("account %d paused by pending outbox", id)
		}
		if a.enginePause(rt)() {
			t.Fatalf("account %d engine pause check true with a pending outbox", id)
		}
	}
}

// Effective N is read when the live job begins: throttled to 1 mid-run, a live
// job soft-pauses background after the in-flight chunk, runs, and background
// resumes. The chunk is never aborted.
func TestLiveJobSoftPausesBackgroundWhenThrottledToOne(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	a.ensureAccountScheduler(a.ctx, id)
	art := a.accountSync(id)
	art.pool.SetConfigured(3)
	art.pool.SetEffective(1) // adaptive throttle fired mid-run

	pauseCheck := a.enginePause(art)
	chunkRunning := make(chan struct{}, 4)
	chunkRelease := make(chan struct{})
	bgDone := make(chan struct{})
	runs := 0
	art.sched.Enqueue(syncsched.Job{
		Priority: syncsched.PriorityBackgroundBody,
		Kind:     syncsched.JobFetchBodies,
		Run: func(ctx context.Context) error {
			runs++
			if runs > 1 {
				close(bgDone)
				return nil
			}
			chunkRunning <- struct{}{}
			select {
			case <-chunkRelease: // the in-flight chunk finishes on its own
			case <-ctx.Done():
				return ctx.Err() // would mean a hard abort
			}
			if pauseCheck() {
				return psync.ErrSoftPaused
			}
			close(bgDone)
			return nil
		},
	})
	<-chunkRunning

	liveRan := make(chan struct{})
	liveErr := make(chan error, 1)
	go func() {
		liveErr <- a.enqueueLiveAndWait(a.ctx, id, syncsched.JobOnDemandBody, 0, nil, func(context.Context) error {
			close(liveRan)
			return nil
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !pauseCheck() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !pauseCheck() {
		t.Fatal("live job did not soft-pause background at effective N=1")
	}
	select {
	case <-liveRan:
		t.Fatal("live job ran before the in-flight chunk finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(chunkRelease)
	select {
	case err := <-liveErr:
		if err != nil {
			t.Fatalf("live job: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("live job never ran after the chunk")
	}
	select {
	case <-bgDone:
	case <-time.After(2 * time.Second):
		t.Fatal("background did not resume after the live job")
	}
	if art.sched.SoftPauseRequested() {
		t.Fatal("soft-pause not cleared after the live job")
	}
}

// A live job on account A (effective N=1) pauses only A's scheduler.
func TestLiveJobOnOneAccountLeavesOtherUnpaused(t *testing.T) {
	a := newHoldTestApp(t)
	idA, _ := seedHoldAccount(t, a, "a@example.test")
	idB, _ := seedHoldAccount(t, a, "b@example.test")
	a.ensureAccountScheduler(a.ctx, idA)
	a.ensureAccountScheduler(a.ctx, idB)
	rtA, rtB := a.accountSync(idA), a.accountSync(idB)
	rtA.pool.SetConfigured(1)
	rtB.pool.SetConfigured(1)

	release, _ := a.beginLivePause(idA)
	if !rtA.sched.SoftPauseRequested() {
		t.Fatal("A should soft-pause for its live job at N=1")
	}
	if rtB.sched.SoftPauseRequested() || a.enginePause(rtB)() {
		t.Fatal("B paused by A's live job")
	}
	release()
	if rtA.sched.SoftPauseRequested() {
		t.Fatal("A pause not cleared")
	}
}

// At effective N>=2 a live job reserves a slot and pauses nothing.
func TestLiveJobAtEffectiveTwoDoesNotPause(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	a.ensureAccountScheduler(a.ctx, id)
	rt := a.accountSync(id)
	rt.pool.SetConfigured(3)
	rt.pool.SetEffective(2)
	release, _ := a.beginLivePause(id)
	defer release()
	if rt.sched.SoftPauseRequested() {
		t.Fatal("effective N=2 must not soft-pause")
	}
}
