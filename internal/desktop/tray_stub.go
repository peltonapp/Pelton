//go:build !windows && !linux

package desktop

import "strconv"

// No tray on macOS (see tray.go): the Dock icon reopens the hidden window,
// and Quit lives in the native menu.

func (a *App) startTray() {}

func (a *App) stopTray() {}

// badgeCap is the largest count the Dock badge spells out; past it, it says
// 999+ rather than growing wider than the icon.
const badgeCap = 999

// setPlatformBadge puts the unread count on the Dock icon, or clears it at
// zero. It waits on the main thread, so it must not be called from there; the
// bound call that drives it never is.
func (a *App) setPlatformBadge(count int) {
	if a.dock == nil {
		return
	}
	var err error
	switch {
	case count > badgeCap:
		err = a.dock.SetBadge(strconv.Itoa(badgeCap) + "+")
	case count > 0:
		err = a.dock.SetBadge(strconv.Itoa(count))
	default:
		err = a.dock.RemoveBadge()
	}
	if err != nil {
		a.log.Debug("dock badge", "err", err)
	}
}
