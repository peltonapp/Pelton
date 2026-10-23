package desktop

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// While an account is held, UI paths get a quiet no-op: opening a message
// shows the stub, and Sync does not report a failure.
func TestHeldAccountUIPathsAreQuiet(t *testing.T) {
	a := newHoldTestApp(t)
	id, folderID := seedHoldAccount(t, a, "quiet@example.test")
	var fetched atomic.Int32
	a.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error {
		fetched.Add(1)
		return nil
	}
	var dialed atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		dialed.Add(1)
		return &fakeIMAP{}, nil
	}
	stub := &storage.Message{AccountID: id, FolderID: folderID, RemoteID: "stub-1", Subject: "stub"}
	if _, err := a.store.UpsertMessageListMeta(a.ctx, stub); err != nil {
		t.Fatal(err)
	}

	release := a.holdAccountSync(id)
	if err := a.fetchMessageBodyOnDemand(stub); err != nil {
		t.Errorf("on-demand body while held = %v, want nil (stub shown)", err)
	}
	if err := a.SyncAccountNow(id); err != nil {
		t.Errorf("SyncAccountNow while held = %v, want nil", err)
	}
	if a.accountSync(id) != nil {
		t.Error("a held account got a scheduler")
	}
	if fetched.Load() != 0 || dialed.Load() != 0 {
		t.Errorf("held account ran sync work: %d fetches, %d connections", fetched.Load(), dialed.Load())
	}
	release()
	if err := a.fetchMessageBodyOnDemand(stub); err != nil {
		t.Fatalf("on-demand body after release: %v", err)
	}
	if fetched.Load() != 1 {
		t.Errorf("fetches after release = %d, want 1", fetched.Load())
	}
}

// Removing a mailbox is a hard cancel of that account only: its worker,
// sessions and scheduler stop, its state goes, and nothing brings them back.
// The other account keeps running.
func TestDeleteAccountHardCancelsOnlyThatAccount(t *testing.T) {
	// The rig's parked sessions ignore cancellation, so the removal waits
	// stopAccountWorkerTimeout for them before it gives up; 5 s of nothing.
	old := stopAccountWorkerTimeout
	stopAccountWorkerTimeout = 100 * time.Millisecond
	t.Cleanup(func() { stopAccountWorkerTimeout = old })
	r := newTwoAccountRig(t)
	a, idA, idB := r.a, r.idA, r.idB
	jobA := r.park(t, idA)
	jobB := r.park(t, idB)
	plantQuiescentWorker(a, idA)

	if err := a.DeleteAccount(idA); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	if !r.sessionClosed(t, r.accA) {
		t.Error("removed account's session was not closed")
	}
	recv(t, jobA.cancelled, 2*time.Second, "removed account's job was not cancelled")
	if a.accountSync(idA) != nil {
		t.Error("removed account's scheduler survived")
	}
	if workerFor(a, idA) != nil {
		t.Error("removed account's worker survived")
	}
	if hasAccountState(a, idA) {
		t.Error("removed account's state entry was kept")
	}

	if r.sessionClosed(t, r.accB) {
		t.Error("other account's session was closed")
	}
	noRecv(t, jobB.cancelled, 100*time.Millisecond, "other account's job was cancelled")
	if a.accountSync(idB) == nil {
		t.Error("other account's scheduler was removed")
	}

	// Nothing brings the removed account's runtime back.
	if err := trySync(a, idA); err == nil {
		t.Error("ensureAccountSync recreated a removed account's runtime")
	}
	stale := &storage.Message{AccountID: idA, FolderID: r.folderA, RemoteID: "x"}
	_ = a.fetchMessageBodyOnDemand(stale)
	if a.accountSync(idA) != nil {
		t.Error("a scheduler came back for the removed account")
	}
	if hasAccountState(a, idA) {
		t.Error("a state entry came back for the removed account")
	}
}

// A task that outlives its account's removal must not bring the account's
// state back, nor tell the ui about mail or progress of an account it no
// longer lists.
func TestRemovedAccountStateStaysGone(t *testing.T) {
	a := newHoldTestApp(t)
	id, folderID := seedHoldAccount(t, a, "a@example.test")
	a.forgetAccountState(id)
	var events []string
	var mu sync.Mutex
	a.emitForTest = func(name string, _ any) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	}
	_ = a.accountTally(id)
	a.emitSyncProgress(id, "a@example.test", "", syncCounts{Folder: "INBOX"})
	folder := storage.Folder{ID: folderID, AccountID: id}
	a.announceStored(folder, []int64{1})
	a.announceStoredBodies(folder, psync.FolderSyncResult{New: 1})
	a.accountStatesMu.Lock()
	_, back := a.accountStates[id]
	a.accountStatesMu.Unlock()
	if back {
		t.Error("the removed account's state was created again")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) > 0 {
		t.Errorf("events %v for a removed account, want none", events)
	}
}
