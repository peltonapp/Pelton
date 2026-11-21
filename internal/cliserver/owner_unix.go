//go:build !windows

package cliserver

import (
	"os"
	"syscall"
)

// ownedByUser reports whether the current user owns the file.
func ownedByUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

func socketLike(os.FileInfo) bool { return false }
