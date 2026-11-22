package desktop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/sync/throttle"
)

// R9-3 regression tests: a large opened message is fetched in chunks by a
// normal-lane task (fetchLargeBodyOnDemand). Each chunk is one FETCH; its
// outcome must reach the throttle once, from the unit that ran it, never
// once per caller waiting on a joined task, and not for a unit that skipped
// a stale task.

// limitChunkIMAP serves partial fetches from pagingIMAP, or refuses them
// with a connection-limit NO while fail is set.
type limitChunkIMAP struct {
	pagingIMAP
	cmu   sync.Mutex
	fail  bool
	calls int
}

func (c *limitChunkIMAP) FetchPartial(uid imap.UID, offset, length int64) (*pimap.Message, error) {
	c.cmu.Lock()
	c.calls++
	fail := c.fail
	c.cmu.Unlock()
	if fail {
		return nil, errors.New("NO [LIMIT] too many open connections")
	}
	return c.pagingIMAP.FetchPartial(uid, offset, length)
}

func (c *limitChunkIMAP) setFail(fail bool) {
	c.cmu.Lock()
	c.fail = fail
	c.cmu.Unlock()
}

func (c *limitChunkIMAP) callCount() int {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return c.calls
}

// largeOpenRig is an account at N=4 with one stub of three chunks' size.
func largeOpenRig(t *testing.T) (*App, *limitChunkIMAP, *accountSync, *storage.Message) {
	t.Helper()
	c := &limitChunkIMAP{pagingIMAP: pagingIMAP{n: 1, sources: map[imap.UID][]byte{}}, fail: true}
	a, account, folder := reconcileInbox(t, c)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, account.ID)
	budget := rt.sched.ChunkBudget().Get()
	size := 2*budget + budget/2
	c.sources[1] = bigSource(int(size))
	seedStub(t, a, folder, 1, size)
	id, err := a.store.MessageIDByRemoteID(a.ctx, folder.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return a, c, rt, m
}

// throttleCounts records the outcomes the throttle counts from here on, by
// kind, until the test ends.
func throttleCounts(t *testing.T) func() map[throttle.Kind]int {
	t.Helper()
	var mu sync.Mutex
	counts := map[throttle.Kind]int{}
	afterThrottleStep = func(sig throttle.Signal) {
		mu.Lock()
		counts[sig.Kind]++
		mu.Unlock()
	}
	t.Cleanup(func() { afterThrottleStep = nil })
	return func() map[throttle.Kind]int {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[throttle.Kind]int, len(counts))
		for k, n := range counts {
			out[k] = n
			delete(counts, k)
		}
		return out
	}
}

// Two refused chunk FETCHes halve N from 4 to 2; a stale open (the body
// came another way) is no server operation and counts as nothing; a
// successful open of three chunks counts exactly three successes, and with a
// probe due they grow N.
func TestLargeOpenOutcomesReachTheThrottle(t *testing.T) {
	a, c, rt, m := largeOpenRig(t)
	counted := throttleCounts(t)
	for range 2 {
		if err := a.fetchMessageBodyOnDemand(m); err == nil {
			t.Fatal("expected the refused chunk to fail the open")
		}
	}
	if c.callCount() != 2 {
		t.Fatalf("fixture: %d partial fetches, want 2", c.callCount())
	}
	if n := rt.pool.Effective(); n != 2 {
		t.Fatalf("two refused large-open chunks left N=%d, want 2", n)
	}
	if got := counted(); got[throttle.Refused] != 2 || len(got) != 1 {
		t.Fatalf("two refused chunks counted %v, want 2 refusals", got)
	}

	// A probe is due from here on: one success counted raises N to 3.
	advanceThrottleClock(rt.shared, time.Hour)

	// Stale: the caller's copy is a stub, the row is complete.
	if _, err := a.store.FillMessageBody(a.ctx, storage.BodyTarget{}, &storage.Message{AccountID: m.AccountID, FolderID: m.FolderID, UID: m.UID, RemoteID: m.RemoteID, BodyPlain: "came by sync"}, nil); err != nil {
		t.Fatal(err)
	}
	for range syncThrottleRecoverThreshold {
		if err := a.fetchMessageBodyOnDemand(m); err != nil {
			t.Fatal(err)
		}
	}
	if c.callCount() != 2 {
		t.Fatalf("a stale open fetched: %d partial fetches", c.callCount())
	}
	if n := rt.pool.Effective(); n != 2 {
		t.Fatalf("stale opens counted as successes: N=%d, want 2", n)
	}
	if got := counted(); len(got) != 0 {
		t.Fatalf("stale opens counted %v, want nothing", got)
	}

	// A successful open of three chunks is three good operations.
	if err := a.store.DeleteMessage(a.ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		t.Fatal(err)
	}
	seedStub(t, a, *folder, 1, m.SizeBytes)
	id, err := a.store.MessageIDByRemoteID(a.ctx, m.FolderID, "1")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	c.setFail(false)
	if err := a.fetchMessageBodyOnDemand(fresh); err != nil {
		t.Fatal(err)
	}
	if got := c.callCount() - 2; got != syncThrottleRecoverThreshold {
		t.Fatalf("fixture: the open took %d chunks, want %d", got, syncThrottleRecoverThreshold)
	}
	if got := counted(); got[throttle.OK] != syncThrottleRecoverThreshold || len(got) != 1 {
		t.Fatalf("an open of %d good chunks counted %v, want exactly %d successes", syncThrottleRecoverThreshold, got, syncThrottleRecoverThreshold)
	}
	if n := rt.pool.Effective(); n != 3 {
		t.Fatalf("three good chunks left N=%d, want 3", n)
	}
}

// Two callers opening the same large message join one task: its one
// refused FETCH counts once, so a single later refusal is what halves N.
func TestLargeOpenJoinedWaitersCountOnce(t *testing.T) {
	a, c, rt, m := largeOpenRig(t)
	started, release := make(chan struct{}), make(chan struct{})
	rt.sched.Enqueue(imapTask{Kind: syncsched.KindListStubs, FolderID: m.FolderID, Barrier: true, Run: func(context.Context, mailClient) error {
		close(started)
		<-release
		return nil
	}})
	recv(t, started, 2*time.Second, "the barrier never ran")
	out := make(chan error, 2)
	for range 2 {
		go func() { out <- a.fetchMessageBodyOnDemand(m) }()
	}
	waitUntil(t, 2*time.Second, "the two opens never joined one queued task", func() bool {
		return rt.sched.Stats().Normal == 1
	})
	time.Sleep(50 * time.Millisecond) // the second Enqueue lands in the same entry
	if st := rt.sched.Stats(); st.Normal != 1 {
		close(release)
		t.Fatalf("fixture: want one joined queued open, got %+v", st)
	}
	close(release)
	for range 2 {
		if err := recv(t, out, 5*time.Second, "an open never returned"); err == nil {
			t.Fatal("expected the refused chunk to fail both callers")
		}
	}
	if c.callCount() != 1 {
		t.Fatalf("fixture: %d partial fetches, want 1", c.callCount())
	}
	if n := rt.pool.Effective(); n != 4 {
		t.Fatalf("one refused FETCH joined by two callers changed N to %d, want 4", n)
	}
	if err := a.fetchMessageBodyOnDemand(m); err == nil {
		t.Fatal("expected the refused chunk to fail the open")
	}
	if n := rt.pool.Effective(); n != 2 {
		t.Fatalf("the joined refusal was not counted: N=%d after a second refusal, want 2", n)
	}
}
