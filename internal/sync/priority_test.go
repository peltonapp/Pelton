package sync

import (
	"context"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// the first sync of a large folder used to show nothing until the whole folder
// was down. Stored ids are handed over as they arrive.
func TestSyncAnnouncesMessagesAsTheyArrive(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	ids := make([]string, 60)
	for i := range ids {
		ids[i] = strconvFormat(uint32(i + 1))
	}
	adapter := &fakeAdapter{ids: ids}
	engine := NewEngine(adapter, db, nil)

	var batches [][]int64
	engine.OnStored = func(f storage.Folder, stored []int64) {
		if f.ID != folder.ID {
			t.Errorf("announced folder %d, want %d", f.ID, folder.ID)
		}
		batches = append(batches, stored)
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2)}
	engine := NewEngine(adapter, db, nil)
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	ids := make([]string, 120)
	for i := range ids {
		ids[i] = strconvFormat(uint32(i + 1))
	}
	adapter := &fakeAdapter{ids: ids}
	engine := NewEngine(adapter, db, nil)

	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.New != len(ids) {
		t.Fatalf("stored %d messages, want %d", res.New, len(ids))
	}
	want := (len(ids) + fetchBatch - 1) / fetchBatch
	if adapter.commands != want {
		t.Errorf("issued %d fetch commands for %d messages, want %d", adapter.commands, len(ids), want)
	}
	if len(adapter.fetched) != len(ids) {
		t.Errorf("fetched %d messages, want %d", len(adapter.fetched), len(ids))
	}
}

// newest first still holds: the newest id has to be in the first batch, or a
// large mailbox shows years-old mail for as long as it takes to reach today.
func TestSyncFetchesNewestBatchFirst(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	ids := make([]string, 120)
	for i := range ids {
		ids[i] = strconvFormat(uint32(i + 1))
	}
	adapter := &fakeAdapter{ids: ids}
	if _, err := NewEngine(adapter, db, nil).SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if adapter.fetched[0] != "120" {
		t.Errorf("first message fetched was id %s, want the newest (120)", adapter.fetched[0])
	}
}

func strconvFormat(uid uint32) string {
	return fakeIDs(uid)[0]
}
