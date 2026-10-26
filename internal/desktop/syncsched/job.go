// Package syncsched runs one account's mail-server work as small tasks on
// pooled sessions. It knows no protocol: a Provider hands out what a worker
// holds (an IMAP connection, a JMAP request token), a Push config watches
// for changes, and desktop writes each task's Run.
//
// Two lanes, both FIFO, new tasks always at the tail:
//   - the fast lane: tiny work the user waits for (one opened body, one
//     action, the new stubs after an IDLE push);
//   - the normal lane: sync work, ordered by Class, FIFO within a class.
//
// Every session is a worker, at most effective N of them (after a shrink,
// workers above N leave at the end of their task). After each task a
// worker takes the next one; see pick. Nothing is paused mid-task: the end of
// a task is the only safe point, so every task is bounded. Work that can grow
// (a body campaign) is a generator: one task per folder that does one batch
// and returns ErrAgain to go back to the tail, so the queue holds at most one
// entry per folder and kind. An entry with a Width (a folder's bodies) runs as
// up to that many copies at once, each one unit on its own session; it is
// still one entry, with one Done.
package syncsched

import (
	"context"
	"errors"
	"fmt"
)

// FastCap is the fast-lane length above which the oldest queued on-demand
// body is dropped. User actions are never dropped.
const FastCap = 64

// Kind is what a task does. It picks the lane, the default class and the
// per-folder rules.
type Kind int

const (
	// Normal lane.

	// KindFolderTree lists the account's folders. With Task.Barrier it is the
	// account's first task: nothing else starts until it ends.
	KindFolderTree Kind = iota
	// KindListStubs lists one folder (first, timed, manual sync, and further
	// "load older" pages).
	KindListStubs
	// KindBodies downloads at most the batch size missing bodies of one folder.
	KindBodies
	// KindEmptyTrash empties the Trash folder.
	KindEmptyTrash
	// KindOffline downloads at most the batch size messages for offline use.
	KindOffline
	// KindFullCheck re-lists one folder in full.
	KindFullCheck
	// KindOpenedBody fetches, a chunk per unit, the body of an opened message
	// too large for the fast lane. The user waits for it: it runs in the top
	// class and waits for no list or body work of its folder.
	KindOpenedBody

	// Fast lane.

	// KindOnDemandBody fetches the body of the one message the user opened.
	KindOnDemandBody
	// KindAttachment fetches one attachment or inline image of a stub.
	KindAttachment
	// KindSource fetches one message's raw source.
	KindSource
	// KindMove moves, archives, trashes or moves back one message.
	KindMove
	// KindFlags sets seen, star or colour on one message.
	KindFlags
	// KindFolderOp creates, renames or deletes one folder.
	KindFolderOp
	// KindSentCopy appends one sent message to Sent.
	KindSentCopy
	// KindNewMail lists the new stubs of one folder after an IDLE push. Its
	// bodies go to the normal lane.
	KindNewMail
	// KindLoadOlder lists one page of older stubs ("load older"). Its bodies
	// and further pages go to the normal lane.
	KindLoadOlder
)

var kindNames = [...]string{
	"folder-tree", "list", "bodies", "empty-trash", "offline", "full-check", "opened-body",
	"on-demand-body", "attachment", "source", "move", "flags", "folder-op", "sent-copy", "new-mail", "load-older",
}

func (k Kind) String() string {
	if int(k) >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Fast reports whether tasks of this kind go to the fast lane.
func (k Kind) Fast() bool { return k >= KindOnDemandBody }

// lists reports whether the kind lists a folder: its bodies wait for it,
// and no other list of the folder runs beside it. Emptying Trash lists it to
// push the deletes.
func (k Kind) lists() bool {
	return k == KindListStubs || k == KindFullCheck || k == KindNewMail || k == KindLoadOlder || k == KindEmptyTrash
}

// droppable reports whether a full fast lane may drop a queued task of this
// kind: the user can ask again, and nothing is lost on the server.
func (k Kind) droppable() bool { return k == KindOnDemandBody || k == KindAttachment }

// Class orders the normal lane. Higher classes go first. ClassAuto takes the
// kind's default (DefaultClass).
type Class int

const (
	ClassAuto Class = iota
	ClassFullCheck
	ClassOffline
	ClassEmptyTrash
	ClassBodies
	ClassStubs
	// ClassManualSync is every task of a manual Sync, whatever its kind.
	ClassManualSync
)

// DefaultClass is the normal-lane class of a kind outside a manual Sync.
func DefaultClass(k Kind) Class {
	switch k {
	case KindFolderTree, KindListStubs:
		return ClassStubs
	case KindBodies:
		return ClassBodies
	case KindEmptyTrash:
		return ClassEmptyTrash
	case KindOffline:
		return ClassOffline
	case KindFullCheck:
		return ClassFullCheck
	case KindOpenedBody:
		return ClassManualSync
	}
	return ClassAuto
}

// Task is one bounded unit of work. Run gets a logged-in session of its own
// for the whole call. Run should re-check the store for what is still
// missing, so a stale or duplicate task ends at once.
type Task[C any] struct {
	Kind Kind
	// Class orders a normal-lane task; ClassAuto uses DefaultClass(Kind).
	// Fast-lane tasks ignore it.
	Class Class
	// FolderID is the folder the task works on, 0 for account-wide work.
	FolderID int64
	// Key dedups. While a task with the same key is queued (or, for a width
	// entry, queued or running), Enqueue adds nothing and the new Done runs
	// when that one ends. A queued task does not start while another one
	// with its key runs. The merged task is a barrier when either of the two
	// is.
	Key string
	// Barrier holds every other task of the account while it is queued or
	// running. It starts only once no other task runs, and several queued
	// barriers run one at a time in queue order.
	Barrier bool
	// Run does one unit of work. Returning ErrAgain puts the task back at the
	// tail of its lane (one unit done, more left); Done waits for the end.
	// With Width set, Run is called by several workers at once and must be
	// safe for that.
	Run func(ctx context.Context, session C) error
	// Width, when set on a normal-lane task, lets its one entry run as up to
	// Width(n) copies at once, n being the provider's Limit at the pick. It
	// is read at every pick and clamped to [1, the normal sessions]: N less
	// the one a Park watcher (IMAP IDLE) keeps, and Tunables.MaxBodyWidth.
	// Each copy is one unit on its own session; the entry stays in its lane
	// while copies run and goes to the tail of its class at each dispatch,
	// so folders still take turns. Copies coordinate what they fetch
	// themselves (Claims). Per copy result: ErrAgain keeps the entry open;
	// ErrSaturated holds it until another unit of the folder ends; nil ends
	// it once every copy is back (a later ErrAgain reopens it); another
	// error fails it: no new copy, and Done gets that error after the
	// others drained. A task with the entry's Key joins it while it is
	// queued or running; a join after a copy ended with nil reopens it, and
	// a join while copies run gets one more unit, started after the join.
	// Nil runs one copy, exactly as a task without a width.
	Width func(n int) int
	// Done, when set, runs once with Run's final result, or with why Run
	// never ran (ErrStopped, ErrDropped, a dial error). It runs on no lock
	// and may Enqueue.
	Done func(err error)
}

var (
	// ErrAgain is what Run returns after one unit when more work is left.
	// The session stays with the worker.
	ErrAgain = errors.New("syncsched: more work left")
	// ErrAgainBroken is ErrAgain (errors.Is matches both) for a unit that
	// failed on a session that is no longer usable, such as a dropped
	// connection: the task goes back to the tail, the session is closed and
	// the next task dials a fresh one. Join it with the cause for logs,
	// errors.Join(ErrAgainBroken, err). Options.Broken is not asked about
	// ErrAgain results.
	ErrAgainBroken = fmt.Errorf("%w; the session is broken", ErrAgain)
	// ErrSaturated is ErrAgain (errors.Is matches both) for a unit that
	// found work left but none it could claim, because other units of its
	// folder hold it. The task is not dispatched again until another task of
	// its folder ends, so it never spins; with nothing of the folder running
	// it is dispatched again at once (the holders ended meanwhile).
	ErrSaturated = fmt.Errorf("%w; the rest is claimed by running units", ErrAgain)
	// ErrStopped is the Done error of a task dropped because the scheduler stopped.
	ErrStopped = errors.New("syncsched: scheduler stopped")
	// ErrDropped is the Done error of an on-demand task dropped from a full fast lane.
	ErrDropped = errors.New("syncsched: dropped from a full fast lane")
	// ErrIdleUnsupported is what Push.Park or Push.Listen returns when the
	// server has no push. The scheduler then stops trying.
	ErrIdleUnsupported = errors.New("syncsched: server does not support idle")
)
