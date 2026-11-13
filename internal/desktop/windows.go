package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"sync/atomic"

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
		a.saveWindowSize(w, settingMessageWindowWidth, settingMessageWindowHeight)
	})
	return nil
}

// saveWindowSize remembers the size a secondary window was closed at, for the
// next one of its kind. Maximised and fullscreen sizes are not worth keeping.
func (a *App) saveWindowSize(w *application.WebviewWindow, widthKey, heightKey string) {
	if a.store == nil || w.IsMaximised() || w.IsFullscreen() {
		return
	}
	width, height := w.Size()
	if width <= 0 || height <= 0 {
		return
	}
	for key, value := range map[string]int{widthKey: width, heightKey: height} {
		if err := a.store.SetInt(a.ctx, key, value); err != nil {
			a.log.Error("save window size", "key", key, "err", err)
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
// otherwise. A compose window asking to close has already settled its draft,
// so its closing hook lets it go.
func (a *App) CloseCurrentWindow(ctx context.Context) {
	w, _ := ctx.Value(application.WindowKey).(application.Window)
	if w == nil || a.win == nil || w.Name() == a.win.Name() {
		a.CloseWindow()
		return
	}
	if cw, ok := a.composeWindows.Load(w.Name()); ok {
		cw.(*composeWindow).closing.Store(true)
	}
	w.Close()
}

// remembered size of compose windows, shared by all of them.
const (
	settingComposeWindowWidth  = "compose_window_width"
	settingComposeWindowHeight = "compose_window_height"
)

const (
	defaultComposeWindowWidth  = 760
	defaultComposeWindowHeight = 680
	minComposeWindowWidth      = 520
	minComposeWindowHeight     = 420
)

// EventComposeCloseRequest asks a compose window to close its compose the way
// its own close button does, so unsaved text gets the save or discard prompt
// instead of vanishing with the window.
const EventComposeCloseRequest = "compose:close-request"

// composeWindow is the backend's record of an open compose window.
type composeWindow struct {
	// closing is set once the frontend has settled the draft and asked for
	// the window to go; until then the close button is turned into a request.
	closing atomic.Bool
}

// OpenComposeWindow moves a compose pane into a window of its own (#573).
// session is the pane's state as the frontend keeps it; the new window picks
// it up once with TakeComposeSession, so it never travels in a url.
func (a *App) OpenComposeWindow(session string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.wapp == nil {
		return nil
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := hex.EncodeToString(raw)
	a.composeHandoff.Store(token, session)

	name := "compose-" + token
	query := url.Values{}
	query.Set("window", "compose")
	query.Set("token", token)

	opts := application.WebviewWindowOptions{
		Name:             name,
		Title:            "Pelton",
		Width:            max(a.intSetting(settingComposeWindowWidth, defaultComposeWindowWidth), minComposeWindowWidth),
		Height:           max(a.intSetting(settingComposeWindowHeight, defaultComposeWindowHeight), minComposeWindowHeight),
		MinWidth:         minComposeWindowWidth,
		MinHeight:        minComposeWindowHeight,
		URL:              "/?" + query.Encode(),
		BackgroundColour: a.windowOptions.BackgroundColour,
		BackgroundType:   a.windowOptions.BackgroundType,
		Mac:              a.windowOptions.Mac,
		Windows:          a.windowOptions.Windows,
	}
	cw := &composeWindow{}
	a.composeWindows.Store(name, cw)
	w := a.wapp.Window.NewWithOptions(opts)
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		// a quit takes every window with it; the drafts autosave as they are
		// typed, so nothing is asked then.
		if !cw.closing.Load() && !a.quitRequested.Load() {
			e.Cancel()
			w.DispatchWailsEvent(&application.CustomEvent{Name: EventComposeCloseRequest})
			return
		}
		a.saveWindowSize(w, settingComposeWindowWidth, settingComposeWindowHeight)
		a.composeWindows.Delete(name)
		a.composeHandoff.Delete(token)
	})
	return nil
}

// TakeComposeSession hands a compose window the pane it was opened for, once.
func (a *App) TakeComposeSession(token string) string {
	session, ok := a.composeHandoff.LoadAndDelete(token)
	if !ok {
		return ""
	}
	return session.(string)
}
