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
	c.claim(1)
	c.claim(2)
	if !c.clear() || c.len() != 0 || c.clear() {
		t.Fatal("clear did not report and remove every claim exactly once")
	}
}
