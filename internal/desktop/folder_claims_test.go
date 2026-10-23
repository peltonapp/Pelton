package desktop

import "testing"

func TestFolderClaims(t *testing.T) {
	var c folderClaims
	if !c.claim(7) {
		t.Fatal("first claim of a free folder failed")
	}
	if c.claim(7) {
		t.Fatal("a held folder was claimed twice")
	}
	if c.claimOrMark(7) {
		t.Fatal("claimOrMark took a held folder")
	}
	// the mark keeps the folder held for one rerun, then frees it.
	if !c.release(7) {
		t.Fatal("release after a mark did not ask for a rerun")
	}
	if !c.held(7) {
		t.Fatal("a folder asked to rerun was let go")
	}
	if c.release(7) {
		t.Fatal("a second release asked for another rerun")
	}
	if c.held(7) {
		t.Fatal("folder still held after its last release")
	}
	// drop forgets a mark without a rerun.
	c.claim(8)
	c.claimOrMark(8)
	c.drop(8)
	if c.held(8) || c.len() != 0 {
		t.Fatalf("drop left %d claims", c.len())
	}
}

// A holder's token lets it go only of its own claim: once it let go and
// another holder took the folder, its late release or drop leaves that one.
func TestFolderClaimTokens(t *testing.T) {
	var c folderClaims
	first := c.claimTok(7)
	if first == 0 || c.claimTok(7) != 0 {
		t.Fatal("claimTok did not take a free folder exactly once")
	}
	if c.releaseOwned(7, first) {
		t.Fatal("an unmarked release asked for a rerun")
	}
	second := c.claimOrMarkTok(7)
	if second == 0 || second == first {
		t.Fatalf("second claim token = %d, want a new one (first %d)", second, first)
	}
	c.dropOwned(7, first)
	if c.releaseOwned(7, first) || !c.held(7) {
		t.Fatal("the first holder's late drop or release freed the second holder's claim")
	}
	if c.claimOrMarkTok(7) != 0 {
		t.Fatal("claimOrMarkTok took a held folder")
	}
	if !c.releaseOwned(7, second) || !c.held(7) {
		t.Fatal("a marked release did not keep the claim for a rerun")
	}
	c.dropOwned(7, second)
	if c.held(7) {
		t.Fatal("the holder's drop left its claim")
	}
}
