package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// close button actions stored under settingCloseAction.
const (
	// closeActionBackground hides the window and keeps sync running.
	closeActionBackground = "background"
	// closeActionQuit exits the app.
	closeActionQuit = "quit"
)

// quitApp exits the app from an explicit user action (the Quit menu item, the
// tray's Quit). It marks the quit as intended so the closing hook lets the
// window go instead of treating it as a window close.
func (a *App) quitApp() {
	if a.wapp == nil {
		return
	}
	a.quitRequested.Store(true)
	a.wapp.Quit()
}

// hideOnClose reports whether closing the window should hide it and leave sync,
// idle and the outbox worker running, rather than exit. That is the default; the
// "quit" close action and an explicit quit (quitApp) both exit instead.
func (a *App) hideOnClose() bool {
	if a.quitRequested.Load() {
		return false
	}
	return a.stringSetting(settingCloseAction, closeActionBackground) != closeActionQuit
}

// CloseWindow does what the window's close button does, for the Close Window
// menu item and its Cmd+W accelerator: honour the close action setting, so the
// two never disagree about what closing means.
func (a *App) CloseWindow() {
	if a.wapp == nil {
		return
	}
	if !a.hideOnClose() {
		a.quitApp()
		return
	}
	a.hideApp()
}

// onWindowClosing is the hook on the main window's close. With the background
// close action it cancels the close and hides the app instead; otherwise it
// cancels the close and quits, so the shutdown path is the same one the Quit
// menu item takes. A close that is part of a quit already under way passes.
func (a *App) onWindowClosing(e *application.WindowEvent) {
	if a.quitRequested.Load() {
		return
	}
	e.Cancel()
	if a.hideOnClose() {
		a.hideApp()
		return
	}
	a.quitApp()
}
