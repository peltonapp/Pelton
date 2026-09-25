//go:build !windows

package desktop

// Mica is a Windows 11 window material, so there is nothing to ask for
// anywhere else.
func supportsMica() bool {
	return false
}
