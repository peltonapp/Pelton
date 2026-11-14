package sync

import (
	"context"
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestImapFlagsToStorageReadsReplyState(t *testing.T) {
	tests := []struct {
		name  string
		flags []imap.Flag
		want  storage.Flag
	}{
		{"answered", []imap.Flag{imap.FlagAnswered}, storage.FlagAnswered},
		{"forwarded", []imap.Flag{imap.FlagForwarded}, storage.FlagForwarded},
		{"forwarded keyword in another case", []imap.Flag{"$forwarded"}, storage.FlagForwarded},
		{"both with seen", []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagForwarded}, storage.FlagSeen | storage.FlagAnswered | storage.FlagForwarded},
		{"other keywords ignored", []imap.Flag{"$Label1", "$Junk", imap.FlagDraft}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imapFlagsToStorage(tt.flags); got != tt.want {
				t.Fatalf("imapFlagsToStorage(%v) = %d, want %d", tt.flags, got, tt.want)
			}
		})
	}
}

func TestSyncAdoptsServerReplyState(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2}, flags: map[uint32][]imap.Flag{
		1: {imap.FlagSeen, imap.FlagAnswered},
		2: {imap.FlagSeen},
	}}
	engine := NewEngine(client, db, nil)
	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	states, _ := db.ListMessageStates(ctx, folder.ID)
	if !states[0].Flags.Has(storage.FlagAnswered) {
		t.Fatalf("uid 1 flags = %d, want answered from the first fetch", states[0].Flags)
	}

	// another client forwards uid 2, and nothing else changes.
	client.flags[2] = []imap.Flag{imap.FlagSeen, imap.FlagForwarded}
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
	client.flags[1] = []imap.Flag{imap.FlagAnswered}
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
		name     string
		kind     string
		refuse   bool
		wantFlag imap.Flag
		wantHas  bool
	}{
		{"reply pushes answered", storage.ReplyKindReply, false, imap.FlagAnswered, true},
		{"forward pushes keyword", storage.ReplyKindForward, false, imap.FlagForwarded, true},
		{"refused keyword does not block the rest", storage.ReplyKindForward, true, imap.FlagForwarded, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			client := &fakeClient{
				uids:           []uint32{1},
				flags:          map[uint32][]imap.Flag{1: {imap.FlagSeen}},
				messageIDs:     map[uint32]string{1: "orig@example.com"},
				refuseKeywords: tt.refuse,
			}
			engine := NewEngine(client, db, nil)
			if _, err := engine.SyncFolder(ctx, folder); err != nil {
				t.Fatalf("initial sync: %v", err)
			}

			if err := db.RecordOutgoingReply(ctx, folder.AccountID, "<out@example.com>", "orig@example.com", tt.kind); err != nil {
				t.Fatalf("record: %v", err)
			}
			changed, err := db.MarkReplySent(ctx, folder.AccountID, "out@example.com")
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
			if got := slices.Contains(client.flags[1], tt.wantFlag); got != tt.wantHas {
				t.Fatalf("server flags = %v, want %s present = %v", client.flags[1], tt.wantFlag, tt.wantHas)
			}
			if slices.Contains(client.flags[1], imap.FlagSeen) {
				t.Fatalf("server flags = %v, the unread change did not reach the server", client.flags[1])
			}
			states, _ = db.ListMessageStates(ctx, folder.ID)
			if states[0].PendingFlags {
				t.Fatal("pending flags left set after the push")
			}
		})
	}
}
