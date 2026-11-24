//go:build !darwin && !linux

package desktop

// watchScreenLock does nothing on Windows: the session lock notification
// needs a window registered with WTSRegisterSessionNotification, which Wails
// does not expose. The idle time and system sleep still lock the app.
func (a *App) watchScreenLock() {}
