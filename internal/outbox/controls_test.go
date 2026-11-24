package outbox

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPrioritiseMovesAMessageToTheFront(t *testing.T) {
	ctx := context.Background()
	q, accountID := newTestQueue(t)

	first, _ := q.Enqueue(ctx, sampleMessage(accountID))
	second, _ := q.Enqueue(ctx, sampleMessage(accountID))
	third, _ := q.Enqueue(ctx, sampleMessage(accountID))

	for _, id := range []int64{third, second} {
		moved, err := q.Prioritise(ctx, id)
		if err != nil || !moved {
			t.Fatalf("prioritise %d = %v, %v; want moved", id, moved, err)
		}
	}

	// the last one moved goes first, then the one moved before it, then the
	// untouched rest in queue order.
	var order []int64
	for {
		m, err := q.claimDue(ctx, time.Now().UTC().Add(time.Second))
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if m == nil {
			break
		}
		order = append(order, m.ID)
	}
	want := []int64{second, third, first}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("claim order = %v, want %v", order, want)
	}

	moved, err := q.Prioritise(ctx, first)
	if err != nil {
		t.Fatalf("prioritise sending: %v", err)
	}
	if moved {
		t.Error("a message already sending was moved in the queue")
	}
}

func TestWorkerSendsAPrioritisedMessageFirst(t *testing.T) {
	ctx := context.Background()
	q, accountID := newTestQueue(t)

	first, _ := q.Enqueue(ctx, sampleMessage(accountID))
	_, _ = q.Enqueue(ctx, sampleMessage(accountID))
	last, _ := q.Enqueue(ctx, sampleMessage(accountID))
	if _, err := q.Prioritise(ctx, last); err != nil {
		t.Fatalf("prioritise: %v", err)
	}

	tx := &orderTransmitter{}
	if err := NewWorker(q, tx).DrainOnce(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(tx.order) != 3 || tx.order[0] != last || tx.order[1] != first {
		t.Fatalf("sent %v, want %d first and then %d", tx.order, last, first)
	}
}

type orderTransmitter struct {
	mu    sync.Mutex
	order []int64
}

func (o *orderTransmitter) Transmit(_ context.Context, m Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.order = append(o.order, m.ID)
	return nil
}

func TestWithdrawHoldsAQueuedMessage(t *testing.T) {
	ctx := context.Background()
	q, accountID := newTestQueue(t)
	log := NewActivityLog(10, nil)

	id, _ := q.Enqueue(ctx, Message{
		AccountID:    accountID,
		EnvelopeFrom: "me@example.com",
		Recipients:   []string{"a@example.com"},
		Raw:          []byte("x"),
		NotBefore:    time.Now().Add(time.Hour),
	})
	worker := NewWorker(q, &fakeTransmitter{}, WithActivity(log))

	got, err := worker.Withdraw(ctx, id)
	if err != nil || got != WithdrawHeld {
		t.Fatalf("withdraw = %v, %v; want held", got, err)
	}
	m := findByID(t, q, id)
	if m.State != StateFailed || m.LastError != CancelledReason {
		t.Fatalf("message is %q (%q), want failed as cancelled", m.State, m.LastError)
	}
	entries := log.Entries()
	if len(entries) != 1 || entries[0].Step != StepCancelled || entries[0].MessageID != id {
		t.Fatalf("activity = %+v, want one cancelled entry", entries)
	}

	again, err := worker.Withdraw(ctx, id)
	if err != nil || again != WithdrawNone {
		t.Fatalf("second withdraw = %v, %v; want none", again, err)
	}
}

// stallTransmitter announces each transmission, then waits for ctx or release.
// With commit set it commits first, as the smtp sender does at the end of DATA.
type stallTransmitter struct {
	commit    bool
	started   chan struct{}
	release   chan struct{}
	commitErr chan error
}

func (s *stallTransmitter) Transmit(ctx context.Context, m Message) error {
	Report(ctx, StepConnecting)
	if s.commit {
		err := Commit(ctx)
		s.commitErr <- err
		if err != nil {
			return err
		}
	}
	close(s.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
		return nil
	}
}

func TestWithdrawAbortsATransmissionBeforeCommit(t *testing.T) {
	ctx := context.Background()
	q, accountID := newTestQueue(t)
	log := NewActivityLog(10, nil)
	id, _ := q.Enqueue(ctx, sampleMessage(accountID))

	tx := &stallTransmitter{started: make(chan struct{}), release: make(chan struct{})}
	worker := NewWorker(q, tx, WithActivity(log))
	drained := make(chan error, 1)
	go func() { drained <- worker.DrainOnce(ctx) }()
	<-tx.started

	got, err := worker.Withdraw(ctx, id)
	if err != nil || got != WithdrawHeld {
		t.Fatalf("withdraw = %v, %v; want held", got, err)
	}
	if err := <-drained; err != nil {
		t.Fatalf("drain: %v", err)
	}
	m := findByID(t, q, id)
	if m.State != StateFailed || m.LastError != CancelledReason {
		t.Fatalf("message is %q (%q), want failed as cancelled", m.State, m.LastError)
	}
	var steps []string
	for _, e := range log.Entries() {
		steps = append(steps, e.Step)
	}
	if len(steps) != 2 || steps[0] != StepConnecting || steps[1] != StepCancelled {
		t.Fatalf("activity steps = %v, want connecting then cancelled", steps)
	}
}

func TestWithdrawAfterCommitIsTooLate(t *testing.T) {
	ctx := context.Background()
	q, accountID := newTestQueue(t)
	id, _ := q.Enqueue(ctx, sampleMessage(accountID))

	tx := &stallTransmitter{commit: true, started: make(chan struct{}), release: make(chan struct{}), commitErr: make(chan error, 1)}
	worker := NewWorker(q, tx)
	drained := make(chan error, 1)
	go func() { drained <- worker.DrainOnce(ctx) }()
	<-tx.started

	got, err := worker.Withdraw(ctx, id)
	if err != nil || got != WithdrawTooLate {
		t.Fatalf("withdraw = %v, %v; want too late", got, err)
	}
	close(tx.release)
	if err := <-drained; err != nil {
		t.Fatalf("drain: %v", err)
	}
	if m := findByID(t, q, id); m.State != StateSent {
		t.Fatalf("message is %q, want sent", m.State)
	}
}

// commitTransmitter commits at once and reports whether it was allowed to.
type commitTransmitter struct {
	committed chan bool
}

func (c *commitTransmitter) Transmit(ctx context.Context, _ Message) error {
	err := Commit(ctx)
	c.committed <- err == nil
	return err
}

// A cancel landing while the worker claims, transmits or commits must agree
// with what happened: held means the message never went out, too late means
// it did, and nothing else is possible for a message that was waiting.
func TestWithdrawRacingTheWorkerAgreesWithTheOutcome(t *testing.T) {
	ctx := context.Background()
	for i := range 40 {
		q, accountID := newTestQueue(t)
		id, _ := q.Enqueue(ctx, sampleMessage(accountID))
		tx := &commitTransmitter{committed: make(chan bool, 1)}
		worker := NewWorker(q, tx)

		drained := make(chan error, 1)
		go func() { drained <- worker.DrainOnce(ctx) }()
		if i%2 == 1 {
			time.Sleep(time.Duration(i) * 50 * time.Microsecond)
		}
		got, err := worker.Withdraw(ctx, id)
		if err != nil {
			t.Fatalf("round %d: withdraw: %v", i, err)
		}
		if err := <-drained; err != nil {
			t.Fatalf("round %d: drain: %v", i, err)
		}

		var committed, transmitted bool
		select {
		case committed = <-tx.committed:
			transmitted = true
		default:
		}
		m := findByID(t, q, id)
		switch got {
		case WithdrawHeld:
			if committed || m.State != StateFailed || m.LastError != CancelledReason {
				t.Fatalf("round %d: held, but committed=%v and the message is %q (%q)", i, committed, m.State, m.LastError)
			}
		case WithdrawTooLate:
			if !committed || m.State != StateSent {
				t.Fatalf("round %d: too late, but committed=%v and the message is %q", i, committed, m.State)
			}
		default:
			if !transmitted || !committed || m.State != StateSent {
				t.Fatalf("round %d: withdraw found nothing, but transmitted=%v committed=%v and the message is %q", i, transmitted, committed, m.State)
			}
		}
	}
}

func TestActivityLogKeepsTheNewestEntries(t *testing.T) {
	var pushed []uint64
	log := NewActivityLog(3, func(e Entry) { pushed = append(pushed, e.Seq) })
	for i := range 5 {
		log.Add(Entry{MessageID: int64(i), Step: StepQueued})
	}
	entries := log.Entries()
	if len(entries) != 3 || entries[0].MessageID != 2 || entries[2].MessageID != 4 {
		t.Fatalf("entries = %+v, want messages 2, 3, 4 oldest first", entries)
	}
	if entries[2].Seq != 5 || entries[2].At.IsZero() {
		t.Fatalf("newest entry = %+v, want seq 5 with a time", entries[2])
	}
	if len(pushed) != 5 {
		t.Fatalf("onAdd saw %v, want every entry", pushed)
	}

	var none *ActivityLog
	none.Add(Entry{Step: StepQueued})
	if none.Entries() != nil {
		t.Error("a nil log returned entries")
	}
}
