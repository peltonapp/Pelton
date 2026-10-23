//go:build windows

package storage

import "golang.org/x/sys/windows"

// freeDiskBytes reports the space available to this user on the volume
// holding dir. ok is false where it cannot be read.
func freeDiskBytes(dir string) (free uint64, ok bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var avail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &totalFree); err != nil {
		return 0, false
	}
	return avail, true
}
