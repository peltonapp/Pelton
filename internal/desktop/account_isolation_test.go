package desktop

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
)

// closeRecordingIMAP is a fakeIMAP that reports Close. Its Login can be made
// to block until release closes, ignoring Close, which is how a stuck FETCH on
// a slow server looks to the worker that owns it.
type closeRecordingIMAP struct {
	fakeIMAP
	closed    atomic.Bool
	loginGate chan struct{}
	loginErr  error
	loggingIn chan struct{}
}

func (c *closeRecordingIMAP) Close() error { c.closed.Store(true); return nil }

func (c *closeRecordingIMAP) Login() error {
	if c.loggingIn != nil {
		close(c.loggingIn)
	}
	if c.loginGate != nil {
		<-c.loginGate
	}
	return c.loginErr
}

// Stopping one account's worker must not close another account's IMAP
// sessions or cancel its scheduler tasks. Before per-account runtimes the abort
// closed every tracked session in the app, so both mailboxes reported a failed
// sync after one was stopped. A normal stop leaves A's running task alone (it
// ends at its own end); only the hard stop of account removal closes A's
// session.
func TestStopAccountWorkerLeavesOtherAccountSessionsAndJobs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		hard        bool
		wantAClosed bool
	}{
		{"normal stop", false, false},
		{"removal abort", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := stopAccountWorkerTimeout
			stopAccountWorkerTimeout = 100 * time.Millisecond
			t.Cleanup(func() { stopAccountWorkerTimeout = old })
			r := newTwoAccountRig(t)
			a, idA, idB := r.a, r.idA, r.idB
			jobB := r.park(t, idB) // a task running on B's scheduler

			if tc.hard {
				a.abortAccountWorker(idA)
			} else {
				a.stopAccountWorker(idA)
			}

			if got := r.sessionClosed(t, r.accA); got != tc.wantAClosed {
				t.Fatalf("A's session closed = %v, want %v", got, tc.wantAClosed)
			}
			if r.sessionClosed(t, r.accB) {
				t.Fatal("B's session was closed by A's stop")
			}
			noRecv(t, jobB.cancelled, 100*time.Millisecond, "B's task was cancelled by A's stop")
			if a.accountSync(idB) == nil {
				t.Fatal("B's scheduler was removed by A's stop")
			}
			if a.accountSync(idA) != nil {
				t.Fatal("A's scheduler survived A's stop")
			}
		})
	}
}

// A worker that outlives stopAccountWorker's wait must not tear down the
// scheduler its replacement started. Before, the late exit deleted the map
// entry by account id, so Sync did nothing after the worker was replaced.
func TestLateOldWorkerExitKeepsNewScheduler(t *testing.T) {
	old := stopAccountWorkerTimeout
	stopAccountWorkerTimeout = 50 * time.Millisecond
	t.Cleanup(func() { stopAccountWorkerTimeout = old })

	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "late@example.test")

	// The first connection's Login hangs past the stop timeout and ignores
	// Close. Later connections are refused at once, so the new worker settles
	// into its retry wait holding its scheduler.
	stuck := &closeRecordingIMAP{
		loginGate: make(chan struct{}),
		loginErr:  errors.New("aborted"),
		loggingIn: make(chan struct{}),
	}
	var calls atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		if calls.Add(1) == 1 {
			return stuck, nil
		}
		return &closeRecordingIMAP{loginErr: errors.New("refused")}, nil
	}
	var releaseOnce sync.Once
	releaseStuck := func() { releaseOnce.Do(func() { close(stuck.loginGate) }) }
	t.Cleanup(releaseStuck)

	a.startAccountWorker(id)
	recv(t, stuck.loggingIn, 2*time.Second, "first worker never reached login")
	a.workersMu.Lock()
	oldDone := a.workers[id].done
	a.workersMu.Unlock()

	a.startAccountWorker(id) // waits stopAccountWorkerTimeout, then gives up
	var rt *accountSync
	waitUntil(t, 2*time.Second, "new worker never started a scheduler", func() bool {
		rt = a.accountSync(id)
		return rt != nil
	})

	releaseStuck()
	recv(t, oldDone, 2*time.Second, "old worker never exited")

	if got := a.accountSync(id); got != rt {
		t.Fatalf("scheduler after late exit = %p, want new worker's %p", got, rt)
	}
	if rt.sched.Stopped() {
		t.Fatal("new worker's scheduler was stopped by the old worker's exit")
	}
}

// A scheduler first started by one sync run must keep serving the account
// after that run's context ends. Bound to the run, it died with it and the
// next run's jobs waited on a queue nobody served.
func TestSchedulerOutlivesTheRunThatStartedIt(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "run@example.test")
	useFakeIMAP(a)
	t.Cleanup(func() { a.stopAccountScheduler(id, false) })

	runCtx, endRun := context.WithCancel(a.ctx)
	a.ensureAccountSync(runCtx, id)
	endRun()

	acc, err := a.store.GetAccount(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan struct{})
	err = a.runPooled(a.ctx, *acc, poolTask{Kind: syncsched.KindListStubs, Class: syncsched.ClassManualSync, Run: func(context.Context, mailClient) error {
		close(ran)
		return nil
	}})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	select {
	case <-ran:
	default:
		t.Fatal("second run's job never ran")
	}
}
