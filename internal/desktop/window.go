package desktop

import wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

// showWindow restores and focuses the main window. It is used by the tray
// icon, by the single-instance handler (a second launch surfaces the running
// window), and by the macOS mailto url handler. A nil context means the webview
// is not up yet, so there is nothing to show.
//
// Show before unminimise: a window hidden by close-to-tray is hidden, not
// minimised, and unminimising a hidden window on Windows can leave it mapped
// but never painted.
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowShow(a.ctx)
	wailsruntime.WindowUnminimise(a.ctx)
	if a.pendingMaximise.Swap(false) {
		wailsruntime.WindowMaximise(a.ctx)
	}
	bringToFront(a.ctx)
}
