package syncsched

import "sync"

// Claims are the message ids the running units of a folder's body work have
// taken, so concurrent units (copies of one width entry, and the offline
// download beside them) never fetch the same id twice. A claim lives inside
// one unit only: take right after reading the store what is missing, and
// defer the release on the next line, so a returned error, a panic, a
// cancel or a stop all free it. Nothing is persisted.
//
// Ids are remote ids within one folder. The zero value is ready to use and
// it is safe for concurrent use.
type Claims struct {
	mu   sync.Mutex
	next uint64
	// held maps folder, then remote id, to the token that holds it.
	held map[int64]map[string]uint64
	// owned is what each live token holds, so Release need not scan.
	owned map[uint64]claim
}

type claim struct {
	folder int64
	ids    []string
}

// Take claims, for a new token, the first ids (in the order given, at most
// max) that no other token of the folder holds, and returns the token and
// what it got. An id repeated in ids is taken once. A take that got nothing
// still returns a token; its Release does nothing.
func (c *Claims) Take(folder int64, ids []string, max int) (tok uint64, got []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	tok = c.next
	if max <= 0 || len(ids) == 0 {
		return tok, nil
	}
	if c.held == nil {
		c.held = make(map[int64]map[string]uint64)
		c.owned = make(map[uint64]claim)
	}
	held := c.held[folder]
	for _, id := range ids {
		if len(got) == max {
			break
		}
		if _, ok := held[id]; ok {
			continue
		}
		if held == nil {
			held = make(map[string]uint64)
			c.held[folder] = held
		}
		held[id] = tok
		got = append(got, id)
	}
	if len(got) > 0 {
		c.owned[tok] = claim{folder: folder, ids: got}
	}
	return tok, got
}

// Release frees the ids tok holds in folder. A token released already, or
// one of another folder, frees nothing, so a deferred release never frees a
// later holder's claim.
func (c *Claims) Release(folder int64, tok uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	own, ok := c.owned[tok]
	if !ok || own.folder != folder {
		return
	}
	held := c.held[folder]
	for _, id := range own.ids {
		if held[id] == tok {
			delete(held, id)
		}
	}
	delete(c.owned, tok)
	if len(held) == 0 {
		delete(c.held, folder)
	}
}

// Held is how many ids of folder are claimed now, by every token.
func (c *Claims) Held(folder int64) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.held[folder])
}

// Claimed reports whether a token holds id in folder now.
func (c *Claims) Claimed(folder int64, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.held[folder][id]
	return ok
}
