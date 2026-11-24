package desktop

import (
	"context"
	"testing"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

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

// blockedRunApp is a hold test app with one seeded account whose every
// session parks in SELECT until the test ends, and a recorder on its
// progress events.
func blockedRunApp(t *testing.T) (*App, storage.Account, *progressRecorder) {
	t.Helper()
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
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(id)
	return a, *acc, rec
}

func TestIMAPInitialSyncCancelledRunClosesProgress(t *testing.T) {
	a, acc, rec := blockedRunApp(t)
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	rec.cancel = cancel

	if err := a.syncIMAPInitialPass(ctx, acc, nil); err == nil {
		t.Fatal("cancelled run returned nil")
	}
	assertClosedOnce(t, rec.snapshot())
}
