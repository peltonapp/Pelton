package syncsched

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// Two schedulers of one account in a Group: the old one stopped while its
// list of folder 1 and a barrier-free task ran. The new one starts work on
// other folders at once, but a list or bodies of folder 1 and a barrier
// only once the old list ended, and a running barrier of the old one holds
// everything.
func TestGroupKeepsAStoppedSchedulersRunningWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := &server{t: t}
		srv.limit.Store(4)
		p := pool.New(4)
		sessions := pool.NewSessions(p, srv.dial, time.Minute)
		t.Cleanup(sessions.Close)
		var g Group
		old := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		release := make(chan struct{})
		r := &rig{}
		old.Enqueue(r.task("oldList", KindListStubs, 1, release))
		synctest.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		if err := old.Stop(ctx); err == nil {
			t.Fatal("old Stop returned while its list ran")
		}

		next := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		t.Cleanup(func() { _ = next.Stop(context.Background()) })
		next.Enqueue(r.task("other", KindListStubs, 2, nil))
		next.Enqueue(r.task("sameList", KindListStubs, 1, nil))
		next.Enqueue(r.task("sameBodies", KindBodies, 1, nil))
		synctest.Wait()
		if got := r.order(); len(got) != 2 || got[1] != "other" {
			t.Fatalf("runs before the old list ended = %v, want only another folder's", got)
		}
		close(release)
		synctest.Wait()
		if got := r.order(); len(got) != 4 {
			t.Fatalf("runs after the old list ended = %v, want the folder's list and bodies too", got)
		}
	})
}

// A barrier waits for the other scheduler's running task, and a running
// barrier of the other scheduler holds every task.
func TestGroupBarriersAcrossSchedulers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := &server{t: t}
		srv.limit.Store(4)
		p := pool.New(4)
		sessions := pool.NewSessions(p, srv.dial, time.Minute)
		t.Cleanup(sessions.Close)
		var g Group
		r := &rig{}
		a := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		b := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		t.Cleanup(func() { _ = a.Stop(context.Background()); _ = b.Stop(context.Background()) })

		hold := make(chan struct{})
		a.Enqueue(r.task("a", KindBodies, 7, hold))
		synctest.Wait()
		barrier := r.task("barrier", KindFolderTree, 0, nil)
		barrier.Barrier = true
		b.Enqueue(barrier)
		synctest.Wait()
		if got := r.order(); len(got) != 1 {
			t.Fatalf("runs = %v, want the barrier waiting for the other scheduler's task", got)
		}
		close(hold)
		synctest.Wait()
		if got := r.order(); len(got) != 2 || got[1] != "barrier" {
			t.Fatalf("runs = %v, want the barrier after the task", got)
		}

		hold2 := make(chan struct{})
		held := r.task("heldBarrier", KindFolderTree, 0, hold2)
		held.Barrier = true
		a.Enqueue(held)
		synctest.Wait()
		b.Enqueue(r.task("fast", KindMove, 3, nil))
		synctest.Wait()
		if got := r.order(); len(got) != 3 {
			t.Fatalf("runs = %v, want the other scheduler's task held by the running barrier", got)
		}
		close(hold2)
		synctest.Wait()
		if got := r.order(); len(got) != 4 || got[3] != "fast" {
			t.Fatalf("runs = %v, want the task after the barrier", got)
		}
	})
}

// sharedView is one scheduler's provider over sessions other schedulers
// use too: its Close leaves them open.
type sharedView struct{ *pool.Sessions[*conn] }

func (v sharedView) Close() { v.CloseIdle() }

// Two schedulers of one Group admit work at the same moment: one barrier
// each, enqueued concurrently. The check and the record of a running task
// are one step on the Group, so the barriers never run side by side, and the
// one that lost the race still runs once the other ended.
func TestGroupConcurrentBarriersNeverOverlap(t *testing.T) {
	// The race is lost or won anew each iteration. 300 is a meaningful
	// default; PELTON_STRESS=1 runs the 10,000 that hunt for a rare loss.
	iterations := 300
	if os.Getenv("PELTON_STRESS") != "" {
		iterations = 10000
	}
	if testing.Short() {
		iterations = 200
	}
	for i := range iterations {
		srv := &server{t: t}
		srv.limit.Store(4)
		p := pool.New(4)
		sessions := pool.NewSessions(p, srv.dial, time.Minute)
		var g Group
		a := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		b := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		var inside, overlaps atomic.Int32
		var wg sync.WaitGroup
		barrier := func() Task[*conn] {
			wg.Add(1)
			return Task[*conn]{Kind: KindFolderTree, Barrier: true,
				Run: func(context.Context, *conn) error {
					if inside.Add(1) > 1 {
						overlaps.Add(1)
					}
					runtime.Gosched()
					inside.Add(-1)
					return nil
				},
				Done: func(error) { wg.Done() },
			}
		}
		ta, tb := barrier(), barrier()
		start := make(chan struct{})
		var enq sync.WaitGroup
		enq.Add(2)
		go func() { defer enq.Done(); <-start; a.Enqueue(ta) }()
		go func() { defer enq.Done(); <-start; b.Enqueue(tb) }()
		close(start)
		enq.Wait()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: a barrier never ran", i)
		}
		_ = a.Stop(context.Background())
		_ = b.Stop(context.Background())
		sessions.Close()
		if n := overlaps.Load(); n > 0 {
			t.Fatalf("iteration %d: two barriers of one Group ran at once", i)
		}
	}
}

// Two schedulers of one Group, each with a saturated unit of folder 1 while
// s1's first body copy runs a long fetch holding the ids: s1's second copy
// and s2's offline unit. A saturated end frees no ids, so neither may wake
// the other into running again: they wait, held, for the long copy. Before
// the fix each saturated end reopened the other's held unit, and the two
// bounced off each other for as long as the fetch ran. When the long copy
// ends, s2's unit runs and both tasks end once.
func TestGroupSaturatedUnitsDoNotBounce(t *testing.T) {
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

		gate := make(chan struct{})
		var fetched atomic.Bool // the long copy's ids are released
		release := sync.OnceFunc(func() { fetched.Store(true); close(gate) })
		t.Cleanup(release) // runs before the Stops, so a failed run ends
		var c copies
		var bodyDones, offlineDones doneLog
		s1.Enqueue(widthTask(widthOf(2), func(context.Context) error {
			call, _ := c.enter()
			defer c.leave()
			if call == 1 {
				<-gate
				return nil
			}
			if fetched.Load() {
				return nil
			}
			return ErrSaturated
		}, bodyDones.add))
		synctest.Wait()
		if got := c.calls.Load(); got != 2 {
			t.Fatalf("%d body units, want the long copy and one saturated copy", got)
		}

		// bound stops a bounce, so a failing run ends instead of spinning.
		const bound = 1000
		var offline atomic.Int32
		s2.Enqueue(Task[*conn]{Kind: KindOffline, FolderID: 1, Key: "offline:1",
			Run: func(context.Context, *conn) error {
				if offline.Add(1) > bound || fetched.Load() {
					return nil
				}
				return ErrSaturated
			}, Done: offlineDones.add})
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if b, o := c.calls.Load(), offline.Load(); b != 2 || o != 1 {
			t.Fatalf("during the long fetch: %d body units, %d offline units; want 2 and 1 (saturated units must wait, not bounce)", b, o)
		}

		release()
		synctest.Wait()
		if o := offline.Load(); o != 2 {
			t.Fatalf("%d offline units, want a second one after the long copy ended", o)
		}
		if got := bodyDones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("body Done %v, want one nil", got)
		}
		if got := offlineDones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("offline Done %v, want one nil", got)
		}
	})
}

// A saturated end that was the last running task of its folder in the
// Group still tells the other schedulers: a held entry there waited for it,
// and no other end will come. Here the old scheduler was stopped while its
// offline unit of folder 1 ran; the new scheduler's body entry saturated on
// it and is held. The old unit ends saturated (and is not run again: its
// scheduler stopped), so the new entry must reopen.
func TestGroupLastSaturatedEndReopensHeldEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := &server{t: t}
		srv.limit.Store(4)
		p := pool.New(4)
		sessions := pool.NewSessions(p, srv.dial, time.Minute)
		t.Cleanup(sessions.Close)
		var g Group
		old := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		gate := make(chan struct{})
		var oldDones doneLog
		old.Enqueue(Task[*conn]{Kind: KindOffline, FolderID: 1, Key: "offline:1",
			Run: func(context.Context, *conn) error {
				<-gate
				return ErrSaturated
			}, Done: oldDones.add})
		synctest.Wait()

		next := New(context.Background(), sharedView{sessions}, Options{Group: &g})
		t.Cleanup(func() { _ = next.Stop(context.Background()) })
		var calls atomic.Int32
		var dones doneLog
		next.Enqueue(widthTask(widthOf(1), func(context.Context) error {
			if calls.Add(1) == 1 {
				return ErrSaturated
			}
			return nil
		}, dones.add))
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := calls.Load(); got != 1 {
			t.Fatalf("%d body units while the old unit ran, want 1 (held)", got)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		if err := old.Stop(ctx); err == nil {
			t.Fatal("old Stop returned while its unit ran")
		}
		close(gate)
		synctest.Wait()
		if got := oldDones.get(); len(got) != 1 || !errors.Is(got[0], ErrStopped) {
			t.Fatalf("old Done %v, want ErrStopped", got)
		}
		if got := calls.Load(); got != 2 {
			t.Fatalf("%d body units, want the held entry reopened by the last end", got)
		}
		if got := dones.get(); len(got) != 1 || got[0] != nil {
			t.Fatalf("Done %v, want one nil", got)
		}
	})
}

// Group.end tells the other schedulers of every end, except a saturated one
// (freed false) while its owner still runs a task of the same shape: then
// nothing they could act on changed. A saturated end that was the last of
// its shape is told, as not freed.
func TestGroupEndTellsWhatChanged(t *testing.T) {
	var g Group
	a, b := new(int), new(int)
	type call struct {
		folder int64
		freed  bool
	}
	var got []call
	g.join(a, func(int64, bool) { t.Fatal("a told of its own end") })
	g.join(b, func(folder int64, freed bool) { got = append(got, call{folder, freed}) })
	body := shape{kind: KindBodies, folderID: 1, key: "bodies:1"}
	for range 3 {
		if !g.reserve(a, body) {
			t.Fatal("reserve refused a's own copy")
		}
	}
	tell := func(fns []func()) {
		for _, fn := range fns {
			fn()
		}
	}
	tell(g.end(a, body, false))
	if len(got) != 0 {
		t.Fatalf("told %v of a saturated copy beside a running one, want nothing", got)
	}
	tell(g.end(a, body, true))
	if len(got) != 1 || got[0] != (call{1, true}) {
		t.Fatalf("told %v of a copy that freed ids, want {1 true}", got)
	}
	tell(g.end(a, body, false))
	if len(got) != 2 || got[1] != (call{1, false}) {
		t.Fatalf("told %v of the last copy's saturated end, want {1 false}", got)
	}
}
