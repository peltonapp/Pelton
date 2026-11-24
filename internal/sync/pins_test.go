package sync

import (
	"context"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// pinAdapter keeps pins as the server would, as a keyword per message.
type pinAdapter struct {
	fakeAdapter
	keeps  bool
	pinned map[string]bool
}

func (a *pinAdapter) Pins(string) (map[string]bool, bool) {
	out := make(map[string]bool, len(a.ids))
	for _, id := range a.ids {
		out[id] = a.pinned[id]
	}
	return out, a.keeps
}

func (a *pinAdapter) SetPin(_ context.Context, _, remoteID string, pinned bool) error {
	a.pinned[remoteID] = pinned
	return nil
}

func pinnedUIDs(t *testing.T, db *storage.DB, folder storage.Folder) map[uint32]storage.PinState {
	t.Helper()
	pins, err := db.FolderPinStates(context.Background(), folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	return pins
}

func syncAndComplete(t *testing.T, engine *Engine, folder storage.Folder) {
	t.Helper()
	if _, err := engine.SyncFolder(context.Background(), folder); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.CompleteFolder(context.Background(), folder); err != nil {
		t.Fatal(err)
	}
}

func TestSyncPinsAsKeyword(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &pinAdapter{fakeAdapter: fakeAdapter{ids: fakeIDs(1, 2, 3)}, keeps: true, pinned: map[string]bool{"2": true}}
	engine := NewEngine(adapter, db, nil)
	syncAndComplete(t, engine, folder)
	// a pin set elsewhere, or before a reinstall, comes down.
	pins := pinnedUIDs(t, db, folder)
	if len(pins) != 1 || !pins[2].Pinned || pins[2].Pending {
		t.Fatalf("after first sync pins = %+v, want uid 2 pinned", pins)
	}

	// a local pin goes up and a local unpin clears the keyword.
	states, _ := db.ListMessageStates(ctx, folder.ID)
	byUID := map[uint32]int64{}
	for _, s := range states {
		byUID[s.UID] = s.ID
	}
	if err := db.SetPinned(ctx, byUID[3], true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPinned(ctx, byUID[2], false); err != nil {
		t.Fatal(err)
	}
	syncAndComplete(t, engine, folder)
	if !adapter.pinned["3"] || adapter.pinned["2"] {
		t.Errorf("server pins = %v, want 3 only", adapter.pinned)
	}
	pins = pinnedUIDs(t, db, folder)
	if len(pins) != 1 || !pins[3].Pinned || pins[3].Pending {
		t.Errorf("after push pins = %+v, want uid 3 pinned and settled", pins)
	}

	// another client unpinning wins once nothing is pending here.
	adapter.pinned["3"] = false
	syncAndComplete(t, engine, folder)
	if pins = pinnedUIDs(t, db, folder); len(pins) != 0 {
		t.Errorf("after remote unpin pins = %+v, want none", pins)
	}
}

func TestSyncPinsStayLocalWithoutKeywords(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &pinAdapter{fakeAdapter: fakeAdapter{ids: fakeIDs(1)}, pinned: map[string]bool{}}
	engine := NewEngine(adapter, db, nil)
	syncAndComplete(t, engine, folder)
	states, _ := db.ListMessageStates(ctx, folder.ID)
	if err := db.SetPinned(ctx, states[0].ID, true); err != nil {
		t.Fatal(err)
	}
	syncAndComplete(t, engine, folder)
	if adapter.pinned["1"] {
		t.Error("the pin went to a mailbox that cannot keep it")
	}
	pins := pinnedUIDs(t, db, folder)
	if !pins[1].Pinned || pins[1].Pending {
		t.Errorf("pins = %+v, want uid 1 pinned locally and settled", pins)
	}
}
