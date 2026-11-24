package sync

import (
	"context"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestSyncAdoptsServerReplyState(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2), flags: map[string]storage.Flag{
		"1": storage.FlagSeen | storage.FlagAnswered,
		"2": storage.FlagSeen,
	}}
	engine := NewEngine(adapter, db, nil)
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	states, _ := db.ListMessageStates(ctx, folder.ID)
	if !states[0].Flags.Has(storage.FlagAnswered) {
		t.Fatalf("uid 1 flags = %d, want answered from the first fetch", states[0].Flags)
	}

	// another client forwards uid 2, and nothing else changes.
	adapter.flags["2"] = storage.FlagSeen | storage.FlagForwarded
	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.ReplyStateChanged != 1 {
		t.Fatalf("ReplyStateChanged = %d, want 1", res.ReplyStateChanged)
	}
	states, _ = db.ListMessageStates(ctx, folder.ID)
	if states[1].Flags != storage.FlagSeen|storage.FlagForwarded {
		t.Fatalf("uid 2 flags = %d, want seen and forwarded", states[1].Flags)
	}

	// a seen-only change elsewhere is not a reply state change.
	adapter.flags["1"] = storage.FlagAnswered
	res, err = engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("third sync: %v", err)
	}
	if res.ReplyStateChanged != 0 {
		t.Fatalf("ReplyStateChanged = %d after a seen change, want 0", res.ReplyStateChanged)
	}
}

func TestSyncPushesReplyStateSetLocally(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want storage.Flag
	}{
		{"reply pushes answered", storage.ReplyKindReply, storage.FlagAnswered},
		{"forward pushes forwarded", storage.ReplyKindForward, storage.FlagForwarded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			adapter := &fakeAdapter{
				ids:        fakeIDs(1),
				flags:      map[string]storage.Flag{"1": storage.FlagSeen},
				messageIDs: map[string]string{"1": "orig@example.com"},
			}
			engine := NewEngine(adapter, db, nil)
			if _, err := engine.SyncFolder(ctx, folder); err != nil {
				t.Fatalf("initial sync: %v", err)
			}

			if err := db.RecordOutgoingReply(ctx, folder.AccountID, "<out@example.com>", "orig@example.com", tt.kind); err != nil {
				t.Fatalf("record: %v", err)
			}
			changed, err := db.MarkReplySent(ctx, "out@example.com")
			if err != nil || len(changed) != 1 {
				t.Fatalf("mark sent = %v, %v; want one change", changed, err)
			}
			// the user marks it unread before the sync gets to run.
			states, _ := db.ListMessageStates(ctx, folder.ID)
			if err := db.MarkFlagsPending(ctx, states[0].ID, states[0].Flags&^storage.FlagSeen); err != nil {
				t.Fatalf("mark unread: %v", err)
			}

			if _, err := engine.SyncFolder(ctx, folder); err != nil {
				t.Fatalf("push sync: %v", err)
			}
			if got := adapter.flags["1"]; !got.Has(tt.want) || got.Has(storage.FlagSeen) {
				t.Fatalf("server flags = %d, want %d set and seen cleared", got, tt.want)
			}
			states, _ = db.ListMessageStates(ctx, folder.ID)
			if states[0].PendingFlags {
				t.Fatal("pending flags left set after the push")
			}
		})
	}
}
