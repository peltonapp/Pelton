//go:build !darwin && !linux && !windows

package desktop

import "errors"

// On any other platform there is no known way to read or set the default mail
// handler, so detection reports "unknown" and the set action is unsupported.

func isDefaultMailHandler() (isDefault bool, known bool) {
	return false, false
}

func isDefaultCalendarHandler() (isDefault bool, known bool) {
	return false, false
}

func setDefaultCalendarHandler() error {
	return errors.New("opening .ics files with Pelton is not supported on this platform")
}

func setDefaultMailHandler() error {
	return errors.New("setting the default mail client is not supported on this platform")
}
