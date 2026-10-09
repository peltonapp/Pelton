package desktop

import (
	"context"
	"sync"
	"testing"

	pimap "github.com/peltonapp/Pelton/internal/imap"
)

// progressRecorder collects the progress events of one account and cancels the
// run when its first non-closing event arrives.
type progressRecorder struct {
	mu     sync.Mutex
	events []SyncProgressEvent
	cancel context.CancelFunc
}

func (r *progressRecorder) hook(accountID int64) func(SyncProgressEvent) {
	return func(e SyncProgressEvent) {
		if e.AccountID != accountID {
			return
		}
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
		if e.Folder != "" && r.cancel != nil {
			r.cancel()
		}
	}
}

func (r *progressRecorder) snapshot() []SyncProgressEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SyncProgressEvent(nil), r.events...)
}

func closings(events []SyncProgressEvent) int {
	n := 0
	for _, e := range events {
		if e.Folder == "" {
			n++
		}
	}
	return n
}

// blockingIMAP parks SELECT until gate closes, so a run is always mid-folder
// when the test cancels it.
type blockingIMAP struct {
	fakeIMAP
	gate chan struct{}
}

func (b *blockingIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	<-b.gate
	return &pimap.Mailbox{Name: mailbox}, nil
}

func TestIMAPInitialSyncCancelledRunClosesProgress(t *testing.T) {
	a := newHoldTestApp(t)
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
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	rec := &progressRecorder{cancel: cancel}
	a.syncProgressEmitForTest = rec.hook(id)

	if err := a.syncIMAPInitialPass(ctx, *acc, nil); err == nil {
		t.Fatal("cancelled run returned nil")
	}
	ev := rec.snapshot()
	if len(ev) == 0 || ev[len(ev)-1].Folder != "" {
		t.Fatalf("last progress event %+v, want a closing one", ev)
	}
	if n := closings(ev); n != 1 {
		t.Fatalf("closing events = %d, want 1", n)
	}
}
