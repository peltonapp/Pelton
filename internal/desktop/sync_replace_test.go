package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
)

// A runtime replaced while one of its list units still runs keeps that
// folder: the replacement's list of the same folder starts only once the
// old unit ended, while work on another folder goes ahead at once. The
// folder-list claim the old unit holds is the account's, so the
// replacement sees it too.
func TestReplacementWaitsForTheOldRuntimesListOfTheFolder(t *testing.T) {
	old := stopAccountWorkerTimeout
	stopAccountWorkerTimeout = 20 * time.Millisecond
	t.Cleanup(func() { stopAccountWorkerTimeout = old })
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	setParallel(t, a, 3)
	useFakeIMAP(a)
	const folderX, folderY = 101, 102

	oldRT := mustAccountSync(t, a, id)
	release := make(chan struct{})
	oldStarted := make(chan struct{})
	oldRT.sched.Enqueue(imapTask{Kind: syncsched.KindListStubs, FolderID: folderX, Run: func(context.Context, mailClient) error {
		tok := a.accountShared(id).claimFolderList(folderX)
		if tok == 0 {
			t.Error("old list could not claim its folder")
		}
		close(oldStarted)
		<-release
		a.accountShared(id).releaseFolderList(folderX, tok)
		return nil
	}})
	recv(t, oldStarted, 2*time.Second, "old list never started")
	// The stop gives up waiting while the old list still runs.
	a.stopAccountScheduler(id, false)
	newRT := mustAccountSync(t, a, id)
	if newRT == oldRT {
		t.Fatal("no new runtime")
	}
	if a.accountShared(id).claimFolderList(folderX) != 0 {
		t.Fatal("the replacement claimed a folder the old runtime is still listing")
	}

	sameFolder := signalJob(newRT.sched, syncsched.KindListStubs, folderX, nil)
	otherFolder := signalJob(newRT.sched, syncsched.KindListStubs, folderY, nil)
	recv(t, otherFolder, 2*time.Second, "a list of another folder waited for the old runtime")
	noRecv(t, sameFolder, 200*time.Millisecond, "the replacement listed the folder beside the old runtime's list")
	close(release)
	recv(t, sameFolder, 2*time.Second, "the replacement's list never started after the old one ended")
}
