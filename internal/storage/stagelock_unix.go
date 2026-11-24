//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile takes an exclusive lock on f without waiting. busy is true when
// another open file holds it, in this process or another; the lock goes away
// with the file's last close or the holder's death.
func tryLockFile(f *os.File) (busy bool, err error) {
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}
