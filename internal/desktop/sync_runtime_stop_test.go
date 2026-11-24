package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
)

// A profile switch while list tasks finish does not wait for the stop
// timeout. A list's Done looks the account's runtime up on the scheduler's
// own worker; it finds the runtime stale and must not stop it there, which
// would wait for that very worker until the timeout, and the switch with it.
func TestProfileSwitchWhileListTasksFinishDoesNotWait(t *testing.T) {
	old := stopAccountWorkerTimeout
	stopAccountWorkerTimeout = 3 * time.Second
	t.Cleanup(func() { stopAccountWorkerTimeout = old })
	a := newHoldTestApp(t)
	id, fid := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	rt := mustAccountSync(t, a, id)

	started, release, doneRan := make(chan struct{}), make(chan struct{}), make(chan struct{})
	rt.sched.Enqueue(imapTask{
		Kind: syncsched.KindListStubs, FolderID: fid, Key: folderKey(syncsched.KindListStubs, fid),
		Run: func(context.Context, mailClient) error {
			close(started)
			<-release
			return nil
		},
		Done: func(error) {
			// What a list's follow-up (its bodies, say) does.
			_, _ = a.ensureAccountSync(a.ctx, id)
			close(doneRan)
		},
	})
	recv(t, started, 2*time.Second, "list never started")

	// The account's worker: busy until the list's Done has run (a first
	// sync waiting for its plan), then it stops its runtime as it exits.
	wctx, cancel := context.WithCancel(context.Background())
	wdone := make(chan struct{})
	a.workersMu.Lock()
	a.workers[id] = &accountWorker{cancel: cancel, done: wdone}
	a.workersMu.Unlock()
	go func() {
		defer close(wdone)
		<-wctx.Done()
		<-doneRan
		a.stopAccountSchedulerIf(id, rt)
	}()

	switched := make(chan time.Duration, 1)
	go func() {
		begin := time.Now()
		a.endProfileSession()
		switched <- time.Since(begin)
	}()
	waitUntil(t, 2*time.Second, "the session never ended", func() bool { return rt.ctx.Err() != nil })
	close(release)
	took := recv(t, switched, 10*time.Second, "the profile switch never ended")
	if took >= time.Second {
		t.Fatalf("the profile switch took %v, want it not to wait for the stop timeout", took)
	}
}

// A load-older listing still queued when its account's scheduler stops is
// dropped with it, and its Done queues nothing: looking the runtime up
// there built a new one for an account whose sync had just been stopped (a
// profile switch, say).
func TestStoppedOlderListingBuildsNoRuntime(t *testing.T) {
	a := newHoldTestApp(t)
	id, fid := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	setParallel(t, a, 1)
	rt := mustAccountSync(t, a, id)
	account := mustAccount(t, a, id)
	folder, err := a.store.GetFolder(a.ctx, fid)
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	recv(t, signalJob(rt.sched, syncsched.KindBodies, 0, release), 2*time.Second, "blocking task never started")
	a.enqueueOlderPages(account, *folder, newOlderList(10, 50, 500))

	stopped := make(chan struct{})
	go func() {
		a.stopAccountScheduler(id, false)
		close(stopped)
	}()
	waitUntil(t, 2*time.Second, "the scheduler never stopped", rt.sched.Stopped)
	close(release)
	recv(t, stopped, 5*time.Second, "the stop never returned")
	if got := a.accountSync(id); got != nil {
		t.Fatal("a runtime was built for the account after its scheduler stopped")
	}
}
