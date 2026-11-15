package outbox

import (
	"slices"
	"sync"
	"time"
)

// Steps an activity entry records. The ui words each one; Detail and RetryAt
// carry what a step needs beyond the message it is about.
const (
	StepQueued      = "queued"
	StepConnecting  = "connecting"
	StepSigningIn   = "signingIn"
	StepSending     = "sending"
	StepSent        = "sent"
	StepFailed      = "failed"
	StepRetry       = "retry"
	StepCancelled   = "cancelled"
	StepPrioritised = "prioritised"
)

// Entry is one line of the send activity log.
type Entry struct {
	// Seq numbers entries in the order they were added, from 1.
	Seq        uint64
	At         time.Time
	MessageID  int64
	Step       string
	Recipients []string
	// Detail is the error of a failed step, empty otherwise.
	Detail string
	// RetryAt is when a StepRetry entry's message is tried again.
	RetryAt time.Time
}

// ActivityLog is a fixed size, memory only ring of send activity, for the
// live log in the ui. It is never written to disk, so it is gone on quit. A nil
// *ActivityLog is a valid log that records nothing.
type ActivityLog struct {
	mu      sync.Mutex
	entries []Entry
	next    int
	full    bool
	seq     uint64
	onAdd   func(Entry)
}

// NewActivityLog returns a log holding the last size entries. onAdd, when not
// nil, is called with every entry after it is recorded, outside the lock; it
// must not block.
func NewActivityLog(size int, onAdd func(Entry)) *ActivityLog {
	return &ActivityLog{entries: make([]Entry, max(size, 1)), onAdd: onAdd}
}

// Add records e, stamping its Seq and, when unset, its time, and returns it
// as recorded. The oldest entry makes way once the log is full.
func (l *ActivityLog) Add(e Entry) Entry {
	if l == nil {
		return e
	}
	l.mu.Lock()
	l.seq++
	e.Seq = l.seq
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	e.Recipients = slices.Clone(e.Recipients)
	l.entries[l.next] = e
	l.next = (l.next + 1) % len(l.entries)
	if l.next == 0 {
		l.full = true
	}
	onAdd := l.onAdd
	l.mu.Unlock()
	if onAdd != nil {
		onAdd(e)
	}
	return e
}

// Entries returns the recorded entries, oldest first.
func (l *ActivityLog) Entries() []Entry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.full {
		return slices.Clone(l.entries[:l.next])
	}
	out := make([]Entry, 0, len(l.entries))
	out = append(out, l.entries[l.next:]...)
	return append(out, l.entries[:l.next]...)
}

// record adds a step about message m.
func (l *ActivityLog) record(m Message, step, detail string, retryAt time.Time) {
	l.Add(Entry{MessageID: m.ID, Step: step, Recipients: m.Recipients, Detail: detail, RetryAt: retryAt})
}
