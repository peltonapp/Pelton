package desktop

// folderClaims is the set of folders one kind of sync job currently holds,
// with a rerun mark for a request that arrived while a folder was held. It is
// not safe for concurrent use: its owner guards it with accountSync.mu.
type folderClaims struct {
	byID map[int64]bool // folder id -> rerun requested
}

// claim takes a free folder. It returns false when the folder is held.
func (c *folderClaims) claim(id int64) bool {
	if _, busy := c.byID[id]; busy {
		return false
	}
	if c.byID == nil {
		c.byID = make(map[int64]bool)
	}
	c.byID[id] = false
	return true
}

// claimOrMark takes a free folder, or marks a held one for a rerun and
// returns false.
func (c *folderClaims) claimOrMark(id int64) bool {
	if _, busy := c.byID[id]; busy {
		c.byID[id] = true
		return false
	}
	return c.claim(id)
}

// release ends a hold. A folder marked meanwhile stays held with the mark
// cleared, and release returns true: the holder runs once more.
func (c *folderClaims) release(id int64) (rerun bool) {
	if c.byID[id] {
		c.byID[id] = false
		return true
	}
	delete(c.byID, id)
	return false
}

// drop ends a hold and forgets any rerun mark.
func (c *folderClaims) drop(id int64) { delete(c.byID, id) }

// held reports whether the folder is held.
func (c *folderClaims) held(id int64) bool {
	_, ok := c.byID[id]
	return ok
}

func (c *folderClaims) len() int { return len(c.byID) }

// clear drops every hold and reports whether there was any.
func (c *folderClaims) clear() (had bool) {
	had = len(c.byID) > 0
	c.byID = nil
	return had
}
