package syncsched

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// copies counts the units of one width entry running at once.
type copies struct {
	calls, running, peak atomic.Int32
}

// enter counts a unit in and returns its call number (from 1) and how many
// run now, itself included.
func (c *copies) enter() (call, now int32) {
	call = c.calls.Add(1)
	now = c.running.Add(1)
	for p := c.peak.Load(); now > p && !c.peak.CompareAndSwap(p, now); p = c.peak.Load() {
	}
	return call, now
}

func (c *copies) leave() { c.running.Add(-1) }

// workQueue is a folder's backlog of units: each Run takes one; the last
// one returns nil, the others ErrAgain.
type workQueue struct{ left, done atomic.Int32 }

func (q *workQueue) unit() error {
	if q.left.Add(-1) < 0 {
		return nil
	}
	q.done.Add(1)
	if q.left.Load() > 0 {
		return ErrAgain
	}
	return nil
}

func widthOf(w int) func(int) int { return func(int) int { return w } }

// widthTask is a body generator of folder 1 that may run up to width
// copies; run is each unit.
func widthTask(width func(int) int, run func(ctx context.Context) error, done func(error)) Task[*conn] {
	return Task[*conn]{Kind: KindBodies, FolderID: 1, Key: "bodies:1", Width: width,
		Run:  func(ctx context.Context, _ *conn) error { return run(ctx) },
		Done: done}
}

// doneLog records every Done result.
type doneLog struct {
	mu   sync.Mutex
	errs []error
}

func (d *doneLog) add(err error) {
	d.mu.Lock()
	d.errs = append(d.errs, err)
	d.mu.Unlock()
}

func (d *doneLog) get() []error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.errs)
}

// At N=4 with IDLE holding one session, one folder's body entry runs on the
// other three sessions at once, never more, and every unit runs once.
func TestWidthEntryFillsTheSessionsIdleLeaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 4)
		synctest.Wait()
		var c copies
		var q workQueue
		q.left.Store(10)
		gate := make(chan struct{})
		var dones doneLog
		r.s.Enqueue(widthTask(widthOf(10), func(context.Context) error {
			c.enter()
			defer c.leave()
			<-gate
			return q.unit()
		}, dones.add))
		synctest.Wait()
		if got := c.running.Load(); got != 3 || r.parked.Load() != 1 {
			t.Fatalf("running %d parked %d, want 3 copies beside IDLE", got, r.parked.Load())
		}
		close(gate)
		synctest.Wait()
		if c.peak.Load() != 3 {
			t.Fatalf("peak %d copies, want 3", c.peak.Load())
		}
		if q.done.Load() != 10 {
			t.Fatalf("%d units done, want 10", q.done.Load())
		}
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
		if st := r.s.Stats(); st.Normal != 0 || st.Running != 0 {
			t.Fatalf("stats %+v after the end, want nothing queued or running", st)
		}
	})
}

// While the copies hold every normal session, IDLE's session still serves
// the fast lane before any copy ends.
func TestWidthEntryLeavesTheFastLaneToIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 4)
		synctest.Wait()
		var c copies
		gate := make(chan struct{})
		r.s.Enqueue(widthTask(widthOf(10), func(context.Context) error {
			c.enter()
			defer c.leave()
			<-gate
			return nil
		}, nil))
		synctest.Wait()
		r.s.Enqueue(r.task("open", KindOnDemandBody, 9, nil))
		synctest.Wait()
		if !slices.Contains(r.order(), "open") || c.running.Load() != 3 {
			t.Fatalf("ran %v with %d copies running, want the fast task served by IDLE first", r.order(), c.running.Load())
		}
		close(gate)
		synctest.Wait()
	})
}

// A shrink from 4 to 2 lets the running copies end, then runs one copy at
// a time beside IDLE, and no session is dialled above the new cap.
func TestWidthFollowsAShrink(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 4)
		synctest.Wait()
		var c copies
		var q workQueue
		q.left.Store(12)
		gate := make(chan struct{})
		var shrunk atomic.Bool
		r.s.Enqueue(widthTask(widthOf(10), func(context.Context) error {
			call, now := c.enter()
			defer c.leave()
			if call > 3 && shrunk.Load() && now > 1 {
				t.Errorf("unit %d ran beside %d others after the shrink to 2", call, now-1)
			}
			<-gate
			return q.unit()
		}, nil))
		synctest.Wait()
		if c.running.Load() != 3 {
			t.Fatalf("running %d, want 3", c.running.Load())
		}
		shrunk.Store(true)
		r.p.SetEffective(2)
		r.srv.limit.Store(2)
		close(gate)
		synctest.Wait()
		if q.done.Load() != 12 {
			t.Fatalf("%d units done, want 12", q.done.Load())
		}
		if o := r.srv.open.Load(); o > 2 {
			t.Fatalf("%d sessions open after the shrink to 2", o)
		}
	})
}

// Tunables.MaxBodyWidth caps the copies below what N allows.
func TestMaxBodyWidthCapsCopies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 4, Options{Tunables: Tunables{MaxBodyWidth: 2}})
		var c copies
		var q workQueue
		q.left.Store(6)
		gate := make(chan struct{})
		r.s.Enqueue(widthTask(widthOf(10), func(context.Context) error {
			c.enter()
			defer c.leave()
			<-gate
			return q.unit()
		}, nil))
		synctest.Wait()
		if got := c.running.Load(); got != 2 {
			t.Fatalf("running %d, want the cap of 2", got)
		}
		close(gate)
		synctest.Wait()
		if c.peak.Load() != 2 || q.done.Load() != 6 {
			t.Fatalf("peak %d done %d, want 2 and 6", c.peak.Load(), q.done.Load())
		}
	})
}

// The width is read at every pick: a width of 1 runs one copy whatever N.
func TestWidthOneRunsOneCopy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 4, Options{})
		var c copies
		var q workQueue
		q.left.Store(5)
		r.s.Enqueue(widthTask(widthOf(1), func(context.Context) error {
			c.enter()
			defer c.leave()
			time.Sleep(time.Second)
			return q.unit()
		}, nil))
		time.Sleep(time.Minute)
		synctest.Wait()
		if c.peak.Load() != 1 || q.done.Load() != 5 {
			t.Fatalf("peak %d done %d, want 1 and 5", c.peak.Load(), q.done.Load())
		}
	})
}

// One copy ends with nil while another runs; that one then returns
// ErrAgain: the entry stays and runs on, and Done comes once, at the end.
func TestWidthCopyEndsWhileSiblingRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		var c copies
		gA, gB := make(chan struct{}), make(chan struct{})
		var dones doneLog
		r.s.Enqueue(widthTask(widthOf(2), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			switch call {
			case 1:
				<-gA
				return nil
			case 2:
				<-gB
				return ErrAgain
			}
			return nil
		}, dones.add))
		synctest.Wait()
		close(gA)
		synctest.Wait()
		if len(dones.get()) != 0 || r.s.Stats().Normal != 1 {
			t.Fatalf("Done %v queued %d after one copy ended, want no Done and the entry kept", dones.get(), r.s.Stats().Normal)
		}
		close(gB)
		synctest.Wait()
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
		// The reopened entry runs up to two more copies; the first to
		// return nil ends it.
		if got := c.calls.Load(); got < 3 {
			t.Fatalf("%d units, want more after the ErrAgain reopened the entry", got)
		}
	})
}

// A copy that finds nothing to claim (ErrSaturated) holds the entry: no
// copy is dispatched again until a sibling ends, which reopens it.
func TestWidthSaturatedHoldsUntilASiblingEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		var c copies
		g1 := make(chan struct{})
		var dones doneLog
		r.s.Enqueue(widthTask(widthOf(3), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			switch call {
			case 1:
				<-g1
				return ErrAgain
			case 2, 3:
				return ErrSaturated
			}
			return nil
		}, dones.add))
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := c.calls.Load(); got != 3 {
			t.Fatalf("%d units while held, want 3: a saturated entry must not spin", got)
		}
		close(g1)
		synctest.Wait()
		if got := c.calls.Load(); got < 4 {
			t.Fatalf("%d units, want the sibling's end to reopen the entry", got)
		}
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
	})
}

// A plain task (no Width) that returns ErrSaturated because another task of
// its folder holds the ids waits for that task's end instead of spinning:
// the offline download beside the folder's body copies.
func TestSaturatedPlainTaskWaitsForTheFolder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		gate := make(chan struct{})
		r.s.Enqueue(r.task("bodies", KindBodies, 1, gate))
		synctest.Wait()
		var calls atomic.Int32
		var dones doneLog
		r.s.Enqueue(Task[*conn]{Kind: KindOffline, FolderID: 1, Key: "offline:1",
			Run: func(context.Context, *conn) error {
				if calls.Add(1) == 1 {
					return ErrSaturated
				}
				return nil
			}, Done: dones.add})
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("%d offline units while the bodies ran, want 1", calls.Load())
		}
		close(gate)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatalf("%d offline units, want a second one after the bodies ended", calls.Load())
		}
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
	})
}

// A saturated copy with nothing else of its folder running is not held:
// the ids it saw claimed were freed before it ended, so it runs again.
func TestSaturatedWithNothingRunningIsNotHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		var calls atomic.Int32
		var dones doneLog
		r.s.Enqueue(widthTask(widthOf(1), func(context.Context) error {
			if calls.Add(1) == 1 {
				return ErrSaturated
			}
			return nil
		}, dones.add))
		synctest.Wait()
		if calls.Load() != 2 || len(dones.get()) != 1 {
			t.Fatalf("%d units, Done %v: want a second unit and the end", calls.Load(), dones.get())
		}
	})
}

// A failed copy stops new copies; Done gets its error once the others
// drained, even though they asked for more.
func TestWidthFailedCopyDrains(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		var c copies
		gate := make(chan struct{})
		boom := errors.New("boom")
		var dones doneLog
		r.s.Enqueue(widthTask(widthOf(3), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			if call == 1 {
				return boom
			}
			<-gate
			return ErrAgain
		}, dones.add))
		synctest.Wait()
		if len(dones.get()) != 0 {
			t.Fatalf("Done %v while copies run", dones.get())
		}
		close(gate)
		synctest.Wait()
		if got := dones.get(); len(got) != 1 || !errors.Is(got[0], boom) {
			t.Fatalf("Done %v, want one boom", got)
		}
		if got := c.calls.Load(); got != 3 {
			t.Fatalf("%d units, want 3: no copy after the failure", got)
		}
	})
}

// Stop while three copies run: one Done(ErrStopped), after the last ends.
func TestWidthStopWaitsForEveryCopy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		var c copies
		gate := make(chan struct{})
		var dones doneLog
		r.s.Enqueue(widthTask(widthOf(3), func(context.Context) error {
			c.enter()
			defer c.leave()
			<-gate
			return ErrAgain
		}, dones.add))
		synctest.Wait()
		stopped := make(chan error, 1)
		go func() { stopped <- r.s.Stop(context.Background()) }()
		synctest.Wait()
		if len(dones.get()) != 0 {
			t.Fatalf("Done %v before the copies ended", dones.get())
		}
		close(gate)
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		if got := dones.get(); len(got) != 1 || !errors.Is(got[0], ErrStopped) {
			t.Fatalf("Done %v, want one ErrStopped", got)
		}
		if c.calls.Load() != 3 {
			t.Fatalf("%d units, want 3", c.calls.Load())
		}
	})
}

// A queued list of the folder keeps new copies from starting (the list goes
// first even in a lower class); a barrier and a full check of the folder
// wait for every running copy.
func TestWidthFolderRules(t *testing.T) {
	t.Run("queued list", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, 2, Options{})
			gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
			var c copies
			r.s.Enqueue(widthTask(widthOf(2), func(context.Context) error {
				call, _ := c.enter()
				defer c.leave()
				r.record("body")
				if int(call) > len(gates) {
					return nil
				}
				<-gates[call-1]
				return ErrAgain
			}, nil))
			synctest.Wait()
			list := r.task("list", KindListStubs, 1, nil)
			list.Class = ClassFullCheck // below the bodies' class
			r.s.Enqueue(list)
			close(gates[0])
			synctest.Wait()
			got := r.order()
			if i := slices.Index(got, "list"); i != 2 {
				t.Fatalf("order %v, want the list right after the two first copies", got)
			}
			close(gates[1])
			synctest.Wait()
		})
	})
	for _, kind := range []Kind{KindFolderTree, KindFullCheck} {
		t.Run(kind.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newRig(t, 3, Options{})
				gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
				var c copies
				r.s.Enqueue(widthTask(widthOf(2), func(context.Context) error {
					call, _ := c.enter()
					defer c.leave()
					if int(call) <= len(gates) {
						<-gates[call-1]
						return ErrAgain
					}
					return nil
				}, nil))
				synctest.Wait()
				other := r.task("other", kind, 1, nil)
				other.Barrier = kind == KindFolderTree
				r.s.Enqueue(other)
				synctest.Wait()
				close(gates[0])
				synctest.Wait()
				if slices.Contains(r.order(), "other") {
					t.Fatalf("%s ran while a copy runs", kind)
				}
				// A queued barrier holds new copies too; a queued full check
				// does not (as it never held a running generator's next unit).
				if other.Barrier && c.calls.Load() != 2 {
					t.Fatalf("%d units, want no new copy while the barrier waits", c.calls.Load())
				}
				close(gates[1])
				synctest.Wait()
				if !slices.Contains(r.order(), "other") {
					t.Fatalf("%s never ran", kind)
				}
			})
		})
	}
}

// Across a Group: a second scheduler's entry with the same key waits for
// every copy of the first's; each copy records one shape and its end takes
// one away.
func TestWidthAcrossGroup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := &server{t: t}
		srv.limit.Store(4)
		p := pool.New(4)
		sessions := pool.NewSessions(p, srv.dial, time.Minute)
		t.Cleanup(sessions.Close)
		var g Group
		s1 := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		s2 := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		t.Cleanup(func() { _ = s1.Stop(context.Background()); _ = s2.Stop(context.Background()) })
		gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
		var c copies
		s1.Enqueue(widthTask(widthOf(2), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			<-gates[call-1]
			return nil
		}, nil))
		synctest.Wait()
		shapes := func() int {
			g.mu.Lock()
			defer g.mu.Unlock()
			return len(g.running[s1])
		}
		if c.running.Load() != 2 || shapes() != 2 {
			t.Fatalf("running %d shapes %d, want 2 copies with a shape each", c.running.Load(), shapes())
		}
		var ran atomic.Bool
		s2.Enqueue(widthTask(widthOf(2), func(context.Context) error { ran.Store(true); return nil }, nil))
		synctest.Wait()
		close(gates[0])
		synctest.Wait()
		if shapes() != 1 || ran.Load() {
			t.Fatalf("shapes %d ran %v after one copy ended, want 1 shape left and s2 waiting", shapes(), ran.Load())
		}
		close(gates[1])
		synctest.Wait()
		if shapes() != 0 || !ran.Load() {
			t.Fatalf("shapes %d ran %v, want s2's entry run after both copies", shapes(), ran.Load())
		}
	})
}

// A task with a width entry's key joins it while it runs: its Run never
// runs, its Done comes with the entry's. A join after a copy ended with nil
// (the entry draining) reopens it, and a join while the only copy runs
// gets one more unit, which reads what the joiner added.
func TestWidthJoin(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, 2, Options{})
			var c copies
			gate := make(chan struct{})
			var dones doneLog
			r.s.Enqueue(widthTask(widthOf(2), func(context.Context) error {
				call, _ := c.enter()
				defer c.leave()
				if call <= 2 {
					<-gate
				}
				return nil
			}, dones.add))
			synctest.Wait()
			var joinerRan atomic.Bool
			if !r.s.EnqueueJoined(widthTask(widthOf(2), func(context.Context) error { joinerRan.Store(true); return nil }, dones.add)) {
				t.Fatal("a task with a running width entry's key did not join it")
			}
			close(gate)
			synctest.Wait()
			if got := dones.get(); len(got) != 2 || got[0] != nil || got[1] != nil {
				t.Fatalf("Done %v, want two nil, one per caller", got)
			}
			if joinerRan.Load() {
				t.Fatal("the joiner's Run ran")
			}
			if r.s.Stats().Normal != 0 {
				t.Fatal("an entry left in the lane")
			}
		})
	})
	t.Run("draining", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, 2, Options{})
			var c copies
			gate := make(chan struct{})
			var dones doneLog
			r.s.Enqueue(widthTask(widthOf(2), func(context.Context) error {
				call, _ := c.enter()
				defer c.leave()
				if call == 2 {
					<-gate
				}
				return nil
			}, dones.add))
			synctest.Wait()
			if c.calls.Load() != 2 {
				t.Fatalf("%d units, want 2", c.calls.Load())
			}
			if !r.s.EnqueueJoined(widthTask(widthOf(2), func(context.Context) error { return nil }, dones.add)) {
				t.Fatal("a join on a draining entry did not join")
			}
			synctest.Wait()
			if got := c.calls.Load(); got != 3 {
				t.Fatalf("%d units, want a third one after the join reopened the entry", got)
			}
			close(gate)
			synctest.Wait()
			if got := dones.get(); len(got) != 2 {
				t.Fatalf("Done %v, want two", got)
			}
		})
	})
	t.Run("one more unit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, 2, Options{})
			var c copies
			gate := make(chan struct{})
			var dones doneLog
			r.s.Enqueue(widthTask(widthOf(1), func(context.Context) error {
				call, _ := c.enter()
				defer c.leave()
				if call == 1 {
					<-gate
				}
				return nil
			}, dones.add))
			synctest.Wait()
			r.s.EnqueueJoined(widthTask(widthOf(1), func(context.Context) error { return nil }, dones.add))
			close(gate)
			synctest.Wait()
			if got := c.calls.Load(); got != 2 {
				t.Fatalf("%d units, want 2: the running unit may have read before the join", got)
			}
			if got := dones.get(); len(got) != 2 {
				t.Fatalf("Done %v, want two", got)
			}
		})
	})
}

// gateProvider refuses a Get with ErrSessionsFull after the test let it
// through hold, and serves sessions once refuse is off.
type gateProvider struct {
	fullProvider
	hold chan struct{}
}

func (p *gateProvider) Get(ctx context.Context) (*conn, error) {
	if p.refuse.Load() {
		<-p.hold
	}
	return p.fullProvider.Get(ctx)
}

// A task put back after the provider refused its session merges with a
// duplicate queued meanwhile: one entry per key, both Done called once.
func TestRestoreMergesAQueuedDuplicate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &gateProvider{hold: make(chan struct{})}
		prov.refuse.Store(true)
		s := New(context.Background(), prov, Options{})
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		var runs atomic.Int32
		var dones doneLog
		mk := func() Task[*conn] {
			return Task[*conn]{Kind: KindListStubs, FolderID: 1, Key: "list:1",
				Run:  func(context.Context, *conn) error { runs.Add(1); return nil },
				Done: dones.add}
		}
		s.Enqueue(mk())
		synctest.Wait() // its worker waits in Get
		if s.EnqueueJoined(mk()) {
			t.Fatal("joined a task whose worker is getting a session")
		}
		prov.hold <- struct{}{} // refused: the first task goes back
		synctest.Wait()
		if got := s.Stats().Normal; got != 1 {
			t.Fatalf("%d queued entries with one key after the refusal, want 1", got)
		}
		prov.refuse.Store(false)
		prov.notify()
		synctest.Wait()
		if runs.Load() != 1 || len(dones.get()) != 2 {
			t.Fatalf("runs %d Done %v, want one run and both Done", runs.Load(), dones.get())
		}
	})
}

// ErrSaturated is an ErrAgain: a caller that only knows ErrAgain (a unit's
// throttle accounting, a retry) treats it as "more left".
func TestErrSaturatedIsErrAgain(t *testing.T) {
	if !errors.Is(ErrSaturated, ErrAgain) || errors.Is(ErrSaturated, ErrAgainBroken) {
		t.Fatal("ErrSaturated must be an ErrAgain that keeps its session")
	}
}

// capProvider claims a Limit of 3 but its server takes 2 sessions: a third
// Get fails with a dial error.
type capProvider struct {
	mu            sync.Mutex
	out, refused  int
	watchers      []func()
	limit, server int
}

var errRefused = errors.New("NO [UNAVAILABLE] Too many simultaneous connections")

func (p *capProvider) Get(context.Context) (*conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.out >= p.server {
		p.refused++
		return nil, errRefused
	}
	p.out++
	return &conn{srv: &server{}}, nil
}

func (p *capProvider) Put(*conn, bool) {
	p.mu.Lock()
	p.out--
	p.mu.Unlock()
}
func (p *capProvider) Limit() int { return p.limit }
func (p *capProvider) Watch(fn func()) func() {
	p.mu.Lock()
	p.watchers = append(p.watchers, fn)
	p.mu.Unlock()
	return func() {}
}
func (p *capProvider) Close() {}

// A copy whose session cannot be had (a server refusing one connection
// too many) while other copies run gives its place back instead of failing
// the entry: the copies with sessions finish the work, Done gets nil, and
// the refused dials stay few (the backoff), not one per pick.
func TestWidthCopyWithoutSessionGivesBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &capProvider{limit: 3, server: 2}
		s := New(context.Background(), prov, Options{})
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		var c copies
		var q workQueue
		q.left.Store(20)
		var dones doneLog
		s.Enqueue(widthTask(widthOf(3), func(context.Context) error {
			c.enter()
			defer c.leave()
			time.Sleep(time.Second)
			return q.unit()
		}, dones.add))
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
		if q.done.Load() != 20 || c.peak.Load() != 2 {
			t.Fatalf("done %d peak %d, want 20 units on the 2 sessions the server takes", q.done.Load(), c.peak.Load())
		}
		prov.mu.Lock()
		refused := prov.refused
		prov.mu.Unlock()
		if refused == 0 || refused > 12 {
			t.Fatalf("%d refused Gets over 10 s of work, want a few", refused)
		}
	})
}

// The last copy's failed Get still ends the entry with the error, as for
// any task: there is no sibling to carry the work.
func TestWidthLastCopyWithoutSessionFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &capProvider{limit: 3, server: 0}
		s := New(context.Background(), prov, Options{})
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		var dones doneLog
		s.Enqueue(widthTask(widthOf(3), func(context.Context) error { return nil }, dones.add))
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := dones.get(); len(got) != 1 || !errors.Is(got[0], errRefused) {
			t.Fatalf("Done %v, want one refusal", got)
		}
	})
}

// A folder's list task queues its bodies as it ends, while its worker still
// holds the session; a new worker for the first copy then dials one
// connection too many. That worker's session carries the work (it picks the
// entry next), so the copy gives back instead of failing the folder.
func TestWidthFirstCopyRefusedBesideAWorkerWithASessionGivesBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &capProvider{limit: 3, server: 1}
		s := New(context.Background(), prov, Options{})
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		var q workQueue
		q.left.Store(5)
		var dones doneLog
		s.Enqueue(Task[*conn]{Kind: KindListStubs, FolderID: 1, Key: "list:1",
			Run: func(context.Context, *conn) error { return nil },
			Done: func(error) {
				s.Enqueue(widthTask(widthOf(3), func(context.Context) error { return q.unit() }, dones.add))
				// The list's worker keeps its session while the new
				// worker's dial is refused and handled.
				time.Sleep(time.Second)
			}})
		time.Sleep(time.Minute)
		synctest.Wait()
		prov.mu.Lock()
		refused := prov.refused
		prov.mu.Unlock()
		if refused == 0 {
			t.Fatal("no refused dial: the first copy's worker never raced the list's session")
		}
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil: the list's session carries the bodies", got)
		}
		if q.done.Load() != 5 {
			t.Fatalf("%d of 5 units done", q.done.Load())
		}
	})
}

// returnProvider is a server that takes one session. Its first refused Get
// tells refusing and returns only once that session came back: the dial
// was in flight while the scheduler's own session was given back.
type returnProvider struct {
	mu        sync.Mutex
	out       int
	refused   int
	refusing  chan struct{}
	put       chan struct{}
	putClosed bool
}

func (p *returnProvider) Get(context.Context) (*conn, error) {
	p.mu.Lock()
	if p.out < 1 {
		p.out++
		p.mu.Unlock()
		return &conn{srv: &server{}}, nil
	}
	p.refused++
	first := p.refused == 1
	p.mu.Unlock()
	if first {
		close(p.refusing)
		<-p.put
	}
	return nil, errRefused
}

func (p *returnProvider) Put(*conn, bool) {
	p.mu.Lock()
	p.out--
	if !p.putClosed {
		p.putClosed = true
		close(p.put)
	}
	p.mu.Unlock()
}
func (p *returnProvider) Limit() int                 { return 3 }
func (p *returnProvider) Watch(func()) (stop func()) { return func() {} }
func (p *returnProvider) Close()                     {}

// The list's worker gives its session back while the first copy's dial,
// refused for that very session, is still in flight. The refusal was the
// scheduler's own session: the copy gives back and the next Get reuses
// it, instead of failing the folder.
func TestWidthCopyRefusedWhileOwnSessionCameBackGivesBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &returnProvider{refusing: make(chan struct{}), put: make(chan struct{})}
		s := New(context.Background(), prov, Options{})
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		var q workQueue
		q.left.Store(5)
		var dones doneLog
		s.Enqueue(Task[*conn]{Kind: KindListStubs, FolderID: 1, Key: "list:1",
			Run: func(context.Context, *conn) error { return nil },
			Done: func(error) {
				// Width 1: the list's worker cannot take a second copy, so
				// it exits and gives its session back.
				s.Enqueue(widthTask(widthOf(1), func(context.Context) error { return q.unit() }, dones.add))
				<-prov.refusing
			}})
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil: the session that came back carries the bodies", got)
		}
		if q.done.Load() != 5 {
			t.Fatalf("%d of 5 units done", q.done.Load())
		}
	})
}

// shrinkProvider hands out the first session at once; later Gets dial
// until shrunk is closed, by when Limit has dropped from 3 to 1.
type shrinkProvider struct {
	mu       sync.Mutex
	gets     int
	limit    atomic.Int32
	shrunk   chan struct{}
	watchers []func()
}

func (p *shrinkProvider) Get(context.Context) (*conn, error) {
	p.mu.Lock()
	p.gets++
	first := p.gets == 1
	p.mu.Unlock()
	if !first {
		<-p.shrunk
	}
	return &conn{srv: &server{}}, nil
}

func (p *shrinkProvider) Put(*conn, bool) {}
func (p *shrinkProvider) Limit() int      { return int(p.limit.Load()) }
func (p *shrinkProvider) Watch(fn func()) (stop func()) {
	p.mu.Lock()
	p.watchers = append(p.watchers, fn)
	p.mu.Unlock()
	return func() {}
}
func (p *shrinkProvider) Close() {}

func (p *shrinkProvider) shrink() {
	p.limit.Store(1)
	close(p.shrunk)
	p.mu.Lock()
	fns := slices.Clone(p.watchers)
	p.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Copies picked at N=3 whose sessions come only after N dropped to 1 (a
// refused login halved it while they dialed) give the copy back before
// their unit: one unit runs at N=1, not three. (In the app the IDLE
// watcher's session, closed for the shrink, is the slot such a late dial
// takes.)
func TestWidthCopyWhoseSessionCameAfterAShrinkGivesBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &shrinkProvider{shrunk: make(chan struct{})}
		prov.limit.Store(3)
		s := New(context.Background(), prov, Options{})
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		var c copies
		var q workQueue
		q.left.Store(6)
		gate := make(chan struct{})
		var dones doneLog
		s.Enqueue(widthTask(widthOf(3), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			if call == 1 {
				<-gate
			}
			return q.unit()
		}, dones.add))
		synctest.Wait()
		if got := c.running.Load(); got != 1 {
			t.Fatalf("%d units before the shrink, want 1 (the others dial)", got)
		}
		prov.shrink()
		synctest.Wait()
		close(gate)
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
		if q.done.Load() != 6 || c.peak.Load() != 1 {
			t.Fatalf("done %d peak %d, want 6 units one at a time", q.done.Load(), c.peak.Load())
		}
	})
}

// Stats.Idle reports the IDLE watcher holding its session, not a worker
// given the role that still dials: a caller waiting for IDLE before it
// queues work must not race that dial for the server's last slot.
func TestStatsIdleWaitsForTheWatchersSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &shrinkProvider{shrunk: make(chan struct{})}
		prov.limit.Store(2)
		prov.gets = 1 // the first Get, IDLE's, dials until released
		s := New(context.Background(), prov, Options{})
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		release := sync.OnceFunc(func() { close(prov.shrunk) })
		t.Cleanup(release) // before Stop, which waits for the dial
		s.EnablePush(Push[*conn]{Park: func(ctx context.Context, _ *conn, wake <-chan struct{}) (bool, error) {
			select {
			case <-wake:
			case <-ctx.Done():
			}
			return false, nil
		}})
		synctest.Wait()
		if s.Stats().Idle {
			t.Fatal("Stats.Idle while the watcher's session still dials")
		}
		release() // the dial ends; Limit stays 2
		synctest.Wait()
		if !s.Stats().Idle {
			t.Fatal("Stats.Idle false with the watcher parked on its session")
		}
	})
}

// A width entry held by a sibling copy reopens when that sibling ends
// saturated too: it read the claims before the holder released them, so
// nothing of the folder runs any more and nothing else would reopen it.
// Held means a task of the folder runs (held ⇒ heldUpLocked).
func TestWidthHeldEntryReopensWhenTheLastCopySaturates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		var c copies
		g1 := make(chan struct{})
		var dones doneLog
		r.s.Enqueue(widthTask(widthOf(2), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			switch call {
			case 1:
				<-g1
				return ErrSaturated
			case 2:
				return ErrSaturated
			}
			return nil
		}, dones.add))
		synctest.Wait()
		if got := c.calls.Load(); got != 2 {
			t.Fatalf("%d units, want 2 (the second held by the first)", got)
		}
		close(g1)
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		st := r.s.Stats()
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("calls %d, Done %v, running %d, normal %d: want the entry reopened and ended once with nil",
				c.calls.Load(), got, st.Running, st.Normal)
		}
		if got := c.calls.Load(); got < 3 {
			t.Fatalf("%d units, want the reopened entry run again", got)
		}
	})
}

// A task of another folder ending in another scheduler of the Group leaves
// a held entry held: its folder's sibling copy still runs, so a new copy
// would only find the same ids claimed.
func TestGroupEndOfAnotherFolderLeavesHeldEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := &server{t: t}
		srv.limit.Store(4)
		p := pool.New(4)
		sessions := pool.NewSessions(p, srv.dial, time.Minute)
		t.Cleanup(sessions.Close)
		var g Group
		s1 := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		s2 := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		t.Cleanup(func() { _ = s1.Stop(context.Background()); _ = s2.Stop(context.Background()) })
		var c copies
		g1 := make(chan struct{})
		var dones doneLog
		s1.Enqueue(widthTask(widthOf(2), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			switch call {
			case 1:
				<-g1
				return nil
			case 2:
				return ErrSaturated
			}
			return nil
		}, dones.add))
		synctest.Wait()
		if got := c.calls.Load(); got != 2 {
			t.Fatalf("%d units, want 2", got)
		}
		r := &rig{}
		s2.Enqueue(r.task("other", KindListStubs, 2, nil))
		synctest.Wait()
		if len(r.order()) != 1 {
			t.Fatalf("runs %v, want the other folder's list", r.order())
		}
		if got := c.calls.Load(); got != 2 {
			t.Fatalf("%d units after another folder's task ended, want the held entry left held", got)
		}
		close(g1)
		synctest.Wait()
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
	})
}

// endlessBodies is a never-ending body backlog of folder: every unit takes
// a while and leaves more work. c counts the units of every such entry
// running at once.
func endlessBodies(folder int64, width int, c *copies) Task[*conn] {
	return Task[*conn]{Kind: KindBodies, FolderID: folder, Key: fmt.Sprintf("bodies:%d", folder), Width: widthOf(width),
		Run: func(ctx context.Context, _ *conn) error {
			c.enter()
			defer c.leave()
			select {
			case <-time.After(10 * time.Second):
			case <-ctx.Done():
			}
			return ErrAgain
		}}
}

// IDLE's slot is reserved from normal work whenever push is on at N>=2, not
// only while the watcher holds its session: when IDLE fails (its dial is
// refused, or a park ends with an error) and waits to retry, no body copy,
// of any folder, takes its slot, so the retry gets a session at once
// instead of waiting for a unit to end, which with a never-ending backlog
// kept push down for as long as bodies ran. The free slot still serves the
// fast lane meanwhile.
func TestIdleSlotStaysReservedWhileIdleRetries(t *testing.T) {
	for _, n := range []int{2, 4} {
		for _, how := range []string{"dial refused", "park failed"} {
			t.Run(fmt.Sprintf("N=%d/%s", n, how), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) { idleSlotStaysReserved(t, n, how) })
			})
		}
	}
}

func idleSlotStaysReserved(t *testing.T, n int, how string) {
	r := newIdleRig(t, n)
	refused := errors.New("login: NO [UNAVAILABLE] Too many simultaneous connections")
	if how == "dial refused" {
		r.srv.dialErr.Store(&refused) // IDLE's first dial
	}
	synctest.Wait()
	r.srv.dialErr.Store(nil)
	var c copies
	for f := range 2 { // two folders: the reserve is not per entry
		r.s.Enqueue(endlessBodies(int64(10+f), n, &c))
	}
	synctest.Wait()
	if how == "park failed" {
		if r.parked.Load() != 1 {
			t.Fatal("IDLE not parked beside the bodies")
		}
		r.err.Store(&refused)
		r.changed <- struct{}{} // IDLE wakes, checks new mail, re-parks: refused
		synctest.Wait()
		r.err.Store(nil)
	}
	if r.parked.Load() != 0 {
		t.Fatal("IDLE parked, want it waiting to retry")
	}
	// IDLE retries after 15 s; bodies run meanwhile on N-1 sessions.
	fast := false
	for range 16 {
		if got := c.running.Load(); got != int32(n-1) {
			t.Fatalf("%d body units run with IDLE waiting to retry at N=%d, want %d", got, n, n-1)
		}
		if !fast {
			r.s.Enqueue(r.task("open", KindOnDemandBody, 9, nil))
			synctest.Wait()
			if !slices.Contains(r.order(), "open") {
				t.Fatal("fast task waited with IDLE's slot free")
			}
			fast = true
		}
		time.Sleep(time.Second)
		synctest.Wait()
	}
	if r.parked.Load() != 1 {
		t.Fatalf("IDLE not back 16 s after its failure (workers %d), want its slot free for the retry", r.s.Stats().Workers)
	}
	for range 60 {
		time.Sleep(time.Second)
		synctest.Wait()
		if got := c.running.Load(); got > int32(n-1) || r.parked.Load() != 1 {
			t.Fatalf("%d units, parked %d, want IDLE parked beside at most %d", got, r.parked.Load(), n-1)
		}
	}
	if p := c.peak.Load(); p != int32(n-1) {
		t.Fatalf("peak %d body units at N=%d, want %d", p, n, n-1)
	}
}
