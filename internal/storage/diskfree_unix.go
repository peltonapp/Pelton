//go:build darwin || linux || freebsd

package storage

import "golang.org/x/sys/unix"

// freeDiskBytes reports the space available to this user on the filesystem
// holding dir. ok is false where it cannot be read.
func freeDiskBytes(dir string) (free uint64, ok bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return clampFree(st.Bavail) * uint64(st.Bsize), true
}

// clampFree turns a free block count into an unsigned one. FreeBSD's is
// signed and goes negative when a filesystem dips into its reserve; that is
// no space, not a huge amount of it.
func clampFree[T int64 | uint64](blocks T) uint64 {
	if blocks < 0 {
		return 0
	}
	return uint64(blocks)
}
