package desktop

import (
	goruntime "runtime"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// applyWindowMaterial puts the window over a native material when the setting
// is on: Liquid Glass on macOS (26 draws the glass, older versions get a plain
// translucent backdrop) and Mica on Windows 11. Linux has no equivalent. The
// material can only be chosen when the window is created, so the setting
// applies from the next launch.
//
// On macOS the glass follows the app's own theme rather than the system's
// where the two differ, so a dark ui does not sit on light glass. A theme
// change while running keeps the glass of the launch until the next one.
func (a *App) applyWindowMaterial(o *application.WebviewWindowOptions) {
	if !a.boolSetting(settingWindowMaterial, false) {
		return
	}
	switch goruntime.GOOS {
	case "darwin":
		style := application.LiquidGlassStyleAutomatic
		switch a.stringSetting(storage.SettingTheme, defaultTheme) {
		case "dark":
			style = application.LiquidGlassStyleDark
		case "light":
			style = application.LiquidGlassStyleLight
		}
		o.Mac.Backdrop = application.MacBackdropLiquidGlass
		o.Mac.LiquidGlass = application.MacLiquidGlass{Style: style}
	case "windows":
		o.BackgroundType = application.BackgroundTypeTranslucent
		o.Windows.BackdropType = application.Mica
	default:
		return
	}
	o.BackgroundColour = application.NewRGBA(0, 0, 0, 0)
	a.windowMaterial = true
}

// WindowMaterialActive reports whether the running window sits over a native
// material, which the frontend needs to know to let it show through. It can
// differ from the setting until the next launch.
func (a *App) WindowMaterialActive() bool {
	return a.windowMaterial
}
