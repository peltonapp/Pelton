package syncsched

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"time"
)

// Policy is how the normal lane orders its tasks.
type Policy int

const (
	// PolicyClasses runs the highest class first, FIFO within a class.
	PolicyClasses Policy = iota
	// PolicyFIFO runs normal tasks in arrival order.
	PolicyFIFO
)

// Provider hands out what a worker holds while it runs tasks: a logged-in
// IMAP connection (pool.Sessions) or a JMAP request token. Limit is the
// current cap (effective N after the auto-adjust); the scheduler starts no
// worker above it, and after a shrink the workers above it leave at the end
// of their task. Get must not block for a free session.
//
// A provider shared by several schedulers should leave out of Limit every
// session it cannot hand to this one, including other users' dials still in
// flight (pool.Sessions.Busy). When Get still refuses with an error that has
// a SessionsFull() method returning true (pool.ErrSessionsFull), the
// scheduler puts the task back unstarted and starts no new worker until
// Watch fires, one of its own sessions is returned, or a short backoff
// (100 ms doubling to 2 s) ends.
type Provider[S any] interface {
	Get(ctx context.Context) (S, error)
	// Put returns a session; broken closes it so the next Get makes a new one.
	Put(s S, broken bool)
	Limit() int
	// Watch runs fn after every change of Limit, including a session coming
	// back from another user of the same sessions. fn may take the
	// scheduler's lock: call it on no lock of the provider.
	Watch(fn func()) (stop func())
	// Close drops the idle sessions; later Puts close theirs.
	Close()
}

// Options configure a Scheduler. The zero value works.
type Options struct {
	// AccountID names the account in logs.
	AccountID int64
	// Tunables are the provider's batch sizes; zero fields are defaults.
	Tunables Tunables
	// Broken reports whether a task error leaves the session unusable, so it
	// is closed and the next task dials. Nil treats every error as broken.
	Broken func(error) bool
	// Policy orders the normal lane.
	Policy Policy
	// Log, when set, gets a debug line with the queue lengths per lane.
	Log *slog.Logger
	// Group, when set, is shared with the account's other schedulers: a
	// task does not start beside a conflicting task another one still runs
	// (see Group).
	Group *Group
}

// Push watches the server for changes. Exactly one of Park and Listen is set.
//
// Park (IMAP IDLE) waits on a pooled session, which counts toward N. At N>=2
// that worker keeps IDLE and serves only the fast lane, and its slot stays
// out of the normal lane also while it dials or waits to retry; at N=1 the only
// session works the normal lane, checks new mail every CheckEvery between
// tasks, and parks when the lane is empty.
//
// Listen (JMAP WebSocket or EventSource) runs on its own connection outside
// the pool and holds no worker; the fast lane is served by free workers.
//
// A change never runs sync work in the watcher: it queues NewMail, a small
// fast-lane task.
type Push[S any] struct {
	// Park waits in IDLE on s until wake closes, ctx ends or the server
	// reports a change (changed true).
	Park func(ctx context.Context, s S, wake <-chan struct{}) (changed bool, err error)
	// Listen blocks on its own connection until ctx ends or it fails. It
	// calls up once connected and changed on every push event.
	Listen func(ctx context.Context, up, changed func()) error
	// NewMail is the fast task queued after a change. Nil queues nothing.
	// It is called on no lock of the scheduler and may read the store.
	NewMail func() Task[S]
	// CheckEvery is the N=1 new-mail interval of a Park watcher. 0 is 30 s.
	CheckEvery time.Duration
	// RetryWait is how long to wait after the watcher failed with err. Nil
	// waits 15 s.
	RetryWait func(err error) time.Duration
	// OnHealth, when set, is told when push comes up or goes down, so the
	// runtime can skip or resume timed sync. It runs on no lock.
	OnHealth func(healthy bool)
}

// UsesPoolSession reports whether the watcher holds one of the N sessions.
func (p Push[S]) UsesPoolSession() bool { return p.Park != nil }

// Stats are a snapshot of one account's queues.
type Stats struct {
	Fast, Normal, Running, Workers int
	Idle                           bool // a worker holds a Park watcher's session
	PushHealthy                    bool
}

// Scheduler runs one account's tasks. See the package doc.
type Scheduler[S any] struct {
	prov    Provider[S]
	opts    Options
	ctx     context.Context
	cancel  context.CancelFunc
	unwatch func()
	stopCtx func() bool
	// body and chunk are the account's adaptive batch limits.
	body  *BodyLimits
	chunk *Adaptive

	mu       sync.Mutex
	fast     []*entry[S]
	normal   []*entry[S]
	active   []*entry[S]
	workers  int
	streak   int // fast tasks picked since the last normal one
	stopping bool
	idle     *idleState[S]
	wg       sync.WaitGroup
	// full is set when the provider refused a session its Limit allowed
	// (ErrSessionsFull: the cap shrank, or another scheduler on the same
	// sessions is still dialing). No new worker starts until the provider
	// notifies (Watch), a session of this scheduler is returned, or the
	// backoff timer fires, so a refusal never turns into a busy loop.
	full      bool
	fullWait  time.Duration // next backoff; doubles up to maxFullWait
	fullTimer *time.Timer
	// changes counts provider notifications, so a refusal that raced one
	// is retried at once instead of waiting for the backoff.
	changes uint64
	// holding counts the workers holding a session (worker.held), gaveBack
	// the sessions they gave back, ever: a refused Get that raced one of
	// them may have been refused for it (ownSessionLocked).
	holding  int
	gaveBack uint64
}

// Backoff after ErrSessionsFull when no notification comes first.
const (
	minFullWait = 100 * time.Millisecond
	maxFullWait = 2 * time.Second
)

type entry[S any] struct {
	t     Task[S]
	class Class
	dones []func(error)
	// shape is what Options.Group recorded when the task started (for a
	// width entry: when its first running copy started; every copy records
	// one).
	shape shape
	// state is open (may be dispatched) or held (ErrSaturated: waits for
	// another unit of its folder to end; held only while one runs, see
	// relaxLocked); a width entry may also be ended
	// (a copy returned nil) or failed (a copy returned err) while its other
	// copies drain.
	state entryState
	// running counts a width entry's copies running now. A width entry
	// stays in its lane while they run.
	running int
	// err is a failed width entry's first error.
	err error
	// rejoin is set when a task joined a width entry while copies ran and
	// no copy has started since: those copies may have read the store
	// before the joiner's change, so a nil from them does not end it.
	rejoin bool
}

type entryState int

const (
	stateOpen entryState = iota
	stateHeld
	stateEnded
	stateFailed
)

// wide reports whether e is a width entry (Task.Width, normal lane).
func (e *entry[S]) wide() bool { return e.t.Width != nil && !e.t.Kind.Fast() }

type idleState[S any] struct {
	cfg         Push[S]
	disabled    bool
	retryAt     time.Time
	worker      *worker
	wake        chan struct{} // non-nil while parked
	lastNewMail time.Time
	healthy     bool
	listenStop  context.CancelFunc
}

// worker is one worker goroutine. held is set while it holds a session
// (guarded by Scheduler.mu).
type worker struct{ idle, held bool }

// New starts a scheduler for one account. Cancelling ctx is the hard stop
// (app quit): running tasks see it and the queue is dropped.
func New[S any](ctx context.Context, prov Provider[S], opts Options) *Scheduler[S] {
	opts.Tunables = opts.Tunables.withDefaults()
	runCtx, cancel := context.WithCancel(ctx)
	s := &Scheduler[S]{
		prov: prov, opts: opts, ctx: runCtx, cancel: cancel,
		body:  NewBodyLimits(opts.Tunables),
		chunk: NewAdaptive(opts.Tunables.ChunkBytes, MinChunkBytes, opts.Tunables.ChunkBytes),
	}
	s.unwatch = prov.Watch(s.sessionsChanged)
	s.stopCtx = context.AfterFunc(runCtx, func() { s.drop() })
	if opts.Group != nil {
		opts.Group.join(s, s.groupEnded)
	}
	return s
}

// Tunables are the provider's batch sizes.
func (s *Scheduler[S]) Tunables() Tunables { return s.opts.Tunables }

// BodyLimits are the account's adaptive count and byte budget of one body
// task. Call Observe with each batch's duration.
func (s *Scheduler[S]) BodyLimits() *BodyLimits { return s.body }

// ChunkBudget is the account's adaptive partial-fetch size for a message
// larger than the body budget.
func (s *Scheduler[S]) ChunkBudget() *Adaptive { return s.chunk }

// Limit is the provider's cap now.
func (s *Scheduler[S]) Limit() int { return s.prov.Limit() }

// Enqueue adds t at the tail of its lane. A duplicate of a queued task joins
// it (see Task.Key) and lifts it to the higher of the two classes. After
// Stop, t's Done gets ErrStopped.
func (s *Scheduler[S]) Enqueue(t Task[S]) { s.EnqueueJoined(t) }

// EnqueueJoined is Enqueue, and reports whether t joined a queued duplicate.
// A joined t's Run never runs: the queued task's Run does the work, and t's
// Done runs when that task ends. A duplicate of a running task is queued,
// not joined, except that a width entry (Task.Width) is joined while it
// runs too.
func (s *Scheduler[S]) EnqueueJoined(t Task[S]) bool {
	s.mu.Lock()
	if s.stoppingLocked() {
		s.mu.Unlock()
		callDone(t.Done, ErrStopped)
		return false
	}
	dropped, joined := s.enqueueLocked(&entry[S]{t: t, class: classOf(t), dones: doneList(t.Done)})
	s.dispatchLocked()
	s.logLocked("enqueue", t)
	s.mu.Unlock()
	for _, e := range dropped {
		e.done(ErrDropped)
	}
	return joined
}

func classOf[S any](t Task[S]) Class {
	if t.Class == ClassAuto {
		return DefaultClass(t.Kind)
	}
	return t.Class
}

func doneList(fn func(error)) []func(error) {
	if fn == nil {
		return nil
	}
	return []func(error){fn}
}

// enqueueLocked appends e, or merges it into a queued duplicate (joined),
// which for a width entry may be running. It returns on-demand entries
// dropped to keep the fast lane under FastCap.
func (s *Scheduler[S]) enqueueLocked(e *entry[S]) (dropped []*entry[S], joined bool) {
	lane := s.laneOf(e)
	if q := s.duplicateLocked(e); q != nil {
		// A manual Sync joining a queued list lifts it to its class.
		q.class = max(q.class, e.class)
		// A barrier joining a plain task keeps the merged one a barrier.
		q.t.Barrier = q.t.Barrier || e.t.Barrier
		q.dones = append(q.dones, e.dones...)
		// The joiner may have added work: a held entry may claim it, and
		// running copies may have read the store before it was added.
		if q.state == stateHeld || q.state == stateEnded {
			q.state = stateOpen
		}
		if q.running > 0 {
			q.rejoin = true
		}
		return nil, true
	}
	*lane = append(*lane, e)
	for len(s.fast) > FastCap {
		i := -1
		for j, q := range s.fast {
			if q.t.Kind.droppable() {
				i = j
				break
			}
		}
		if i < 0 {
			break
		}
		dropped = append(dropped, s.fast[i])
		s.fast = append(s.fast[:i], s.fast[i+1:]...)
	}
	return dropped, false
}

// laneOf is the lane e queues in.
func (s *Scheduler[S]) laneOf(e *entry[S]) *[]*entry[S] {
	if e.t.Kind.Fast() {
		return &s.fast
	}
	return &s.normal
}

// duplicateLocked is the queued entry e would join: the same Key, not a
// failed width entry that is draining (that one ends with its own error;
// e queues behind it).
func (s *Scheduler[S]) duplicateLocked(e *entry[S]) *entry[S] {
	if e.t.Key == "" {
		return nil
	}
	for _, q := range *s.laneOf(e) {
		if q != e && q.t.Key == e.t.Key && q.state != stateFailed {
			return q
		}
	}
	return nil
}

// EnablePush starts watching for changes with cfg. A later call does
// nothing: one account has one watcher.
func (s *Scheduler[S]) EnablePush(cfg Push[S]) {
	if cfg.CheckEvery <= 0 {
		cfg.CheckEvery = 30 * time.Second
	}
	if cfg.RetryWait == nil {
		cfg.RetryWait = func(error) time.Duration { return 15 * time.Second }
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idle != nil || s.stopping {
		return
	}
	s.idle = &idleState[S]{cfg: cfg, lastNewMail: time.Now()}
	if cfg.Park == nil && cfg.Listen != nil {
		ctx, stop := context.WithCancel(s.ctx)
		s.idle.listenStop = stop
		s.wg.Go(func() { s.listen(ctx) })
		return
	}
	s.dispatchLocked()
}

// listen runs a Listen watcher outside the pool until stop, with a wait
// after every failure.
func (s *Scheduler[S]) listen(ctx context.Context) {
	cfg := s.idle.cfg
	for ctx.Err() == nil {
		err := cfg.Listen(ctx, func() { s.setHealth(true) }, func() {
			if cfg.NewMail != nil {
				s.Enqueue(cfg.NewMail())
			}
		})
		s.setHealth(false)
		if ctx.Err() != nil || errors.Is(err, ErrIdleUnsupported) {
			return
		}
		wait := cfg.RetryWait(err)
		if s.opts.Log != nil {
			s.opts.Log.Debug("push failed, retrying later", "account", s.opts.AccountID, "wait", wait, "err", err)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// PushHealthy reports whether the watcher is up now.
func (s *Scheduler[S]) PushHealthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idle != nil && s.idle.healthy
}

func (s *Scheduler[S]) setHealth(h bool) {
	s.mu.Lock()
	if s.idle == nil || s.idle.healthy == h {
		s.mu.Unlock()
		return
	}
	s.idle.healthy = h
	fn := s.idle.cfg.OnHealth
	s.mu.Unlock()
	if fn != nil {
		fn(h)
	}
}

// Stopped reports whether Stop, Abort or the parent context ended the scheduler.
func (s *Scheduler[S]) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stoppingLocked()
}

// Stats returns the queue lengths and worker count.
func (s *Scheduler[S]) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		Fast: len(s.fast), Normal: len(s.normal), Running: len(s.active),
		// A worker given the IDLE role may still be dialing: Idle waits
		// for its session, as documented.
		Workers: s.workers, Idle: s.idle != nil && s.idle.worker != nil && s.idle.worker.held,
		PushHealthy: s.idle != nil && s.idle.healthy,
	}
}

// Stop drops the queued tasks (their Done gets ErrStopped) and waits until
// the running ones end at their own end, or until ctx is done. Nothing is
// cancelled: a task still running after ctx ends finishes in the background
// and then returns its session to the provider. Stop closes the provider
// (Provider.Close) once it stops waiting.
func (s *Scheduler[S]) Stop(ctx context.Context) error {
	s.drop()
	if s.opts.Group != nil {
		s.opts.Group.leave(s)
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	var err error
	select {
	case <-done:
	case <-ctx.Done():
		err = ctx.Err()
	}
	s.unwatch()
	s.stopCtx()
	s.mu.Lock()
	if s.fullTimer != nil {
		s.fullTimer.Stop()
		s.fullTimer = nil
	}
	s.mu.Unlock()
	s.prov.Close()
	return err
}

// Abort is the hard stop for account removal: it cancels the context
// running tasks see, then stops, waiting for them until ctx is done. The
// caller should also close the account's connections so a blocked command
// returns.
func (s *Scheduler[S]) Abort(ctx context.Context) error {
	s.cancel()
	return s.Stop(ctx)
}

// drop marks the scheduler stopping and ends every queued task. A width
// entry with copies still running is left to its last copy, which ends it.
func (s *Scheduler[S]) drop() {
	s.mu.Lock()
	s.stopping = true
	var queued []*entry[S]
	for _, e := range slices.Concat(s.fast, s.normal) {
		if e.running == 0 {
			queued = append(queued, e)
		}
	}
	s.fast, s.normal = nil, nil
	s.wakeIdleLocked()
	if s.idle != nil && s.idle.listenStop != nil {
		s.idle.listenStop()
	}
	s.mu.Unlock()
	for _, e := range queued {
		e.done(ErrStopped)
	}
}

func (s *Scheduler[S]) dispatch() {
	s.mu.Lock()
	s.dispatchLocked()
	s.mu.Unlock()
}

// groupEnded is the Group's wake: a task of folder ended in another
// scheduler of the account. When it freed ids (freed), the held entries of
// that folder may find them now, so they try again, as after an end here
// (finish). An end that freed nothing (ErrSaturated, or a task that never
// ran) opens only the held entries nothing of their folder holds up any
// more (relaxLocked): reopening the others would only saturate them again,
// and their saturated end would wake this scheduler's back, without end.
// Held entries of other folders stay held (their folder's running task did
// not end). Either way it dispatches: the end may have lifted an exclusion.
func (s *Scheduler[S]) groupEnded(folder int64, freed bool) {
	s.mu.Lock()
	if freed {
		s.unholdLocked(folder)
	}
	s.relaxLocked(folder)
	s.dispatchLocked()
	s.mu.Unlock()
}

// sessionsChanged is the provider's Watch callback: the cap changed or a
// session came back, so a refused Get may succeed now.
func (s *Scheduler[S]) sessionsChanged() {
	s.mu.Lock()
	s.changes++
	s.full = false
	s.dispatchLocked()
	s.mu.Unlock()
}

// noteFullLocked records a Get refused with ErrSessionsFull and arms the
// backoff timer, which retries if no notification comes first. seen is
// s.changes read before that Get: when a notification came in between,
// nothing is recorded and the next dispatch asks again.
func (s *Scheduler[S]) noteFullLocked(seen uint64) {
	if s.changes != seen {
		return
	}
	s.full = true
	if s.fullTimer != nil {
		return
	}
	wait := max(s.fullWait, minFullWait)
	s.fullWait = min(wait*2, maxFullWait)
	var t *time.Timer
	t = time.AfterFunc(wait, func() {
		s.mu.Lock()
		if s.fullTimer == t { // not stopped and replaced meanwhile
			s.fullTimer = nil
			s.full = false
			s.dispatchLocked()
		}
		s.mu.Unlock()
	})
	s.fullTimer = t
}

// gotSession records that w holds a session and ends the backoff after a
// successful Get: the provider has room again, so workers may start at once
// and a later refusal waits the shortest backoff, not what is left of a
// long timer.
func (s *Scheduler[S]) gotSession(w *worker) {
	s.mu.Lock()
	w.held = true
	s.holding++
	s.fullWait = 0
	if s.fullTimer != nil {
		s.fullTimer.Stop()
		s.fullTimer = nil
	}
	if s.full {
		s.full = false
		s.dispatchLocked()
	}
	s.mu.Unlock()
}

// dropSession records that w gave its session back. It runs after Put
// returned, so a Get that read gaveBack before it may have missed that
// session (ownSessionLocked), and one that read it after finds the session
// back with the provider.
func (s *Scheduler[S]) dropSession(w *worker) {
	s.mu.Lock()
	if w.held {
		w.held = false
		s.holding--
		s.gaveBack++
	}
	s.mu.Unlock()
}

// ownSessionLocked reports whether a refused Get of worker w, which read
// gaveBack as gave before it, may have been refused for a session of this
// scheduler that carries the work instead: one another worker holds for
// normal tasks (it picks a queued entry when its task ends; the IDLE
// watcher's at N>=2 does not), or one given back meanwhile (the next Get
// reuses it).
func (s *Scheduler[S]) ownSessionLocked(w *worker, gave uint64) bool {
	if s.gaveBack != gave {
		return true
	}
	n := s.holding
	if w.held {
		n--
	}
	if iw := s.idleWorkerLocked(); iw != nil && iw != w && iw.held && s.prov.Limit() >= 2 {
		n--
	}
	return n > 0
}

func (s *Scheduler[S]) idleWorkerLocked() *worker {
	if s.idle == nil {
		return nil
	}
	return s.idle.worker
}

// countsSeen returns s.changes and s.gaveBack, read before a Get.
func (s *Scheduler[S]) countsSeen() (changes, gaveBack uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes, s.gaveBack
}

// dispatchLocked starts workers on free sessions and wakes IDLE when it has
// work. A free session goes first, IDLE's second.
func (s *Scheduler[S]) dispatchLocked() {
	if s.stoppingLocked() {
		s.wakeIdleLocked()
		return
	}
	n := s.prov.Limit()
	if s.full {
		// Wait for a notification or the backoff before asking again;
		// IDLE may still be woken below.
		n = min(n, s.workers)
	}
	if s.idleWantedLocked() && s.idle.worker == nil && s.workers < n {
		s.spawnLocked(nil, true)
	}
	for s.workers < n {
		e := s.pickLocked(n, true)
		if e == nil {
			break
		}
		s.spawnLocked(e, false)
	}
	if s.idle != nil && s.idle.wake != nil && s.idleShouldWakeLocked(n) {
		s.wakeIdleLocked()
	}
}

func (s *Scheduler[S]) spawnLocked(first *entry[S], idle bool) {
	w := &worker{idle: idle}
	if idle {
		s.idle.worker = w
	}
	s.workers++
	s.wg.Add(1)
	go s.work(w, first)
}

// idleWantedLocked reports whether a Park watcher should hold a worker now.
func (s *Scheduler[S]) idleWantedLocked() bool {
	return s.idleOnLocked() && !time.Now().Before(s.idle.retryAt)
}

// idleOnLocked reports whether a Park watcher is on: wanted now, dialing,
// or waiting to retry after a failure. Its slot is reserved for as long
// (normalSlotsLocked).
func (s *Scheduler[S]) idleOnLocked() bool {
	return s.idle != nil && s.idle.cfg.Park != nil && !s.idle.disabled
}

// normalSlotsLocked is how many sessions of n may run normal-lane tasks at
// once: n less the one a Park watcher keeps at n>=2. The slot stays
// reserved whether the watcher holds its session, dials it or waits to
// retry, so a retry is never refused for normal work of this scheduler and
// push is never locked out by a long backlog. The free slot serves the fast
// lane meanwhile. At n=1 the watcher shares the only session.
func (s *Scheduler[S]) normalSlotsLocked(n int) int {
	if n >= 2 && s.idleOnLocked() {
		return n - 1
	}
	return n
}

// normalRunningLocked counts the normal-lane tasks running (each copy of a
// width entry once).
func (s *Scheduler[S]) normalRunningLocked() int {
	k := 0
	for _, a := range s.active {
		if !a.t.Kind.Fast() {
			k++
		}
	}
	return k
}

func (s *Scheduler[S]) idleShouldWakeLocked(n int) bool {
	if s.stopping || s.workers > n || !s.idleWantedLocked() {
		return true
	}
	if s.firstFastLocked() != nil {
		return true
	}
	return n == 1 && s.nextNormalLocked() != nil
}

func (s *Scheduler[S]) wakeIdleLocked() {
	if s.idle != nil && s.idle.wake != nil {
		close(s.idle.wake)
		s.idle.wake = nil
	}
}

// pick decides the next task for a worker, removes it from its lane and
// marks it running. The fast lane goes first, except that after n fast tasks
// in a row one eligible normal task goes next, so the normal lane never
// starves. n is the effective pool size. The normal lane's order is the
// Policy (nextNormalLocked). allowNormal is false for the IDLE worker at
// N>=2, which serves only the fast lane; no worker takes a normal task while
// the normal-lane tasks fill their slots (normalSlotsLocked).
//
// With a Group, eligibility's look at the other schedulers is advisory; the
// chosen task starts only once the Group reserved it. When another scheduler
// got there first, the task stays queued and the next eligible one is tried;
// the other scheduler's end wakes this one.
func (s *Scheduler[S]) pickLocked(n int, allowNormal bool) *entry[S] {
	allowNormal = allowNormal && s.normalRunningLocked() < s.normalSlotsLocked(n)
	var skip map[*entry[S]]bool
	for {
		f := s.firstFastSkipLocked(skip)
		var nl *entry[S]
		if allowNormal {
			nl = s.nextNormalSkipLocked(skip)
		}
		var e *entry[S]
		fast := false
		switch {
		case f != nil && (nl == nil || s.streak < n):
			e, fast = f, true
		case nl != nil:
			e = nl
		default:
			return nil
		}
		if s.opts.Group != nil {
			sh := shapeOf(e.t)
			if e.running > 0 {
				sh = e.shape // the copies running record the same shape
			}
			if !s.opts.Group.reserve(s, sh) {
				if skip == nil {
					skip = make(map[*entry[S]]bool)
				}
				skip[e] = true
				continue
			}
			e.shape = sh
		}
		switch {
		case fast:
			s.streak++
			s.fast = remove(s.fast, e)
		case e.wide():
			// One more copy: the entry stays queued, at the tail of its
			// class, so the other folders take their turn.
			s.streak = 0
			e.running++
			e.rejoin = false
			s.normal = append(remove(s.normal, e), e)
		default:
			s.streak = 0
			s.normal = remove(s.normal, e)
		}
		s.active = append(s.active, e)
		return e
	}
}

// capLocked is how many copies of the width entry e may run now: its
// Width at the provider's current Limit, at least 1, at most the normal
// sessions (normalSlotsLocked: N less the one a Park watcher keeps at N>=2,
// so IDLE's session is never counted, also while IDLE retries) and
// Tunables.MaxBodyWidth.
func (s *Scheduler[S]) capLocked(e *entry[S]) int {
	n := s.prov.Limit()
	w := min(e.t.Width(n), s.normalSlotsLocked(n))
	if m := s.opts.Tunables.MaxBodyWidth; m > 0 {
		w = min(w, m)
	}
	return max(w, 1)
}

// endLocked takes e (one copy of it) out of the running tasks and returns
// how to tell the other schedulers of the Group; call those on no lock.
// freed is false for an end that released no ids: see Group.end.
func (s *Scheduler[S]) endLocked(e *entry[S], freed bool) []func() {
	s.active = remove(s.active, e)
	if s.opts.Group == nil {
		return nil
	}
	return s.opts.Group.end(s, e.shape, freed)
}

func (s *Scheduler[S]) firstFastLocked() *entry[S] { return s.firstFastSkipLocked(nil) }

func (s *Scheduler[S]) firstFastSkipLocked(skip map[*entry[S]]bool) *entry[S] {
	for _, e := range s.fast {
		if !skip[e] && s.eligibleLocked(e) {
			return e
		}
	}
	return nil
}

// nextNormalLocked is the normal lane's ordering policy.
func (s *Scheduler[S]) nextNormalLocked() *entry[S] { return s.nextNormalSkipLocked(nil) }

// nextNormalSkipLocked is nextNormalLocked passing over the tasks in skip.
func (s *Scheduler[S]) nextNormalSkipLocked(skip map[*entry[S]]bool) *entry[S] {
	var best *entry[S]
	for _, e := range s.normal {
		if skip[e] || !s.eligibleLocked(e) {
			continue
		}
		if s.opts.Policy == PolicyFIFO {
			return e
		}
		if best == nil || e.class > best.class {
			best = e
		}
	}
	return best
}

// eligibleLocked applies the state, width, barrier, key and per-folder
// rules: a held entry waits, a width entry runs up to its cap of copies,
// bodies of a folder wait for its listing, two lists of one folder never run
// at once, and a full check waits for the folder's list and body work (every
// copy of it).
//
// Barrier: a queued or running barrier holds every other task. A barrier
// itself starts only once no other task runs (a parked IDLE runs none), and
// of several queued barriers only the first in queue order (fast lane, then
// normal lane) may start, so two barriers never wait on each other. A
// barrier waits for no queued task, whatever its FolderID: the queued ones
// wait for it, so a per-folder rule against them would deadlock.
//
// The Group check here is advisory (pickLocked reserves); it keeps IDLE
// from waking for work another scheduler holds.
func (s *Scheduler[S]) eligibleLocked(e *entry[S]) bool {
	if e.state != stateOpen {
		return false
	}
	if e.wide() && e.running >= s.capLocked(e) {
		return false
	}
	if s.opts.Group != nil && s.opts.Group.blocks(s, shapeOf(e.t)) {
		return false
	}
	if e.t.Barrier {
		return len(s.active) == 0 && e == s.firstBarrierLocked()
	}
	for _, a := range s.active {
		if a == e {
			continue // its own running copies
		}
		if a.t.Barrier {
			return false
		}
		if e.t.Key != "" && a.t.Key == e.t.Key {
			return false
		}
		if e.t.FolderID == 0 || a.t.FolderID != e.t.FolderID {
			continue
		}
		switch {
		case e.t.Kind == KindBodies && a.t.Kind.lists():
			return false
		case e.t.Kind.lists() && a.t.Kind.lists():
			return false
		case e.t.Kind == KindFullCheck && a.t.Kind == KindBodies:
			return false
		}
	}
	for _, lane := range [][]*entry[S]{s.fast, s.normal} {
		for _, q := range lane {
			if q == e {
				continue
			}
			if q.t.Barrier {
				return false
			}
			if e.t.FolderID == 0 || q.t.FolderID != e.t.FolderID {
				continue
			}
			switch {
			case e.t.Kind == KindBodies && (q.t.Kind == KindListStubs || q.t.Kind == KindNewMail || q.t.Kind == KindLoadOlder):
				return false
			case e.t.Kind == KindFullCheck && (q.t.Kind == KindListStubs || q.t.Kind == KindBodies):
				return false
			}
		}
	}
	return true
}

// firstBarrierLocked is the queued barrier that goes first.
func (s *Scheduler[S]) firstBarrierLocked() *entry[S] {
	for _, lane := range [][]*entry[S]{s.fast, s.normal} {
		for _, q := range lane {
			if q.t.Barrier {
				return q
			}
		}
	}
	return nil
}

func remove[S any](lane []*entry[S], e *entry[S]) []*entry[S] {
	for i, q := range lane {
		if q == e {
			return append(lane[:i], lane[i+1:]...)
		}
	}
	return lane
}

type action int

const (
	actRun action = iota
	actPark
	actExit
)

// work is one worker: one session, one task at a time.
func (s *Scheduler[S]) work(w *worker, e *entry[S]) {
	defer s.wg.Done()
	var sess S
	have := false
	release := func(broken bool) {
		if have {
			s.prov.Put(sess, broken)
			s.dropSession(w)
			have = false
			// A good session of this scheduler is free: a refused Get may
			// work now. A broken one still holds its slot while it closes;
			// the provider's Watch tells when it is gone.
			if !broken {
				s.sessionsChanged()
			}
		}
	}
	var seen, gave uint64 // provider notifications and given-back sessions before the last Get
	get := func() error {
		if have {
			return nil
		}
		seen, gave = s.countsSeen()
		c, err := s.prov.Get(s.ctx)
		if err != nil {
			return err
		}
		sess, have = c, true
		s.gotSession(w)
		return nil
	}
	for {
		if e != nil {
			if err := get(); err != nil {
				s.noSession(w, e, err, seen, gave)
				s.exit(w)
				return
			}
			if !s.yieldOverCap(e) {
				err := s.run(e, sess)
				if err != nil && s.broken(err) {
					release(true)
				}
				s.finish(e, err)
			}
		}
		next, act, wake := s.next(w)
		switch act {
		case actRun:
			e = next
		case actPark:
			e = nil
			if err := get(); err != nil {
				if full(err) {
					// The cap shrank: no IDLE failure, retry after the backoff.
					s.mu.Lock()
					s.noteFullLocked(seen)
					s.mu.Unlock()
				} else {
					s.setHealth(false)
					s.idleFailed(err)
				}
				s.exit(w)
				return
			}
			s.setHealth(true)
			changed, err := s.idle.cfg.Park(s.ctx, sess, wake)
			s.unpark(wake)
			if err != nil {
				s.setHealth(false)
				release(true)
				if s.ctx.Err() == nil {
					s.idleFailed(err)
				}
				s.exit(w)
				return
			}
			if changed && s.idle.cfg.NewMail != nil {
				s.queueOwn(s.idle.cfg.NewMail())
			}
		default:
			release(false)
			s.exit(w)
			return
		}
	}
}

// full reports a provider refusing a session because the cap shrank after
// the worker started.
func full(err error) bool {
	var f interface{ SessionsFull() bool }
	return errors.As(err, &f) && f.SessionsFull()
}

// noSession handles a Get of worker w that failed for e, which never ran.
// A refusal for the cap (ErrSessionsFull) puts it back (restore). So does
// any failed Get of a copy of a width entry that something else carries:
// its other copies running, or a session of this scheduler the refusal may
// have been for (ownSessionLocked; gave is gaveBack read before the Get),
// such as the session of the list task that queued the entry, still held
// or given back while the dial was in flight. The provider counted the
// failed dial (a server refusing one connection too many), and no new
// worker starts until it notifies or the backoff ends. Otherwise the task
// ends with the error.
func (s *Scheduler[S]) noSession(w *worker, e *entry[S], err error, seen, gave uint64) {
	if full(err) {
		s.restore(e, seen)
		return
	}
	s.mu.Lock()
	if !e.wide() || e.running < 2 && !s.ownSessionLocked(w, gave) {
		s.mu.Unlock()
		s.finish(e, err)
		return
	}
	// The failed dial notified already: back off all the same.
	s.restoreLocked(e, s.changes)
}

// restore puts a task that never ran back where it was, at the head of its
// lane, after the provider refused a session (ErrSessionsFull). A duplicate
// queued meanwhile (its key was running, so it did not join) is merged
// into it: one entry per key. A width entry gives the copy back. No new
// worker starts until the provider notifies or a short backoff ends.
func (s *Scheduler[S]) restore(e *entry[S], seen uint64) {
	s.mu.Lock()
	s.restoreLocked(e, seen)
}

// restoreLocked is restore with s.mu held; it unlocks it.
func (s *Scheduler[S]) restoreLocked(e *entry[S], seen uint64) {
	s.noteFullLocked(seen)
	s.putBackLocked(e)
}

// yieldOverCap gives back a copy of the width entry e that got its session
// after the cap shrank below the copies running (a refused login halved N
// while it dialed), before its unit starts: the copies already running
// carry the work. Its worker keeps the session for the next task or the
// IDLE role, or gives it back as it leaves above the cap. It reports
// whether e was given back.
func (s *Scheduler[S]) yieldOverCap(e *entry[S]) bool {
	s.mu.Lock()
	if !e.wide() || e.running <= s.capLocked(e) {
		s.mu.Unlock()
		return false
	}
	s.putBackLocked(e)
	return true
}

// putBackLocked puts e, which never ran, back where it was (see restore).
// It unlocks s.mu.
func (s *Scheduler[S]) putBackLocked(e *entry[S]) {
	wake := s.endLocked(e, false) // it never ran: it freed nothing
	s.relaxLocked(e.t.FolderID)
	stopped := s.stoppingLocked()
	done, doneErr := false, error(nil)
	switch {
	case e.wide():
		e.running--
		if done, doneErr = s.settleLocked(e); !done && !stopped {
			s.normal = append([]*entry[S]{e}, remove(s.normal, e)...)
		}
	case stopped:
		done, doneErr = true, ErrStopped
	default:
		s.requeueHeadLocked(e)
	}
	s.mu.Unlock()
	for _, fn := range wake {
		fn()
	}
	if done {
		e.done(doneErr)
	}
}

// requeueHeadLocked puts e at the head of its lane, or merges it into a
// duplicate queued there: the duplicate's Run, which is the newer, does the
// work, at e's place, and both Done run with it.
func (s *Scheduler[S]) requeueHeadLocked(e *entry[S]) {
	lane := s.laneOf(e)
	at := e
	if q := s.duplicateLocked(e); q != nil {
		q.class = max(q.class, e.class)
		q.t.Barrier = q.t.Barrier || e.t.Barrier
		q.dones = slices.Concat(e.dones, q.dones)
		at = q
	}
	*lane = append([]*entry[S]{at}, remove(*lane, at)...)
}

// stoppingLocked is true after Stop and once the parent context ended.
func (s *Scheduler[S]) stoppingLocked() bool {
	return s.stopping || s.ctx.Err() != nil
}

// queueOwn queues t without starting a worker for it: the calling worker
// takes it next, on the session it already holds (IDLE's new-mail check).
func (s *Scheduler[S]) queueOwn(t Task[S]) {
	s.mu.Lock()
	if s.stoppingLocked() {
		s.mu.Unlock()
		callDone(t.Done, ErrStopped)
		return
	}
	dropped, _ := s.enqueueLocked(&entry[S]{t: t, class: classOf(t), dones: doneList(t.Done)})
	s.logLocked("push", t)
	s.mu.Unlock()
	for _, e := range dropped {
		e.done(ErrDropped)
	}
}

func (s *Scheduler[S]) run(e *entry[S], sess S) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("syncsched: %s task panicked: %v\n%s", e.t.Kind, r, debug.Stack())
		}
	}()
	if e.t.Run == nil {
		return nil
	}
	return e.t.Run(s.ctx, sess)
}

// broken reports whether the session a task ended with must be closed. An
// ErrAgain unit keeps it unless the task said otherwise with ErrAgainBroken.
func (s *Scheduler[S]) broken(err error) bool {
	if errors.Is(err, ErrAgain) {
		return errors.Is(err, ErrAgainBroken)
	}
	if s.ctx.Err() != nil {
		return true
	}
	if s.opts.Broken == nil {
		return true
	}
	return s.opts.Broken(err)
}

// finish ends a running task (one copy of a width entry): ErrAgain sends it
// back to the tail of its lane, ErrSaturated holds it there while another
// task of its folder runs. Any other end of a task frees the held tasks of
// its folder, since it may have released what they wait for; a saturated
// end frees those that no running task of the folder holds up any more.
func (s *Scheduler[S]) finish(e *entry[S], err error) {
	s.mu.Lock()
	wake := s.endLocked(e, !errors.Is(err, ErrSaturated))
	var dropped []*entry[S]
	done, doneErr := false, err
	if e.wide() {
		s.finishCopyLocked(e, err)
		done, doneErr = s.settleLocked(e)
	} else {
		again := errors.Is(err, ErrAgain)
		switch {
		case again && !s.stoppingLocked():
			e.state = stateOpen
			if errors.Is(err, ErrSaturated) && s.heldUpLocked(e) {
				e.state = stateHeld
			}
			dropped, _ = s.enqueueLocked(e)
		case again:
			done, doneErr = true, ErrStopped
		default:
			done = true
		}
	}
	if !errors.Is(err, ErrSaturated) {
		s.unholdLocked(e.t.FolderID)
	}
	// A saturated end frees nothing, but it may have been the folder's
	// last running task: what still waits must not wait for nothing.
	s.relaxLocked(e.t.FolderID)
	s.dispatchLocked()
	s.mu.Unlock()
	for _, fn := range wake {
		fn()
	}
	for _, d := range dropped {
		d.done(ErrDropped)
	}
	if done {
		e.done(doneErr)
	}
}

// finishCopyLocked records the result of one copy of the width entry e.
func (s *Scheduler[S]) finishCopyLocked(e *entry[S], err error) {
	e.running--
	switch {
	case e.state == stateFailed:
	case errors.Is(err, ErrSaturated):
		if e.state == stateOpen && s.heldUpLocked(e) {
			e.state = stateHeld
		}
	case errors.Is(err, ErrAgain):
		e.state = stateOpen
	case err == nil && e.rejoin:
		e.state = stateOpen // a joiner came after this copy read the store
	case err == nil:
		e.state = stateEnded
	default:
		e.state, e.err = stateFailed, err
	}
}

// settleLocked ends the width entry e once no copy of it runs and it is
// ended or failed, or the scheduler stopped: it leaves its lane, and the
// caller runs Done with the result (once).
func (s *Scheduler[S]) settleLocked(e *entry[S]) (bool, error) {
	if e.running > 0 {
		return false, nil
	}
	var err error
	switch {
	case e.state == stateFailed:
		err = e.err
	case e.state == stateEnded:
	case s.stoppingLocked():
		err = ErrStopped
	default:
		return false, nil
	}
	s.normal = remove(s.normal, e)
	return true, err
}

// heldUpLocked reports whether a task of e's folder other than this copy
// of e runs now, here or in another scheduler of the Group: what a
// saturated unit found claimed is then still held, and that task's end will
// free it (unholdLocked, groupEnded). With none running the claims were
// released before the unit ended, so it must not wait.
func (s *Scheduler[S]) heldUpLocked(e *entry[S]) bool {
	if e.t.FolderID == 0 {
		return false
	}
	for _, a := range s.active {
		if a.t.FolderID == e.t.FolderID {
			return true
		}
	}
	return s.opts.Group != nil && s.opts.Group.runsFolder(s, e.t.FolderID)
}

// relaxLocked keeps the invariant held ⇒ heldUpLocked: a held entry of
// folder with no task of its folder running here or in the Group any more
// (the last one ended, saturated as well, or never ran) opens, since no
// end would reopen it. Call it after a task of folder left s.active.
func (s *Scheduler[S]) relaxLocked(folder int64) {
	if folder == 0 {
		return
	}
	for _, lane := range [][]*entry[S]{s.fast, s.normal} {
		for _, q := range lane {
			if q.t.FolderID == folder && q.state == stateHeld && !s.heldUpLocked(q) {
				q.state = stateOpen
			}
		}
	}
}

// unholdLocked opens the held entries of folder: a task of it ended.
func (s *Scheduler[S]) unholdLocked(folder int64) {
	if folder == 0 {
		return
	}
	for _, lane := range [][]*entry[S]{s.fast, s.normal} {
		for _, q := range lane {
			if q.t.FolderID == folder && q.state == stateHeld {
				q.state = stateOpen
			}
		}
	}
}

func (e *entry[S]) done(err error) {
	for _, fn := range e.dones {
		callDone(fn, err)
	}
}

func callDone(fn func(error), err error) {
	if fn != nil {
		fn(err)
	}
}

// next decides what a worker does after a task. A worker above the cap
// leaves; a worker takes the IDLE role when IDLE has none.
func (s *Scheduler[S]) next(w *worker) (*entry[S], action, <-chan struct{}) {
	if nm := s.dueNewMail(w); nm != nil {
		// Built on no lock: NewMail may read the store.
		s.queueOwn(nm())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stoppingLocked() {
		return nil, actExit, nil
	}
	n := s.prov.Limit()
	if s.workers > n {
		return nil, actExit, nil
	}
	s.roleLocked(w)
	if e := s.pickLocked(n, !w.idle || n == 1); e != nil {
		return e, actRun, nil
	}
	if w.idle {
		wake := make(chan struct{})
		s.idle.wake = wake
		s.idle.lastNewMail = time.Now()
		return nil, actPark, wake
	}
	return nil, actExit, nil
}

// roleLocked gives w the IDLE role when IDLE wants a worker and has none,
// and takes it back when IDLE no longer wants one.
func (s *Scheduler[S]) roleLocked(w *worker) {
	if !w.idle && s.idleWantedLocked() && s.idle.worker == nil {
		w.idle = true
		s.idle.worker = w
	}
	if w.idle && !s.idleWantedLocked() {
		w.idle = false
		if s.idle != nil && s.idle.worker == w {
			s.idle.worker = nil
		}
	}
}

// dueNewMail returns NewMail when w is the only (IDLE) worker at N=1, the
// normal lane has work and the last new-mail check is older than
// CheckEvery. It marks the check done; the caller builds and queues the task
// after unlocking.
func (s *Scheduler[S]) dueNewMail(w *worker) func() Task[S] {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stoppingLocked() {
		return nil
	}
	n := s.prov.Limit()
	if s.workers > n {
		return nil
	}
	s.roleLocked(w)
	if !w.idle || n != 1 || s.nextNormalLocked() == nil || time.Since(s.idle.lastNewMail) < s.idle.cfg.CheckEvery {
		return nil
	}
	s.idle.lastNewMail = time.Now()
	return s.idle.cfg.NewMail
}

func (s *Scheduler[S]) unpark(wake <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idle.wake != nil && (<-chan struct{})(s.idle.wake) == wake {
		s.idle.wake = nil
	}
}

// idleFailed backs IDLE off after an error. ErrIdleUnsupported turns it off.
func (s *Scheduler[S]) idleFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idle == nil {
		return
	}
	if errors.Is(err, ErrIdleUnsupported) {
		s.idle.disabled = true
		return
	}
	wait := s.idle.cfg.RetryWait(err)
	s.idle.retryAt = time.Now().Add(wait)
	time.AfterFunc(wait, s.dispatch)
	if s.opts.Log != nil {
		s.opts.Log.Debug("idle failed, retrying later", "account", s.opts.AccountID, "wait", wait, "err", err)
	}
}

// exit removes a worker whose session is already back in the pool.
func (s *Scheduler[S]) exit(w *worker) {
	s.mu.Lock()
	s.workers--
	if s.idle != nil && s.idle.worker == w {
		s.idle.worker = nil
	}
	s.dispatchLocked()
	s.mu.Unlock()
}

func (s *Scheduler[S]) logLocked(what string, t Task[S]) {
	if s.opts.Log == nil {
		return
	}
	s.opts.Log.Debug("sync queue", "account", s.opts.AccountID, "event", what, "kind", t.Kind.String(),
		"folder", t.FolderID, "fast", len(s.fast), "normal", len(s.normal), "running", len(s.active), "workers", s.workers)
}
