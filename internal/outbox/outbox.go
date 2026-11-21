// Package outbox is the durable send queue. Every send is enqueued here and a
// background worker (worker.go) drains it, transmitting each message and
// retrying with capped exponential backoff. This makes sending feel immediate
// when online and resilient when offline: nothing is sent inline, nothing is
// lost, and a transient network failure just defers the next attempt.
//
// Persistence lives in internal/storage; this package owns the queue semantics,
// the state machine and the retry policy.
package outbox

import (
	"context"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Queue states. These are the canonical values; storage persists them verbatim.
const (
	StateQueued  = "queued"
	StateSending = "sending"
	StateSent    = "sent"
	StateFailed  = "failed"
)

// Retry policy.
const (
	// MaxAttempts is how many times a message is tried before it is marked failed.
	MaxAttempts = 5
	// baseBackoff is the delay after the first failure; it doubles each attempt.
	baseBackoff = 30 * time.Second
	// maxBackoff caps the per-attempt delay.
	maxBackoff = 30 * time.Minute
)

// recipientSep joins envelope recipients in the single stored column.
const recipientSep = "\n"

// Message is a queued message in queue terms, with recipients as a slice.
type Message struct {
	ID            int64
	AccountID     int64
	EnvelopeFrom  string
	Recipients    []string
	Raw           []byte
	State         string
	Attempts      int
	LastError     string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	// NotBefore, when set on enqueue, holds the message queued until this time so
	// the worker does not transmit it before then. It backs the undo-send delay.
	NotBefore time.Time
}

// Queue is the persistent send queue backed by the store.
type Queue struct {
	db *storage.DB
}

// NewQueue returns a queue over the given store.
func NewQueue(db *storage.DB) *Queue {
	return &Queue{db: db}
}

// Enqueue adds a message in the queued state, due immediately, and returns its
// id. The worker picks it up on its next poll.
func (q *Queue) Enqueue(ctx context.Context, m Message) (int64, error) {
	// hold the message until NotBefore when set (the undo-send delay), otherwise
	// it is due immediately.
	next := time.Now().UTC()
	if !m.NotBefore.IsZero() {
		next = m.NotBefore.UTC()
	}
	row := storage.OutboxRow{
		AccountID:     m.AccountID,
		EnvelopeFrom:  m.EnvelopeFrom,
		Recipients:    strings.Join(m.Recipients, recipientSep),
		Raw:           m.Raw,
		State:         StateQueued,
		Attempts:      0,
		NextAttemptAt: next,
	}
	return q.db.InsertOutbox(ctx, row)
}

// claimDue atomically claims the next due message, moving it to sending, passing
// over the accounts in skip. Returns (nil, nil) when nothing is ready.
func (q *Queue) claimDue(ctx context.Context, now time.Time, skip ...int64) (*Message, error) {
	row, err := q.db.ClaimDueOutbox(ctx, StateQueued, StateSending, now, skip)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	m := toMessage(*row)
	return &m, nil
}

// markSent records a successful send.
func (q *Queue) markSent(ctx context.Context, id int64) error {
	return q.db.UpdateOutboxState(ctx, id, StateSent, 0, "", time.Time{})
}

// markAttemptFailed records a failed attempt. It requeues with backoff while
// attempts remain, otherwise marks the message failed, always keeping the last
// error for the user to see. It returns whether the message will be retried.
func (q *Queue) markAttemptFailed(ctx context.Context, m Message, cause string) (retry bool, err error) {
	attempts := m.Attempts + 1
	if attempts >= MaxAttempts {
		return false, q.db.UpdateOutboxState(ctx, m.ID, StateFailed, attempts, cause, time.Time{})
	}
	next := time.Now().UTC().Add(backoff(attempts))
	return true, q.db.UpdateOutboxState(ctx, m.ID, StateQueued, attempts, cause, next)
}

// RequeueStuck moves any rows left in sending (from a crashed run) back to
// queued so they are retried. Returns how many were recovered.
func (q *Queue) RequeueStuck(ctx context.Context) (int, error) {
	return q.db.RequeueOutboxState(ctx, StateSending, StateQueued)
}

// PruneSent removes rows already marked sent and returns how many were cleared.
// The ui calls this after showing the brief "sent" confirmation so the queue
// does not accumulate completed messages.
func (q *Queue) PruneSent(ctx context.Context) (int, error) {
	return q.db.DeleteOutboxByState(ctx, StateSent)
}

// Cancel removes a still-queued message, returning whether it was removed. A
// message that has already moved to sending cannot be cancelled. This backs the
// undo-send window: the message waits queued until its delay passes, and undo
// pulls it back before the worker claims it.
func (q *Queue) Cancel(ctx context.Context, id int64) (bool, error) {
	return q.db.DeleteOutboxIfState(ctx, id, StateQueued)
}

// Retry puts a failed message back in the queue, due immediately and with its
// attempt count and last error cleared, and reports whether it was requeued. A
// message that is no longer failed is left alone.
//
// Nothing else brings a failed message back: once the attempts run out the
// worker is done with it, so a send that failed for a reason the user has since
// fixed would otherwise have to be written again from scratch.
func (q *Queue) Retry(ctx context.Context, id int64) (bool, error) {
	return q.db.UpdateOutboxStateIf(ctx, id, StateFailed, StateQueued, 0, "", time.Now().UTC())
}

// Discard removes a failed message and reports whether it was removed. Only a
// failed message can be discarded: anything still queued or sending is either
// going to be sent or can be cancelled, and neither should be droppable by
// accident. The message is gone afterwards, so the caller is expected to have
// confirmed it.
func (q *Queue) Discard(ctx context.Context, id int64) (bool, error) {
	return q.db.DeleteOutboxIfState(ctx, id, StateFailed)
}

// List returns every queued, sending, sent and failed message for inspection.
func (q *Queue) List(ctx context.Context) ([]Message, error) {
	rows, err := q.db.ListOutbox(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMessage(r))
	}
	return out, nil
}

// backoff returns the capped exponential delay for the given attempt number,
// which is 1 for the first retry.
func backoff(attempt int) time.Duration {
	d := baseBackoff << (attempt - 1)
	if d <= 0 || d > maxBackoff {
		return maxBackoff
	}
	return d
}

func toMessage(r storage.OutboxRow) Message {
	var recipients []string
	if r.Recipients != "" {
		recipients = strings.Split(r.Recipients, recipientSep)
	}
	return Message{
		ID:            r.ID,
		AccountID:     r.AccountID,
		EnvelopeFrom:  r.EnvelopeFrom,
		Recipients:    recipients,
		Raw:           r.Raw,
		State:         r.State,
		Attempts:      r.Attempts,
		LastError:     r.LastError,
		NextAttemptAt: r.NextAttemptAt,
		CreatedAt:     r.CreatedAt,
	}
}

// Pending reports whether anything is waiting to be sent or is mid-send. Sync
// asks before each message it downloads, so a send never waits behind a large
// mailbox.
func (q *Queue) Pending(ctx context.Context) bool {
	n, err := q.db.CountOutboxInStates(ctx, StateQueued, StateSending)
	if err != nil {
		// unreadable is not a reason to stall a sync; the send worker has its
		// own view of the queue and will report the failure.
		return false
	}
	return n > 0
}
