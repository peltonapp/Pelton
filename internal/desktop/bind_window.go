package desktop

// SetWindowTitle updates the native window title so it can reflect context (the
// open message's subject, the current folder, etc.). A blank title is ignored so
// the window never ends up nameless.
func (a *App) SetWindowTitle(title string) {
	if a.win == nil || title == "" {
		return
	}
	a.win.SetTitle(title)
}

// TitleBarDoubleClick runs the system's title-bar double-click action. On macOS
// the app draws its own title bar, so the window server never sees the click
// and cannot act on it; the frontend forwards it here instead. A no-op on every
// other platform, where the native frame handles it.
func (a *App) TitleBarDoubleClick() {
	if a.win == nil {
		return
	}
	switch titleBarDoubleClickAction() {
	case "maximize":
		a.win.ToggleMaximise()
	case "minimize":
		a.win.Minimise()
	}
}

// ToggleFullscreen enters or leaves fullscreen.
func (a *App) ToggleFullscreen() {
	if a.win == nil {
		return
	}
	if a.win.IsFullscreen() {
		a.win.UnFullscreen()
	} else {
		a.win.Fullscreen()
	}
}

// IsFullscreen reports whether the window is fullscreen.
func (a *App) IsFullscreen() bool {
	return a.win != nil && a.win.IsFullscreen()
}

// QuitApp exits the app, as the Quit menu item does.
func (a *App) QuitApp() {
	a.quitApp()
}

// HideApp hides the app, as the Hide menu item does.
func (a *App) HideApp() {
	a.hideApp()
}
