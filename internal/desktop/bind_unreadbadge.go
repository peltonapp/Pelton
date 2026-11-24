package desktop

// SetUnreadBadge puts the unread count on the dock icon, or the unread dot on
// the tray icon on Windows and Linux. The frontend calls it
// whenever the unified inbox count changes, which already covers syncing, marking
// read and deleting, so there is no second count to keep in step here.
//
// A no-op anywhere with neither; see unreadbadge_other.go.
func (a *App) SetUnreadBadge(unread int) {
	if unread < 0 {
		unread = 0
	}
	a.badgeMu.Lock()
	a.unreadBadge = unread
	a.badgeMu.Unlock()
	a.applyUnreadBadge()
}

// applyUnreadBadge pushes the remembered count to the platform, or clears the
// badge when the setting is off or focus mode is on. Also called when either
// changes, so turning it back on does not wait for the next sidebar refresh.
// The lock is held through the push so two calls racing cannot land an older
// count last.
func (a *App) applyUnreadBadge() {
	enabled := a.boolSetting(settingUnreadBadge, true) && !a.focusQuiet()

	a.badgeMu.Lock()
	defer a.badgeMu.Unlock()
	unread := a.unreadBadge
	if !enabled {
		unread = 0
	}
	a.setPlatformBadge(unread)
}
