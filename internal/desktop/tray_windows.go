//go:build windows

package desktop

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The notification-area icon is the Wails system tray (#568).

// trayMu guards tray, the icon once it exists, and trayDotted, the icon with
// the unread dot. trayDotted stays nil if the icon could not be drawn on, and
// the tooltip alone then carries the count.
var (
	trayMu     sync.Mutex
	tray       *application.SystemTray
	trayDotted []byte
)

// startTray brings up the tray icon. Left click reopens the window; right
// click shows the menu, which the tray does on its own once it has one.
func (a *App) startTray() {
	if a.wapp == nil {
		return
	}
	menu := application.NewMenu()
	for _, item := range a.trayMenuItems(a.showWindow) {
		if item.action == nil {
			menu.AddSeparator()
			continue
		}
		action := item.action
		menu.Add(item.label).OnClick(func(*application.Context) { action() })
	}

	t := a.wapp.SystemTray.New()
	if len(a.trayIcon) > 0 {
		t.SetIcon(a.trayIcon)
	}
	t.SetTooltip("Pelton")
	t.SetMenu(menu)
	t.OnClick(a.showWindow)

	var dotted []byte
	if len(a.trayIcon) > 0 {
		var err error
		if dotted, err = dottedICO(a.trayIcon); err != nil {
			a.log.Error("tray icon: draw the unread dot", "err", err)
		}
	}

	trayMu.Lock()
	tray, trayDotted = t, dotted
	trayMu.Unlock()
	// the frontend may have reported a count before the tray was up.
	a.applyUnreadBadge()
}

// stopTray removes the tray icon. Safe to call even if the tray never came up;
// Wails also removes it when the app quits.
func (a *App) stopTray() {
	trayMu.Lock()
	t := tray
	tray = nil
	trayMu.Unlock()
	if t != nil {
		t.Destroy()
	}
}

// setPlatformBadge shows the unread dot on the tray icon, with the count in
// its tooltip. Zero clears both.
func (a *App) setPlatformBadge(count int) {
	trayMu.Lock()
	t, dotted := tray, trayDotted
	trayMu.Unlock()
	if t == nil {
		return
	}
	icon := a.trayIcon
	if count > 0 && dotted != nil {
		icon = dotted
	}
	if len(icon) > 0 {
		t.SetIcon(icon)
		t.SetDarkModeIcon(icon)
	}
	tip := "Pelton"
	if count > 0 {
		tip += "\n" + trayUnreadText(a, count)
	}
	t.SetTooltip(tip)
}

// dottedICO is the icon with the unread dot on every frame.
func dottedICO(ico []byte) ([]byte, error) {
	frames, err := iconFrames(ico)
	if err != nil {
		return nil, err
	}
	for i, f := range frames {
		frames[i] = withDot(f)
	}
	return encodeICO(frames)
}
