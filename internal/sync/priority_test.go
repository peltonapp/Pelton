package sync

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// the reported problem: a message the user pressed send on waited behind a
// 14k-message sync. The sync now asks before every body fetch and stands aside
// while something is queued.
func TestSyncStandsAsideWhileSendingIsQueued(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3, 4}}
	engine := NewEngine(client, db, nil)

	// queued until the sync has stood aside once, which is what the send worker
	// getting its turn looks like from here.
	var asked atomic.Int32
	engine.YieldTo = func() bool {
		return asked.Add(1) <= 2
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if asked.Load() == 0 {
		t.Fatal("sync never asked whether it should stand aside")
	}
	if len(client.fetched) != 4 {
		t.Errorf("fetched %v, want all four once the queue drained", client.fetched)
	}
}

// a cancelled sync must not sit in the yield loop waiting for a queue that
// nothing is draining.
func TestYieldStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3}}
	engine := NewEngine(client, db, nil)
	engine.YieldTo = func() bool {
		cancel()
		return true
	}

	// a cancelled sync reports the cancellation from whichever write it reached
	// first; what matters here is that it came back at all rather than waiting
	// on a queue nothing is draining.
	if _, err := engine.SyncFolder(ctx, folder); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("sync: %v", err)
	}
	if len(client.fetched) > 1 {
		t.Errorf("fetched %v after cancellation, want at most the one in flight", client.fetched)
	}
}

// the first sync of a large folder used to show nothing until the whole folder
// was down. Stored ids are handed over as they arrive.
func TestSyncAnnouncesMessagesAsTheyArrive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	uids := make([]uint32, 60)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	client := &fakeClient{uids: uids}
	engine := NewEngine(client, db, nil)

	var batches [][]int64
	engine.OnStored = func(f storage.Folder, ids []int64) {
		if f.ID != folder.ID {
			t.Errorf("announced folder %d, want %d", f.ID, folder.ID)
		}
		batches = append(batches, ids)
	}

	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(batches) < 2 {
		t.Fatalf("announced %d times for %d messages, want the list to fill as it goes", len(batches), res.New)
	}
	var announced int
	for _, b := range batches {
		announced += len(b)
	}
	if announced != res.New {
		t.Errorf("announced %d ids, stored %d", announced, res.New)
	}
}

// a folder with nothing new announces nothing, so an idle sync does not make
// the list reload for no reason.
func TestSyncAnnouncesNothingWhenNothingIsNew(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2}}
	engine := NewEngine(client, db, nil)
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	called := 0
	engine.OnStored = func(storage.Folder, []int64) { called++ }
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if called != 0 {
		t.Errorf("announced %d times with nothing new", called)
	}
}

// a first sync used to cost one fetch command per message, so a large mailbox
// paid the round trip to the server thousands of times before anything else
// could happen on the connection.
func TestSyncFetchesBodiesInBatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	uids := make([]uint32, 120)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	client := &fakeClient{uids: uids}
	engine := NewEngine(client, db, nil)

	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.New != len(uids) {
		t.Fatalf("stored %d messages, want %d", res.New, len(uids))
	}
	want := (len(uids) + fetchBatch - 1) / fetchBatch
	if client.commands != want {
		t.Errorf("issued %d fetch commands for %d messages, want %d", client.commands, len(uids), want)
	}
	if len(client.fetched) != len(uids) {
		t.Errorf("fetched %d messages, want %d", len(client.fetched), len(uids))
	}
}

// newest first still holds: the newest uid has to be in the first batch, or a
// large mailbox shows years-old mail for as long as it takes to reach today.
func TestSyncFetchesNewestBatchFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	uids := make([]uint32, 120)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	client := &fakeClient{uids: uids}
	if _, err := NewEngine(client, db, nil).SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if client.fetched[0] != 120 {
		t.Errorf("first message fetched was uid %d, want the newest (120)", client.fetched[0])
	}
}
