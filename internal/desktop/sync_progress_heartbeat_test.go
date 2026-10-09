package desktop

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	pimap "github.com/peltonapp/Pelton/internal/imap"
)

func runningEvents(events []SyncProgressEvent) int {
	return len(events) - closings(events)
}

// waitForEvents polls until the recorder holds at least n events.
func waitForEvents(t *testing.T, rec *progressRecorder, n int) []SyncProgressEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ev := rec.snapshot(); len(ev) >= n {
			return ev
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("got %d progress events, want at least %d", len(rec.snapshot()), n)
	return nil
}

func TestProgressHeartbeatResendsLatestRunningEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &App{}
		rec := &progressRecorder{}
		a.syncProgressEmitForTest = rec.hook(1)

		stop := a.startProgressHeartbeat(context.Background(), 1)
		t.Cleanup(stop)
		a.emitSyncProgress(1, "a@example.test", "", syncCounts{Folder: "INBOX", FoldersTotal: 2})
		synctest.Sleep(4 * syncProgressHeartbeat)
		ev := rec.snapshot()
		if len(ev) != 5 {
			t.Fatalf("got %d progress events, want the first one and 4 heartbeats", len(ev))
		}
		for _, e := range ev {
			if e.Folder != "INBOX" || e.FoldersTotal != 2 {
				t.Fatalf("heartbeat sent %+v, want the latest running event", e)
			}
		}

		stop()
		stop()
		a.emitSyncProgress(1, "a@example.test", "", syncCounts{})
		synctest.Sleep(2 * syncProgressHeartbeat)
		ev = rec.snapshot()
		if ev[len(ev)-1].Folder != "" || closings(ev) != 1 {
			t.Fatalf("events after stop %+v, want the closing one last", ev[len(ev)-1:])
		}
	})
}

func TestProgressHeartbeatSilentAfterClosingEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &App{}
		rec := &progressRecorder{}
		a.syncProgressEmitForTest = rec.hook(1)

		a.emitSyncProgress(1, "a@example.test", "", syncCounts{Folder: "INBOX"})
		a.emitSyncProgress(1, "a@example.test", "", syncCounts{})
		stop := a.startProgressHeartbeat(context.Background(), 1)
		synctest.Sleep(2 * syncProgressHeartbeat)
		stop()
		if ev := rec.snapshot(); len(ev) != 2 {
			t.Fatalf("events = %+v, want no re-send after a closing event", ev)
		}
	})
}

func TestProgressHeartbeatIsPerAccount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &App{}
		other := &progressRecorder{}
		a.syncProgressEmitForTest = other.hook(2)

		a.emitSyncProgress(2, "b@example.test", "", syncCounts{Folder: "INBOX"})
		stop := a.startProgressHeartbeat(context.Background(), 1)
		synctest.Sleep(2 * syncProgressHeartbeat)
		stop()
		if ev := other.snapshot(); len(ev) != 1 {
			t.Fatalf("account 2 events = %d, want 1: account 1's heartbeat re-sent it", len(ev))
		}
	})
}

// A run parked in one step keeps its line alive, and nothing running follows
// the closing event once the run ends.
func TestIMAPRunHeartbeatKeepsProgressAlive(t *testing.T) {
	a := newHoldTestApp(t)
	a.progressHeartbeatEvery = 5 * time.Millisecond
	id, _ := seedHoldAccount(t, a, "a@example.test")
	acc, err := a.store.GetAccount(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		return &blockingIMAP{gate: gate}, nil
	}
	rec := &progressRecorder{}
	a.syncProgressEmitForTest = rec.hook(id)

	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.syncIMAPInitialPass(ctx, *acc, nil) }()

	// the run emits once on entering the folder and then blocks in SELECT; the
	// rest can only be heartbeats.
	ev := waitForEvents(t, rec, 6)
	if runningEvents(ev) < 6 {
		t.Fatalf("events %+v, want re-sent running events while blocked", ev)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
	time.Sleep(30 * time.Millisecond)
	ev = rec.snapshot()
	if ev[len(ev)-1].Folder != "" {
		t.Fatalf("last progress event %+v, want a closing one", ev[len(ev)-1])
	}
	if n := closings(ev); n != 1 {
		t.Fatalf("closing events = %d, want 1", n)
	}
}
