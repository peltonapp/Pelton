// Package sync orchestrates Pelton's imap and storage layers to keep a local
// cache and a remote mailbox in agreement, in both directions. It only calls
// the public surface of those two packages and never reaches into their
// internals, so the dependency goes one way: sync depends on imap and storage,
// not the reverse.
//
// The decision logic lives in reconcile.go and is pure: it takes the local and
// server view of a folder and returns a plan, with no database or network
// calls, so every conflict case can be unit tested deterministically. The
// engine never sees an IMAP UID, only a string remote id.
package sync

import (
	"sort"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Conflict policy, documented here so it is obvious and easy to change later:
//
//   - existence: the server wins. if the server no longer has a message it is
//     removed locally even if it was modified locally. a local delete that the
//     server still has is pushed up (the user's delete intent is honoured while
//     the message still exists on both sides).
//   - flags: union/merge. the result is the bitwise OR of local and server
//     flags, so if either side set \Seen or \Flagged it stays set. this means a
//     local "mark unread" (clearing \Seen) is NOT propagated in this version,
//     because union never clears a flag. that is a deliberate v1 limitation: it
//     guarantees we never lose a "seen" state, at the cost of not syncing
//     un-flagging. a future version can track per-flag change direction.
//
// reconcile is pure: same inputs always give the same Decision.

// LocalMessage is reconcile's view of a cached message.
type LocalMessage struct {
	RemoteID      string
	Flags         storage.Flag
	PendingFlags  bool // local flag change not yet pushed
	PendingDelete bool // local delete not yet pushed
}

// ServerMessage is reconcile's view of a message currently on the server.
type ServerMessage struct {
	RemoteID string
	Flags    storage.Flag
}

// Action is the single operation reconcile decides on for one message.
type Action int

const (
	// ActionNone means local and server already agree.
	ActionNone Action = iota
	// ActionFetchNew means the server has a message the cache does not: fetch
	// its body and attachments and insert it.
	ActionFetchNew
	// ActionDeleteLocal means the server no longer has a message the cache does:
	// remove it from the cache. no server call.
	ActionDeleteLocal
	// ActionAdoptServerFlags means the server's flags changed and there is no
	// pending local change, so store the server flags locally.
	ActionAdoptServerFlags
	// ActionPushFlags means push the merged flags to the server, store the merge
	// locally and clear the pending marker.
	ActionPushFlags
	// ActionPushDelete means delete the message on the server, then remove it
	// from the cache and clear the pending marker.
	ActionPushDelete
	// ActionClearPending means a pending local flag change is already satisfied
	// on the server, so just store the merge locally and clear the marker.
	ActionClearPending
)

// Decision is reconcile's output for one message.
type Decision struct {
	RemoteID string
	Action   Action
	// Flags is the target flag set for the local store and any server push,
	// meaningful for the flag-related actions.
	Flags storage.Flag
	// Conflict is true when both sides changed the same message since the last
	// sync, recorded for reporting regardless of the action taken.
	Conflict bool
}

// mergeFlags applies the union policy.
func mergeFlags(local, server storage.Flag) storage.Flag {
	return local | server
}

// Reconcile decides what to do for a single message given the local and server
// views. Either side may be nil (absent). It performs no io.
func Reconcile(local *LocalMessage, server *ServerMessage) Decision {
	switch {
	case local == nil && server != nil:
		return Decision{RemoteID: server.RemoteID, Action: ActionFetchNew}

	case local != nil && server == nil:
		// server wins for existence. if we had a pending flag change it is a lost
		// update, flagged as a conflict; a pending delete simply agrees with the
		// server and is not a conflict.
		return Decision{
			RemoteID: local.RemoteID,
			Action:   ActionDeleteLocal,
			Conflict: local.PendingFlags && !local.PendingDelete,
		}

	case local != nil && server != nil:
		return reconcileBoth(local, server)
	}

	// both nil cannot happen through BuildPlan, but stay total.
	return Decision{Action: ActionNone}
}

func reconcileBoth(local *LocalMessage, server *ServerMessage) Decision {
	// a local delete intent wins while the message still exists on both sides.
	if local.PendingDelete {
		return Decision{RemoteID: local.RemoteID, Action: ActionPushDelete}
	}

	if local.PendingFlags {
		merged := mergeFlags(local.Flags, server.Flags)
		// without a stored baseline we cannot prove both sides changed, so we
		// treat it as a conflict only when the server independently has a managed
		// flag the local copy lacks. a pending change the server simply has not
		// received yet (server flags are a subset of local) is not a conflict.
		conflict := server.Flags&^local.Flags != 0
		if merged == server.Flags {
			// server already has everything we wanted, nothing to push up.
			return Decision{RemoteID: local.RemoteID, Action: ActionClearPending, Flags: merged, Conflict: conflict}
		}
		return Decision{RemoteID: local.RemoteID, Action: ActionPushFlags, Flags: merged, Conflict: conflict}
	}

	// no pending local change: the server is authoritative for flags.
	if server.Flags != local.Flags {
		return Decision{RemoteID: local.RemoteID, Action: ActionAdoptServerFlags, Flags: server.Flags}
	}
	return Decision{RemoteID: local.RemoteID, Action: ActionNone}
}

// BuildPlan reconciles a whole folder. It returns one Decision per message
// across the union of local and server remote ids, in newest-first servers
// order (then local-only ids), so the engine can walk the plan front to back.
// Pure: no io.
//
// floorID is the folder's sync floor (see SyncFolder): a server message strictly
// after it in the newest-first servers slice is outside the sync window and is
// left alone entirely, neither fetched nor counted as a server-side deletion.
// A remote id below the floor that IS cached locally still reconciles normally,
// so lowering the window can never orphan or silently drop mail the user
// already has. Ids are ordered by their position in servers, not by string sort.
func BuildPlan(locals []LocalMessage, servers []ServerMessage, floorID string) []Decision {
	localByID := make(map[string]LocalMessage, len(locals))
	for _, l := range locals {
		localByID[l.RemoteID] = l
	}

	floorIdx := -1
	if floorID != "" {
		floorIdx = -2 // not found: treat every server id as below the floor
		for i, s := range servers {
			if s.RemoteID == floorID {
				floorIdx = i
				break
			}
		}
	}

	plan := make([]Decision, 0, len(localByID)+len(servers))
	seen := make(map[string]struct{}, len(localByID)+len(servers))

	for i, s := range servers {
		id := s.RemoteID
		seen[id] = struct{}{}
		_, hasLocal := localByID[id]
		// skip uncached server ids strictly after the floor
		if floorID != "" && (floorIdx < 0 || i > floorIdx) && !hasLocal {
			continue
		}
		local, hasLocal := localByID[id]
		var lp *LocalMessage
		if hasLocal {
			l := local
			lp = &l
		}
		sp := s
		plan = append(plan, Reconcile(lp, &sp))
	}

	for _, l := range locals {
		if _, ok := seen[l.RemoteID]; ok {
			continue
		}
		local := l
		plan = append(plan, Reconcile(&local, nil))
	}
	return plan
}

// BuildDeltaPlan reconciles a folder from a Delta instead of a full snapshot.
// Only messages the delta names get a Decision; every other cached message is
// unchanged by definition. floorUID is the folder's numeric sync floor (0 =
// none): a new message whose LegacyUID is below it is outside the window and
// is skipped, as in BuildPlan. A message with no LegacyUID has no
// numeric position and is always admitted. Pure: no io.
//
// Order: Changed in delta order, then members missing locally (highest uid
// first), then local messages absent from Members, then Removed.
func BuildDeltaPlan(locals []LocalMessage, d Delta, floorUID uint32) []Decision {
	localByID := make(map[string]LocalMessage, len(locals))
	for _, l := range locals {
		localByID[l.RemoteID] = l
	}
	belowFloor := func(uid uint32) bool { return floorUID != 0 && uid != 0 && uid < floorUID }

	var plan []Decision
	seen := make(map[string]struct{}, len(d.Changed)+len(d.Removed))
	for _, h := range d.Changed {
		if _, dup := seen[h.RemoteID]; dup {
			continue
		}
		seen[h.RemoteID] = struct{}{}
		local, has := localByID[h.RemoteID]
		if !has && belowFloor(h.LegacyUID) {
			continue
		}
		var lp *LocalMessage
		if has {
			l := local
			lp = &l
		}
		plan = append(plan, Reconcile(lp, &ServerMessage{RemoteID: h.RemoteID, Flags: h.Flags}))
	}

	if d.Members != nil {
		members := make(map[string]struct{}, len(d.Members))
		var missing []Header
		for _, m := range d.Members {
			members[m.RemoteID] = struct{}{}
			if _, ok := seen[m.RemoteID]; ok {
				continue
			}
			if _, has := localByID[m.RemoteID]; has || belowFloor(m.LegacyUID) {
				continue
			}
			missing = append(missing, m)
		}
		sort.SliceStable(missing, func(i, j int) bool { return missing[i].LegacyUID > missing[j].LegacyUID })
		for _, m := range missing {
			seen[m.RemoteID] = struct{}{}
			plan = append(plan, Decision{RemoteID: m.RemoteID, Action: ActionFetchNew})
		}
		for _, l := range locals {
			if _, ok := members[l.RemoteID]; ok {
				continue
			}
			if _, ok := seen[l.RemoteID]; ok {
				continue
			}
			seen[l.RemoteID] = struct{}{}
			gone := l
			plan = append(plan, Reconcile(&gone, nil))
		}
	}

	for _, id := range d.Removed {
		if _, ok := seen[id]; ok {
			continue
		}
		local, has := localByID[id]
		if !has {
			continue
		}
		seen[id] = struct{}{}
		gone := local
		plan = append(plan, Reconcile(&gone, nil))
	}
	return plan
}
