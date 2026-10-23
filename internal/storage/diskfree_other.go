//go:build !(darwin || linux || freebsd || windows)

package storage

// freeDiskBytes cannot read free space on this platform; callers then skip
// their space checks.
func freeDiskBytes(string) (uint64, bool) { return 0, false }
