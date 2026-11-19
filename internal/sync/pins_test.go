package sync

import (
	"context"
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
)

func pinnedUIDs(t *testing.T, db *storage.DB, folder storage.Folder) map[uint32]storage.PinState {
	t.Helper()
	pins, err := db.FolderPinStates(context.Background(), folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	return pins
}

func TestSyncPinsAsKeyword(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{
		uids:           []uint32{1, 2, 3},
		permanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagWildcard},
		flags:          map[uint32][]imap.Flag{2: {pinKeyword}},
	}
	engine := NewEngine(client, db, nil)
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
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
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(client.flags[3], pinKeyword) || slices.Contains(client.flags[2], pinKeyword) {
		t.Errorf("server flags = %v, want $Pinned on 3 only", client.flags)
	}
	pins = pinnedUIDs(t, db, folder)
	if len(pins) != 1 || !pins[3].Pinned || pins[3].Pending {
		t.Errorf("after push pins = %+v, want uid 3 pinned and settled", pins)
	}

	// another client unpinning wins once nothing is pending here.
	client.flags[3] = nil
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	if pins = pinnedUIDs(t, db, folder); len(pins) != 0 {
		t.Errorf("after remote unpin pins = %+v, want none", pins)
	}
}

func TestSyncPinsStayLocalWithoutKeywords(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1}, permanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagFlagged}}
	engine := NewEngine(client, db, nil)
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	states, _ := db.ListMessageStates(ctx, folder.ID)
	if err := db.SetPinned(ctx, states[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	if len(client.flags[1]) != 0 {
		t.Errorf("server flags = %v, want nothing stored", client.flags[1])
	}
	pins := pinnedUIDs(t, db, folder)
	if !pins[1].Pinned || pins[1].Pending {
		t.Errorf("pins = %+v, want uid 1 pinned locally and settled", pins)
	}
}
