//go:build windows

package desktop

import "os/exec"

// Windows does not let an app set itself as the default mail handler
// programmatically, and reliable detection depends on the ProgId the installer
// registers under SOFTWARE\Clients\Mail. Until that is wired, detection returns
// "unknown" (so the About line stays hidden rather than guessing wrong) and the
// set action opens the Settings "Default apps" page for the user to choose.

func isDefaultMailHandler() (isDefault bool, known bool) {
	return false, false
}

// .ics files: the installer lists Pelton under the extension's
// OpenWithProgids, so it shows in "Open with" without taking the default. The
// default itself is the user's to pick in Settings.
func isDefaultCalendarHandler() (isDefault bool, known bool) {
	return false, false
}

func setDefaultCalendarHandler() error {
	return setDefaultMailHandler()
}

func setDefaultMailHandler() error {
	// "start" is a cmd builtin, so it runs through cmd /c; the empty title arg
	// keeps start from treating the URI as the window title.
	return exec.Command("cmd", "/c", "start", "", "ms-settings:defaultapps").Run()
}
