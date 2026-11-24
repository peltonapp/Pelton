//go:build linux

package desktop

import (
	"os/exec"
	"strings"
)

// peltonDesktopFile is the .desktop id shipped in build/linux/pelton.desktop,
// which declares the mailto scheme handler.
const peltonDesktopFile = "pelton.desktop"

func isDefaultMailHandler() (isDefault bool, known bool) {
	out, err := exec.Command("xdg-settings", "get", "default-url-scheme-handler", "mailto").Output()
	if err != nil {
		// no xdg-settings (headless, minimal desktop): cannot answer reliably.
		return false, false
	}
	return strings.TrimSpace(string(out)) == peltonDesktopFile, true
}

func isDefaultCalendarHandler() (isDefault bool, known bool) {
	out, err := exec.Command("xdg-mime", "query", "default", "text/calendar").Output()
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(string(out)) == peltonDesktopFile, true
}

func setDefaultCalendarHandler() error {
	return exec.Command("xdg-mime", "default", peltonDesktopFile, "text/calendar").Run()
}

func setDefaultMailHandler() error {
	return exec.Command("xdg-settings", "set", "default-url-scheme-handler", "mailto", peltonDesktopFile).Run()
}
