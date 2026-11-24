package desktop

import (
	"slices"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

func folder(id int64, name string) storage.Folder {
	return storage.Folder{ID: id, Name: name}
}

// the run total is the sum of what each folder's plan asked for, and it grows
// as folders open rather than being guessed at the start.
func TestTallySumsFoldersAsTheyOpen(t *testing.T) {
	var tally syncTally
	tally.begin(3)

	if c := tally.counts(); c.Total != 0 || c.Done != 0 {
		t.Fatalf("a fresh run reports %d of %d, want 0 of 0", c.Done, c.Total)
	}

	tally.enterFolder("INBOX")
	c := tally.record(psync.FolderProgress{Folder: folder(1, "INBOX"), Done: 0, Total: 200})
	if c.Total != 200 || c.Done != 0 {
		t.Errorf("after the first plan: %d of %d, want 0 of 200", c.Done, c.Total)
	}

	c = tally.record(psync.FolderProgress{Folder: folder(1, "INBOX"), Done: 50, Total: 200})
	if c.Done != 50 || c.Total != 200 {
		t.Errorf("mid folder: %d of %d, want 50 of 200", c.Done, c.Total)
	}
	if c.FolderDone != 50 || c.FolderTotal != 200 {
		t.Errorf("folder counts %d of %d, want 50 of 200", c.FolderDone, c.FolderTotal)
	}

	// a second folder adds to the total rather than replacing it, and repeated
	// reports for the same folder do not double count.
	tally.finishFolder()
	tally.enterFolder("Archive")
	tally.record(psync.FolderProgress{Folder: folder(1, "INBOX"), Done: 200, Total: 200})
	c = tally.record(psync.FolderProgress{Folder: folder(2, "Archive"), Done: 10, Total: 40})
	if c.Total != 240 || c.Done != 210 {
		t.Errorf("across two folders: %d of %d, want 210 of 240", c.Done, c.Total)
	}
	if c.Folder != "Archive" || c.FoldersDone != 1 || c.FoldersTotal != 3 {
		t.Errorf("folder line = %q %d of %d, want Archive 1 of 3", c.Folder, c.FoldersDone, c.FoldersTotal)
	}
}

// a folder with nothing to fetch reports 0 of 0 rather than leaving the last
// folder's numbers standing, which would show a bar that never finishes.
func TestTallyClearsFolderCountsOnEntry(t *testing.T) {
	var tally syncTally
	tally.begin(2)
	tally.enterFolder("INBOX")
	tally.record(psync.FolderProgress{Folder: folder(1, "INBOX"), Done: 30, Total: 30})

	tally.enterFolder("Junk")
	c := tally.counts()
	if c.FolderDone != 0 || c.FolderTotal != 0 {
		t.Errorf("new folder starts at %d of %d, want 0 of 0", c.FolderDone, c.FolderTotal)
	}
	// the run total still holds what the first folder did.
	if c.Done != 30 || c.Total != 30 {
		t.Errorf("run counts %d of %d, want 30 of 30", c.Done, c.Total)
	}
}

// a second run starts from nothing, so yesterday's numbers cannot leak into it.
func TestTallyResetsBetweenRuns(t *testing.T) {
	var tally syncTally
	tally.begin(1)
	tally.enterFolder("INBOX")
	tally.record(psync.FolderProgress{Folder: folder(1, "INBOX"), Done: 100, Total: 100})
	tally.end()

	tally.begin(1)
	if c := tally.counts(); c.Done != 0 || c.Total != 0 || c.Folder != "" {
		t.Errorf("new run reports %q %d of %d, want empty 0 of 0", c.Folder, c.Done, c.Total)
	}
}

// Two runs of one account that overlap share the line: the first to end
// neither resets nor closes it, and sync:state stays running until the last.
func TestOverlappingRunsCloseTheLineOnce(t *testing.T) {
	a := &App{ctx: t.Context()}
	rec := &progressRecorder{}
	progress := rec.hook(1)
	var states []bool
	a.emitForTest = func(name string, payload any) {
		progress(name, payload)
		if e, ok := payload.(SyncStateEvent); ok && name == EventSyncState {
			states = append(states, e.Running)
		}
	}
	first := a.startSyncRun(t.Context(), 1, "a@example.test", "", 2)
	first.enter("INBOX")
	second := a.startSyncRun(t.Context(), 1, "a@example.test", "", 3)
	a.accountTally(1).record(psync.FolderProgress{Folder: storage.Folder{ID: 1, Name: "INBOX"}, Done: 1, Total: 4})
	first.finish(2)
	if n := closings(rec.snapshot()); n != 0 {
		t.Fatalf("the first run to end closed the line (%d closings)", n)
	}
	if c := a.accountTally(1).counts(); c.FoldersTotal != 5 || c.Total != 4 || c.Phase != SyncPhaseBodies {
		t.Fatalf("counts after the first run %+v, want 5 folders, 4 bodies, bodies phase", c)
	}
	second.finish(3)
	assertClosedOnce(t, rec.snapshot())
	if !slices.Equal(states, []bool{true, false}) {
		t.Fatalf("sync:state %v, want [true false]", states)
	}
}

// A run lists its folders side by side, so the folder count is the folders
// finished, not the position of the last one that started.
func TestTallyCountsFinishedFoldersOnly(t *testing.T) {
	var tally syncTally
	tally.begin(3)
	for _, name := range []string{"INBOX", "Sent", "Archive"} {
		tally.enterFolder(name)
	}
	if c := tally.counts(); c.FoldersDone != 0 {
		t.Fatalf("%d folders done with three still listing, want 0", c.FoldersDone)
	}
	tally.finishFolder()
	if c := tally.counts(); c.FoldersDone != 1 || c.FoldersTotal != 3 {
		t.Fatalf("folders %d of %d, want 1 of 3", c.FoldersDone, c.FoldersTotal)
	}
}
