package syncsched

import (
	"container/heap"
	"context"
	"errors"
	"sync"

	mailsync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// Scheduler runs one account's sync jobs. Live work is started before
// background work. Background stubs are started before background bodies,
// and both before a full reconcile.
// An in-flight job is not cancelled when a higher-priority job arrives;
// Stop is the hard cancel.
//
// Soft-pause holds the background queue: the current job can finish its
// chunk and return ErrSoftPaused, and the next background job waits until
// ClearSoftPause. Live jobs still run. Only a live job or IDLE on an account
// whose effective pool size is 1 requests it; send never does.
type Scheduler struct {
	accountID int64

	mu      sync.Mutex
	cond    *sync.Cond
	pq      priorityQueue
	seq     uint64
	queued  [numPriorities]int
	active  [numPriorities]int
	paused  bool
	stopped bool
	started bool

	// bgLaunching counts background jobs taken off the queue that have not
	// yet passed the hold check in launch. bgStarts counts those that did.
	bgLaunching int
	bgStarts    uint64
	// beforeBackgroundGate, when set by a test, runs after a background job
	// gets its pool slot and before the hold check.
	beforeBackgroundGate func()

	pool   *pool.Pool
	cancel context.CancelFunc

	loop sync.WaitGroup
	jobs sync.WaitGroup
}

// New returns a scheduler for one account. Jobs sit queued until Start.
func New(accountID int64) *Scheduler {
	s := &Scheduler{accountID: accountID}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Enqueue copies j onto the queue. It is safe to call from a running job.
// After Stop, new jobs are dropped.
func (s *Scheduler) Enqueue(j Job) {
	job := j
	job.RemoteIDs = append([]string(nil), j.RemoteIDs...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.push(&job)
	s.cond.Broadcast()
}

// RequestSoftPause holds the background queue after the current chunk.
// Live jobs are not held. Calling it again while already paused is a no-op.
func (s *Scheduler) RequestSoftPause() {
	s.mu.Lock()
	s.paused = true
	s.cond.Broadcast()
	s.mu.Unlock()
}

// ClearSoftPause lets background jobs start again.
func (s *Scheduler) ClearSoftPause() {
	s.mu.Lock()
	s.paused = false
	s.cond.Broadcast()
	s.mu.Unlock()
}

// SoftPauseRequested reports whether background work should hold before the
// next chunk. Job funcs assign this to Engine.PauseCheck.
func (s *Scheduler) SoftPauseRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// LiveBusy reports whether a live job is queued or running. IMAP IDLE is not
// itself a scheduler job; it watches this so it can hand the live pool slot
// to manual sync or an on-demand body.
func (s *Scheduler) LiveBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queued[PriorityLive] > 0 || s.active[PriorityLive] > 0
}

// BackgroundBusy reports whether a list, body, or reconcile job is queued or running.
func (s *Scheduler) BackgroundBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queued[PriorityBackgroundStubs] > 0 || s.active[PriorityBackgroundStubs] > 0 ||
		s.queued[PriorityBackgroundBody] > 0 || s.active[PriorityBackgroundBody] > 0 ||
		s.queued[PriorityBackgroundReconcile] > 0 || s.active[PriorityBackgroundReconcile] > 0
}

// BackgroundStarts returns how many background jobs have got a pool slot and
// passed the hold check, so their Run is committed. It only grows. Pass the
// value to WaitBackgroundStarted.
func (s *Scheduler) BackgroundStarts() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bgStarts
}

// WaitBackgroundStarted blocks until a background job has started since the
// BackgroundStarts value since. A job counts once it holds a pool slot and is
// past the hold check: a soft-pause requested after that cannot requeue it
// without running. It returns nil early when no background job can start:
// none is queued or launching, the queue is held (soft-pause or a live job),
// or the scheduler is not running. It returns ctx's error when ctx ends first.
func (s *Scheduler) WaitBackgroundStarted(ctx context.Context, since uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stop()
	for {
		if s.bgStarts > since {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.stopped || !s.started || s.heldLocked() {
			return nil
		}
		if s.bgLaunching == 0 && s.queued[PriorityBackgroundStubs] == 0 && s.queued[PriorityBackgroundBody] == 0 && s.queued[PriorityBackgroundReconcile] == 0 {
			return nil
		}
		s.cond.Wait()
	}
}

// MaxBackgroundSlots returns the pool's background concurrency cap, or 1 if
// Start has not wired a pool yet.
func (s *Scheduler) MaxBackgroundSlots() int {
	s.mu.Lock()
	p := s.pool
	s.mu.Unlock()
	if p == nil {
		return 1
	}
	return p.MaxBackgroundSlots()
}

// Start runs the queue until ctx is cancelled or Stop. runWorkers is ignored;
// the pool decides how many jobs may hold a slot. Start is a no-op if the
// scheduler is already running.
func (s *Scheduler) Start(ctx context.Context, p *pool.Pool, runWorkers int) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stopped = false
	s.pool = p
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	// take holds background jobs back on the pool's background cap. A pool
	// that grows has to wake it, or they wait for an unrelated Enqueue or
	// job end.
	p.OnResize(func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})

	stopWake := context.AfterFunc(runCtx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})

	s.loop.Go(func() {
		defer stopWake()
		s.loopJobs(runCtx)
	})
}

// Stop hard-cancels queued and in-flight work. It waits until the runner
// and any job goroutines have returned.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.started = false
	cancel := s.cancel
	s.cancel = nil
	s.cond.Broadcast()
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.loop.Wait()
	s.jobs.Wait()
}

func (s *Scheduler) loopJobs(ctx context.Context) {
	for {
		job, ok := s.take(ctx)
		if !ok {
			return
		}
		if ctx.Err() != nil {
			s.endLaunch(job)
			s.finishActive(job.Priority)
			return
		}
		s.launch(ctx, job)
	}
}

// take pops the next job that is allowed to start. Live is preferred, and
// only one live job is active at a time so a later live job still beats
// background. Stubs beat bodies, and a full reconcile waits until no list or
// body work is queued or running. Background waits while soft-pause is set
// or a live job is queued or running, and while the pool's background slots
// are all taken: a popped job would only wait in Acquire, and a stubs job
// enqueued later could not overtake it there.
func (s *Scheduler) take(ctx context.Context) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if ctx.Err() != nil || s.stopped {
			return nil, false
		}
		if s.queued[PriorityLive] > 0 {
			if s.active[PriorityLive] == 0 {
				return s.popActive(), true
			}
			s.cond.Wait()
			continue
		}
		if s.paused || s.active[PriorityLive] > 0 || s.pq.Len() == 0 {
			s.cond.Wait()
			continue
		}
		if s.activeBackgroundLocked() >= s.pool.MaxBackgroundSlots() {
			s.cond.Wait()
			continue
		}
		top := s.pq[0].job
		if top.Priority == PriorityBackgroundBody && (s.queued[PriorityBackgroundStubs] > 0 || s.active[PriorityBackgroundStubs] > 0) {
			s.cond.Wait()
			continue
		}
		if top.Priority == PriorityBackgroundReconcile && s.backgroundWorkLocked() {
			s.cond.Wait()
			continue
		}
		return s.popActive(), true
	}
}

func (s *Scheduler) popActive() *Job {
	job := s.pop()
	s.active[job.Priority]++
	if job.Priority != PriorityLive {
		s.bgLaunching++
	}
	return job
}

func (s *Scheduler) launch(ctx context.Context, job *Job) {
	kind := pool.Background
	if job.Priority == PriorityLive {
		kind = pool.Live
	}
	s.jobs.Go(func() {
		// Drop the active count before a requeue so the dispatcher cannot
		// start this same job a second time while it is still marked active.
		released := false
		releaseActive := func() {
			if released {
				return
			}
			released = true
			s.finishActive(job.Priority)
		}
		defer releaseActive()
		// A background job that never reaches the hold check still has to
		// leave bgLaunching, or WaitBackgroundStarted would wait for it.
		gated := false
		defer func() {
			if !gated {
				s.endLaunch(job)
			}
		}()

		if err := s.pool.Acquire(ctx, kind); err != nil {
			return
		}
		defer s.pool.Release(kind)
		if ctx.Err() != nil {
			return
		}
		if kind == pool.Background {
			if s.beforeBackgroundGate != nil {
				s.beforeBackgroundGate()
			}
			gated = true
			if !s.passBackgroundGate() {
				releaseActive()
				s.requeueLaunched(job)
				return
			}
		}
		if job.Run == nil {
			return
		}
		attempt := context.WithValue(ctx, remoteIDsKey{}, append([]string(nil), job.RemoteIDs...))
		err := job.Run(attempt)
		releaseActive()
		s.onResult(ctx, job, err)
	})
}

// passBackgroundGate is the hold check for a background job that has its
// pool slot. When the queue is not held it marks the job started in the same
// critical section, so a soft-pause after this point lets it run.
func (s *Scheduler) passBackgroundGate() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heldLocked() {
		return false
	}
	s.bgLaunching--
	s.bgStarts++
	s.cond.Broadcast()
	return true
}

func (s *Scheduler) heldLocked() bool {
	return s.paused || s.active[PriorityLive] > 0 || s.queued[PriorityLive] > 0
}

// backgroundWorkLocked reports whether list or body work is queued or running.
// A full reconcile waits for it, so it never competes with a sync campaign.
func (s *Scheduler) backgroundWorkLocked() bool {
	return s.queued[PriorityBackgroundStubs] > 0 || s.active[PriorityBackgroundStubs] > 0 ||
		s.queued[PriorityBackgroundBody] > 0 || s.active[PriorityBackgroundBody] > 0
}

func (s *Scheduler) activeBackgroundLocked() int {
	return s.active[PriorityBackgroundStubs] + s.active[PriorityBackgroundBody] + s.active[PriorityBackgroundReconcile]
}

// requeueLaunched puts a held background job back on the queue and drops it
// from bgLaunching together, so waiters never see it as neither queued nor
// launching.
func (s *Scheduler) requeueLaunched(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bgLaunching--
	if !s.stopped {
		s.push(job)
	}
	s.cond.Broadcast()
}

// endLaunch drops a background job that will not run from bgLaunching.
func (s *Scheduler) endLaunch(job *Job) {
	if job.Priority == PriorityLive {
		return
	}
	s.mu.Lock()
	s.bgLaunching--
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *Scheduler) onResult(ctx context.Context, job *Job, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	if !errors.Is(err, mailsync.ErrSoftPaused) {
		return
	}
	if rem, ok := mailsync.SoftPauseRemaining(err); ok {
		job.RemoteIDs = append([]string(nil), rem...)
		if len(job.RemoteIDs) == 0 {
			return
		}
	}
	s.requeue(job)
}

func (s *Scheduler) requeue(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.push(job)
	s.cond.Broadcast()
}

func (s *Scheduler) finishActive(p Priority) {
	s.mu.Lock()
	if s.active[p] > 0 {
		s.active[p]--
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *Scheduler) push(job *Job) {
	heap.Push(&s.pq, &entry{job: job, seq: s.seq})
	s.seq++
	s.queued[job.Priority]++
}

func (s *Scheduler) pop() *Job {
	e := heap.Pop(&s.pq).(*entry)
	s.queued[e.job.Priority]--
	return e.job
}

type entry struct {
	job *Job
	seq uint64
}

type priorityQueue []*entry

func (p priorityQueue) Len() int { return len(p) }

func (p priorityQueue) Less(i, j int) bool {
	if p[i].job.Priority != p[j].job.Priority {
		return p[i].job.Priority > p[j].job.Priority
	}
	return p[i].seq < p[j].seq
}

func (p priorityQueue) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func (p *priorityQueue) Push(x any) {
	*p = append(*p, x.(*entry))
}

func (p *priorityQueue) Pop() any {
	old := *p
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*p = old[:n-1]
	return item
}
