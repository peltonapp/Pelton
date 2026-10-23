package desktop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

func mustAccount(t *testing.T, a *App, id int64) storage.Account {
	t.Helper()
	acc, err := a.store.GetAccount(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return *acc
}

// SMTP is outside the pool, but the Sent copy after it is pool work: with
// the account's only session busy the copy waits for it, and no second
// IMAP connection is opened for it.
func TestSentCopyWaitsForTheAccountsOnlySession(t *testing.T) {
	client := &fakeIMAP{}
	app, account := sendingAccount(t, client)
	var dials atomic.Int32
	app.newIMAPClient = func(pimap.Config) (mailClient, error) {
		dials.Add(1)
		return client, nil
	}
	rt := mustAccountSync(t, app, account.ID)
	rt.pool.SetConfigured(1)
	t.Cleanup(func() { app.stopAccountScheduler(account.ID, false) })
	release := make(chan struct{})
	recv(t, signalJob(rt.sched, syncsched.KindBodies, 0, release), 2*time.Second, "busy task never started")

	sent := make(chan error, 1)
	go func() {
		sent <- (&accountTransmitter{app: app}).Transmit(context.Background(), outbox.Message{
			AccountID: account.ID, EnvelopeFrom: "me@example.com",
			Recipients: []string{"you@example.com"}, Raw: []byte("Subject: hi\r\n\r\nbody\r\n"),
		})
	}()
	noRecv(t, sent, 200*time.Millisecond, "the Sent copy ran while the only session was busy")
	close(release)
	if err := recv(t, sent, 5*time.Second, "send never finished"); err != nil {
		t.Fatalf("Transmit: %v", err)
	}
	if len(client.appended) != 1 || dials.Load() != 1 {
		t.Fatalf("appended %d copies over %d dials, want 1 over 1", len(client.appended), dials.Load())
	}
}

// Work on account A (N=1, busy) never delays account B.
func TestFastTaskOnOneAccountIgnoresAnotherBusyAccount(t *testing.T) {
	a := newHoldTestApp(t)
	useFakeIMAP(a)
	idA, _ := seedHoldAccount(t, a, "a@example.test")
	idB, _ := seedHoldAccount(t, a, "b@example.test")
	rtA := mustAccountSync(t, a, idA)
	rtA.pool.SetConfigured(1)
	mustAccountSync(t, a, idB).pool.SetConfigured(1)
	hold := make(chan struct{})
	defer close(hold)
	recv(t, signalJob(rtA.sched, syncsched.KindListStubs, 1, hold), 2*time.Second, "A's task never started")

	ran := make(chan error, 1)
	go func() {
		ran <- a.runNow(a.ctx, mustAccount(t, a, idB), syncsched.KindFlags, 0, func(context.Context, mailClient) error { return nil })
	}()
	if err := recv(t, ran, 2*time.Second, "B's action waited for A"); err != nil {
		t.Fatal(err)
	}
}
