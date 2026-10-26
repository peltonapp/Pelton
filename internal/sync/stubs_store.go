package sync

import (
	"context"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// A listing stores its stubs a chunk per write transaction rather than one
// per stub (a commit each) or a page at once (the write lock held for the
// whole page). SQLite has one writer, so a user's flag change or move waits
// for whatever transaction is open; a chunk keeps that wait short.
const (
	// stubTxRows is the most stubs one transaction takes.
	stubTxRows = 100
	// stubTxBudget is how long one transaction may keep taking stubs. It
	// only bites on a slow disk; a chunk normally commits well inside it.
	stubTxBudget = 25 * time.Millisecond
)

// busyDelays and busyTotals are SQLite's default busy handler schedule in
// milliseconds (sqliteDefaultBusyCallback), which busy_timeout uses: a
// writer that finds the lock taken sleeps busyDelays[i] once it has waited
// busyTotals[i], and only then tries again.
var (
	busyDelays = [...]int{1, 2, 5, 10, 15, 20, 25, 25, 25, 50, 50, 100}
	busyTotals = [...]int{0, 1, 3, 8, 18, 33, 53, 78, 103, 128, 178, 228}
)

// stubTxGap is how long a listing waits after a transaction that held the
// lock for held before it begins the next, when another connection is in
// use (storage.DB.OthersActive); with none, nobody can be waiting and it
// goes straight on. A writer that queued behind that
// transaction has waited at most held and is asleep in the busy handler for
// at most the step it reached by then; the gap outlasts that step, so the
// writer wakes to a free lock instead of finding the next chunk in its way.
func stubTxGap(held time.Duration) time.Duration {
	ms := int(held / time.Millisecond)
	step := busyDelays[0]
	for i, total := range busyTotals {
		if total > ms {
			break
		}
		step = busyDelays[i]
	}
	return time.Duration(step+1) * time.Millisecond
}

// storeListStubs writes envelope rows for messages that have list metadata
// so the message list can render before bodies arrive. Headers without
// HasListMeta are skipped. It returns the stored row ids and how many
// stubs failed, so callers can hold the cursor back.
//
// The rows are prepared first and written in chunks (stubTxRows,
// stubTxBudget), with a gap between transactions for other writers. A stub
// that fails is left out and the rest still store. A cancelled context
// stops at the next chunk: the stubs already prepared still store, so a
// cancel never loses what was listed before it.
func (e *Engine) storeListStubs(ctx context.Context, folder storage.Folder, remoteIDs []string, headersByID map[string]Header) ([]int64, int) {
	w := e.writeStubs(ctx, folder, remoteIDs, headersByID, false)
	return w.ids, w.failed
}

// writeStubs is storeListStubs. With stopAtFailure the first stub that
// fails ends it, and w.stored is then the stubs before it.
func (e *Engine) writeStubs(ctx context.Context, folder storage.Folder, remoteIDs []string, headersByID map[string]Header, stopAtFailure bool) *stubWriter {
	w := &stubWriter{e: e, ctx: ctx, stopAtFailure: stopAtFailure, ids: make([]int64, 0, len(remoteIDs))}
	for _, rid := range remoteIDs {
		if ctx.Err() != nil || w.stopped {
			break
		}
		h, ok := headersByID[rid]
		if !ok || !h.HasListMeta {
			continue
		}
		if e.beforeListStub != nil {
			if err := e.beforeListStub(rid); err != nil {
				e.log.Error("store list stub failed", "remote_id", rid, "err", err)
				w.failed++
				if stopAtFailure {
					// The stubs before it still store.
					w.flush()
					w.stopped = true
					break
				}
				continue
			}
			if ctx.Err() != nil {
				break
			}
		}
		w.rows = append(w.rows, listStub(folder, h))
		if len(w.rows) == stubTxRows {
			w.flush()
		}
	}
	w.flush()
	return w
}

// stubWriter is one writeStubs call's chunked writes.
type stubWriter struct {
	e             *Engine
	ctx           context.Context
	stopAtFailure bool
	rows          []storage.Message
	// ids are the stored rows and stored their remote ids, in order.
	ids    []int64
	stored []string
	failed int
	// stopped is set once a failure ended a stopAtFailure write.
	stopped bool
	// gap is the wait owed before the next transaction (stubTxGap).
	gap time.Duration
}

// flush stores the prepared rows, a transaction at a time. The writes do
// not take the caller's cancel: a chunk is short, and its rows are already
// listed.
func (w *stubWriter) flush() {
	write := context.WithoutCancel(w.ctx)
	for len(w.rows) > 0 && !w.stopped {
		w.yield()
		b, err := w.e.store.UpsertMessageListMetaBatch(write, w.rows, stubTxBudget)
		if err != nil {
			w.e.log.Error("store list stubs failed", "count", len(w.rows), "err", err)
			w.failed += len(w.rows)
			w.rows = w.rows[:0]
			w.stopped = w.stopAtFailure
			return
		}
		ok := b.Done
		if b.Err != nil {
			ok--
			w.e.log.Error("store list stub failed", "remote_id", w.rows[ok].RemoteID, "err", b.Err)
			w.failed++
			w.stopped = w.stopAtFailure
		}
		w.ids = append(w.ids, b.IDs[:ok]...)
		for _, m := range w.rows[:ok] {
			w.stored = append(w.stored, m.RemoteID)
		}
		if w.e.afterStubTx != nil {
			w.e.afterStubTx(b.Done, b.Held)
		}
		w.gap = 0
		if w.e.store.OthersActive() {
			w.gap = stubTxGap(b.Held)
		}
		w.rows = append(w.rows[:0], w.rows[b.Done:]...)
	}
}

// yield waits out the gap owed by the last transaction, or less if the
// context ends; the next chunk still stores.
func (w *stubWriter) yield() {
	if w.gap <= 0 {
		return
	}
	t := time.NewTimer(w.gap)
	defer t.Stop()
	select {
	case <-t.C:
	case <-w.ctx.Done():
	}
	w.gap = 0
}

// listStub is the stub row for a listed header.
func listStub(folder storage.Folder, h Header) storage.Message {
	return storage.Message{
		AccountID:      folder.AccountID,
		FolderID:       folder.ID,
		UID:            h.LegacyUID,
		RemoteID:       h.RemoteID,
		Subject:        h.Subject,
		FromAddress:    h.From,
		FromName:       h.FromName,
		ToAddresses:    h.To,
		Date:           h.Date,
		Flags:          h.Flags,
		BodyPlain:      h.Preview,
		HasAttachments: h.HasAttachment,
		SizeBytes:      h.Size,
	}
}
