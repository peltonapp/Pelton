package sync

import "slices"

// The sync window (#175). A folder's cache does not have to cover the whole
// mailbox: sync_floor_id records the opaque remote id at the low edge of the
// newest-first window, and everything after that in the server list is left on
// the server until the user asks for it. Without this the first sync of a
// decade-old mailbox downloads every message body, oldest first, and the user
// watches 2016 arrive for half an hour before today's mail shows up.
//
// The floor is a remote id rather than a count because ids are what reconcile
// works in and what the server can be asked about cheaply. "" always means "no
// floor": the folder is cached in full, which is the state every folder synced
// by an older version is already in, so an upgrade changes nothing for them.
// servers is always newest-first; ordering is by position in that slice, not
// by string sort.

// floorForLimit returns the floor that keeps exactly the limit newest messages
// in the window. It returns "" when the limit is unlimited (<= 0) or the folder
// already has no more than limit messages, since there is then nothing to hold
// back.
func floorForLimit(servers []ServerMessage, limit int) string {
	if limit <= 0 || len(servers) <= limit {
		return ""
	}
	return servers[limit-1].RemoteID
}

// floorRef is a stored window floor: its remote id, its legacy uid when the
// protocol has one (0 otherwise), and how many listed server messages are
// cached, which places a vanished floor when there is no uid to go by.
type floorRef struct {
	ID     string
	UID    uint32
	Cached int
}

// floorIndex returns the floor's position in servers. When the floor message
// is gone from the server, found is false and idx is the slot it held: the
// first message older than it by legacy uid, or, without uids, the first one
// after the cached ones. idx is len(servers) when nothing is older.
func floorIndex(servers []ServerMessage, f floorRef) (idx int, found bool) {
	for i, s := range servers {
		if s.RemoteID == f.ID {
			return i, true
		}
	}
	if f.UID > 0 && slices.ContainsFunc(servers, func(s ServerMessage) bool { return s.LegacyUID != 0 }) {
		for i, s := range servers {
			if s.LegacyUID != 0 && s.LegacyUID < f.UID {
				return i, false
			}
		}
		return len(servers), false
	}
	return min(f.Cached, len(servers)), false
}

// lowerFloor returns the floor that admits the batch newest messages currently
// below current, for one "load older" step. It returns "" once fewer than batch
// messages remain below the floor, which is how the folder reaches "fully
// cached" and stops offering to fetch more. A floor message deleted on the
// server still marks where the window ends (see floorIndex).
func lowerFloor(servers []ServerMessage, current floorRef, batch int) string {
	if current.ID == "" {
		return ""
	}
	idx, found := floorIndex(servers, current)
	if found {
		idx++
	}
	below := servers[idx:]
	if batch <= 0 || len(below) <= batch {
		return ""
	}
	return below[batch-1].RemoteID
}

// normalizeFloor clears a floor that no longer holds anything back, which
// happens when the server-side messages below it were deleted. Without this the
// ui would keep offering a "load older" that fetches nothing. A floor message
// deleted on the server moves the floor to the next older message, so the
// window keeps its size instead of opening onto the whole folder.
func normalizeFloor(servers []ServerMessage, floor floorRef) string {
	if floor.ID == "" {
		return ""
	}
	idx, _ := floorIndex(servers, floor)
	if idx+1 >= len(servers) {
		return ""
	}
	return servers[idx].RemoteID
}
