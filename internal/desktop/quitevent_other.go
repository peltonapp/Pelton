//go:build !darwin

package desktop

// watchQuitEvent is macOS only: elsewhere there is no Dock, and the tray's
// Quit already goes through quitApp.
func (a *App) watchQuitEvent() {}
