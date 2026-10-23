package desktop

import (
	"context"
	"sync"

	psync "github.com/peltonapp/Pelton/internal/sync"
)

// A sync run reports whether it is listing stubs or fetching bodies, so the
// status bar can say which (#313).
const (
	SyncPhaseStubs  = "stubs"
	SyncPhaseBodies = "bodies"
)

// syncTally accumulates what a sync run is doing, so the status bar can show a
// real progress bar rather than a spinner (#313).
//
// The counts come from the reconcile plans, which know exactly how many bodies
// each folder will fetch, so the total is what is being downloaded rather than
// how big the mailboxes are. It grows as folders open: the grand total cannot
// be known before the last folder is reconciled, and inventing one by asking
// every folder its size up front would both cost a round trip per folder and
// report a number that a resync would never reach.
//
// It is guarded because folder syncs report from the sync goroutine while the
// ui reads through the events they emit.
type syncTally struct {
	mu sync.Mutex
	// per folder, so a folder that reports 40 of 200 twice is not counted twice.
	done  map[int64]int
	total map[int64]int
	// the folder being fetched right now, and its share of the counts.
	folderID    int64
	folderName  string
	folderDone  int
	folderTotal int
	// mailboxes finished and how many the runs cover.
	foldersDone  int
	foldersTotal int
	// phase is SyncPhaseStubs while a folder is listed, SyncPhaseBodies once
	// bodies come in.
	phase string
	// runs is how many sync runs of the account share the tally now. An
	// overlapping run adds its folders instead of resetting the counts.
	runs int
}

// begin starts a run over n folders. The first run resets what the last one
// left; a run that overlaps another adds to it.
func (t *syncTally) begin(folders int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs++
	if t.runs > 1 {
		t.foldersTotal += folders
		return
	}
	t.done = make(map[int64]int, folders)
	t.total = make(map[int64]int, folders)
	t.folderID, t.folderName = 0, ""
	t.folderDone, t.folderTotal = 0, 0
	t.foldersDone, t.foldersTotal = 0, folders
	t.phase = ""
}

// end ends one run and reports whether it was the last, which closes the line.
func (t *syncTally) end() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs = max(t.runs-1, 0)
	return t.runs == 0
}

// idle reports that no sync run of the account is going.
func (t *syncTally) idle() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runs == 0
}

// enterFolder records that a folder is now being listed. Folders of a run
// are listed side by side, so entering one says nothing about how many are
// done; finishFolder counts those.
func (t *syncTally) enterFolder(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.folderName = name
	t.folderDone, t.folderTotal = 0, 0
	t.phase = SyncPhaseStubs
}

// finishFolder counts one more folder of the run as done, listed and
// filled or failed.
func (t *syncTally) finishFolder() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.foldersDone++
}

// record takes one folder's progress and returns the run's counts with it
// folded in.
func (t *syncTally) record(p psync.FolderProgress) syncCounts {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done == nil {
		t.done, t.total = map[int64]int{}, map[int64]int{}
	}
	t.done[p.Folder.ID] = p.Done
	t.total[p.Folder.ID] = p.Total
	t.folderID, t.folderName = p.Folder.ID, p.Folder.Name
	t.folderDone, t.folderTotal = p.Done, p.Total
	t.phase = SyncPhaseBodies
	return t.countsLocked()
}

// counts is the run's totals as they stand.
func (t *syncTally) counts() syncCounts {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.countsLocked()
}

// closing is counts() with the folder cleared, the event that tells the ui this
// account's run is over.
func (t *syncTally) closing() syncCounts {
	c := t.counts()
	c.Folder, c.Phase = "", ""
	return c
}

func (t *syncTally) countsLocked() syncCounts {
	c := syncCounts{
		Folder:       t.folderName,
		FolderDone:   t.folderDone,
		FolderTotal:  t.folderTotal,
		FoldersDone:  t.foldersDone,
		FoldersTotal: t.foldersTotal,
		Phase:        t.phase,
	}
	for _, n := range t.done {
		c.Done += n
	}
	for _, n := range t.total {
		c.Total += n
	}
	return c
}

// syncCounts is a snapshot of a run for one progress event.
type syncCounts struct {
	Done         int
	Total        int
	Folder       string
	FolderDone   int
	FolderTotal  int
	FoldersDone  int
	FoldersTotal int
	// Phase is stubs or bodies during a sync run.
	Phase string
}

// beginSyncState counts one more sync run going and returns its end. The
// first run emits sync:state running and the last one to end emits stopped,
// so runs of several accounts, or overlapping runs of one, do not end each
// other's state.
func (a *App) beginSyncState() (end func()) {
	a.syncStateMu.Lock()
	a.syncStateRuns++
	if a.syncStateRuns == 1 {
		a.emit(EventSyncState, SyncStateEvent{Running: true})
	}
	a.syncStateMu.Unlock()
	return sync.OnceFunc(func() {
		a.syncStateMu.Lock()
		defer a.syncStateMu.Unlock()
		a.syncStateRuns--
		if a.syncStateRuns == 0 {
			a.emit(EventSyncState, SyncStateEvent{Running: false})
		}
	})
}

// syncRun is one counted sync run of an account: its share of the progress
// line, the line's heartbeat and sync:state. Overlapping runs of the account
// share the tally; only the last one to end closes the line.
type syncRun struct {
	a             *App
	accountID     int64
	email, server string
	tally         *syncTally
	stopBeat      func()
	endState      func()
	once          sync.Once
}

// startSyncRun begins a run over folders folders.
func (a *App) startSyncRun(ctx context.Context, accountID int64, email, server string, folders int) *syncRun {
	r := &syncRun{a: a, accountID: accountID, email: email, server: server, tally: a.accountTally(accountID)}
	r.endState = a.beginSyncState()
	r.tally.begin(folders)
	r.stopBeat = a.startProgressHeartbeat(ctx, accountID)
	return r
}

// enter shows the folder as the one being listed.
func (r *syncRun) enter(name string) {
	r.tally.enterFolder(name)
	r.a.emitSyncProgress(r.accountID, r.email, r.server, r.tally.counts())
}

// folderDone counts one folder of the run as done.
func (r *syncRun) folderDone() {
	r.tally.finishFolder()
	r.a.emitSyncProgress(r.accountID, r.email, r.server, r.tally.counts())
}

// finish ends the run with all its folders done. The line closes full when
// no other run of the account is going.
func (r *syncRun) finish(folders int) {
	r.end(func() syncCounts {
		c := r.tally.closing()
		c.FoldersDone = max(c.FoldersDone, folders)
		return c
	})
}

// closeIdleProgress closes the account's progress line when bodies a pass
// left running reopened it (they report progress after the run ended) and
// no sync run of the account is going. The check and the closing event are
// made under the account's progress lock: a run starting meanwhile sends
// its first event after the closing one, never before it, and a line the
// run already closed is not closed again.
func (a *App) closeIdleProgress(accountID int64, email string) {
	if a.accountRemoved(accountID) {
		return
	}
	st := a.accountStateFor(accountID)
	st.progressMu.Lock()
	defer st.progressMu.Unlock()
	if st.lastProgress == nil || !st.tally.idle() {
		return
	}
	a.emitSyncProgressLocked(st, progressEvent(accountID, email, "", st.tally.closing()))
}

// abort ends a run cut short; a no-op after finish.
func (r *syncRun) abort() { r.end(r.tally.closing) }

func (r *syncRun) end(final func() syncCounts) {
	r.once.Do(func() {
		// The heartbeat must be gone before the close is sent.
		r.stopBeat()
		if r.tally.end() {
			r.a.emitSyncProgress(r.accountID, r.email, r.server, final())
		}
		r.endState()
	})
}
