package outbox

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// defaultPollInterval is how often the worker checks for newly due messages when
// the queue is idle. A new enqueue is picked up within this window.
const defaultPollInterval = 5 * time.Second

// ErrCancelled is what Commit returns once the user has cancelled the message
// being transmitted.
var ErrCancelled = errors.New("outbox: send cancelled")

// Transmitter sends one fully built message. It is implemented by the smtp
// sender, which transmits over SMTP and appends to the Sent folder. Defining it
// here keeps the worker decoupled from the smtp and imap packages.
//
// A transmitter that can be stopped part way calls Commit right before the
// step that hands the message over for good, and gives up when ctx is
// cancelled before then. Report records its progress in the activity log.
type Transmitter interface {
	Transmit(ctx context.Context, m Message) error
}

// Worker drains the queue in the background. It is cancellable via context and
// survives transient transmit errors by leaving the message queued for a later
// attempt, so queued mail is never lost to a flaky network.
//
// Each account sends one message at a time, in queue order, but accounts send
// alongside each other: a server that is slow or hangs holds up only the mail
// going out through it.
type Worker struct {
	queue       *Queue
	transmitter Transmitter
	log         *slog.Logger
	activity    *ActivityLog
	interval    time.Duration
	onChange    func()

	// mu guards busy and flights, and is held across a claim, so a message is
	// never claimed without its flight being findable.
	mu       sync.Mutex
	busy     map[int64]bool
	flights  map[int64]*flight
	inflight sync.WaitGroup
	// wake is nudged when an account finishes a message, so its next one goes
	// out straight away instead of at the next poll.
	wake chan struct{}
}

// Option configures a Worker.
type Option func(*Worker)

// WithLogger sets the worker's logger.
func WithLogger(log *slog.Logger) Option {
	return func(w *Worker) { w.log = log }
}

// WithActivity sets the in-memory log the worker and its transmitter record
// each step in.
func WithActivity(log *ActivityLog) Option {
	return func(w *Worker) { w.activity = log }
}

// WithOnChange registers a callback fired whenever a message's state changes
// (claimed, sent or failed). The app uses it to push an outbox-changed event to
// the ui after the new state is persisted, so the ui never gets stuck showing a
// stale "sending". The callback must not block.
func WithOnChange(fn func()) Option {
	return func(w *Worker) {
		if fn != nil {
			w.onChange = fn
		}
	}
}

// WithInterval overrides the idle poll interval.
func WithInterval(d time.Duration) Option {
	return func(w *Worker) {
		if d > 0 {
			w.interval = d
		}
	}
}

// NewWorker builds a worker for the queue and transmitter.
func NewWorker(queue *Queue, transmitter Transmitter, opts ...Option) *Worker {
	w := &Worker{
		queue:       queue,
		transmitter: transmitter,
		log:         slog.New(slog.DiscardHandler),
		interval:    defaultPollInterval,
		onChange:    func() {},
		busy:        map[int64]bool{},
		flights:     map[int64]*flight{},
		wake:        make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Run drains the queue until ctx is cancelled, polling for new work in between.
// It first recovers any messages stranded in the sending state by a prior crash.
// It returns ctx.Err() on cancellation.
func (w *Worker) Run(ctx context.Context) error {
	if n, err := w.queue.RequeueStuck(ctx); err != nil {
		w.log.Warn("could not requeue stranded messages", "err", err)
	} else if n > 0 {
		w.log.Info("requeued stranded messages", "count", n)
		w.onChange()
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		if _, err := w.dispatch(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			// a claim error is unusual (db level); log and keep the worker alive.
			w.log.Error("outbox drain error", "err", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

// DrainOnce sends every message that is currently due and waits for them, then
// returns. Useful for tests and for a one-shot flush. A transmit failure on one
// message does not stop the others; it is recorded and the message is requeued
// or failed.
func (w *Worker) DrainOnce(ctx context.Context) error {
	for {
		started, err := w.dispatch(ctx)
		w.inflight.Wait()
		if err != nil {
			return err
		}
		if started == 0 {
			return nil
		}
	}
}

// dispatch claims every due message of an account that is not already sending
// and starts it, returning how many it started. It does not wait for them.
func (w *Worker) dispatch(ctx context.Context) (int, error) {
	started := 0
	for {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		f, err := w.claim(ctx)
		if err != nil {
			return started, err
		}
		if f == nil {
			return started, nil
		}
		w.inflight.Add(1)
		started++
		// the row is now in the sending state; let the ui reflect that, then
		// reflect the final outcome once process records it.
		w.onChange()
		go func() {
			defer w.inflight.Done()
			w.process(ctx, f)
			w.mu.Lock()
			delete(w.busy, f.msg.AccountID)
			delete(w.flights, f.msg.ID)
			w.mu.Unlock()
			close(f.done)
			w.onChange()
			select {
			case w.wake <- struct{}{}:
			default:
			}
		}()
	}
}

// claim claims the next due message of an idle account and registers its
// flight, or returns nil when nothing is due.
func (w *Worker) claim(ctx context.Context) (*flight, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	skip := make([]int64, 0, len(w.busy))
	for id := range w.busy {
		skip = append(skip, id)
	}
	m, err := w.queue.claimDue(ctx, time.Now().UTC(), skip...)
	if err != nil || m == nil {
		return nil, err
	}
	sendCtx, cancel := context.WithCancel(ctx)
	f := &flight{msg: *m, activity: w.activity, cancel: cancel, done: make(chan struct{})}
	f.ctx = context.WithValue(sendCtx, flightKey{}, f)
	w.busy[m.AccountID] = true
	w.flights[m.ID] = f
	return f, nil
}

// process transmits one claimed message and records the outcome.
func (w *Worker) process(ctx context.Context, f *flight) {
	m := f.msg
	err := w.transmitter.Transmit(f.ctx, m)
	f.cancel()
	if err == nil {
		f.outcome = StateSent
		if e := w.queue.markSent(ctx, m.ID); e != nil {
			w.log.Error("failed to mark message sent", "id", m.ID, "err", e)
			return
		}
		w.activity.record(m, StepSent, "", time.Time{})
		w.log.Info("message sent", "id", m.ID, "to", m.Recipients)
		return
	}

	if f.wasAborted() {
		held, e := w.queue.hold(ctx, m.ID, StateSending)
		if e != nil {
			w.log.Error("failed to record a cancelled send", "id", m.ID, "err", e)
			return
		}
		if held {
			f.outcome = CancelledReason
			w.activity.record(m, StepCancelled, "", time.Time{})
		}
		return
	}

	f.outcome = StateFailed
	next, e := w.queue.markAttemptFailed(ctx, m, err.Error())
	if e != nil {
		w.log.Error("failed to record send failure", "id", m.ID, "err", e)
		return
	}
	w.activity.record(m, StepFailed, err.Error(), time.Time{})
	if !next.IsZero() {
		w.activity.record(m, StepRetry, "", next)
		w.log.Warn("send failed, will retry", "id", m.ID, "attempt", m.Attempts+1, "err", err)
	} else {
		w.log.Error("send failed permanently", "id", m.ID, "attempts", m.Attempts+1, "err", err)
	}
}

// Withdrawal is the result of Worker.Withdraw.
type Withdrawal int

const (
	// WithdrawNone means the message was neither waiting nor being
	// transmitted: it was already sent, failed, or is gone.
	WithdrawNone Withdrawal = iota
	// WithdrawHeld means the message is out of the send order, failed with
	// CancelledReason: taken out of the queue, or stopped mid-transmission
	// before the server took it.
	WithdrawHeld
	// WithdrawTooLate means the server had already accepted the message.
	WithdrawTooLate
)

// Withdraw stops message id from going out. A queued message is held at once.
// A message being transmitted is aborted when the transmitter has not yet
// committed it, and Withdraw waits for the transmission to wind down before
// reporting how it ended. A message that went out anyway is WithdrawTooLate.
func (w *Worker) Withdraw(ctx context.Context, id int64) (Withdrawal, error) {
	w.mu.Lock()
	f := w.flights[id]
	if f == nil {
		held, err := w.queue.hold(ctx, id, StateQueued)
		w.mu.Unlock()
		if err != nil || !held {
			return WithdrawNone, err
		}
		if m, err := w.queue.get(ctx, id); err == nil {
			w.activity.record(m, StepCancelled, "", time.Time{})
		}
		w.onChange()
		return WithdrawHeld, nil
	}
	w.mu.Unlock()

	if !f.abort() {
		return WithdrawTooLate, nil
	}
	select {
	case <-f.done:
	case <-ctx.Done():
		return WithdrawNone, ctx.Err()
	}
	switch f.outcome {
	case CancelledReason:
		return WithdrawHeld, nil
	case StateSent:
		return WithdrawTooLate, nil
	default:
		return WithdrawNone, nil
	}
}

// flightKey keys the flight in a transmission's context.
type flightKey struct{}

// flight is one message being transmitted.
type flight struct {
	msg      Message
	activity *ActivityLog
	ctx      context.Context
	cancel   context.CancelFunc
	// done closes once the outcome is recorded and outcome is final: a
	// state, CancelledReason, or empty when recording it failed.
	done    chan struct{}
	outcome string

	mu        sync.Mutex
	committed bool
	aborted   bool
}

// abort cancels the transmission unless it is committed, and reports whether
// it did.
func (f *flight) abort() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.committed {
		return false
	}
	f.aborted = true
	f.cancel()
	return true
}

func (f *flight) wasAborted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aborted
}

// Commit marks the point after which a transmission can no longer be called
// back, and returns an error instead when it has already been cancelled. A
// transmitter calls it right before handing the message over for good (the end
// of the SMTP DATA) and gives up on an error. Outside a worker's transmission
// it only reports ctx's own error.
func Commit(ctx context.Context) error {
	f, _ := ctx.Value(flightKey{}).(*flight)
	if f == nil {
		return ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.aborted {
		return ErrCancelled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.committed = true
	return nil
}

// Report records a transmission step, such as StepConnecting, in the activity
// log of the worker running it. Outside a worker's transmission it does
// nothing.
func Report(ctx context.Context, step string) {
	if f, _ := ctx.Value(flightKey{}).(*flight); f != nil {
		f.activity.record(f.msg, step, "", time.Time{})
	}
}
