//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly || windows)

package storage

import "os"

// tryLockFile has no file lock on this platform: every staging tree counts as
// left behind, which is only wrong while another process on the same profile
// is between its commit and the tree's removal.
func tryLockFile(*os.File) (busy bool, err error) { return false, nil }
