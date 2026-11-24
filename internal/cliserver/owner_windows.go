//go:build windows

package cliserver

import "os"

// ownedByUser is true on Windows, where the folder sits in the user's own
// profile and inherits its access list.
func ownedByUser(os.FileInfo) bool { return true }

// socketLike accepts the reparse point a Unix domain socket leaves on
// Windows.
func socketLike(info os.FileInfo) bool { return info.Mode()&os.ModeIrregular != 0 }
