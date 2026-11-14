//go:build windows

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// bringToFront raises the window above every other application.
//
// Show maps the window but does not necessarily raise it. Windows refuses to
// let a process that does not own the foreground steal it, and when a second
// launch surfaces the running instance the caller is the new process while the
// window belongs to the old one, so nothing comes forward: double-clicking the
// desktop icon with Pelton in the tray appeared to do nothing at all.
//
// Making the window topmost and immediately dropping it back goes through
// SetWindowPos, which has no such restriction, and leaves the z-order with
// Pelton on top. There is no always-on-top preference for this to fight with.
func (a *App) bringToFront() {
	if a.win == nil {
		return
	}
	a.win.SetAlwaysOnTop(true)
	a.win.SetAlwaysOnTop(false)
}

// setNativeTheme switches the title bar between dark and light, or back to
// following the system.
func (a *App) setNativeTheme(t nativeTheme) {
	if a.win == nil || !w32.SupportsThemes() {
		return
	}
	dark := t == nativeThemeDark
	if t == nativeThemeSystem {
		dark = w32.IsCurrentlyDarkMode()
	}
	application.InvokeAsync(func() {
		if hwnd := uintptr(a.win.NativeWindow()); hwnd != 0 {
			w32.SetTheme(hwnd, dark)
		}
	})
}
