package desktop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	psync "github.com/peltonapp/Pelton/internal/sync"
)

// countingList is a folder list that records how many runs overlap. The
// first run blocks until release is closed.
type countingList struct {
	running, peak, runs atomic.Int32
	firstIn             chan struct{}
	release             chan struct{}
	paused              atomic.Bool
}

func newCountingList() *countingList {
	return &countingList{firstIn: make(chan struct{}), release: make(chan struct{})}
}

func (c *countingList) run() error {
	n := c.running.Add(1)
	defer c.running.Add(-1)
	for {
		p := c.peak.Load()
		if n <= p || c.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if c.runs.Add(1) == 1 {
		close(c.firstIn)
		<-c.release
	}
	if c.paused.Load() {
		return psync.ErrSoftPaused
	}
	return nil
}

func TestFolderListCoalescesConcurrentRequests(t *testing.T) {
	app := &App{syncs: map[int64]*accountSync{1: {ended: make(chan struct{})}}}
	ctx := context.Background()
	c := newCountingList()

	first := make(chan error, 1)
	go func() { first <- app.listFolderOnce(ctx, 1, 7, c.run) }()
	<-c.firstIn

	// Two more requests while the first is blocked: neither may list now.
	for range 2 {
		if err := app.listFolderOnce(ctx, 1, 7, c.run); err != nil {
			t.Fatalf("coalesced request: %v", err)
		}
	}
	if got := c.runs.Load(); got != 1 {
		t.Fatalf("lists started while the first runs = %d, want 1", got)
	}
	// Another folder is not held back by this one.
	other := newCountingList()
	close(other.release)
	if err := app.listFolderOnce(ctx, 1, 8, other.run); err != nil || other.runs.Load() != 1 {
		t.Fatalf("other folder runs = %d, err %v; want 1, nil", other.runs.Load(), err)
	}

	close(c.release)
	if err := <-first; err != nil {
		t.Fatalf("first request: %v", err)
	}
	if got := c.runs.Load(); got != 2 {
		t.Fatalf("lists after the first ended = %d, want 2 (exactly one follow-up)", got)
	}
	if got := c.peak.Load(); got != 1 {
		t.Fatalf("concurrent lists of one folder = %d, want 1", got)
	}

	if err := app.listFolderOnce(ctx, 1, 7, c.run); err != nil {
		t.Fatalf("request after completion: %v", err)
	}
	if got := c.runs.Load(); got != 3 {
		t.Fatalf("request after completion ran %d lists in total, want 3", got)
	}
}

// A soft-paused list is requeued by the scheduler and lists again, so it
// lets go of the folder rather than running a follow-up of its own.
func TestFolderListReleasesOnSoftPause(t *testing.T) {
	app := &App{syncs: map[int64]*accountSync{1: {ended: make(chan struct{})}}}
	ctx := context.Background()
	c := newCountingList()
	c.paused.Store(true)

	first := make(chan error, 1)
	go func() { first <- app.listFolderOnce(ctx, 1, 7, c.run) }()
	<-c.firstIn
	if err := app.listFolderOnce(ctx, 1, 7, c.run); err != nil {
		t.Fatalf("coalesced request: %v", err)
	}
	close(c.release)
	if err := <-first; !errors.Is(err, psync.ErrSoftPaused) {
		t.Fatalf("first request err %v, want ErrSoftPaused", err)
	}
	if got := c.runs.Load(); got != 1 {
		t.Fatalf("lists after a soft pause = %d, want 1", got)
	}
	c.paused.Store(false)
	if err := app.listFolderOnce(ctx, 1, 7, c.run); err != nil || c.runs.Load() != 2 {
		t.Fatalf("request after a soft pause: runs %d, err %v; want 2, nil", c.runs.Load(), err)
	}
}
