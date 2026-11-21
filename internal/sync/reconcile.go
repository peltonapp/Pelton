// Package sync orchestrates Pelton's imap and storage layers to keep a local
// cache and a remote mailbox in agreement, in both directions. It only calls
// the public surface of those two packages and never reaches into their
// internals, so the dependency goes one way: sync depends on imap and storage,
// not the reverse.
//
// The decision logic lives in reconcile.go and is pure: it takes the local and
// server view of a folder and returns a plan, with no database or network
// calls, so every conflict case can be unit tested deterministically.
package sync

import (
	"slices"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Conflict policy, documented here so it is obvious and easy to change later:
//
//   - existence: the server wins. if the server no longer has a message it is
//     removed locally even if it was modified locally. a local delete that the
//     server still has is pushed up (the user's delete intent is honoured while
//     the message still exists on both sides).
//   - flags: per bit. with a pending local change, the bits it touched take the
//     local value, set or cleared, and every other bit takes the server value.
//     without one the server is authoritative.
//
// reconcile is pure: same inputs always give the same Decision.

// LocalMessage is reconcile's view of a cached message.
type LocalMessage struct {
	UID          uint32
	Flags        storage.Flag
	PendingFlags bool // local flag change not yet pushed
	// PendingMask is the flag bits the pending change touched; 0 means \Seen and
	// \Flagged.
	PendingMask   storage.Flag
	PendingDelete bool // local delete not yet pushed
}

// ServerMessage is reconcile's view of a message currently on the server.
type ServerMessage struct {
	UID   uint32
	Flags storage.Flag
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
	UID    uint32
	Action Action
	// Flags is the target flag set for the local store and any server push,
	// meaningful for the flag-related actions.
	Flags storage.Flag
	// Clear is the flag bits to remove on the server, for ActionPushFlags.
	Clear storage.Flag
	// Conflict is true when both sides changed the same message since the last
	// sync, recorded for reporting regardless of the action taken.
	Conflict bool
}

// mergeFlags takes the local value for the bits in mask and the server value
// for the rest.
func mergeFlags(local, server, mask storage.Flag) storage.Flag {
	return local&mask | server&^mask
}

// pendingMask is the bits a pending change owns: the recorded mask, or \Seen
// and \Flagged when none was recorded.
func pendingMask(l *LocalMessage) storage.Flag {
	if l.PendingMask == 0 {
		return storage.FlagSeen | storage.FlagFlagged
	}
	return l.PendingMask
}

// Reconcile decides what to do for a single message given the local and server
// views. Either side may be nil (absent). It performs no io.
func Reconcile(local *LocalMessage, server *ServerMessage) Decision {
	switch {
	case local == nil && server != nil:
		return Decision{UID: server.UID, Action: ActionFetchNew}

	case local != nil && server == nil:
		// server wins for existence. if we had a pending flag change it is a lost
		// update, flagged as a conflict; a pending delete simply agrees with the
		// server and is not a conflict.
		return Decision{
			UID:      local.UID,
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
		return Decision{UID: local.UID, Action: ActionPushDelete}
	}

	if local.PendingFlags {
		mask := pendingMask(local)
		merged := mergeFlags(local.Flags, server.Flags, mask)
		// without a stored baseline we cannot prove both sides changed, so we
		// treat it as a conflict only when the server differs on a bit the local
		// change did not touch.
		conflict := (server.Flags^local.Flags)&^mask != 0
		if merged == server.Flags {
			return Decision{UID: local.UID, Action: ActionClearPending, Flags: merged, Conflict: conflict}
		}
		return Decision{UID: local.UID, Action: ActionPushFlags, Flags: merged, Clear: server.Flags &^ merged, Conflict: conflict}
	}

	// no pending local change: the server is authoritative for flags.
	if server.Flags != local.Flags {
		return Decision{UID: local.UID, Action: ActionAdoptServerFlags, Flags: server.Flags}
	}
	return Decision{UID: local.UID, Action: ActionNone}
}

// BuildPlan reconciles a whole folder. It returns one Decision per message
// across the union of local and server uids, in ascending uid order so the plan
// is deterministic and easy to test. Pure: no io.
//
// floor is the folder's sync floor (see SyncFolder): a server message below it
// is outside the sync window and is left alone entirely, neither fetched nor
// counted as a server-side deletion. A uid below the floor that IS cached
// locally still reconciles normally, so lowering the window can never orphan or
// silently drop mail the user already has.
func BuildPlan(locals []LocalMessage, servers []ServerMessage, floor uint32) []Decision {
	localByUID := make(map[uint32]LocalMessage, len(locals))
	for _, l := range locals {
		localByUID[l.UID] = l
	}
	serverByUID := make(map[uint32]ServerMessage, len(servers))
	for _, s := range servers {
		serverByUID[s.UID] = s
	}

	uids := unionUIDs(localByUID, serverByUID)
	plan := make([]Decision, 0, len(uids))
	for _, uid := range uids {
		local, hasLocal := localByUID[uid]
		server, hasServer := serverByUID[uid]
		if uid < floor && !hasLocal {
			continue
		}
		var lp *LocalMessage
		var sp *ServerMessage
		if hasLocal {
			l := local
			lp = &l
		}
		if hasServer {
			s := server
			sp = &s
		}
		plan = append(plan, Reconcile(lp, sp))
	}
	return plan
}

func unionUIDs(local map[uint32]LocalMessage, server map[uint32]ServerMessage) []uint32 {
	set := make(map[uint32]struct{}, len(local)+len(server))
	for uid := range local {
		set[uid] = struct{}{}
	}
	for uid := range server {
		set[uid] = struct{}{}
	}
	uids := make([]uint32, 0, len(set))
	for uid := range set {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	return uids
}
