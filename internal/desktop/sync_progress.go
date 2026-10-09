package desktop

import (
	"sync"

	psync "github.com/peltonapp/Pelton/internal/sync"
)

// A phased initial sync reports stubs before bodies so the status bar can say
// which phase is running (#313). Verify is the background full reconcile.
const (
	SyncPhaseStubs  = "stubs"
	SyncPhaseBodies = "bodies"
	SyncPhaseVerify = "verify"
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
	// mailboxes finished and how many this run covers.
	foldersDone  int
	foldersTotal int
}

// begin starts a run over n folders, dropping whatever the last one left.
func (t *syncTally) begin(folders int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.done = make(map[int64]int, folders)
	t.total = make(map[int64]int, folders)
	t.folderID, t.folderName = 0, ""
	t.folderDone, t.folderTotal = 0, 0
	t.foldersDone, t.foldersTotal = 0, folders
}

// enterFolder records that a folder is now the one being worked on.
func (t *syncTally) enterFolder(index int, name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.foldersDone = index
	t.folderName = name
	t.folderDone, t.folderTotal = 0, 0
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
	c.Folder = ""
	return c
}

func (t *syncTally) countsLocked() syncCounts {
	c := syncCounts{
		Folder:       t.folderName,
		FolderDone:   t.folderDone,
		FolderTotal:  t.folderTotal,
		FoldersDone:  t.foldersDone,
		FoldersTotal: t.foldersTotal,
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
	// Phase is stubs, bodies, or empty when the run is not split (IMAP manual sync).
	Phase string
}
