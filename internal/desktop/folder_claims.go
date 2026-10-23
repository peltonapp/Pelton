package desktop

// folderClaims is the set of folders one kind of sync job currently holds,
// with a rerun mark for a request that arrived while a folder was held. It is
// not safe for concurrent use: its owner guards it with a mutex.
//
// Each claim carries a token. A holder that may outlive its claim's normal
// end (a panic unwinding past it) lets go with releaseOwned or dropOwned and
// its token, which touch the claim only while it is still that holder's: a
// later holder's claim of the same folder has another token.
type folderClaims struct {
	byID map[int64]folderClaim
	last uint64 // the last token handed out
}

type folderClaim struct {
	tok   uint64
	rerun bool
}

// claim takes a free folder. It returns false when the folder is held.
func (c *folderClaims) claim(id int64) bool { return c.claimTok(id) != 0 }

// claimTok takes a free folder and returns the claim's token, or 0 when the
// folder is held.
func (c *folderClaims) claimTok(id int64) uint64 {
	if _, busy := c.byID[id]; busy {
		return 0
	}
	if c.byID == nil {
		c.byID = make(map[int64]folderClaim)
	}
	c.last++
	c.byID[id] = folderClaim{tok: c.last}
	return c.last
}

// claimOrMark takes a free folder, or marks a held one for a rerun and
// returns false.
func (c *folderClaims) claimOrMark(id int64) bool { return c.claimOrMarkTok(id) != 0 }

// claimOrMarkTok is claimOrMark returning the claim's token, 0 when the
// folder was held and is marked instead.
func (c *folderClaims) claimOrMarkTok(id int64) uint64 {
	if cl, busy := c.byID[id]; busy {
		cl.rerun = true
		c.byID[id] = cl
		return 0
	}
	return c.claimTok(id)
}

// release ends a hold. A folder marked meanwhile stays held with the mark
// cleared, and release returns true: the holder runs once more.
func (c *folderClaims) release(id int64) (rerun bool) {
	cl, ok := c.byID[id]
	if !ok {
		return false
	}
	return c.releaseOwned(id, cl.tok)
}

// releaseOwned is release by the holder of token tok. It leaves another
// holder's claim alone and then returns false.
func (c *folderClaims) releaseOwned(id int64, tok uint64) (rerun bool) {
	cl, ok := c.byID[id]
	if !ok || cl.tok != tok {
		return false
	}
	if cl.rerun {
		c.byID[id] = folderClaim{tok: tok}
		return true
	}
	delete(c.byID, id)
	return false
}

// drop ends a hold and forgets any rerun mark.
func (c *folderClaims) drop(id int64) { delete(c.byID, id) }

// dropOwned is drop by the holder of token tok. It leaves another holder's
// claim alone, so a deferred dropOwned after the claim ended is harmless.
func (c *folderClaims) dropOwned(id int64, tok uint64) {
	if cl, ok := c.byID[id]; ok && cl.tok == tok {
		delete(c.byID, id)
	}
}

// held reports whether the folder is held.
func (c *folderClaims) held(id int64) bool {
	_, ok := c.byID[id]
	return ok
}

func (c *folderClaims) len() int { return len(c.byID) }
