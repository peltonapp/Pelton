package desktop

import (
	goruntime "runtime"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// applyLiquidGlass gives the window Apple's Liquid Glass backdrop when the
// setting is on. It only exists on macOS (26 draws the glass; older versions
// get a plain translucent backdrop), and only at window creation, so the
// setting applies from the next launch.
//
// The glass follows the app's own theme rather than the system's where the
// two differ, so a dark ui does not sit on light glass. A theme change while
// running keeps the glass of the launch until the next one.
func (a *App) applyLiquidGlass(o *application.WebviewWindowOptions) {
	if goruntime.GOOS != "darwin" || !a.boolSetting(settingLiquidGlass, false) {
		return
	}
	style := application.LiquidGlassStyleAutomatic
	switch a.stringSetting(storage.SettingTheme, defaultTheme) {
	case "dark":
		style = application.LiquidGlassStyleDark
	case "light":
		style = application.LiquidGlassStyleLight
	}
	o.Mac.Backdrop = application.MacBackdropLiquidGlass
	o.Mac.LiquidGlass = application.MacLiquidGlass{Style: style}
	o.BackgroundColour = application.NewRGBA(0, 0, 0, 0)
	a.liquidGlass = true
}

// LiquidGlassActive reports whether the running window has the Liquid Glass
// backdrop, which the frontend needs to know to let it show through. It can
// differ from the setting until the next launch.
func (a *App) LiquidGlassActive() bool {
	return a.liquidGlass
}
