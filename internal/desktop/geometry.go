package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// window geometry remembered across launches, so Pelton reopens at the size it
// was left at rather than the same default every time.
const (
	settingWindowWidth     = "window_width"
	settingWindowHeight    = "window_height"
	settingWindowX         = "window_x"
	settingWindowY         = "window_y"
	settingWindowMaximised = "window_maximised"
)

// the window defaults, also the fallback whenever nothing usable is stored.
const (
	defaultWindowWidth  = 1280
	defaultWindowHeight = 820
	minWindowWidth      = 900
	minWindowHeight     = 600
)

// onScreenMargin is how much of the window has to land on the screen for a
// remembered position to be worth restoring. Enough that the title bar stays
// grabbable rather than the window sitting one pixel inside the corner.
const onScreenMargin = 120

// geometry is a remembered window frame. Placed is false when no position was
// stored or the stored one is not usable, in which case the platform picks.
type geometry struct {
	Width     int
	Height    int
	X         int
	Y         int
	Placed    bool
	Maximised bool
}

// fitGeometry clamps a frame to something a screen can actually show. X and Y
// are relative to that screen's work area. Size is bounded by the app's
// minimum and the screen; position is dropped, rather than corrected, when it
// would put the window somewhere the user cannot reach it.
func fitGeometry(g geometry, screenW, screenH int) geometry {
	out := g

	if screenW <= 0 || screenH <= 0 {
		// no usable screen reading: keep the size, drop the position.
		out.Placed = false
		out.Width = clamp(out.Width, minWindowWidth, defaultWindowWidth)
		out.Height = clamp(out.Height, minWindowHeight, defaultWindowHeight)
		return out
	}

	out.Width = clamp(out.Width, minWindowWidth, screenW)
	out.Height = clamp(out.Height, minWindowHeight, screenH)

	if !out.Placed {
		return out
	}
	// far enough onto the screen in both directions to be grabbable, and not
	// past its edges. negative coordinates mean another monitor, which is the
	// case this cannot verify.
	if out.X < 0 || out.Y < 0 ||
		out.X > screenW-onScreenMargin || out.Y > screenH-onScreenMargin {
		out.Placed = false
	}
	return out
}

// clamp bounds v to [lo, hi], falling back to lo when the range is inverted
// (a screen smaller than the app's own minimum).
func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// savedGeometry reads the remembered frame. A zero width or height means
// nothing usable is stored, so the caller leaves the window alone.
func (a *App) savedGeometry() geometry {
	g := geometry{
		Width:     a.intSetting(settingWindowWidth, 0),
		Height:    a.intSetting(settingWindowHeight, 0),
		Maximised: a.boolSetting(settingWindowMaximised, false),
	}
	x := a.intSetting(settingWindowX, -1)
	y := a.intSetting(settingWindowY, -1)
	if x >= 0 && y >= 0 {
		g.X, g.Y, g.Placed = x, y, true
	}
	return g
}

// applySavedGeometry puts the remembered frame into the options the window is
// created with, so it opens where it was left instead of resizing after it
// appears. Screens are not known yet at this point; keepOnScreen checks the
// result once the window is up.
func (a *App) applySavedGeometry(o *application.WebviewWindowOptions) {
	g := a.savedGeometry()
	if g.Width <= 0 || g.Height <= 0 {
		return
	}
	o.Width = max(g.Width, minWindowWidth)
	o.Height = max(g.Height, minWindowHeight)
	if g.Placed {
		o.InitialPosition = application.WindowXY
		o.X, o.Y = g.X, g.Y
	}
	if g.Maximised {
		// maximising would show a window that is meant to start hidden, so it
		// waits for the first showWindow.
		if a.startedHidden {
			a.pendingMaximise.Store(true)
		} else {
			o.StartState = application.WindowStateMaximised
		}
	}
}

// keepOnScreen moves the window back into reach when its remembered frame
// does not fit any attached screen: a monitor that has since been unplugged,
// or a resolution that shrank. It runs once the window is up and the screen
// list is known.
func (a *App) keepOnScreen() {
	if a.win == nil || a.wapp == nil || a.win.IsMaximised() || a.win.IsFullscreen() {
		return
	}
	x, y := a.win.Position()
	w, h := a.win.Size()
	if w <= 0 || h <= 0 {
		return
	}
	var screen *application.Screen
	for _, s := range a.wapp.Screen.GetAll() {
		b := s.WorkArea
		if x >= b.X && y >= b.Y && x < b.X+b.Width && y < b.Y+b.Height {
			screen = s
			break
		}
	}
	if screen == nil {
		screen = a.wapp.Screen.GetPrimary()
	}
	if screen == nil {
		return
	}
	b := screen.WorkArea
	g := fitGeometry(geometry{Width: w, Height: h, X: x - b.X, Y: y - b.Y, Placed: true}, b.Width, b.Height)
	if g.Width != w || g.Height != h {
		a.win.SetSize(g.Width, g.Height)
	}
	if !g.Placed {
		a.win.Center()
	}
}

// saveGeometry records the current frame. Called on shutdown, which covers
// quitting and closing the window, since a maximised window's own size is not
// worth storing (unmaximising would then restore to the full screen).
func (a *App) saveGeometry() {
	if a.win == nil || a.store == nil {
		return
	}
	maximised := a.win.IsMaximised()
	if err := a.store.SetBool(a.ctx, settingWindowMaximised, maximised); err != nil {
		a.log.Error("save window maximised", "err", err)
	}
	if maximised {
		return
	}
	w, h := a.win.Size()
	x, y := a.win.Position()
	if w <= 0 || h <= 0 {
		return
	}
	for key, value := range map[string]int{
		settingWindowWidth:  w,
		settingWindowHeight: h,
		settingWindowX:      x,
		settingWindowY:      y,
	} {
		if err := a.store.SetInt(a.ctx, key, value); err != nil {
			a.log.Error("save window geometry", "key", key, "err", err)
		}
	}
}
