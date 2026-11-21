package desktop

import goruntime "runtime"

// showWindow restores and focuses the main window. It is used by the tray
// icon, by the single-instance handler (a second launch surfaces the running
// window), and by the macOS mailto url handler. Before the window exists there
// is nothing to show.
//
// Show before unminimise: a window hidden by close-to-tray is hidden, not
// minimised, and unminimising a hidden window on Windows can leave it mapped
// but never painted.
func (a *App) showWindow() {
	if a.win == nil {
		return
	}
	// Show alone is enough on Windows and Linux, but on macOS a window hidden
	// with [NSApp hide:] needs the app itself unhidden too.
	if goruntime.GOOS == "darwin" && a.wapp != nil {
		a.wapp.Show()
	}
	a.win.Show()
	a.win.UnMinimise()
	if a.pendingMaximise.Swap(false) {
		a.win.Maximise()
	}
	a.win.Focus()
	a.bringToFront()
}

// hideApp hides the app the way the close button does with the background
// close action. On macOS this is [NSApp hide:], which the Dock icon can undo;
// ordering the window out instead would strand it. On Windows and Linux it
// hides the window.
func (a *App) hideApp() {
	if goruntime.GOOS == "darwin" {
		if a.wapp != nil {
			a.wapp.Hide()
		}
		return
	}
	if a.win != nil {
		a.win.Hide()
	}
}

// openURL opens a link in the system browser.
func (a *App) openURL(url string) {
	if a.wapp == nil {
		return
	}
	if err := a.wapp.Browser.OpenURL(url); err != nil {
		a.log.Error("open url", "err", err)
	}
}

// nativeTheme is the theme the native window chrome is asked to follow.
type nativeTheme int

const (
	nativeThemeSystem nativeTheme = iota
	nativeThemeDark
	nativeThemeLight
)
