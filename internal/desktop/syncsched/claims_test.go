package syncsched

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

// Take hands out the first unclaimed ids in the caller's order, up to max,
// and skips the ones another token holds, in this folder only.
func TestClaimsTakeSkipsHeldAndKeepsOrder(t *testing.T) {
	var c Claims
	_, got := c.Take(1, []string{"9", "8", "7"}, 2)
	if !slices.Equal(got, []string{"9", "8"}) {
		t.Fatalf("first take %v, want [9 8]", got)
	}
	_, got = c.Take(1, []string{"9", "8", "7", "6", "5"}, 2)
	if !slices.Equal(got, []string{"7", "6"}) {
		t.Fatalf("second take %v, want [7 6]", got)
	}
	_, got = c.Take(2, []string{"9", "8"}, 5)
	if !slices.Equal(got, []string{"9", "8"}) {
		t.Fatalf("another folder's take %v, want [9 8]: claims are per folder", got)
	}
	if n := c.Held(1); n != 4 {
		t.Fatalf("held %d in folder 1, want 4", n)
	}
	if !c.Claimed(1, "7") || c.Claimed(1, "5") {
		t.Fatal("Claimed disagrees with the takes")
	}
	if _, got := c.Take(1, []string{"9", "8"}, 5); len(got) != 0 {
		t.Fatalf("take of held ids got %v, want none", got)
	}
	if _, got := c.Take(1, []string{"4", "4"}, 5); !slices.Equal(got, []string{"4"}) {
		t.Fatalf("take of a repeated id got %v, want it once", got)
	}
}

// Release frees only its own token's ids; a stale or unknown token, or
// another folder, frees nothing.
func TestClaimsReleaseFreesOnlyItsToken(t *testing.T) {
	var c Claims
	a, _ := c.Take(1, []string{"1", "2"}, 5)
	b, _ := c.Take(1, []string{"3"}, 5)
	if a == b {
		t.Fatal("two takes share a token")
	}
	c.Release(2, a) // wrong folder
	if c.Held(1) != 3 {
		t.Fatalf("held %d after a release in another folder, want 3", c.Held(1))
	}
	c.Release(1, a)
	if c.Held(1) != 1 || !c.Claimed(1, "3") || c.Claimed(1, "1") {
		t.Fatalf("after releasing a: held %d, want only b's id 3", c.Held(1))
	}
	c.Release(1, a) // twice: no-op
	if c.Held(1) != 1 {
		t.Fatal("a second release of a freed b's id")
	}
	// A new owner of id 1 is not freed by the old token.
	d, got := c.Take(1, []string{"1"}, 1)
	if len(got) != 1 {
		t.Fatal("a released id cannot be taken again")
	}
	c.Release(1, a)
	if !c.Claimed(1, "1") {
		t.Fatal("a stale token freed a later holder's id")
	}
	c.Release(1, b)
	c.Release(1, d)
	if c.Held(1) != 0 {
		t.Fatalf("held %d after every release, want 0", c.Held(1))
	}
	// An empty take still gets a token, whose release does nothing.
	e, got := c.Take(1, nil, 5)
	if len(got) != 0 {
		t.Fatal("an empty take got ids")
	}
	c.Release(1, e)
}

// 50 goroutines taking and releasing from one folder never hold the same
// id at once.
func TestClaimsConcurrentTakeNeverHandsOutAnIdTwice(t *testing.T) {
	var c Claims
	ids := make([]string, 200)
	for i := range ids {
		ids[i] = fmt.Sprint(i)
	}
	var mu sync.Mutex
	owner := map[string]int{}
	var wg sync.WaitGroup
	for g := range 50 {
		wg.Go(func() {
			for range 100 {
				tok, got := c.Take(7, ids, 5)
				mu.Lock()
				for _, id := range got {
					if o, ok := owner[id]; ok {
						t.Errorf("id %s handed to %d while %d holds it", id, g, o)
					}
					owner[id] = g
				}
				mu.Unlock()
				mu.Lock()
				for _, id := range got {
					delete(owner, id)
				}
				mu.Unlock()
				c.Release(7, tok)
			}
		})
	}
	wg.Wait()
	if n := c.Held(7); n != 0 {
		t.Fatalf("held %d after all released, want 0", n)
	}
}
