package sync

import (
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func local(uid uint32, flags storage.Flag, pendingFlags, pendingDelete bool) *LocalMessage {
	return &LocalMessage{UID: uid, Flags: flags, PendingFlags: pendingFlags, PendingDelete: pendingDelete}
}

func server(uid uint32, flags storage.Flag) *ServerMessage {
	return &ServerMessage{UID: uid, Flags: flags}
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		local        *LocalMessage
		server       *ServerMessage
		wantAction   Action
		wantFlags    storage.Flag
		wantConflict bool
	}{
		{
			name:       "new on server",
			local:      nil,
			server:     server(10, storage.FlagSeen),
			wantAction: ActionFetchNew,
		},
		{
			name:       "deleted on server, no local change",
			local:      local(10, storage.FlagSeen, false, false),
			server:     nil,
			wantAction: ActionDeleteLocal,
		},
		{
			name:         "deleted on server but flagged locally is a conflict",
			local:        local(10, storage.FlagFlagged, true, false),
			server:       nil,
			wantAction:   ActionDeleteLocal,
			wantConflict: true,
		},
		{
			name:       "deleted on server and pending local delete agrees",
			local:      local(10, storage.FlagSeen, false, true),
			server:     nil,
			wantAction: ActionDeleteLocal,
		},
		{
			name:       "in agreement",
			local:      local(10, storage.FlagSeen, false, false),
			server:     server(10, storage.FlagSeen),
			wantAction: ActionNone,
		},
		{
			name:       "server flags changed, adopt locally",
			local:      local(10, 0, false, false),
			server:     server(10, storage.FlagSeen),
			wantAction: ActionAdoptServerFlags,
			wantFlags:  storage.FlagSeen,
		},
		{
			name:       "local flag change pushed up",
			local:      local(10, storage.FlagSeen, true, false),
			server:     server(10, 0),
			wantAction: ActionPushFlags,
			wantFlags:  storage.FlagSeen,
		},
		{
			name:         "both changed flags, union merge is a conflict",
			local:        local(10, storage.FlagSeen, true, false),
			server:       server(10, storage.FlagFlagged),
			wantAction:   ActionPushFlags,
			wantFlags:    storage.FlagSeen | storage.FlagFlagged,
			wantConflict: true,
		},
		{
			name:       "pending flags already satisfied on server, just clear",
			local:      local(10, storage.FlagSeen, true, false),
			server:     server(10, storage.FlagSeen|storage.FlagFlagged),
			wantAction: ActionClearPending,
			wantFlags:  storage.FlagSeen | storage.FlagFlagged,
			// server already has \Seen and more, but its set differs from ours so
			// it still counts as a divergence/conflict.
			wantConflict: true,
		},
		{
			name:       "pending delete with message still on server",
			local:      local(10, storage.FlagSeen, false, true),
			server:     server(10, storage.FlagSeen),
			wantAction: ActionPushDelete,
		},
		{
			name:       "pending delete wins over server flag change",
			local:      local(10, storage.FlagSeen, false, true),
			server:     server(10, storage.FlagFlagged),
			wantAction: ActionPushDelete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reconcile(tt.local, tt.server)
			if got.Action != tt.wantAction {
				t.Errorf("action = %v, want %v", got.Action, tt.wantAction)
			}
			if got.Flags != tt.wantFlags {
				t.Errorf("flags = %d, want %d", got.Flags, tt.wantFlags)
			}
			if got.Conflict != tt.wantConflict {
				t.Errorf("conflict = %v, want %v", got.Conflict, tt.wantConflict)
			}
		})
	}
}

func TestMergeFlagsUnion(t *testing.T) {
	t.Parallel()
	got := mergeFlags(storage.FlagSeen, storage.FlagFlagged)
	want := storage.FlagSeen | storage.FlagFlagged
	if got != want {
		t.Fatalf("mergeFlags = %d, want %d", got, want)
	}
	// union never clears: seen locally, unseen on server, stays seen.
	if got := mergeFlags(storage.FlagSeen, 0); got != storage.FlagSeen {
		t.Fatalf("mergeFlags dropped a flag: got %d", got)
	}
}

func TestBuildPlanCoversUnionOfUIDsInOrder(t *testing.T) {
	t.Parallel()
	locals := []LocalMessage{
		*local(3, storage.FlagSeen, false, false), // adopt nothing, agree? server has flagged -> adopt
		*local(5, storage.FlagSeen, false, false), // deleted on server
	}
	servers := []ServerMessage{
		*server(1, storage.FlagSeen),                     // new on server
		*server(3, storage.FlagSeen|storage.FlagFlagged), // server changed flags
	}

	plan := BuildPlan(locals, servers, 0)

	if len(plan) != 3 {
		t.Fatalf("plan length = %d, want 3", len(plan))
	}
	// deterministic ascending uid order: 1, 3, 5
	wantUIDs := []uint32{1, 3, 5}
	wantActions := []Action{ActionFetchNew, ActionAdoptServerFlags, ActionDeleteLocal}
	for i, d := range plan {
		if d.UID != wantUIDs[i] {
			t.Errorf("plan[%d].UID = %d, want %d", i, d.UID, wantUIDs[i])
		}
		if d.Action != wantActions[i] {
			t.Errorf("plan[%d].Action = %v, want %v", i, d.Action, wantActions[i])
		}
	}
}

func TestBuildPlanEmpty(t *testing.T) {
	t.Parallel()
	if plan := BuildPlan(nil, nil, 0); len(plan) != 0 {
		t.Fatalf("empty plan length = %d, want 0", len(plan))
	}
}
