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

// Transmitter sends one fully built message. It is implemented by the smtp
// sender, which transmits over SMTP and appends to the Sent folder. Defining it
// here keeps the worker decoupled from the smtp and imap packages.
type Transmitter interface {
	Transmit(ctx context.Context, m Message) error
}

// Worker drains the queue in the background. It is cancellable via context and
// survives transient transmit errors by leaving the message queued for a later
// attempt, so queued mail is never lost to a flaky network.
//
// Each account sends one message at a time, in the order they were queued, but
// accounts send alongside each other: a server that is slow or hangs holds up
// only the mail going out through it.
type Worker struct {
	queue       *Queue
	transmitter Transmitter
	log         *slog.Logger
	interval    time.Duration
	onChange    func()

	mu       sync.Mutex
	busy     map[int64]bool
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
		m, err := w.queue.claimDue(ctx, time.Now().UTC(), w.busyAccounts()...)
		if err != nil {
			return started, err
		}
		if m == nil {
			return started, nil
		}
		w.mu.Lock()
		w.busy[m.AccountID] = true
		w.mu.Unlock()
		w.inflight.Add(1)
		started++
		// the row is now in the sending state; let the ui reflect that, then
		// reflect the final outcome once process records it.
		w.onChange()
		go func(m Message) {
			defer w.inflight.Done()
			w.process(ctx, m)
			w.mu.Lock()
			delete(w.busy, m.AccountID)
			w.mu.Unlock()
			w.onChange()
			select {
			case w.wake <- struct{}{}:
			default:
			}
		}(*m)
	}
}

func (w *Worker) busyAccounts() []int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	ids := make([]int64, 0, len(w.busy))
	for id := range w.busy {
		ids = append(ids, id)
	}
	return ids
}

// process transmits one claimed message and records the outcome.
func (w *Worker) process(ctx context.Context, m Message) {
	err := w.transmitter.Transmit(ctx, m)
	if err == nil {
		if e := w.queue.markSent(ctx, m.ID); e != nil {
			w.log.Error("failed to mark message sent", "id", m.ID, "err", e)
			return
		}
		w.log.Info("message sent", "id", m.ID, "to", m.Recipients)
		return
	}

	retry, e := w.queue.markAttemptFailed(ctx, m, err.Error())
	if e != nil {
		w.log.Error("failed to record send failure", "id", m.ID, "err", e)
		return
	}
	if retry {
		w.log.Warn("send failed, will retry", "id", m.ID, "attempt", m.Attempts+1, "err", err)
	} else {
		w.log.Error("send failed permanently", "id", m.ID, "attempts", m.Attempts+1, "err", err)
	}
}
