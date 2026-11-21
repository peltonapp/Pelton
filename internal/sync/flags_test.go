package sync

import (
	"context"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestSyncPushesFlagRemoval(t *testing.T) {
	tests := []struct {
		name       string
		serverHas  storage.Flag
		localAfter storage.Flag
		wantServer storage.Flag
	}{
		{
			name:       "unflag",
			serverHas:  storage.FlagSeen | storage.FlagFlagged,
			localAfter: storage.FlagSeen,
			wantServer: storage.FlagSeen,
		},
		{
			name:       "mark unread",
			serverHas:  storage.FlagSeen | storage.FlagFlagged,
			localAfter: storage.FlagFlagged,
			wantServer: storage.FlagFlagged,
		},
		{
			name:       "clear both",
			serverHas:  storage.FlagSeen | storage.FlagFlagged,
			localAfter: 0,
			wantServer: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			adapter := &fakeAdapter{ids: fakeIDs(1), flags: map[string]storage.Flag{"1": tt.serverHas}}
			engine := NewEngine(adapter, db, nil)

			if _, err := engine.SyncFolder(ctx, folder); err != nil {
				t.Fatalf("initial sync: %v", err)
			}
			states, err := db.ListMessageStates(ctx, folder.ID)
			if err != nil || len(states) != 1 {
				t.Fatalf("states = %v, err = %v", states, err)
			}
			id := states[0].ID
			if err := db.MarkFlagsPending(ctx, id, tt.localAfter); err != nil {
				t.Fatalf("mark pending: %v", err)
			}

			if _, err := engine.SyncFolder(ctx, folder); err != nil {
				t.Fatalf("second sync: %v", err)
			}

			if got := adapter.flags["1"]; got != tt.wantServer {
				t.Fatalf("server flags = %d, want %d", got, tt.wantServer)
			}
			m, err := db.GetMessage(ctx, id)
			if err != nil {
				t.Fatalf("get message: %v", err)
			}
			if m.Flags != tt.localAfter {
				t.Fatalf("local flags = %d, want %d", m.Flags, tt.localAfter)
			}

			// a later sync with nothing pending must stay put.
			if _, err := engine.SyncFolder(ctx, folder); err != nil {
				t.Fatalf("third sync: %v", err)
			}
			m, _ = db.GetMessage(ctx, id)
			if m.Flags != tt.localAfter {
				t.Fatalf("local flags after resync = %d, want %d", m.Flags, tt.localAfter)
			}
		})
	}
}

func TestSyncKeepsServerChangesToFlagsNotTouchedLocally(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1), flags: map[string]storage.Flag{"1": storage.FlagFlagged}}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	states, _ := db.ListMessageStates(ctx, folder.ID)
	id := states[0].ID

	// another client marks the message read while the user unflags it here.
	adapter.flags["1"] = storage.FlagFlagged | storage.FlagSeen
	if err := db.MarkFlagsPending(ctx, id, 0); err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if got := adapter.flags["1"]; got != storage.FlagSeen {
		t.Fatalf("server flags = %d, want only \\Seen", got)
	}
	m, _ := db.GetMessage(ctx, id)
	if m.Flags != storage.FlagSeen {
		t.Fatalf("local flags = %d, want \\Seen", m.Flags)
	}
}

func TestReconcilePendingMaskDecidesWhichSideWins(t *testing.T) {
	pending := func(flags, mask storage.Flag) *LocalMessage {
		return &LocalMessage{RemoteID: "1", Flags: flags, PendingFlags: true, PendingMask: mask}
	}
	tests := []struct {
		name      string
		local     *LocalMessage
		server    storage.Flag
		wantAct   Action
		wantFlags storage.Flag
		wantClear storage.Flag
	}{
		{"unflag is pushed", pending(0, storage.FlagFlagged), storage.FlagFlagged, ActionPushFlags, 0, storage.FlagFlagged},
		{"unflag already on server", pending(0, storage.FlagFlagged), 0, ActionClearPending, 0, 0},
		{"untouched bit follows server", pending(0, storage.FlagFlagged), storage.FlagFlagged | storage.FlagSeen, ActionPushFlags, storage.FlagSeen, storage.FlagFlagged},
		{"untouched bit cleared on server", pending(storage.FlagSeen|storage.FlagFlagged, storage.FlagFlagged), storage.FlagSeen, ActionPushFlags, storage.FlagSeen | storage.FlagFlagged, 0},
		{"no recorded mask treats every bit as changed", pending(0, 0), storage.FlagSeen | storage.FlagFlagged, ActionPushFlags, 0, storage.FlagSeen | storage.FlagFlagged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reconcile(tt.local, &ServerMessage{RemoteID: "1", Flags: tt.server})
			if got.Action != tt.wantAct || got.Flags != tt.wantFlags || got.Clear != tt.wantClear {
				t.Fatalf("got action %v flags %d clear %d, want %v %d %d",
					got.Action, got.Flags, got.Clear, tt.wantAct, tt.wantFlags, tt.wantClear)
			}
		})
	}
}
