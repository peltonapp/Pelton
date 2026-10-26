//go:build darwin || linux || freebsd

package storage

import "testing"

// A filesystem in its reserve reports negative free blocks on FreeBSD; that
// is no space, not 2^64 blocks of it.
func TestClampFree(t *testing.T) {
	t.Parallel()
	if got := clampFree(int64(-3)); got != 0 {
		t.Errorf("clampFree(-3) = %d, want 0", got)
	}
	if got := clampFree(int64(7)); got != 7 {
		t.Errorf("clampFree(int64 7) = %d, want 7", got)
	}
	if got := clampFree(uint64(7)); got != 7 {
		t.Errorf("clampFree(uint64 7) = %d, want 7", got)
	}
}
