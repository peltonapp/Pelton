package sync

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

// lowerFloor returns the floor that admits the batch newest messages currently
// below current, for one "load older" step. It returns "" once fewer than batch
// messages remain below the floor, which is how the folder reaches "fully
// cached" and stops offering to fetch more. If current is missing from servers,
// it returns "".
func lowerFloor(servers []ServerMessage, current string, batch int) string {
	if current == "" {
		return ""
	}
	idx := -1
	for i, s := range servers {
		if s.RemoteID == current {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	below := servers[idx+1:]
	if batch <= 0 || len(below) <= batch {
		return ""
	}
	return below[batch-1].RemoteID
}

// normalizeFloor clears a floor that no longer holds anything back, which
// happens when the server-side messages below it were deleted. Without this the
// ui would keep offering a "load older" that fetches nothing.
func normalizeFloor(servers []ServerMessage, floorID string) string {
	if floorID == "" {
		return ""
	}
	floorIdx := -1
	for i, s := range servers {
		if s.RemoteID == floorID {
			floorIdx = i
			break
		}
	}
	if floorIdx < 0 {
		return ""
	}
	if floorIdx+1 >= len(servers) {
		return ""
	}
	return floorID
}
