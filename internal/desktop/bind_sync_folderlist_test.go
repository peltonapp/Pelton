package desktop

import (
	"context"
	"sync/atomic"
	"testing"
)

// countingList is a folder list that records how many runs overlap. The
// first run blocks until release is closed.
type countingList struct {
	running, peak, runs atomic.Int32
	firstIn             chan struct{}
	release             chan struct{}
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
	return nil
}

func TestFolderListCoalescesConcurrentRequests(t *testing.T) {
	app := &App{sessionPools: map[int64]*accountSessions{1: {}}}
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

// A cancelled list lets go of the folder without a follow-up, so the next
// request lists again.
func TestFolderListReleasesOnCancel(t *testing.T) {
	app := &App{sessionPools: map[int64]*accountSessions{1: {}}}
	ctx, cancel := context.WithCancel(context.Background())
	c := newCountingList()

	first := make(chan error, 1)
	go func() { first <- app.listFolderOnce(ctx, 1, 7, c.run) }()
	<-c.firstIn
	if err := app.listFolderOnce(ctx, 1, 7, c.run); err != nil {
		t.Fatalf("coalesced request: %v", err)
	}
	cancel()
	close(c.release)
	<-first
	if got := c.runs.Load(); got != 1 {
		t.Fatalf("lists after a cancel = %d, want 1", got)
	}
	if err := app.listFolderOnce(context.Background(), 1, 7, c.run); err != nil || c.runs.Load() != 2 {
		t.Fatalf("request after a cancel: runs %d, err %v; want 2, nil", c.runs.Load(), err)
	}
}
