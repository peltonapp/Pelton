package desktop

import (
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

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

// fitGeometry clamps a remembered frame to something the current screen can
// actually show. Size is bounded by the app's minimum and the screen; position
// is dropped, rather than corrected, when it would put the window somewhere the
// user cannot reach it.
//
// The position check is deliberately conservative. Wails' ScreenGetAll reports
// each screen's size but not where it sits in the desktop's coordinate space,
// so there is no way to tell whether an off-primary position belongs to a
// monitor that is still attached. Rather than risk restoring a window onto a
// display that has been unplugged, anything outside the current screen is
// treated as not usable and the platform places the window instead. The cost is
// that a window left on a second monitor comes back on the first one.
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

// restoreGeometry applies the remembered frame to the window. Called from
// startup, once the store is open, so the window is already on screen: the size
// change is visible as a brief resize on launch, which is the price of the
// settings table being the single source of truth for everything the app
// remembers.
func (a *App) restoreGeometry() {
	if a.ctx == nil {
		return
	}
	g := a.savedGeometry()
	if g.Width <= 0 || g.Height <= 0 {
		return
	}

	screenW, screenH := 0, 0
	if screens, err := wailsruntime.ScreenGetAll(a.ctx); err == nil {
		for _, s := range screens {
			if s.IsCurrent {
				screenW, screenH = s.Size.Width, s.Size.Height
				break
			}
		}
	}

	g = fitGeometry(g, screenW, screenH)
	wailsruntime.WindowSetSize(a.ctx, g.Width, g.Height)
	if g.Placed {
		wailsruntime.WindowSetPosition(a.ctx, g.X, g.Y)
	} else {
		wailsruntime.WindowCenter(a.ctx)
	}
	// after the size, so unmaximising lands on the remembered frame.
	if g.Maximised {
		if a.startedHidden {
			a.pendingMaximise.Store(true)
			return
		}
		wailsruntime.WindowMaximise(a.ctx)
	}
}

// saveGeometry records the current frame. Called on shutdown, which covers
// quitting and closing the window, since a maximised window's own size is not
// worth storing (unmaximising would then restore to the full screen).
func (a *App) saveGeometry() {
	if a.ctx == nil || a.store == nil {
		return
	}
	maximised := wailsruntime.WindowIsMaximised(a.ctx)
	if err := a.store.SetBool(a.ctx, settingWindowMaximised, maximised); err != nil {
		a.log.Error("save window maximised", "err", err)
	}
	if maximised {
		return
	}
	w, h := wailsruntime.WindowGetSize(a.ctx)
	x, y := wailsruntime.WindowGetPosition(a.ctx)
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
