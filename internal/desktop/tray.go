//go:build windows || linux

package desktop

import "fmt"

// The tray icon. With the background close action, closing the window leaves
// Pelton syncing with no visible way to get it back or quit it; the tray icon
// is that way. Left click reopens the window, right click shows the menu
// below, wired to the same runtime calls and menu action events as the native
// menu. What serves the icon is per platform: tray_windows.go for the
// notification area, tray_linux.go for the StatusNotifierItem on the session
// bus. macOS has no tray, the Dock icon reopens the hidden window there.

// trayMenuItem is one entry of the tray menu; a nil action marks a separator.
type trayMenuItem struct {
	label  string
	action func()
}

// trayMenuItems is the tray menu. show is what brings the window forward on
// the platform at hand. Labels use the same Go-side translation table as the
// native menu; like that menu, the tray is built in the language active at
// startup.
func (a *App) trayMenuItems(show func()) []trayMenuItem {
	s := menuStringsFor(a.stringSetting(settingLanguage, "en"))
	return []trayMenuItem{
		{label: s.openWindow, action: show},
		{label: s.compose, action: func() {
			show()
			a.emit(EventMenu, "compose")
		}},
		{label: s.syncNow, action: func() {
			a.emit(EventMenu, "sync")
		}},
		{label: s.lockVaults, action: a.lockMailVaults},
		{},
		{label: s.quit, action: a.quitApp},
	}
}

// trayUnreadText is the unread count as the tray tooltip shows it. Unlike the
// menu, it follows the language setting as it changes, since it is rebuilt on
// every count anyway.
func trayUnreadText(a *App, count int) string {
	return fmt.Sprintf(menuStringsFor(a.stringSetting(settingLanguage, "en")).trayUnread, count)
}
