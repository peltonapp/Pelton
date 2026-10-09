package sync

import (
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func local(id string, flags storage.Flag, pendingFlags, pendingDelete bool) *LocalMessage {
	return &LocalMessage{RemoteID: id, Flags: flags, PendingFlags: pendingFlags, PendingDelete: pendingDelete}
}

func server(id string, flags storage.Flag) *ServerMessage {
	return &ServerMessage{RemoteID: id, Flags: flags}
}

func TestReconcile(t *testing.T) {
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
			server:     server("10", storage.FlagSeen),
			wantAction: ActionFetchNew,
		},
		{
			name:       "deleted on server, no local change",
			local:      local("10", storage.FlagSeen, false, false),
			server:     nil,
			wantAction: ActionDeleteLocal,
		},
		{
			name:         "deleted on server but flagged locally is a conflict",
			local:        local("10", storage.FlagFlagged, true, false),
			server:       nil,
			wantAction:   ActionDeleteLocal,
			wantConflict: true,
		},
		{
			name:       "deleted on server and pending local delete agrees",
			local:      local("10", storage.FlagSeen, false, true),
			server:     nil,
			wantAction: ActionDeleteLocal,
		},
		{
			name:       "in agreement",
			local:      local("10", storage.FlagSeen, false, false),
			server:     server("10", storage.FlagSeen),
			wantAction: ActionNone,
		},
		{
			name:       "server flags changed, adopt locally",
			local:      local("10", 0, false, false),
			server:     server("10", storage.FlagSeen),
			wantAction: ActionAdoptServerFlags,
			wantFlags:  storage.FlagSeen,
		},
		{
			name:       "local flag change pushed up",
			local:      local("10", storage.FlagSeen, true, false),
			server:     server("10", 0),
			wantAction: ActionPushFlags,
			wantFlags:  storage.FlagSeen,
		},
		{
			name:         "both changed flags, union merge is a conflict",
			local:        local("10", storage.FlagSeen, true, false),
			server:       server("10", storage.FlagFlagged),
			wantAction:   ActionPushFlags,
			wantFlags:    storage.FlagSeen | storage.FlagFlagged,
			wantConflict: true,
		},
		{
			name:       "pending flags already satisfied on server, just clear",
			local:      local("10", storage.FlagSeen, true, false),
			server:     server("10", storage.FlagSeen|storage.FlagFlagged),
			wantAction: ActionClearPending,
			wantFlags:  storage.FlagSeen | storage.FlagFlagged,
			// server already has \Seen and more, but its set differs from ours so
			// it still counts as a divergence/conflict.
			wantConflict: true,
		},
		{
			name:       "pending delete with message still on server",
			local:      local("10", storage.FlagSeen, false, true),
			server:     server("10", storage.FlagSeen),
			wantAction: ActionPushDelete,
		},
		{
			name:       "pending delete wins over server flag change",
			local:      local("10", storage.FlagSeen, false, true),
			server:     server("10", storage.FlagFlagged),
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
	locals := []LocalMessage{
		*local("3", storage.FlagSeen, false, false), // adopt nothing, agree? server has flagged -> adopt
		*local("5", storage.FlagSeen, false, false), // deleted on server
	}
	servers := []ServerMessage{
		*server("1", storage.FlagSeen),                     // new on server
		*server("3", storage.FlagSeen|storage.FlagFlagged), // server changed flags
	}

	plan := BuildPlan(locals, servers, "")

	if len(plan) != 3 {
		t.Fatalf("plan length = %d, want 3", len(plan))
	}
	// newest-first by servers order, then local-only: 1, 3, 5
	wantIDs := []string{"1", "3", "5"}
	wantActions := []Action{ActionFetchNew, ActionAdoptServerFlags, ActionDeleteLocal}
	for i, d := range plan {
		if d.RemoteID != wantIDs[i] {
			t.Errorf("plan[%d].RemoteID = %q, want %q", i, d.RemoteID, wantIDs[i])
		}
		if d.Action != wantActions[i] {
			t.Errorf("plan[%d].Action = %v, want %v", i, d.Action, wantActions[i])
		}
	}
}

func TestBuildPlanEmpty(t *testing.T) {
	if plan := BuildPlan(nil, nil, ""); len(plan) != 0 {
		t.Fatalf("empty plan length = %d, want 0", len(plan))
	}
}

func TestBuildDeltaPlan(t *testing.T) {
	hdr := func(id string, uid uint32, flags storage.Flag) Header {
		return Header{RemoteID: id, LegacyUID: uid, Flags: flags}
	}
	type want struct {
		id     string
		action Action
		flags  storage.Flag
	}
	tests := []struct {
		name     string
		locals   []LocalMessage
		delta    Delta
		floorUID uint32
		want     []want
	}{
		{
			name:     "new message above floor is fetched",
			delta:    Delta{Changed: []Header{hdr("9", 9, 0)}},
			floorUID: 5,
			want:     []want{{"9", ActionFetchNew, 0}},
		},
		{
			name:     "new message below floor is skipped",
			delta:    Delta{Changed: []Header{hdr("3", 3, 0)}},
			floorUID: 5,
			want:     nil,
		},
		{
			name:     "opaque id without uid ignores the numeric floor",
			delta:    Delta{Changed: []Header{hdr("e1", 0, 0)}},
			floorUID: 5,
			want:     []want{{"e1", ActionFetchNew, 0}},
		},
		{
			name:   "server flag change without pending is adopted",
			locals: []LocalMessage{*local("a", 0, false, false)},
			delta:  Delta{Changed: []Header{hdr("a", 0, storage.FlagSeen)}},
			want:   []want{{"a", ActionAdoptServerFlags, storage.FlagSeen}},
		},
		{
			name:   "pending flags merge with the real server flags",
			locals: []LocalMessage{*local("a", storage.FlagSeen, true, false)},
			delta:  Delta{Changed: []Header{hdr("a", 0, storage.FlagFlagged)}},
			want:   []want{{"a", ActionPushFlags, storage.FlagSeen | storage.FlagFlagged}},
		},
		{
			name:   "pending flags already on the server only clear the marker",
			locals: []LocalMessage{*local("a", storage.FlagSeen, true, false)},
			delta:  Delta{Changed: []Header{hdr("a", 0, storage.FlagSeen)}},
			want:   []want{{"a", ActionClearPending, storage.FlagSeen}},
		},
		{
			name:   "removed cached id is deleted locally, unknown removed id is ignored",
			locals: []LocalMessage{*local("a", 0, false, false), *local("b", 0, false, false)},
			delta:  Delta{Removed: []string{"a", "zzz"}},
			want:   []want{{"a", ActionDeleteLocal, 0}},
		},
		{
			name:   "pending delete on a message the delta reports is pushed",
			locals: []LocalMessage{*local("a", 0, false, true)},
			delta:  Delta{Changed: []Header{hdr("a", 0, 0)}},
			want:   []want{{"a", ActionPushDelete, 0}},
		},
		{
			name:   "unchanged cached messages get no decision",
			locals: []LocalMessage{*local("a", 0, false, false), *local("b", 0, false, false)},
			delta:  Delta{Changed: []Header{hdr("c", 0, 0)}},
			want:   []want{{"c", ActionFetchNew, 0}},
		},
		{
			name:   "local id missing from members is deleted",
			locals: []LocalMessage{*local("4", 0, false, false), *local("6", 0, false, false)},
			delta:  Delta{Members: []Header{hdr("6", 6, 0)}},
			want:   []want{{"4", ActionDeleteLocal, 0}},
		},
		{
			name:     "member above floor missing locally is fetched, newest first",
			locals:   []LocalMessage{*local("6", 0, false, false)},
			delta:    Delta{Members: []Header{hdr("6", 6, 0), hdr("7", 7, 0), hdr("9", 9, 0), hdr("2", 2, 0)}},
			floorUID: 5,
			want:     []want{{"9", ActionFetchNew, 0}, {"7", ActionFetchNew, 0}},
		},
		{
			name:  "changed id that is also a member is decided once",
			delta: Delta{Changed: []Header{hdr("8", 8, storage.FlagSeen)}, Members: []Header{hdr("8", 8, 0)}},
			want:  []want{{"8", ActionFetchNew, 0}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := BuildDeltaPlan(tt.locals, tt.delta, tt.floorUID)
			if len(plan) != len(tt.want) {
				t.Fatalf("plan = %+v, want %+v", plan, tt.want)
			}
			for i, w := range tt.want {
				d := plan[i]
				if d.RemoteID != w.id || d.Action != w.action {
					t.Fatalf("plan[%d] = %+v, want %+v", i, d, w)
				}
				if w.flags != 0 && d.Flags != w.flags {
					t.Fatalf("plan[%d] flags = %v, want %v", i, d.Flags, w.flags)
				}
			}
		})
	}
}
