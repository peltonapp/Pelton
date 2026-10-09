package syncsched

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	mailsync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

func TestLiveRunsBeforeBackgroundEvenIfBgQueuedFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		done := make(chan struct{})
		var finished int
		rec := func(name string) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			finished++
			if finished == 2 {
				close(done)
			}
		}

		s := New(1)
		s.Enqueue(Job{
			Priority: PriorityBackgroundBody,
			Kind:     JobFetchBodies,
			Run: func(context.Context) error {
				rec("bg")
				return nil
			},
		})
		releaseLive := make(chan struct{})
		s.Enqueue(Job{
			Priority: PriorityLive,
			Kind:     JobManualSync,
			Run: func(ctx context.Context) error {
				rec("live")
				select {
				case <-releaseLive:
				case <-ctx.Done():
				}
				return nil
			},
		})

		s.Start(context.Background(), pool.New(3), 1)
		t.Cleanup(s.Stop)

		// Background must not start while the live job is still running.
		synctest.Wait()
		mu.Lock()
		got := append([]string(nil), order...)
		mu.Unlock()
		if len(got) != 1 || got[0] != "live" {
			t.Fatalf("while live runs, order %v, want [live]", got)
		}
		close(releaseLive)
		waitCh(t, done)

		mu.Lock()
		defer mu.Unlock()
		if len(order) != 2 || order[0] != "live" || order[1] != "bg" {
			t.Fatalf("order %v, want [live bg]", order)
		}
	})
}

func TestStubsRunBeforeBodies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		done := make(chan struct{})
		var finished int
		rec := func(name string) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			finished++
			if finished == 2 {
				close(done)
			}
		}

		s := New(1)
		s.Enqueue(Job{
			Priority: PriorityBackgroundBody,
			Kind:     JobFetchBodies,
			FolderID: 7,
			Run: func(context.Context) error {
				rec("bodies")
				return nil
			},
		})
		s.Enqueue(Job{
			Priority: PriorityBackgroundStubs,
			Kind:     JobListStubs,
			FolderID: 7,
			Run: func(context.Context) error {
				rec("stubs")
				return nil
			},
		})

		s.Start(context.Background(), pool.New(3), 0)
		t.Cleanup(s.Stop)
		waitCh(t, done)

		mu.Lock()
		defer mu.Unlock()
		if len(order) != 2 || order[0] != "stubs" || order[1] != "bodies" {
			t.Fatalf("order %v, want [stubs bodies]", order)
		}
	})
}

func TestSoftPauseHoldsBackgroundUntilCleared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(1)
		bgStarted := make(chan struct{})
		liveDone := make(chan struct{})

		s.RequestSoftPause()
		if !s.SoftPauseRequested() {
			t.Fatal("soft pause should be requested")
		}
		s.Enqueue(Job{
			Priority: PriorityBackgroundBody,
			Kind:     JobFetchBodies,
			Run: func(context.Context) error {
				close(bgStarted)
				return nil
			},
		})
		s.Enqueue(Job{
			Priority: PriorityLive,
			Kind:     JobOnDemandBody,
			Run: func(context.Context) error {
				close(liveDone)
				return nil
			},
		})

		s.Start(context.Background(), pool.New(3), 0)
		t.Cleanup(s.Stop)
		waitCh(t, liveDone)

		synctest.Wait()
		select {
		case <-bgStarted:
			t.Fatal("background ran while soft-paused")
		default:
		}

		s.ClearSoftPause()
		if s.SoftPauseRequested() {
			t.Fatal("soft pause should be cleared")
		}
		waitCh(t, bgStarted)
	})
}

func TestSoftPausedBodyYieldsToLiveThenResumes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		done := make(chan struct{})
		rec := func(name string) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			if len(order) == 3 {
				close(done)
			}
		}

		var s *Scheduler
		var calls int
		var firstIDs []string
		s = New(4)
		s.Enqueue(Job{
			Priority:  PriorityBackgroundBody,
			Kind:      JobFetchBodies,
			FolderID:  3,
			RemoteIDs: []string{"a", "b"},
			Run: func(ctx context.Context) error {
				mu.Lock()
				calls++
				n := calls
				if n == 1 {
					firstIDs = append([]string(nil), RemoteIDs(ctx)...)
				}
				mu.Unlock()
				rec("bg")
				if n == 1 {
					s.Enqueue(Job{
						Priority: PriorityLive,
						Kind:     JobNewMail,
						Run: func(context.Context) error {
							rec("live")
							s.ClearSoftPause()
							return nil
						},
					})
					s.RequestSoftPause()
					return mailsync.ErrSoftPaused
				}
				return nil
			},
		})

		s.Start(context.Background(), pool.New(3), 0)
		t.Cleanup(s.Stop)
		waitCh(t, done)

		mu.Lock()
		defer mu.Unlock()
		if len(order) != 3 || order[0] != "bg" || order[1] != "live" || order[2] != "bg" {
			t.Fatalf("order %v, want [bg live bg]", order)
		}
		if len(firstIDs) != 2 || firstIDs[0] != "a" || firstIDs[1] != "b" {
			t.Fatalf("first attempt ids %v, want [a b]", firstIDs)
		}
	})
}

func TestLiveUsesReservedSlotWhileBackgroundWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		p := pool.New(3)
		if err := p.Acquire(ctx, pool.Background); err != nil {
			t.Fatal(err)
		}
		if err := p.Acquire(ctx, pool.Background); err != nil {
			t.Fatal(err)
		}

		s := New(1)
		bgRan := make(chan struct{})
		liveRan := make(chan struct{})
		s.Enqueue(Job{
			Priority: PriorityBackgroundBody,
			Kind:     JobFetchBodies,
			Run: func(context.Context) error {
				close(bgRan)
				return nil
			},
		})
		s.Start(ctx, p, 0)
		t.Cleanup(s.Stop)

		synctest.Wait()
		select {
		case <-bgRan:
			t.Fatal("background took the reserved live slot")
		default:
		}

		s.Enqueue(Job{
			Priority: PriorityLive,
			Kind:     JobManualSync,
			Run: func(context.Context) error {
				close(liveRan)
				return nil
			},
		})
		waitCh(t, liveRan)

		select {
		case <-bgRan:
			t.Fatal("background ran while both background slots were held")
		default:
		}

		p.Release(pool.Background)
		waitCh(t, bgRan)
	})
}

func TestStopCancelsWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(1)
		ran := make(chan struct{})
		s.RequestSoftPause()
		s.Enqueue(Job{
			Priority: PriorityBackgroundStubs,
			Kind:     JobListStubs,
			Run: func(context.Context) error {
				close(ran)
				return nil
			},
		})
		s.Start(context.Background(), pool.New(1), 0)

		stopped := make(chan struct{})
		go func() {
			s.Stop()
			close(stopped)
		}()
		waitCh(t, stopped)

		synctest.Wait()
		select {
		case <-ran:
			t.Fatal("background ran after stop")
		default:
		}
	})
}

func TestReconcileRunsAfterStubsAndBodies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(1)
		var mu sync.Mutex
		var order []string
		record := func(name string) func(context.Context) error {
			return func(context.Context) error {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
				return nil
			}
		}
		done := make(chan struct{})
		s.Enqueue(Job{Priority: PriorityBackgroundReconcile, Kind: JobFullReconcile, Run: func(ctx context.Context) error {
			_ = record("reconcile")(ctx)
			close(done)
			return nil
		}})
		s.Enqueue(Job{Priority: PriorityBackgroundBody, Kind: JobFetchBodies, Run: record("bodies")})
		s.Enqueue(Job{Priority: PriorityBackgroundStubs, Kind: JobListStubs, Run: record("stubs")})
		s.Start(context.Background(), pool.New(1), 0)
		t.Cleanup(s.Stop)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("reconcile never ran")
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.Join(order, ",") != "stubs,bodies,reconcile" {
			t.Fatalf("order %v, want stubs, bodies, reconcile", order)
		}
	})
}

func TestBackgroundBusyCountsReconcile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(1)
		s.Enqueue(Job{Priority: PriorityBackgroundReconcile, Kind: JobFullReconcile})
		if !s.BackgroundBusy() {
			t.Fatal("a queued reconcile is background work")
		}
	})
}

func waitCh(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

// IDLE at N=1 frees the only slot and must not soft-pause again until a
// queued background job is past the hold check, or that job requeues
// without running and the campaign starves. The wait has to cover the gap
// between the job's Acquire returning and that check.
func TestWaitBackgroundStartedCoversAcquireToGateGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(1)
		p := pool.New(1)
		if err := p.Acquire(context.Background(), pool.Live); err != nil {
			t.Fatalf("hold live slot: %v", err)
		}
		s.RequestSoftPause()

		atGate := make(chan struct{})
		openGate := make(chan struct{})
		var gateOnce sync.Once
		s.beforeBackgroundGate = func() {
			gateOnce.Do(func() { close(atGate) })
			<-openGate
		}
		ran := make(chan struct{})
		s.Enqueue(Job{
			Priority: PriorityBackgroundStubs,
			Kind:     JobListStubs,
			Run: func(context.Context) error {
				close(ran)
				return nil
			},
		})
		s.Start(context.Background(), p, 0)
		t.Cleanup(s.Stop)
		// Cleanups run last-in first-out: open the gate before Stop waits
		// for the job parked on it, so a failed assertion cannot deadlock.
		openGateOnce := sync.OnceFunc(func() { close(openGate) })
		t.Cleanup(openGateOnce)

		since := s.BackgroundStarts()
		p.Release(pool.Live)
		s.ClearSoftPause()

		// Start waiting only once the job has its slot and is parked before
		// the hold check: it is off the queue and not yet started.
		waitCh(t, atGate)
		waited := make(chan error, 1)
		go func() { waited <- s.WaitBackgroundStarted(context.Background(), since) }()
		synctest.Wait()
		select {
		case err := <-waited:
			t.Fatalf("wait returned (%v) while the job had the slot but had not passed the hold check", err)
		default:
		}

		openGateOnce()
		select {
		case err := <-waited:
			if err != nil {
				t.Fatalf("wait: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("wait did not return after the background job started")
		}
		// The caller soft-pauses again right away. The job is already committed
		// and must still run instead of requeueing.
		s.RequestSoftPause()
		waitCh(t, ran)
		if got := s.BackgroundStarts(); got != since+1 {
			t.Fatalf("background starts = %d, want %d", got, since+1)
		}
	})
}

// A body job queued behind a running stubs job is neither held nor launching,
// yet it will start once the stubs job ends. A wait that began in that window
// must keep waiting for it, not report that nothing can start.
func TestWaitBackgroundStartedWaitsForQueuedJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(1)
		stubsRunning := make(chan struct{})
		finishStubs := make(chan struct{})
		// Release the stubs job before Stop waits for it, so a failed
		// assertion cannot deadlock.
		finishStubsOnce := sync.OnceFunc(func() { close(finishStubs) })
		s.Enqueue(Job{
			Priority: PriorityBackgroundStubs,
			Kind:     JobListStubs,
			Run: func(context.Context) error {
				close(stubsRunning)
				<-finishStubs
				return nil
			},
		})
		bodyRan := make(chan struct{})
		s.Enqueue(Job{
			Priority: PriorityBackgroundBody,
			Kind:     JobListStubs,
			Run: func(context.Context) error {
				close(bodyRan)
				return nil
			},
		})
		s.Start(context.Background(), pool.New(1), 0)
		t.Cleanup(s.Stop)
		t.Cleanup(finishStubsOnce)

		waitCh(t, stubsRunning)
		synctest.Wait()
		since := s.BackgroundStarts()
		waited := make(chan error, 1)
		go func() { waited <- s.WaitBackgroundStarted(context.Background(), since) }()
		synctest.Wait()
		select {
		case err := <-waited:
			t.Fatalf("wait returned (%v) while a background job was still queued", err)
		default:
		}

		finishStubsOnce()
		select {
		case err := <-waited:
			if err != nil {
				t.Fatalf("wait: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("wait did not return after the queued job started")
		}
		waitCh(t, bodyRan)
		if got := s.BackgroundStarts(); got != since+1 {
			t.Fatalf("background starts = %d, want %d", got, since+1)
		}
	})
}

func TestWaitBackgroundStartedReturnsWhenNothingCanStart(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *Scheduler)
	}{
		{name: "nothing queued", setup: func(*Scheduler) {}},
		{name: "soft-paused", setup: func(s *Scheduler) {
			s.RequestSoftPause()
			s.Enqueue(Job{Priority: PriorityBackgroundBody, Kind: JobFetchBodies})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := New(1)
				p := pool.New(1)
				if err := p.Acquire(context.Background(), pool.Live); err != nil {
					t.Fatalf("hold live slot: %v", err)
				}
				t.Cleanup(func() { p.Release(pool.Live) })
				tt.setup(s)
				s.Start(context.Background(), p, 0)
				t.Cleanup(s.Stop)

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				start := time.Now()
				if err := s.WaitBackgroundStarted(ctx, s.BackgroundStarts()); err != nil {
					t.Fatalf("wait: %v", err)
				}
				// Bubble time only passes if the wait blocks until the ctx deadline.
				if d := time.Since(start); d != 0 {
					t.Fatalf("wait took %v with nothing able to start", d)
				}
			})
		})
	}
}

func TestWaitBackgroundStartedEndsWithContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(1)
		p := pool.New(1)
		if err := p.Acquire(context.Background(), pool.Live); err != nil {
			t.Fatalf("hold live slot: %v", err)
		}
		t.Cleanup(func() { p.Release(pool.Live) })
		s.Enqueue(Job{Priority: PriorityBackgroundStubs, Kind: JobListStubs})
		s.Start(context.Background(), p, 0)
		t.Cleanup(s.Stop)

		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		if err := s.WaitBackgroundStarted(ctx, s.BackgroundStarts()); err == nil {
			t.Fatal("wait returned nil while the job was still waiting for a slot")
		}
	})
}

// Queued reconciles must stay in the queue while the background slots are
// full, so stubs enqueued later still start before them.
func TestStubsOvertakeQueuedReconcilesWhenSlotsFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		done := make(chan struct{})
		rec := func(name string) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			if len(order) == 4 {
				close(done)
			}
		}

		s := New(1)
		release := make(chan struct{})
		s.Enqueue(Job{
			Priority: PriorityBackgroundReconcile,
			Kind:     JobFullReconcile,
			FolderID: 1,
			Run: func(ctx context.Context) error {
				rec("reconcile1")
				select {
				case <-release:
				case <-ctx.Done():
				}
				return nil
			},
		})
		for i, name := range []string{"reconcile2", "reconcile3"} {
			s.Enqueue(Job{
				Priority: PriorityBackgroundReconcile,
				Kind:     JobFullReconcile,
				FolderID: int64(i + 2),
				Run: func(context.Context) error {
					rec(name)
					return nil
				},
			})
		}

		// N=2 leaves one background slot.
		s.Start(context.Background(), pool.New(2), 0)
		t.Cleanup(s.Stop)
		synctest.Wait()

		s.Enqueue(Job{
			Priority: PriorityBackgroundStubs,
			Kind:     JobListStubs,
			FolderID: 9,
			Run: func(context.Context) error {
				rec("stubs")
				return nil
			},
		})
		synctest.Wait()
		close(release)
		waitCh(t, done)

		mu.Lock()
		defer mu.Unlock()
		want := []string{"reconcile1", "stubs", "reconcile2", "reconcile3"}
		if strings.Join(order, ",") != strings.Join(want, ",") {
			t.Fatalf("order %v, want %v", order, want)
		}
	})
}

// Raising the pool's effective size is the only event here: the waiting
// background job must start on it, not on some later Enqueue or job end.
func TestBackgroundStartsWhenPoolGrows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := pool.New(3)
		p.SetEffective(1)
		s := New(1)
		release := make(chan struct{})
		firstIn := make(chan struct{})
		secondIn := make(chan struct{})
		s.Enqueue(Job{
			Priority: PriorityBackgroundStubs,
			Kind:     JobListStubs,
			FolderID: 1,
			Run: func(ctx context.Context) error {
				close(firstIn)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return nil
			},
		})
		s.Enqueue(Job{
			Priority: PriorityBackgroundStubs,
			Kind:     JobListStubs,
			FolderID: 2,
			Run: func(context.Context) error {
				close(secondIn)
				return nil
			},
		})
		s.Start(context.Background(), p, 0)
		t.Cleanup(s.Stop)
		t.Cleanup(sync.OnceFunc(func() { close(release) }))

		waitCh(t, firstIn)
		synctest.Wait()
		select {
		case <-secondIn:
			t.Fatal("second background job started with one background slot")
		default:
		}

		p.SetEffective(3)
		synctest.Wait()
		select {
		case <-secondIn:
		default:
			t.Fatal("second background job did not start after the pool grew")
		}
	})
}
