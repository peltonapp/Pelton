package sync

import (
	"context"
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestSyncPushesFlagRemoval(t *testing.T) {
	tests := []struct {
		name       string
		serverHas  []imap.Flag
		localAfter storage.Flag
		wantServer []imap.Flag
	}{
		{
			name:       "unflag",
			serverHas:  []imap.Flag{imap.FlagSeen, imap.FlagFlagged},
			localAfter: storage.FlagSeen,
			wantServer: []imap.Flag{imap.FlagSeen},
		},
		{
			name:       "mark unread",
			serverHas:  []imap.Flag{imap.FlagSeen, imap.FlagFlagged},
			localAfter: storage.FlagFlagged,
			wantServer: []imap.Flag{imap.FlagFlagged},
		},
		{
			name:       "unflag keeps flags pelton does not model",
			serverHas:  []imap.Flag{imap.FlagFlagged, imap.FlagAnswered, "$Label1"},
			localAfter: 0,
			wantServer: []imap.Flag{imap.FlagAnswered, "$Label1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			client := &fakeClient{uids: []uint32{1}, flags: map[uint32][]imap.Flag{1: slices.Clone(tt.serverHas)}}
			engine := NewEngine(client, db, nil)

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

			got := slices.Clone(client.flags[1])
			slices.Sort(got)
			want := slices.Clone(tt.wantServer)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("server flags = %v, want %v", got, want)
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
	client := &fakeClient{uids: []uint32{1}, flags: map[uint32][]imap.Flag{1: {imap.FlagFlagged}}}
	engine := NewEngine(client, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	states, _ := db.ListMessageStates(ctx, folder.ID)
	id := states[0].ID

	// another client marks the message read while the user unflags it here.
	client.flags[1] = []imap.Flag{imap.FlagFlagged, imap.FlagSeen}
	if err := db.MarkFlagsPending(ctx, id, 0); err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if got := client.flags[1]; !slices.Equal(got, []imap.Flag{imap.FlagSeen}) {
		t.Fatalf("server flags = %v, want only \\Seen", got)
	}
	m, _ := db.GetMessage(ctx, id)
	if m.Flags != storage.FlagSeen {
		t.Fatalf("local flags = %d, want \\Seen", m.Flags)
	}
}

func TestReconcilePendingMaskDecidesWhichSideWins(t *testing.T) {
	pending := func(flags, mask storage.Flag) *LocalMessage {
		return &LocalMessage{UID: 1, Flags: flags, PendingFlags: true, PendingMask: mask}
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
			got := Reconcile(tt.local, &ServerMessage{UID: 1, Flags: tt.server})
			if got.Action != tt.wantAct || got.Flags != tt.wantFlags || got.Clear != tt.wantClear {
				t.Fatalf("got action %v flags %d clear %d, want %v %d %d",
					got.Action, got.Flags, got.Clear, tt.wantAct, tt.wantFlags, tt.wantClear)
			}
		})
	}
}
