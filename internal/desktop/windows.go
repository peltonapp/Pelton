package desktop

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Secondary windows (#573, #298): a message opened in a window of its own,
// next to the main one. Each loads the same frontend with a query string that
// tells main.ts which root to mount. They share the backend, the store and the
// event bus with the main window; closing one only closes that window.

// remembered size of message windows, shared by all of them.
const (
	settingMessageWindowWidth  = "message_window_width"
	settingMessageWindowHeight = "message_window_height"
)

const (
	defaultMessageWindowWidth  = 860
	defaultMessageWindowHeight = 760
	minMessageWindowWidth      = 480
	minMessageWindowHeight     = 360
)

// messageWindowName is the window name for a message, so a second open of the
// same message brings its window forward instead of opening another.
func messageWindowName(id int64) string {
	return fmt.Sprintf("message-%d", id)
}

// OpenMessageWindow shows a message in its own window, or brings forward the
// window it is already open in. title is the window title, normally the
// subject.
func (a *App) OpenMessageWindow(id int64, title string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.wapp == nil {
		return nil
	}
	name := messageWindowName(id)
	if w, ok := a.wapp.Window.GetByName(name); ok {
		w.Show()
		w.Focus()
		return nil
	}
	if title == "" {
		title = "Pelton"
	}

	query := url.Values{}
	query.Set("window", "message")
	query.Set("id", strconv.FormatInt(id, 10))

	opts := application.WebviewWindowOptions{
		Name:      name,
		Title:     title,
		Width:     max(a.intSetting(settingMessageWindowWidth, defaultMessageWindowWidth), minMessageWindowWidth),
		Height:    max(a.intSetting(settingMessageWindowHeight, defaultMessageWindowHeight), minMessageWindowHeight),
		MinWidth:  minMessageWindowWidth,
		MinHeight: minMessageWindowHeight,
		URL:       "/?" + query.Encode(),
		// the same look as the main window: the dark surface before the first
		// paint, the hidden macOS title bar, the theme-following caption bar.
		BackgroundColour: a.windowOptions.BackgroundColour,
		BackgroundType:   a.windowOptions.BackgroundType,
		Mac:              a.windowOptions.Mac,
		Windows:          a.windowOptions.Windows,
	}
	w := a.wapp.Window.NewWithOptions(opts)
	w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.saveMessageWindowSize(w)
	})
	return nil
}

// saveMessageWindowSize remembers the size a message window was closed at, for
// the next one. Maximised and fullscreen sizes are not worth keeping.
func (a *App) saveMessageWindowSize(w *application.WebviewWindow) {
	if a.store == nil || w.IsMaximised() || w.IsFullscreen() {
		return
	}
	width, height := w.Size()
	if width <= 0 || height <= 0 {
		return
	}
	for key, value := range map[string]int{
		settingMessageWindowWidth:  width,
		settingMessageWindowHeight: height,
	} {
		if err := a.store.SetInt(a.ctx, key, value); err != nil {
			a.log.Error("save message window size", "key", key, "err", err)
		}
	}
}

// focusedSecondaryWindow returns the focused window when it is not the main
// one, or nil.
func (a *App) focusedSecondaryWindow() application.Window {
	if a.wapp == nil || a.win == nil {
		return nil
	}
	current := a.wapp.Window.Current()
	if current == nil || current.Name() == a.win.Name() {
		return nil
	}
	return current
}

// CloseCurrentWindow closes the window it is called from when that is a
// secondary window, and does what the main window's close button does
// otherwise.
func (a *App) CloseCurrentWindow() {
	if w := a.focusedSecondaryWindow(); w != nil {
		w.Close()
		return
	}
	a.CloseWindow()
}
