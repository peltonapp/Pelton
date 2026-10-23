package syncsched

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// server counts the sessions open against it and fails the test when they
// exceed the cap.
type server struct {
	t                 *testing.T
	limit             atomic.Int32
	open, peak, dials atomic.Int32
	dialErr           atomic.Pointer[error]
}

type conn struct {
	srv    *server
	id     int32
	closed atomic.Bool
}

func (c *conn) Close() error {
	if !c.closed.Swap(true) {
		c.srv.open.Add(-1)
	}
	return nil
}

func (s *server) dial(context.Context) (*conn, error) {
	if p := s.dialErr.Load(); p != nil {
		return nil, *p
	}
	n := s.open.Add(1)
	for p := s.peak.Load(); n > p && !s.peak.CompareAndSwap(p, n); p = s.peak.Load() {
	}
	if l := s.limit.Load(); n > l {
		s.t.Errorf("open sessions = %d, over the cap %d", n, l)
	}
	return &conn{srv: s, id: s.dials.Add(1)}, nil
}

type rig struct {
	s    *Scheduler[*conn]
	p    *pool.Pool
	srv  *server
	mu   sync.Mutex
	runs []string
}

func newRig(t *testing.T, n int, opts Options) *rig {
	t.Helper()
	srv := &server{t: t}
	srv.limit.Store(int32(n))
	p := pool.New(n)
	r := &rig{p: p, srv: srv}
	r.s = New(context.Background(), pool.NewSessions(p, srv.dial, time.Minute), opts)
	t.Cleanup(func() { _ = r.s.Stop(context.Background()) })
	return r
}

func (r *rig) record(name string) {
	r.mu.Lock()
	r.runs = append(r.runs, name)
	r.mu.Unlock()
}

func (r *rig) order() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.runs)
}

// task records its name when it runs. hold, when set, blocks the run.
func (r *rig) task(name string, kind Kind, folder int64, hold <-chan struct{}) Task[*conn] {
	return Task[*conn]{Kind: kind, FolderID: folder, Run: func(ctx context.Context, c *conn) error {
		r.record(name)
		if hold != nil {
			<-hold
		}
		return nil
	}}
}

// blockAll fills every worker with a held normal task and returns the gate.
func (r *rig) blockAll(t *testing.T, n int) chan struct{} {
	t.Helper()
	gate := make(chan struct{})
	for i := range n {
		r.s.Enqueue(r.task(fmt.Sprintf("block%d", i), KindFullCheck, int64(1000+i), gate))
	}
	synctest.Wait()
	if got := r.s.Stats().Running; got != n {
		t.Fatalf("running = %d, want %d", got, n)
	}
	return gate
}

// At N=1 the starvation rule lets one normal task through after each fast one.
func TestFastLaneBeforeNormalAndClassOrder(t *testing.T) {
	tests := []struct {
		name   string
		policy Policy
		want   string
	}{
		{"classes", PolicyClasses, "fast1 manual fast2 stubs bodies trash offline check"},
		{"fifo", PolicyFIFO, "fast1 check fast2 bodies offline trash stubs manual"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newRig(t, 1, Options{Policy: tt.policy})
				gate := r.blockAll(t, 1)
				r.s.Enqueue(r.task("check", KindFullCheck, 1, nil))
				r.s.Enqueue(r.task("bodies", KindBodies, 2, nil))
				r.s.Enqueue(r.task("offline", KindOffline, 4, nil))
				r.s.Enqueue(r.task("fast1", KindOnDemandBody, 5, nil))
				r.s.Enqueue(r.task("trash", KindEmptyTrash, 6, nil))
				r.s.Enqueue(r.task("stubs", KindListStubs, 7, nil))
				manual := r.task("manual", KindBodies, 8, nil)
				manual.Class = ClassManualSync
				r.s.Enqueue(manual)
				r.s.Enqueue(r.task("fast2", KindMove, 9, nil))
				close(gate)
				synctest.Wait()
				got := strings.Join(r.order()[1:], " ")
				if got != tt.want {
					t.Fatalf("order\n got %s\nwant %s", got, tt.want)
				}
			})
		})
	}
}

// TestPickStarvationRule: after N fast picks in a row one eligible normal
// task goes next, even with the fast lane still full.
func TestPickStarvationRule(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			s := &Scheduler[*conn]{prov: pool.NewSessions[*conn](pool.New(n), nil, 0)}
			for i := range 20 {
				s.fast = append(s.fast, &entry[*conn]{t: Task[*conn]{Kind: KindMove, Key: fmt.Sprint("m", i)}})
			}
			for i := range 4 {
				s.normal = append(s.normal, &entry[*conn]{t: Task[*conn]{Kind: KindBodies, FolderID: int64(i + 1)}, class: ClassBodies})
			}
			var got []byte
			for {
				e := s.pickLocked(n, true)
				if e == nil {
					break
				}
				s.active = nil
				if e.t.Kind.Fast() {
					got = append(got, 'F')
				} else {
					got = append(got, 'N')
				}
			}
			unit := strings.Repeat("F", n) + "N"
			want := strings.Repeat(unit, 4) + strings.Repeat("F", 20-4*n)
			if string(got) != want {
				t.Fatalf("picks %s, want %s", got, want)
			}
		})
	}
}

func TestPickStarvationRuleIdleServesFastOnly(t *testing.T) {
	s := &Scheduler[*conn]{prov: pool.NewSessions[*conn](pool.New(2), nil, 0)}
	s.fast = []*entry[*conn]{{t: Task[*conn]{Kind: KindMove}}, {t: Task[*conn]{Kind: KindMove}}, {t: Task[*conn]{Kind: KindMove}}}
	s.normal = []*entry[*conn]{{t: Task[*conn]{Kind: KindBodies}, class: ClassBodies}}
	for range 3 {
		e := s.pickLocked(2, false)
		if e == nil || !e.t.Kind.Fast() {
			t.Fatalf("idle worker picked %+v, want a fast task", e)
		}
	}
	if e := s.pickLocked(2, false); e != nil {
		t.Fatal("idle worker took a normal task")
	}
}

func TestStarvationRuleEndToEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		gate := r.blockAll(t, 1)
		for i := range 4 {
			r.s.Enqueue(r.task("F", KindMove, int64(i), nil))
		}
		units := 0
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 7, Key: "b7", Run: func(context.Context, *conn) error {
			r.record("N")
			units++
			if units < 3 {
				return ErrAgain
			}
			return nil
		}})
		close(gate)
		synctest.Wait()
		if got := strings.Join(r.order()[1:], ""); got != "FNFNFNF" {
			t.Fatalf("order %s, want FNFNFNF", got)
		}
	})
}

func TestFastTaskStartsAtEndOfCurrentTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		unit := make(chan struct{})
		units := 0
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 1, Key: "b1", Run: func(context.Context, *conn) error {
			r.record("N")
			<-unit
			units++
			if units < 5 {
				return ErrAgain
			}
			return nil
		}})
		synctest.Wait()
		r.s.Enqueue(r.task("F", KindOnDemandBody, 2, nil))
		unit <- struct{}{}
		synctest.Wait()
		if got := strings.Join(r.order(), ""); got != "NFN" {
			t.Fatalf("order %s, want the fast task right after the running unit (NFN)", got)
		}
		for range 4 {
			unit <- struct{}{}
		}
	})
}

func TestManualSyncDoesNotBlockOnDemandBody(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newRig(t, n, Options{})
				gate := make(chan struct{}, 8) // each value ends one task
				// A manual Sync of more folders than sessions, every task held.
				for f := range 6 {
					task := r.task("manual", KindListStubs, int64(f+1), gate)
					task.Class = ClassManualSync
					r.s.Enqueue(task)
				}
				synctest.Wait()
				opened := make(chan error, 1)
				body := r.task("body", KindOnDemandBody, 1, nil)
				body.Done = func(err error) { opened <- err }
				r.s.Enqueue(body)
				// One manual task ends: its session takes the body next.
				gate <- struct{}{}
				synctest.Wait()
				if err := <-opened; err != nil {
					t.Fatal(err)
				}
				order := r.order()
				if i := slices.Index(order, "body"); i != n {
					t.Fatalf("body ran at %d in %v, want right after the first manual task ended (%d)", i, order, n)
				}
				close(gate)
				synctest.Wait()
			})
		})
	}
}

func TestDedup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		gate := r.blockAll(t, 1)
		var runs, dones atomic.Int32
		mk := func() Task[*conn] {
			return Task[*conn]{Kind: KindListStubs, FolderID: 5, Key: "list:5",
				Run:  func(context.Context, *conn) error { runs.Add(1); return nil },
				Done: func(error) { dones.Add(1) }}
		}
		for range 3 {
			r.s.Enqueue(mk())
		}
		if got := r.s.Stats().Normal; got != 1 {
			t.Fatalf("queued = %d, want 1", got)
		}
		close(gate)
		synctest.Wait()
		if runs.Load() != 1 || dones.Load() != 3 {
			t.Fatalf("runs %d dones %d, want 1 and 3", runs.Load(), dones.Load())
		}
	})
}

// EnqueueJoined tells a task that joined a queued duplicate, whose Run will
// never run, from one that was queued itself, also beside a running
// duplicate.
func TestEnqueueJoined(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		hold := make(chan struct{})
		var runs atomic.Int32
		mk := func(wait bool) Task[*conn] {
			return Task[*conn]{Kind: KindBodies, FolderID: 5, Key: "bodies:5",
				Run: func(context.Context, *conn) error {
					runs.Add(1)
					if wait {
						<-hold
					}
					return nil
				}}
		}
		if r.s.EnqueueJoined(mk(true)) {
			t.Fatal("the first task joined nothing yet")
		}
		synctest.Wait()
		if r.s.EnqueueJoined(mk(false)) {
			t.Fatal("a duplicate of a running task joined it")
		}
		if !r.s.EnqueueJoined(mk(false)) {
			t.Fatal("a duplicate of a queued task did not join it")
		}
		close(hold)
		synctest.Wait()
		if got := runs.Load(); got != 2 {
			t.Fatalf("runs = %d, want 2", got)
		}
	})
}

func TestDedupLiftsClass(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		gate := r.blockAll(t, 1)
		r.s.Enqueue(r.task("bodies", KindBodies, 2, nil))
		check := r.task("check", KindFullCheck, 1, nil)
		check.Key = "check:1"
		r.s.Enqueue(check) // lowest class, behind the bodies
		manual := check
		manual.Class = ClassManualSync // a manual Sync asks for the same check
		r.s.Enqueue(manual)
		close(gate)
		synctest.Wait()
		if got := strings.Join(r.order()[1:], " "); got != "check bodies" {
			t.Fatalf("order %s, want the lifted check first", got)
		}
	})
}

func TestQueuedDuplicateWaitsForRunningOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		gate := make(chan struct{})
		var running, peak atomic.Int32
		mk := func() Task[*conn] {
			return Task[*conn]{Kind: KindListStubs, FolderID: 5, Key: "list:5", Run: func(context.Context, *conn) error {
				n := running.Add(1)
				peak.Store(max(peak.Load(), n))
				<-gate
				running.Add(-1)
				return nil
			}}
		}
		r.s.Enqueue(mk())
		synctest.Wait()
		r.s.Enqueue(mk()) // the running one may have missed a change: queue one more
		synctest.Wait()
		if st := r.s.Stats(); st.Running != 1 || st.Normal != 1 {
			t.Fatalf("stats %+v, want 1 running and 1 queued", st)
		}
		close(gate)
		synctest.Wait()
		if peak.Load() != 1 {
			t.Fatal("two tasks with one key ran at once")
		}
	})
}

func TestPerFolderRules(t *testing.T) {
	tests := []struct {
		name    string
		running Kind
		queued  Kind
		sameDir bool
		runsNow bool
	}{
		{"bodies wait for listing of their folder", KindListStubs, KindBodies, true, false},
		{"bodies of another folder do not wait", KindListStubs, KindBodies, false, true},
		{"bodies wait for new-mail listing", KindNewMail, KindBodies, true, false},
		{"bodies wait for a full check list", KindFullCheck, KindBodies, true, false},
		{"one list per folder", KindListStubs, KindListStubs, true, false},
		{"load older waits for a list", KindListStubs, KindLoadOlder, true, false},
		{"lists of other folders run", KindListStubs, KindListStubs, false, true},
		{"full check waits for bodies", KindBodies, KindFullCheck, true, false},
		{"full check of another folder runs", KindBodies, KindFullCheck, false, true},
		{"bodies beside bodies of another key", KindBodies, KindOffline, true, true},
		{"full check waits for emptying Trash", KindEmptyTrash, KindFullCheck, true, false},
		{"emptying Trash waits for a full check", KindFullCheck, KindEmptyTrash, true, false},
		{"an opened large body does not wait for a list", KindListStubs, KindOpenedBody, true, true},
		{"nor for the folder's bodies", KindBodies, KindOpenedBody, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newRig(t, 3, Options{})
				gate := make(chan struct{})
				r.s.Enqueue(r.task("first", tt.running, 1, gate))
				synctest.Wait()
				folder := int64(1)
				if !tt.sameDir {
					folder = 2
				}
				r.s.Enqueue(r.task("second", tt.queued, folder, nil))
				synctest.Wait()
				ran := slices.Contains(r.order(), "second")
				if ran != tt.runsNow {
					t.Fatalf("second ran while first runs = %v, want %v", ran, tt.runsNow)
				}
				close(gate)
				synctest.Wait()
				if !slices.Contains(r.order(), "second") {
					t.Fatal("second never ran")
				}
			})
		})
	}
}

func TestBodiesWaitForQueuedListOfTheirFolderOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{Policy: PolicyFIFO})
		gate := r.blockAll(t, 1)
		r.s.Enqueue(r.task("bodiesX", KindBodies, 1, nil))
		r.s.Enqueue(r.task("bodiesY", KindBodies, 2, nil))
		r.s.Enqueue(r.task("listX", KindListStubs, 1, nil))
		close(gate)
		synctest.Wait()
		if got := strings.Join(r.order()[1:], " "); got != "bodiesY listX bodiesX" {
			t.Fatalf("order %s", got)
		}
	})
}

func TestBarrierHoldsEverythingElse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		gate := make(chan struct{})
		tree := r.task("tree", KindFolderTree, 0, gate)
		tree.Barrier = true
		r.s.Enqueue(tree)
		r.s.Enqueue(r.task("list", KindListStubs, 1, nil))
		r.s.Enqueue(r.task("body", KindOnDemandBody, 2, nil))
		synctest.Wait()
		if got := r.order(); len(got) != 1 {
			t.Fatalf("ran %v while the folder tree runs", got)
		}
		close(gate)
		synctest.Wait()
		if got := len(r.order()); got != 3 {
			t.Fatalf("ran %d tasks after the barrier, want 3", got)
		}
	})
}

// TestGeneratorsRoundRobinAcrossFolders: generators go back to the tail
// after each unit, so folders take turns.
func TestGeneratorsRoundRobinAcrossFolders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		gate := r.blockAll(t, 1)
		var doneA, doneB atomic.Int32
		gen := func(name string, units int, done *atomic.Int32) Task[*conn] {
			left := units
			return Task[*conn]{Kind: KindBodies, FolderID: int64(name[0]), Key: "bodies:" + name,
				Run: func(context.Context, *conn) error {
					r.record(name)
					left--
					if left > 0 {
						return ErrAgain
					}
					return nil
				},
				Done: func(error) { done.Add(1) }}
		}
		r.s.Enqueue(gen("A", 3, &doneA))
		r.s.Enqueue(gen("B", 2, &doneB))
		close(gate)
		synctest.Wait()
		if got := strings.Join(r.order()[1:], ""); got != "ABABA" {
			t.Fatalf("order %s, want ABABA", got)
		}
		if doneA.Load() != 1 || doneB.Load() != 1 {
			t.Fatalf("Done calls A=%d B=%d, want 1 each", doneA.Load(), doneB.Load())
		}
	})
}

// TestQueueStaysBounded: a folder with 50k bodies missing is one queued task
// at any time, and every body still arrives.
func TestQueueStaysBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		var mu sync.Mutex
		missing := map[int64]int{1: 50_000, 2: 1_234, 3: 0}
		maxQueued := 0
		var gen func(folder int64) Task[*conn]
		gen = func(folder int64) Task[*conn] {
			return Task[*conn]{Kind: KindBodies, FolderID: folder, Key: fmt.Sprint("bodies:", folder),
				Run: func(context.Context, *conn) error {
					mu.Lock()
					defer mu.Unlock()
					// Re-check what is missing, take one batch.
					missing[folder] -= min(50, missing[folder])
					st := r.s.Stats()
					maxQueued = max(maxQueued, st.Normal)
					if missing[folder] > 0 {
						return ErrAgain
					}
					return nil
				}}
		}
		for f := range int64(3) {
			r.s.Enqueue(gen(f + 1))
			r.s.Enqueue(gen(f + 1)) // a second campaign for the same folder dedups
		}
		synctest.Wait()
		mu.Lock()
		defer mu.Unlock()
		for f, n := range missing {
			if n != 0 {
				t.Fatalf("folder %d still misses %d bodies", f, n)
			}
		}
		if maxQueued > 3 {
			t.Fatalf("normal lane held %d tasks, want at most one per folder (3)", maxQueued)
		}
		if peak := r.srv.peak.Load(); peak > 3 {
			t.Fatalf("peak sessions %d", peak)
		}
	})
}

func TestStaleTaskIsCheap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		var fetched atomic.Int32
		done := map[int]bool{}
		for i := range 20 {
			done[i] = i%2 == 0 // half were filled meanwhile
		}
		for i := range 20 {
			r.s.Enqueue(Task[*conn]{Kind: KindOnDemandBody, Key: fmt.Sprint("open:", i), Run: func(context.Context, *conn) error {
				if done[i] {
					return nil
				}
				fetched.Add(1)
				return nil
			}})
		}
		synctest.Wait()
		if fetched.Load() != 10 || r.srv.dials.Load() != 1 {
			t.Fatalf("fetched %d dials %d, want 10 and 1", fetched.Load(), r.srv.dials.Load())
		}
	})
}

func TestFastLaneCapDropsOldestOnDemandOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		gate := r.blockAll(t, 1)
		var dropped []string
		var mu sync.Mutex
		add := func(name string, kind Kind) {
			task := r.task(name, kind, 0, nil)
			task.Done = func(err error) {
				if errors.Is(err, ErrDropped) {
					mu.Lock()
					dropped = append(dropped, name)
					mu.Unlock()
				}
			}
			r.s.Enqueue(task)
		}
		for i := range 10 {
			add(fmt.Sprint("move", i), KindMove)
		}
		for i := range FastCap {
			add(fmt.Sprint("open", i), KindOnDemandBody)
		}
		if got := r.s.Stats().Fast; got != FastCap {
			t.Fatalf("fast lane = %d, want %d", got, FastCap)
		}
		close(gate)
		synctest.Wait()
		mu.Lock()
		defer mu.Unlock()
		want := []string{"open0", "open1", "open2", "open3", "open4", "open5", "open6", "open7", "open8", "open9"}
		if !slices.Equal(dropped, want) {
			t.Fatalf("dropped %v, want %v", dropped, want)
		}
		for i := range 10 {
			if !slices.Contains(r.order(), fmt.Sprint("move", i)) {
				t.Fatalf("user action move%d was dropped", i)
			}
		}
	})
}

func TestSessionsReusedAcrossTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		for i := range 10 {
			r.s.Enqueue(r.task("t", KindListStubs, int64(i), nil))
			synctest.Wait()
		}
		if d := r.srv.dials.Load(); d != 1 {
			t.Fatalf("dials = %d, want 1 for sequential tasks", d)
		}
	})
}

func TestBrokenSessionIsRedialed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		netErr := errors.New("connection reset")
		r := newRig(t, 1, Options{Broken: func(err error) bool { return errors.Is(err, netErr) }})
		var errs []error
		for _, err := range []error{nil, errors.New("NO no such message"), netErr, nil} {
			r.s.Enqueue(Task[*conn]{Kind: KindSource, Run: func(context.Context, *conn) error { return err },
				Done: func(e error) { errs = append(errs, e) }})
			synctest.Wait()
		}
		if d := r.srv.dials.Load(); d != 2 {
			t.Fatalf("dials = %d, want 2 (one redial after the connection error)", d)
		}
		if len(errs) != 4 || !errors.Is(errs[2], netErr) {
			t.Fatalf("Done errors %v", errs)
		}
	})
}

func TestDialErrorEndsTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		dialErr := errors.New("no credentials")
		r.srv.dialErr.Store(&dialErr)
		got := make(chan error, 1)
		r.s.Enqueue(Task[*conn]{Kind: KindOnDemandBody, Run: func(context.Context, *conn) error {
			t.Error("ran without a session")
			return nil
		}, Done: func(err error) { got <- err }})
		synctest.Wait()
		if err := <-got; !errors.Is(err, dialErr) {
			t.Fatalf("Done = %v, want the dial error", err)
		}
		if st := r.s.Stats(); st.Workers != 0 {
			t.Fatalf("stats %+v after a failed dial", st)
		}
	})
}

func TestPanicEndsTaskNotWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		var got error
		r.s.Enqueue(Task[*conn]{Kind: KindMove, Run: func(context.Context, *conn) error { panic("boom") },
			Done: func(err error) { got = err }})
		r.s.Enqueue(r.task("after", KindMove, 0, nil))
		synctest.Wait()
		if got == nil || !strings.Contains(got.Error(), "boom") {
			t.Fatalf("Done = %v", got)
		}
		if !slices.Contains(r.order(), "after") {
			t.Fatal("the next task did not run after a panic")
		}
	})
}

func TestStopIsGraceful(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		gate := make(chan struct{})
		var runCtxErr error
		var running sync.WaitGroup
		running.Add(1)
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 1, Run: func(ctx context.Context, c *conn) error {
			running.Done()
			<-gate
			runCtxErr = ctx.Err()
			return nil
		}})
		running.Wait()
		var queuedErr, lateErr error
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 2, Done: func(err error) { queuedErr = err }})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := r.s.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop with a task still running = %v, want the deadline", err)
		}
		if !errors.Is(queuedErr, ErrStopped) {
			t.Fatalf("queued Done = %v, want ErrStopped", queuedErr)
		}
		r.s.Enqueue(Task[*conn]{Kind: KindMove, Done: func(err error) { lateErr = err }})
		if !errors.Is(lateErr, ErrStopped) {
			t.Fatalf("Enqueue after Stop: Done = %v, want ErrStopped", lateErr)
		}
		close(gate)
		synctest.Wait()
		if runCtxErr != nil {
			t.Fatalf("a normal stop cancelled the running task: %v", runCtxErr)
		}
		if o := r.srv.open.Load(); o != 0 {
			t.Fatalf("%d sessions open after stop", o)
		}
	})
}

func TestStopEndsGeneratorAtUnitEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		unit := make(chan struct{})
		units := 0
		var got error
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 1, Key: "b", Run: func(context.Context, *conn) error {
			<-unit
			units++
			return ErrAgain
		}, Done: func(err error) { got = err }})
		synctest.Wait()
		stopped := make(chan error, 1)
		go func() { stopped <- r.s.Stop(context.Background()) }()
		synctest.Wait()
		unit <- struct{}{}
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		if units != 1 || !errors.Is(got, ErrStopped) {
			t.Fatalf("units %d Done %v, want 1 unit then ErrStopped", units, got)
		}
	})
}

func TestAbortCancelsRunningTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		var got error
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, Run: func(ctx context.Context, c *conn) error {
			<-ctx.Done()
			return ctx.Err()
		}, Done: func(err error) { got = err }})
		synctest.Wait()
		_ = r.s.Abort(context.Background())
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("Done = %v, want context.Canceled", got)
		}
	})
}

func TestParentContextIsHardStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, quit := context.WithCancel(context.Background())
		srv := &server{t: t}
		srv.limit.Store(1)
		p := pool.New(1)
		s := New(ctx, pool.NewSessions(p, srv.dial, time.Minute), Options{})
		gate := make(chan struct{})
		s.Enqueue(Task[*conn]{Kind: KindBodies, Run: func(ctx context.Context, c *conn) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-gate:
				return nil
			}
		}})
		var queued error
		s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 3, Done: func(err error) { queued = err }})
		synctest.Wait()
		quit()
		synctest.Wait()
		if !errors.Is(queued, ErrStopped) {
			t.Fatalf("queued Done = %v after app quit", queued)
		}
		_ = s.Stop(context.Background())
	})
}

func TestShrinkDropsWorkersAtTaskEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		gate := make(chan struct{})
		for f := range 9 {
			r.s.Enqueue(r.task("x", KindListStubs, int64(f+1), gate))
		}
		synctest.Wait()
		if st := r.s.Stats(); st.Workers != 3 {
			t.Fatalf("workers %d, want 3", st.Workers)
		}
		r.p.SetEffective(1)
		r.srv.limit.Store(3) // the three running keep their sessions
		close(gate)
		synctest.Wait()
		if peak := r.s.Stats().Workers; peak != 0 {
			t.Fatalf("workers after drain %d", peak)
		}
		if o := r.srv.open.Load(); o > 1 {
			t.Fatalf("open sessions %d after shrink to 1", o)
		}
	})
}

func TestGrowStartsMoreWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		r.p.SetEffective(1)
		r.srv.limit.Store(1)
		gate := make(chan struct{})
		for f := range 3 {
			r.s.Enqueue(r.task("x", KindListStubs, int64(f+1), gate))
		}
		synctest.Wait()
		if w := r.s.Stats().Workers; w != 1 {
			t.Fatalf("workers %d at N=1", w)
		}
		r.srv.limit.Store(3)
		r.p.SetEffective(3)
		synctest.Wait()
		if w := r.s.Stats().Workers; w != 3 {
			t.Fatalf("workers %d after growing to 3", w)
		}
		close(gate)
	})
}

// idleRig is a scheduler with IDLE whose Park records the sessions it parks on.
type idleRig struct {
	*rig
	parks   atomic.Int32
	parked  atomic.Int32
	changed chan struct{} // a send makes Park report a server change
	newMail atomic.Int32
	err     atomic.Pointer[error]
}

func newIdleRig(t *testing.T, n int) *idleRig {
	ir := &idleRig{rig: newRig(t, n, Options{}), changed: make(chan struct{})}
	ir.s.EnablePush(Push[*conn]{
		Park: func(ctx context.Context, c *conn, wake <-chan struct{}) (bool, error) {
			if p := ir.err.Load(); p != nil {
				return false, *p
			}
			ir.parks.Add(1)
			ir.parked.Add(1)
			defer ir.parked.Add(-1)
			select {
			case <-wake:
				return false, nil
			case <-ctx.Done():
				return false, ctx.Err()
			case <-ir.changed:
				return true, nil
			}
		},
		NewMail: func() Task[*conn] {
			return Task[*conn]{Kind: KindNewMail, FolderID: 1, Key: "new-mail", Run: func(context.Context, *conn) error {
				ir.newMail.Add(1)
				ir.record("newmail")
				return nil
			}}
		},
		CheckEvery: 30 * time.Second,
	})
	return ir
}

func TestIdleKeepsOneSessionAtN2(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 2)
		synctest.Wait()
		if r.parked.Load() != 1 {
			t.Fatal("IDLE is not parked")
		}
		gate := make(chan struct{})
		for f := range 3 {
			r.s.Enqueue(r.task("list", KindListStubs, int64(f+1), gate))
		}
		synctest.Wait()
		if st := r.s.Stats(); st.Workers != 2 || st.Running != 1 || r.parked.Load() != 1 {
			t.Fatalf("stats %+v parked %d: want IDLE on one session and one normal worker", st, r.parked.Load())
		}
		close(gate)
		synctest.Wait()
	})
}

func TestIdleServesFastLaneAfterFreeSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 3)
		synctest.Wait()
		gate := make(chan struct{})
		r.s.Enqueue(r.task("list", KindListStubs, 1, gate))
		synctest.Wait()
		// One free session left: the fast task takes it, IDLE stays parked.
		r.s.Enqueue(r.task("open1", KindOnDemandBody, 2, nil))
		synctest.Wait()
		if r.parks.Load() != 1 {
			t.Fatalf("IDLE was woken (%d parks) while a session was free", r.parks.Load())
		}
		// Now every session is busy: IDLE's own session serves the fast lane.
		r.s.Enqueue(r.task("list2", KindListStubs, 3, gate))
		synctest.Wait()
		r.s.Enqueue(r.task("open2", KindOnDemandBody, 4, nil))
		synctest.Wait()
		if !slices.Contains(r.order(), "open2") {
			t.Fatal("fast task waited although IDLE's session could serve it")
		}
		if r.parks.Load() != 2 || r.parked.Load() != 1 {
			t.Fatalf("parks %d parked %d, want IDLE back after the fast task", r.parks.Load(), r.parked.Load())
		}
		if st := r.s.Stats(); st.Workers != 3 {
			t.Fatalf("workers %d, want 3", st.Workers)
		}
		close(gate)
		synctest.Wait()
	})
}

func TestIdleAtN1WorksNormalLaneThenParks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 1)
		synctest.Wait()
		if r.parked.Load() != 1 {
			t.Fatal("IDLE not parked with nothing queued")
		}
		units := 0
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 2, Key: "b", Run: func(context.Context, *conn) error {
			r.record("N")
			time.Sleep(20 * time.Second)
			units++
			if units < 6 {
				return ErrAgain
			}
			return nil
		}})
		synctest.Wait()
		if r.parked.Load() != 0 {
			t.Fatal("IDLE kept the only session while the normal lane has work")
		}
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if units != 6 || r.parked.Load() != 1 {
			t.Fatalf("units %d parked %d, want the campaign done and IDLE back", units, r.parked.Load())
		}
		// 6 units of 20 s with a 30 s check: new mail is checked between units.
		got := strings.Join(r.order(), " ")
		if r.newMail.Load() < 2 || !strings.Contains(got, "N newmail N") {
			t.Fatalf("order %s: want short new-mail checks between units", got)
		}
		if d := r.srv.dials.Load(); d != 1 {
			t.Fatalf("dials %d, want the one session reused by IDLE and tasks", d)
		}
	})
}

func TestIdleChangeQueuesNewMailOnItsSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 2)
		synctest.Wait()
		r.changed <- struct{}{}
		synctest.Wait()
		if r.newMail.Load() != 1 || r.parked.Load() != 1 {
			t.Fatalf("new mail %d parked %d", r.newMail.Load(), r.parked.Load())
		}
		if d := r.srv.dials.Load(); d != 1 {
			t.Fatalf("dials %d: the new-mail task must reuse IDLE's session", d)
		}
	})
}

func TestIdleUnsupportedTurnsIdleOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 1)
		e := ErrIdleUnsupported
		r.err.Store(&e)
		synctest.Wait()
		if st := r.s.Stats(); st.Workers != 0 || st.Idle {
			t.Fatalf("stats %+v, want no worker kept for IDLE", st)
		}
		r.s.Enqueue(r.task("list", KindListStubs, 1, nil))
		synctest.Wait()
		if !slices.Contains(r.order(), "list") {
			t.Fatal("normal work stuck after IDLE was turned off")
		}
	})
}

func TestIdleErrorRetriesLater(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 2)
		e := errors.New("connection reset")
		r.err.Store(&e)
		synctest.Wait()
		r.err.Store(nil)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if r.parked.Load() != 0 {
			t.Fatal("IDLE retried before the wait")
		}
		time.Sleep(6 * time.Second)
		synctest.Wait()
		if r.parked.Load() != 1 {
			t.Fatal("IDLE did not come back after the retry wait")
		}
	})
}

// TestSessionsNeverExceedN runs fast and normal load, with IDLE, at once and
// checks the server never sees more than N sessions.
func TestSessionsNeverExceedN(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		for _, idle := range []bool{false, true} {
			t.Run(fmt.Sprintf("N=%d/idle=%v", n, idle), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var r *rig
					if idle {
						ir := newIdleRig(t, n)
						r = ir.rig
						go func() {
							for range 20 {
								time.Sleep(7 * time.Second)
								select {
								case ir.changed <- struct{}{}:
								default:
								}
							}
						}()
					} else {
						r = newRig(t, n, Options{})
					}
					var wg sync.WaitGroup
					for p := range 6 {
						rng := rand.New(rand.NewPCG(uint64(n), uint64(p)))
						wg.Go(func() {
							for i := range 30 {
								kind := []Kind{KindOnDemandBody, KindMove, KindListStubs, KindBodies, KindFullCheck, KindOffline}[rng.IntN(6)]
								hold := time.Duration(rng.IntN(3000)) * time.Millisecond
								left := 1 + rng.IntN(3)
								broken := rng.IntN(10) == 0
								r.s.Enqueue(Task[*conn]{Kind: kind, FolderID: int64(1 + rng.IntN(4)), Key: fmt.Sprint(p, ":", i),
									Run: func(context.Context, *conn) error {
										time.Sleep(hold)
										left--
										if left > 0 {
											return ErrAgain
										}
										if broken {
											return errors.New("broken")
										}
										return nil
									}})
								time.Sleep(time.Duration(rng.IntN(2000)) * time.Millisecond)
							}
						})
					}
					wg.Wait()
					time.Sleep(2 * time.Hour)
					synctest.Wait()
					if peak := r.srv.peak.Load(); peak > int32(n) {
						t.Fatalf("peak sessions %d, N %d", peak, n)
					}
					if st := r.s.Stats(); st.Fast+st.Normal+st.Running != 0 {
						t.Fatalf("work left: %+v", st)
					}
				})
			})
		}
	}
}

// TestListenWatcherHoldsNoSession: a watcher on its own connection (JMAP
// WebSocket) takes no worker; its events queue a fast task that any free
// worker runs, and its health is reported.
func TestListenWatcherHoldsNoSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		events := make(chan struct{})
		fail := make(chan error)
		var health []bool
		var newMail atomic.Int32
		r.s.EnablePush(Push[*conn]{
			Listen: func(ctx context.Context, up, changed func()) error {
				up()
				for {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case err := <-fail:
						return err
					case <-events:
						changed()
					}
				}
			},
			NewMail: func() Task[*conn] {
				return Task[*conn]{Kind: KindNewMail, Key: "changes", Run: func(context.Context, *conn) error {
					newMail.Add(1)
					return nil
				}}
			},
			RetryWait: func(error) time.Duration { return time.Minute },
			OnHealth:  func(h bool) { health = append(health, h) },
		})
		synctest.Wait()
		if st := r.s.Stats(); st.Workers != 0 || !st.PushHealthy || r.srv.dials.Load() != 0 {
			t.Fatalf("stats %+v dials %d: the watcher must hold no pooled session", st, r.srv.dials.Load())
		}
		gate := r.blockAll(t, 1) // N=1 busy: the event waits for the end of that task
		events <- struct{}{}
		synctest.Wait()
		if newMail.Load() != 0 {
			t.Fatal("push event ran sync work inline")
		}
		close(gate)
		synctest.Wait()
		if newMail.Load() != 1 {
			t.Fatalf("new-mail tasks %d, want 1", newMail.Load())
		}
		fail <- errors.New("socket closed")
		synctest.Wait()
		if r.s.PushHealthy() {
			t.Fatal("push still healthy after the socket closed")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if !slices.Equal(health, []bool{true, false, true}) {
			t.Fatalf("health %v, want up, down, up again after the retry wait", health)
		}
	})
}

func TestTunablesPerProvider(t *testing.T) {
	r := newRig(t, 1, Options{Tunables: Tunables{MaxBodyCount: 500}})
	if got := r.s.Tunables(); got.MaxBodyCount != 500 || got.BodyCount != DefaultBodyCount || got.BodyBytes != DefaultBodyBytes || got.ListPage != ListPage {
		t.Fatalf("tunables %+v", got)
	}
	if got := newRig(t, 1, Options{}).s.Tunables().BodyCount; got != DefaultBodyCount {
		t.Fatalf("default body count %d", got)
	}
}

func TestAdaptiveBudget(t *testing.T) {
	tests := []struct {
		name  string
		start int64
		took  []time.Duration
		want  int64
	}{
		{"fast grows by half", 20 << 20, []time.Duration{time.Second}, 30 << 20},
		{"2 s still grows", 20 << 20, []time.Duration{2 * time.Second}, 30 << 20},
		{"2 to 4 s holds", 20 << 20, []time.Duration{3 * time.Second, 4 * time.Second}, 20 << 20},
		{"slow halves", 20 << 20, []time.Duration{5 * time.Second}, 10 << 20},
		{"grow is capped", 60 << 20, []time.Duration{time.Second, time.Second}, MaxBodyBytes},
		{"shrink has a floor", 1 << 20, []time.Duration{9 * time.Second, 9 * time.Second, 9 * time.Second}, MinBodyBytes},
		{"recovers after slow", 20 << 20, []time.Duration{5 * time.Second, time.Second}, 15 << 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAdaptive(tt.start, MinBodyBytes, MaxBodyBytes)
			for _, d := range tt.took {
				a.Observe(d, true)
			}
			if got := a.Get(); got != tt.want {
				t.Fatalf("budget %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBodyLimitsCountFollowsLink(t *testing.T) {
	tests := []struct {
		name      string
		max       int
		took      time.Duration
		times     int
		wantCount int
	}{
		{"fast link grows, rounded up", 0, time.Second, 1, 8},            // 5 -> 8
		{"fast link grows to the cap", 0, time.Second, 10, MaxBodyCount}, // 5 8 12 18 27 41 50
		{"provider cap", 20, time.Second, 10, 20},
		{"2 to 4 s holds", 0, 3 * time.Second, 5, 5},
		{"slow link halves", 0, 5 * time.Second, 1, 2},
		{"slow link floor is 1", 0, 5 * time.Second, 5, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewBodyLimits(Tunables{MaxBodyCount: tt.max})
			_, startBytes := l.Get()
			for range tt.times {
				l.Observe(tt.took, true)
			}
			count, bytes := l.Get()
			if count != tt.wantCount {
				t.Fatalf("count %d, want %d", count, tt.wantCount)
			}
			switch {
			case tt.took <= growBelow && bytes <= startBytes,
				tt.took > shrinkAbove && bytes >= startBytes:
				t.Fatalf("byte budget %d did not move with the count (start %d)", bytes, startBytes)
			}
		})
	}
}

func TestPlanBatch(t *testing.T) {
	const mb = 1 << 20
	sizes := map[string]int64{"a": 1 * mb, "b": 2 * mb, "c": 30 * mb, "d": 1 * mb, "e": 1 * mb, "f": 1 * mb, "g": 1 * mb}
	size := func(id string) int64 { return sizes[id] }
	tests := []struct {
		name   string
		ids    []string
		count  int
		budget int64
		want   string
		full   bool
	}{
		{"count cap", []string{"a", "d", "e", "f", "g", "b"}, 5, 20 * mb, "adefg", true},
		{"byte cap", []string{"a", "b", "d", "e"}, 5, 3 * mb, "ab", true},
		{"byte cap stops before an oversized one", []string{"a", "c", "d"}, 5, 20 * mb, "a", true},
		{"oversized first goes alone", []string{"c", "a", "d"}, 5, 20 * mb, "c", true},
		{"unknown sizes count as 0", []string{"x", "y"}, 5, 1, "xy", false},
		{"fewer ids than the limits", []string{"a", "b"}, 5, 20 * mb, "ab", false},
		{"empty", nil, 5, 20 * mb, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(PlanBatch(tt.ids, size, tt.count, tt.budget), "")
			if got != tt.want {
				t.Fatalf("batch %q, want %q", got, tt.want)
			}
			batch, full := PlanBatchFull(tt.ids, size, tt.count, tt.budget)
			if strings.Join(batch, "") != tt.want || full != tt.full {
				t.Fatalf("PlanBatchFull %v full %v, want %q full %v", batch, full, tt.want, tt.full)
			}
		})
	}
}

// TestIdleNewMailCheckDropReportsDone: at N=1 the periodic new-mail check
// that IDLE queues between normal tasks can push the fast lane over FastCap.
// The on-demand task it drops must still get Done(ErrDropped), or its caller
// waits forever.
func TestIdleNewMailCheckDropReportsDone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newIdleRig(t, 1)
		synctest.Wait()
		gate := make(chan struct{})
		r.s.Enqueue(r.task("A", KindListStubs, 100, gate))
		synctest.Wait()
		var dropped atomic.Int32
		for i := range FastCap {
			task := r.task(fmt.Sprint("open", i), KindOnDemandBody, int64(i+1), nil)
			task.Done = func(err error) {
				if errors.Is(err, ErrDropped) {
					dropped.Add(1)
				}
			}
			r.s.Enqueue(task)
		}
		r.s.Enqueue(r.task("B", KindListStubs, 200, nil))
		time.Sleep(31 * time.Second)
		close(gate)
		synctest.Wait()
		if got := dropped.Load(); got != 1 {
			t.Fatalf("Done(ErrDropped) ran %d times, want 1 for the task the new-mail check pushed out", got)
		}
	})
}

// TestDedupKeepsBarrier: a barrier task joining a queued plain task with the
// same key makes the queued one a barrier.
func TestDedupKeepsBarrier(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		gate := r.blockAll(t, 2)
		hold := make(chan struct{})
		plain := r.task("tree", KindFolderTree, 0, hold)
		plain.Key = "tree"
		r.s.Enqueue(plain)
		barrier := r.task("tree-barrier", KindFolderTree, 0, nil)
		barrier.Key, barrier.Barrier = "tree", true
		r.s.Enqueue(barrier)
		r.s.Enqueue(r.task("list", KindListStubs, 1, nil))
		if st := r.s.Stats(); st.Normal != 2 {
			t.Fatalf("normal lane %d, want the barrier merged into the queued tree", st.Normal)
		}
		close(gate)
		synctest.Wait()
		ranBeside := slices.Contains(r.order(), "list")
		close(hold)
		synctest.Wait()
		if ranBeside {
			t.Fatal("list ran beside the merged barrier")
		}
		if !slices.Contains(r.order(), "list") {
			t.Fatal("list never ran after the barrier")
		}
	})
}

// TestBodyLimitsGrowOnlyWhenBatchHitLimit: quick batches that did not hit a
// limit (a few new-mail bodies) leave the limits alone; a slow one still
// shrinks them.
func TestBodyLimitsGrowOnlyWhenBatchHitLimit(t *testing.T) {
	l := NewBodyLimits(Tunables{})
	for range 20 {
		l.Observe(100*time.Millisecond, false)
	}
	if count, bytes := l.Get(); count != DefaultBodyCount || bytes != DefaultBodyBytes {
		t.Fatalf("limits %d / %d after small quick batches, want the start %d / %d", count, bytes, DefaultBodyCount, DefaultBodyBytes)
	}
	l.Observe(5*time.Second, false)
	if count, bytes := l.Get(); count != DefaultBodyCount/2 || bytes != DefaultBodyBytes/2 {
		t.Fatalf("limits %d / %d after a slow small batch, want them halved", count, bytes)
	}
	l.Observe(time.Second, true)
	if count, _ := l.Get(); count != 3 {
		t.Fatalf("count %d after a quick full batch, want 3", count)
	}
}

// TestChunkBudgetGrowsBackToStart: the chunk size halves on slow chunks and
// grows back on quick whole chunks, up to its start value and no further.
func TestChunkBudgetGrowsBackToStart(t *testing.T) {
	r := newRig(t, 1, Options{})
	c := r.s.ChunkBudget()
	for range 3 {
		c.Observe(5*time.Second, true)
	}
	if got := c.Get(); got != DefaultChunkBytes/8 {
		t.Fatalf("chunk %d after three slow chunks, want %d", got, DefaultChunkBytes/8)
	}
	c.Observe(time.Second, false)
	if got := c.Get(); got != DefaultChunkBytes/8 {
		t.Fatalf("chunk %d grew on a short last chunk", got)
	}
	for range 10 {
		c.Observe(time.Second, true)
	}
	if got := c.Get(); got != DefaultChunkBytes {
		t.Fatalf("chunk %d after quick chunks, want back at the start %d", got, DefaultChunkBytes)
	}
}

// TestBarrierWaitsForRunningTasks: a barrier starts only once no other task
// of the account runs.
func TestBarrierWaitsForRunningTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		gate := make(chan struct{})
		r.s.Enqueue(r.task("list", KindListStubs, 1, gate))
		synctest.Wait()
		tree := r.task("tree", KindFolderTree, 0, nil)
		tree.Barrier = true
		r.s.Enqueue(tree)
		synctest.Wait()
		early := slices.Contains(r.order(), "tree")
		close(gate)
		synctest.Wait()
		if early {
			t.Fatal("the barrier started beside a running task")
		}
		if got := strings.Join(r.order(), " "); got != "list tree" {
			t.Fatalf("order %s, want list tree", got)
		}
	})
}

// TestTwoBarriersDoNotBlockEachOther: two queued barriers with different
// keys run one after the other, in queue order.
func TestTwoBarriersDoNotBlockEachOther(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 3, Options{})
		gate := r.blockAll(t, 3)
		for _, name := range []string{"b1", "b2"} {
			b := r.task(name, KindFolderTree, 0, nil)
			b.Key, b.Barrier = name, true
			r.s.Enqueue(b)
		}
		r.s.Enqueue(r.task("list", KindListStubs, 1, nil))
		close(gate)
		synctest.Wait()
		if got := strings.Join(r.order()[3:], " "); got != "b1 b2 list" {
			t.Fatalf("order %q, want b1 b2 list", got)
		}
	})
}

// TestParkWithoutNewMail: a Park watcher without NewMail that reports a
// change does not panic; IDLE parks again.
func TestParkWithoutNewMail(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 2, Options{})
		changed := make(chan struct{})
		var parks atomic.Int32
		r.s.EnablePush(Push[*conn]{Park: func(ctx context.Context, _ *conn, wake <-chan struct{}) (bool, error) {
			parks.Add(1)
			select {
			case <-changed:
				return true, nil
			case <-wake:
				return false, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}})
		synctest.Wait()
		changed <- struct{}{}
		synctest.Wait()
		if got := parks.Load(); got != 2 {
			t.Fatalf("parks %d, want IDLE back after the change", got)
		}
	})
}

// fullProvider stands for a view over shared sessions whose Limit counts a
// free session that another scheduler is still dialing: Get refuses with
// ErrSessionsFull until the dial ends and the session comes back.
type fullProvider struct {
	gets     atomic.Int32
	refuse   atomic.Bool
	mu       sync.Mutex
	watchers []func()
}

func (p *fullProvider) Get(context.Context) (*conn, error) {
	p.gets.Add(1)
	if p.refuse.Load() {
		return nil, pool.ErrSessionsFull
	}
	return &conn{srv: &server{}}, nil
}
func (p *fullProvider) Put(*conn, bool) {}
func (p *fullProvider) Limit() int      { return 1 }
func (p *fullProvider) Watch(fn func()) func() {
	p.mu.Lock()
	p.watchers = append(p.watchers, fn)
	p.mu.Unlock()
	return func() {}
}
func (p *fullProvider) Close() {}

// notify is the other scheduler returning its session.
func (p *fullProvider) notify() {
	p.mu.Lock()
	ws := slices.Clone(p.watchers)
	p.mu.Unlock()
	for _, fn := range ws {
		fn()
	}
}

// TestSessionsFullDoesNotSpin: while the shared pool refuses a session the
// view's Limit promised, the scheduler waits for a notification or a short
// backoff instead of re-dispatching at once. Real time, so a busy loop shows.
func TestSessionsFullDoesNotSpin(t *testing.T) {
	prov := &fullProvider{}
	prov.refuse.Store(true)
	s := New(context.Background(), prov, Options{})
	defer s.Stop(context.Background())
	ran := make(chan struct{})
	s.Enqueue(Task[*conn]{Kind: KindListStubs, FolderID: 1, Run: func(context.Context, *conn) error {
		close(ran)
		return nil
	}})
	time.Sleep(500 * time.Millisecond) // a slow dial in the other scheduler
	if got := prov.gets.Load(); got > 10 {
		t.Fatalf("%d Get calls in 500 ms while the pool was full: busy loop", got)
	}
	prov.refuse.Store(false)
	prov.notify()
	select {
	case <-ran:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("the task did not start right after the session came back")
	}
}

// TestIdleRetriesAfterSessionsFull: a refused session (the cap shrank) is no
// IDLE failure: IDLE comes back after the short backoff, not RetryWait.
func TestIdleRetriesAfterSessionsFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := &fullProvider{}
		prov.refuse.Store(true)
		s := New(context.Background(), prov, Options{})
		defer s.Stop(context.Background())
		var parks atomic.Int32
		var downs atomic.Int32
		s.EnablePush(Push[*conn]{
			Park: func(ctx context.Context, _ *conn, wake <-chan struct{}) (bool, error) {
				parks.Add(1)
				select {
				case <-wake:
				case <-ctx.Done():
				}
				return false, nil
			},
			RetryWait: func(error) time.Duration { return time.Hour },
			OnHealth: func(h bool) {
				if !h {
					downs.Add(1)
				}
			},
		})
		synctest.Wait()
		time.Sleep(time.Second)
		prov.refuse.Store(false)
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if parks.Load() != 1 {
			t.Fatal("IDLE did not come back soon after a refused session")
		}
		if got := s.Stats().PushHealthy; !got || downs.Load() != 0 {
			t.Fatalf("healthy %v, downs %d: a refused session is not a push failure", got, downs.Load())
		}
	})
}

// TestAgainBrokenRedials: a generator unit that failed on a dead connection
// returns ErrAgainBroken. It goes back to the tail like ErrAgain, but its
// session is closed and the next task dials a fresh one. A bare ErrAgain
// keeps the session.
func TestAgainBrokenRedials(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		netErr := errors.New("connection reset")
		r := newRig(t, 1, Options{Broken: func(err error) bool { return errors.Is(err, netErr) }})
		results := []error{ErrAgain, errors.Join(ErrAgainBroken, netErr), nil}
		var sessions []*conn
		var final error
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 1, Key: "b", Run: func(_ context.Context, c *conn) error {
			sessions = append(sessions, c)
			err := results[0]
			results = results[1:]
			return err
		}, Done: func(err error) { final = err }})
		synctest.Wait()
		if len(sessions) != 3 || final != nil {
			t.Fatalf("ran %d units, final %v; want 3 units and success", len(sessions), final)
		}
		if sessions[0] != sessions[1] {
			t.Fatal("a bare ErrAgain dropped the session")
		}
		if !sessions[1].closed.Load() || sessions[2] == sessions[1] {
			t.Fatal("ErrAgainBroken kept the dead session")
		}
		if d := r.srv.dials.Load(); d != 2 {
			t.Fatalf("dials = %d, want 2", d)
		}
	})
}

// TestNewMailBuiltOffTheLock: the N=1 new-mail check builds its task
// without holding the scheduler's lock, so NewMail may read the store.
func TestNewMailBuiltOffTheLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		var calls, underLock atomic.Int32
		r.s.EnablePush(Push[*conn]{
			Park: func(ctx context.Context, _ *conn, wake <-chan struct{}) (bool, error) {
				select {
				case <-wake:
				case <-ctx.Done():
				}
				return false, nil
			},
			NewMail: func() Task[*conn] {
				calls.Add(1)
				if r.s.mu.TryLock() {
					r.s.mu.Unlock()
				} else {
					underLock.Add(1)
				}
				return Task[*conn]{Kind: KindNewMail, FolderID: 1, Key: "new-mail"}
			},
		})
		synctest.Wait()
		units := 0
		r.s.Enqueue(Task[*conn]{Kind: KindBodies, FolderID: 2, Key: "b", Run: func(context.Context, *conn) error {
			time.Sleep(20 * time.Second)
			if units++; units < 4 {
				return ErrAgain
			}
			return nil
		}})
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if calls.Load() == 0 {
			t.Fatal("no new-mail check between units")
		}
		if n := underLock.Load(); n > 0 {
			t.Fatalf("NewMail ran %d times under the scheduler's lock", n)
		}
	})
}

// TestBarrierWithFolderIgnoresQueuedFolderRules: a barrier with a FolderID
// does not wait for a queued listing of its folder, which itself waits for
// the barrier; it runs first, then the listing.
func TestBarrierWithFolderIgnoresQueuedFolderRules(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, 1, Options{})
		gate := r.blockAll(t, 1)
		r.s.Enqueue(r.task("list", KindListStubs, 5, nil))
		b := r.task("barrier", KindBodies, 5, nil)
		b.Barrier = true
		r.s.Enqueue(b)
		close(gate)
		synctest.Wait()
		if got := strings.Join(r.order()[1:], " "); got != "barrier list" {
			t.Fatalf("order %q, want barrier list", got)
		}
	})
}

// hungConn's Close blocks until release closes, like a LOGOUT to a server
// that does not answer.
type hungConn struct{ release <-chan struct{} }

func (c *hungConn) Close() error {
	<-c.release
	return nil
}

// refusals counts the Gets the sessions refused with ErrSessionsFull.
type refusals struct {
	*pool.Sessions[*hungConn]
	n atomic.Int32
}

func (r *refusals) Get(ctx context.Context) (*hungConn, error) {
	c, err := r.Sessions.Get(ctx)
	if errors.Is(err, pool.ErrSessionsFull) {
		r.n.Add(1)
	}
	return c, err
}

// TestOwnBrokenPutKeepsBackoff: a session the scheduler puts back broken
// still holds its slot while it closes, so it is no "sessions changed"
// event: it must not end the backoff and start a worker whose Get is
// refused at once.
func TestOwnBrokenPutKeepsBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hung := make(chan struct{})
		sessions := pool.NewSessions(pool.New(2), func(context.Context) (*hungConn, error) {
			return &hungConn{release: hung}, nil
		}, time.Minute)
		prov := &refusals{Sessions: sessions}
		dead := errors.New("connection reset")
		s := New(context.Background(), prov, Options{Broken: func(err error) bool { return errors.Is(err, dead) }})
		closeHung := sync.OnceFunc(func() { close(hung) })
		defer func() {
			closeHung()
			_ = s.Stop(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := sessions.WaitClosed(ctx); err != nil {
				t.Error(err)
			}
		}()
		var ran sync.Map
		task := func(name string, folder int64, gate <-chan struct{}, err error) Task[*hungConn] {
			return Task[*hungConn]{Kind: KindListStubs, FolderID: folder, Run: func(context.Context, *hungConn) error {
				ran.Store(name, true)
				if gate != nil {
					<-gate
				}
				return err
			}}
		}
		gate1, gate2 := make(chan struct{}), make(chan struct{})
		s.Enqueue(task("t1", 1, gate1, dead))
		s.Enqueue(task("t2", 2, gate2, dead))
		synctest.Wait()
		close(gate2) // t2's session breaks; its LOGOUT hangs
		synctest.Wait()
		s.Enqueue(task("t3", 3, nil, nil)) // refused: 1 out + 1 closing
		s.Enqueue(task("t4", 4, nil, nil)) // waits for the backoff
		synctest.Wait()
		if got := prov.n.Load(); got != 1 {
			t.Fatalf("refusals = %d before t1 ends, want 1", got)
		}
		close(gate1) // t1's session breaks too; its worker asks for t3 once
		synctest.Wait()
		if got := prov.n.Load(); got != 2 {
			t.Fatalf("refusals = %d after t1's broken Put, want 2 (no extra worker)", got)
		}
		closeHung()
		synctest.Wait()
		for _, name := range []string{"t3", "t4"} {
			if _, ok := ran.Load(name); !ok {
				t.Fatalf("%s did not run after the closes ended", name)
			}
		}
	})
}

// twoProvider is fullProvider with a cap of 2.
type twoProvider struct{ *fullProvider }

func (twoProvider) Limit() int { return 2 }

// TestGotSessionEndsBackoff: a successful Get ends the backoff. Its timer,
// grown long by repeated refusals, must not hold back a worker after the
// provider had room again.
func TestGotSessionEndsBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prov := twoProvider{&fullProvider{}}
		dead := errors.New("connection reset")
		s := New(context.Background(), Provider[*conn](prov), Options{Broken: func(err error) bool { return errors.Is(err, dead) }})
		defer s.Stop(context.Background())
		var ran sync.Map
		task := func(name string, folder int64, gate <-chan struct{}, err error) Task[*conn] {
			return Task[*conn]{Kind: KindListStubs, FolderID: folder, Run: func(context.Context, *conn) error {
				ran.Store(name, true)
				if gate != nil {
					<-gate
				}
				return err
			}}
		}
		gateA, gateB := make(chan struct{}), make(chan struct{})
		defer close(gateB)
		s.Enqueue(task("A", 1, gateA, dead))
		synctest.Wait()
		prov.refuse.Store(true)
		s.Enqueue(task("B", 2, gateB, nil))
		// Refused at 0, 100 and 300 ms: the next try is at 700 ms.
		time.Sleep(350 * time.Millisecond)
		synctest.Wait()
		if _, ok := ran.Load("B"); ok {
			t.Fatal("B ran while the provider refused")
		}
		prov.refuse.Store(false)
		close(gateA) // A's session breaks; its worker gets a new one for B
		synctest.Wait()
		if _, ok := ran.Load("B"); !ok {
			t.Fatal("B did not run on A's worker")
		}
		s.Enqueue(task("C", 3, nil, nil))
		synctest.Wait()
		if _, ok := ran.Load("C"); !ok {
			t.Fatal("C waited for a backoff timer after a Get had succeeded")
		}
	})
}
