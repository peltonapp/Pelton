package desktop

import (
	"context"
	"fmt"
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

// Throttled to N=1 mid-run, an opened message's body runs as soon as the
// running batch ends, before the campaign's next batch. The batch is never
// cancelled.
func TestOnDemandBodyRunsBetweenBatchesWhenThrottledToOne(t *testing.T) {
	a := newHoldTestApp(t)
	useFakeIMAP(a)
	id, folderID := seedHoldAccount(t, a, "a@example.test")
	rt := mustAccountSync(t, a, id)
	rt.pool.SetConfigured(3)
	rt.pool.SetEffective(1) // adaptive throttle fired mid-run

	var order []string
	var mu = make(chan struct{}, 1)
	record := func(s string) { mu <- struct{}{}; order = append(order, s); <-mu }
	batchIn := make(chan struct{}, 4)
	batchRelease := make(chan struct{})
	units := 0
	rt.sched.Enqueue(imapTask{Kind: syncsched.KindBodies, FolderID: folderID, Key: "bodies",
		Run: func(ctx context.Context, _ mailClient) error {
			units++
			record(fmt.Sprint("batch", units))
			if units == 1 {
				batchIn <- struct{}{}
				select {
				case <-batchRelease:
				case <-ctx.Done():
					return ctx.Err() // would mean a hard abort
				}
			}
			if units < 3 {
				return syncsched.ErrAgain
			}
			return nil
		}})
	recv(t, batchIn, 2*time.Second, "batch never started")

	m := insertStub(t, a, id, folderID, "77")
	opened := make(chan error, 1)
	a.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error {
		record("open")
		return nil
	}
	go func() { opened <- a.fetchMessageBodyOnDemand(m) }()
	waitUntil(t, 2*time.Second, "open never queued", func() bool { return rt.sched.Stats().Fast == 1 })
	close(batchRelease)
	if err := recv(t, opened, 2*time.Second, "open never ran"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "campaign did not finish", func() bool {
		mu <- struct{}{}
		defer func() { <-mu }()
		return len(order) == 4
	})
	if got := fmt.Sprint(order); got != "[batch1 open batch2 batch3]" {
		t.Fatalf("order %s, want the open right after the running batch", got)
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
	recv(t, signalJob(rtA.sched, syncsched.KindFullCheck, 1, hold), 2*time.Second, "A's task never started")

	ran := make(chan error, 1)
	go func() {
		ran <- a.runNow(a.ctx, mustAccount(t, a, idB), syncsched.KindFlags, 0, func(context.Context, mailClient) error { return nil })
	}()
	if err := recv(t, ran, 2*time.Second, "B's action waited for A"); err != nil {
		t.Fatal(err)
	}
}

// A manual Sync is normal-lane work: an opened message's body overtakes it
// at the end of the running task, at every N.
func TestManualSyncDoesNotBlockOnDemandBody(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			a := newHoldTestApp(t)
			useFakeIMAP(a)
			id, folderID := seedHoldAccount(t, a, "a@example.test")
			rt := mustAccountSync(t, a, id)
			rt.pool.SetConfigured(n)
			acc := mustAccount(t, a, id)
			hold := make(chan struct{}, 8) // each value releases one task
			var started atomic.Int32
			manualDone := make(chan error, 8)
			for f := range 6 {
				go func() {
					manualDone <- a.runPooled(a.ctx, acc, poolTask{
						Kind: syncsched.KindListStubs, Class: syncsched.ClassManualSync, FolderID: int64(100 + f),
						Run: func(context.Context, mailClient) error {
							started.Add(1)
							<-hold
							return nil
						},
					})
				}()
			}
			waitUntil(t, 2*time.Second, "manual Sync did not fill the sessions", func() bool { return int(started.Load()) == n })

			m := insertStub(t, a, id, folderID, "88")
			opened := make(chan error, 1)
			var startedAtOpen atomic.Int32
			a.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error {
				startedAtOpen.Store(started.Load())
				return nil
			}
			go func() { opened <- a.fetchMessageBodyOnDemand(m) }()
			waitUntil(t, 2*time.Second, "open never queued", func() bool { return rt.sched.Stats().Fast == 1 })
			hold <- struct{}{} // one manual task ends: its worker takes the open
			if err := recv(t, opened, 2*time.Second, "open never ran"); err != nil {
				t.Fatal(err)
			}
			close(hold)
			for range 6 {
				if err := recv(t, manualDone, 2*time.Second, "manual Sync task never ended"); err != nil {
					t.Fatal(err)
				}
			}
			if got := int(startedAtOpen.Load()); got != n {
				t.Fatalf("open ran after %d manual tasks had started, want %d (before the rest of the Sync)", got, n)
			}
		})
	}
}

// insertStub stores a body-less message row and returns it.
func insertStub(t *testing.T, a *App, accountID, folderID int64, remoteID string) *storage.Message {
	t.Helper()
	id, err := a.store.UpsertMessageListMeta(a.ctx, &storage.Message{AccountID: accountID, FolderID: folderID, UID: 9000, RemoteID: remoteID, Subject: "stub"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyComplete {
		t.Fatal("fixture: stub has a body")
	}
	return m
}
