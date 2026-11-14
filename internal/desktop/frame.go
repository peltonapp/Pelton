package desktop

import (
	goruntime "runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// wantsIntegratedFrame reports whether the settings ask for the main window
// to draw its own title bar instead of the operating system's. macOS has
// always done so in both designs; Windows and Linux do in the new design only,
// since the classic one has no top bar to hold the window buttons.
func (a *App) wantsIntegratedFrame() bool {
	if a.boolSetting(settingNativeTitlebar, false) {
		return false
	}
	if goruntime.GOOS == "darwin" {
		return true
	}
	return a.stringSetting(settingDesign, "new") != "classic"
}

// applyWindowFrame sets the main window's frame from the settings before it
// is created.
func (a *App) applyWindowFrame(o *application.WebviewWindowOptions) {
	a.integratedFrame = a.wantsIntegratedFrame()
	if goruntime.GOOS == "darwin" {
		if !a.integratedFrame {
			o.Mac.TitleBar = application.MacTitleBarDefault
		}
		return
	}
	o.Frameless = a.integratedFrame
}

// refreshWindowFrame brings the running window's frame in line with the
// settings. macOS can only choose its title bar when the window is created,
// so there the change waits for the next launch.
func (a *App) refreshWindowFrame() {
	if goruntime.GOOS == "darwin" || a.win == nil {
		return
	}
	want := a.wantsIntegratedFrame()
	if want == a.integratedFrame {
		return
	}
	a.integratedFrame = want
	a.win.SetFrameless(want)
}

// WindowFrameIntegrated reports whether the running main window draws its own
// title bar, which tells the frontend to show window buttons and a drag
// region. It can differ from the setting until the next launch on macOS.
func (a *App) WindowFrameIntegrated() bool {
	return a.integratedFrame
}

// MinimiseWindow minimises the main window, for the window buttons of an
// integrated title bar.
func (a *App) MinimiseWindow() {
	if a.win != nil {
		a.win.Minimise()
	}
}

// ToggleMaximiseWindow maximises the main window or restores it.
func (a *App) ToggleMaximiseWindow() {
	if a.win != nil {
		a.win.ToggleMaximise()
	}
}

// IsWindowMaximised reports whether the main window is maximised.
func (a *App) IsWindowMaximised() bool {
	return a.win != nil && a.win.IsMaximised()
}
