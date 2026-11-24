package desktop

import "testing"

// A body run's end forgets the folder's width estimate only while no later
// run of the folder started, in any runtime of the account: the estimate
// lives in the account's shared sessions, so the old runtime's generator
// ending after the new runtime's run began must not delete what the new
// run's first unit told.
func TestBodyRunEndKeepsANewerRunsEstimate(t *testing.T) {
	shared := &accountSessions{}
	old := &accountSync{shared: shared}
	next := &accountSync{shared: shared}
	const folder = 7

	oldEnded := old.bodyRun(folder)
	nextEnded := next.bodyRun(folder)
	shared.setBodyBacklog(folder, 3) // the new run's first unit
	oldEnded()
	if got := shared.bodyBacklog(folder); got != 3 {
		t.Fatalf("estimate %d after the old run ended, want the new run's 3", got)
	}
	nextEnded()
	if got := shared.bodyBacklog(folder); got != 1 {
		t.Fatalf("estimate %d after the last run ended, want it forgotten (1)", got)
	}

	// The same within one runtime.
	first := next.bodyRun(folder)
	second := next.bodyRun(folder)
	shared.setBodyBacklog(folder, 4)
	first()
	if got := shared.bodyBacklog(folder); got != 4 {
		t.Fatalf("estimate %d after an older run ended, want the newer run's 4", got)
	}
	second()
	if got := shared.bodyBacklog(folder); got != 1 {
		t.Fatalf("estimate %d after the last run ended, want it forgotten (1)", got)
	}
}

// A folder's run state is dropped once its last run ended, so the map does
// not grow with every folder ever synced; a new run starts afresh.
func TestBodyRunStateIsPrunedAtTheLastEnd(t *testing.T) {
	rt := &accountSync{shared: &accountSessions{}}
	ended := rt.bodyRun(7)
	if _, ok := rt.bodyRuns[7]; !ok {
		t.Fatal("no run state while the run is going")
	}
	ended()
	if n := len(rt.bodyRuns); n != 0 {
		t.Fatalf("%d run states after the last run ended, want 0", n)
	}
	if n := len(rt.shared.backlogRun); n != 0 {
		t.Fatalf("%d shared run marks after the last run ended, want 0", n)
	}
}
