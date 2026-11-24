package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
)

// passSteps records a plan's steps. Inbox's body generator never ends (a
// "download everything" backlog): every batch takes a moment and leaves
// more. The other folders' bodies are in at once.
type passSteps struct {
	mu    sync.Mutex
	steps []string
}

func (p *passSteps) step(ctx context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
	name := initialSyncStep(folder, kind)
	p.mu.Lock()
	p.steps = append(p.steps, name)
	p.mu.Unlock()
	if name != "bodies:INBOX" {
		return nil
	}
	select {
	case <-time.After(5 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	return syncsched.ErrAgain
}

func (p *passSteps) count(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, s := range p.steps {
		if s == name {
			n++
		}
	}
	return n
}

// A timed or manual pass waits for the folder lists only: Inbox's body
// backlog goes on in the background, so the pass returns and the next tick
// lists Inbox and the other folders again. Before, the pass waited for every
// body and a big mailbox blocked timed sync for hours. At N=1 the second
// pass's Inbox bodies join the generator still queued from the first.
func TestTimedPassReturnsAfterListsWhileBodiesRun(t *testing.T) {
	for _, n := range []int{1, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			a := newHoldTestApp(t)
			id, _ := seedHoldAccount(t, a, "a@example.test")
			if _, err := a.store.CreateFolder(a.ctx, &storage.Folder{
				AccountID: id, Name: "Archive", IMAPPath: "Archive", RemoteID: "Archive",
			}); err != nil {
				t.Fatal(err)
			}
			setParallel(t, a, n)
			useFakeIMAP(a)
			account := mustAccount(t, a, id)
			t.Cleanup(func() { a.stopAccountScheduler(id, false) })
			p := &passSteps{}
			pass := func() <-chan error {
				out := make(chan error, 1)
				go func() { out <- a.runSyncPlan(a.ctx, account, syncsched.ClassAuto, p.step, nil, false) }()
				return out
			}
			if err := recv(t, pass(), 5*time.Second, "the first pass waited for Inbox's whole body backlog"); err != nil {
				t.Fatalf("first pass: %v", err)
			}
			if err := recv(t, pass(), 5*time.Second, "the second pass waited for Inbox's whole body backlog"); err != nil {
				t.Fatalf("second pass: %v", err)
			}
			for _, name := range []string{"stubs:INBOX", "stubs:Archive"} {
				if got := p.count(name); got != 2 {
					t.Fatalf("%s ran %d times, want 2 (once per pass)", name, got)
				}
			}
			if p.count("bodies:INBOX") == 0 {
				t.Fatal("Inbox's bodies never started")
			}
		})
	}
}

// A second plan while the first one's Inbox body generator runs joins that
// generator: never two Inbox body batches at once, no batch beyond the
// backlog but the joined task's one closing check, and the second plan
// lists the other folders while the backlog is still going. Both plans
// report done when the generator ends.
func TestSecondPlanJoinsRunningBodyGenerator(t *testing.T) {
	for _, n := range []int{1, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const backlog = 20
				var mu sync.Mutex
				var steps []string
				left, running, most, runs := backlog, 0, 0, 0
				archiveListedAt := -1 // Inbox batches left when the second plan listed Archive
				second := false
				step := func(_ context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
					name := initialSyncStep(folder, kind)
					mu.Lock()
					steps = append(steps, name)
					if name == "stubs:Archive" && second {
						archiveListedAt = left
					}
					if name != "bodies:INBOX" {
						mu.Unlock()
						return nil
					}
					runs++
					running++
					most = max(most, running)
					mu.Unlock()
					time.Sleep(time.Second)
					mu.Lock()
					defer mu.Unlock()
					running--
					if left == 0 {
						return nil
					}
					left--
					return syncsched.ErrAgain
				}
				s := newTestScheduler(t, n)
				done1 := make(chan error, 1)
				enqueueSyncPlan(context.Background(), s, initialSyncFolders(), syncPlan{done: done1, step: step})
				time.Sleep(3500 * time.Millisecond) // a few Inbox batches in
				synctest.Wait()
				mu.Lock()
				second = true
				mu.Unlock()
				done2 := make(chan error, 1)
				enqueueSyncPlan(context.Background(), s, initialSyncFolders(), syncPlan{done: done2, step: step})
				for i, done := range []chan error{done1, done2} {
					if err := recv(t, done, time.Hour, fmt.Sprintf("plan %d never ended", i+1)); err != nil {
						t.Fatalf("plan %d: %v", i+1, err)
					}
				}
				mu.Lock()
				defer mu.Unlock()
				if most != 1 {
					t.Fatalf("%d Inbox body batches ran at once, want 1: %v", most, steps)
				}
				if runs > backlog+2 {
					t.Fatalf("%d Inbox body batches for a backlog of %d: a second generator ran", runs, backlog)
				}
				if n := strings.Count(strings.Join(steps, ","), "stubs:INBOX"); n != 2 {
					t.Fatalf("Inbox listed %d times, want 2: %v", n, steps)
				}
				if archiveListedAt <= 0 {
					t.Fatalf("the second plan listed Archive only after Inbox's backlog (left %d): %v", archiveListedAt, steps)
				}
			})
		})
	}
}

// Bodies that fail after the pass returned still mark the account failed,
// as when the pass waited for them, and the progress line they reopened is
// closed once they end.
func TestPassBodyFailureAfterReturnIsRecorded(t *testing.T) {
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	useFakeIMAP(a)
	account := mustAccount(t, a, id)
	t.Cleanup(func() { a.stopAccountScheduler(id, false) })
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(id)
	start, release := make(chan struct{}), make(chan struct{})
	step := gatedBodies(a, account, start, release, errors.New("NO server broke"))
	if err := a.runSyncPlan(a.ctx, account, syncsched.ClassAuto, step, nil, false); err != nil {
		t.Fatalf("pass: %v", err)
	}
	close(start) // the bodies report progress after the pass closed its line
	if states, _ := a.store.AccountSyncStates(a.ctx); len(states) > 0 && states[0].Failing() {
		t.Fatalf("the account failed before its bodies did: %+v", states)
	}
	closed := closings(rec.snapshot())
	close(release)
	waitUntil(t, 5*time.Second, "the failed bodies were not recorded", func() bool {
		states, err := a.store.AccountSyncStates(a.ctx)
		return err == nil && len(states) == 1 && states[0].Failing() && states[0].Reason == syncFailOther
	})
	waitUntil(t, 5*time.Second, "the bodies' progress line was not closed", func() bool {
		return closings(rec.snapshot()) > closed
	})
}

// listed reports once every folder's list has ended, with the folders whose
// list failed, while a folder's bodies still run; done waits for them.
func TestPlanListedReportsListFailures(t *testing.T) {
	useFastSyncRetryBackoff(t)
	synctest.Test(t, func(t *testing.T) {
		failed := errors.New("NO list")
		release := make(chan struct{})
		var inboxBatches atomic.Int32
		s := newTestScheduler(t, 3)
		listed, done := make(chan error, 1), make(chan error, 1)
		enqueueSyncPlan(context.Background(), s, initialSyncFolders(), syncPlan{listed: listed, done: done,
			step: func(_ context.Context, _ mailClient, folder storage.Folder, kind syncsched.Kind) error {
				switch initialSyncStep(folder, kind) {
				case "stubs:Archive":
					return failed
				case "bodies:INBOX":
					if inboxBatches.Add(1) == 1 {
						return syncsched.ErrAgain // the first batch releases Archive
					}
					<-release
				}
				return nil
			}})
		err := recv(t, listed, time.Minute, "listed never reported")
		if !errors.Is(err, failed) || !strings.Contains(err.Error(), "Archive") {
			t.Fatalf("listed = %v, want Archive's list failure", err)
		}
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("done = %v while Inbox's bodies still run", err)
		default:
		}
		close(release)
		if err := recv(t, done, time.Minute, "done never reported"); !errors.Is(err, failed) {
			t.Fatalf("done = %v, want Archive's list failure", err)
		}
	})
}
