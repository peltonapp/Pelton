package desktop

import (
	"errors"
	"net/url"
	"os"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Quick compose: a system-wide shortcut that opens a small compose window from
// anywhere, without the main window. Off by default. Nothing is registered with
// the operating system while it is off; Wails releases the shortcut on quit.
// The --quick-compose flag opens the same window from the command line, which
// is the way in on desktops that offer no global shortcuts to apps: bind
// "pelton --quick-compose" in the desktop's own keyboard settings, and the
// single instance handler hands it to the running app.

const (
	// settingQuickCompose registers the shortcut. Written only through
	// SetQuickCompose, which registers before it stores.
	settingQuickCompose = "quick_compose"
	// settingQuickComposeShortcut is the shortcut as a Wails accelerator,
	// such as "Ctrl+Alt+M".
	settingQuickComposeShortcut = "quick_compose_shortcut"
)

// quickComposeFlag opens the quick compose window, at launch or in the
// running app.
const quickComposeFlag = "--quick-compose"

// errQuickComposeSetting is returned when SetSetting is asked to write a quick
// compose setting, which has to go through SetQuickCompose.
var errQuickComposeSetting = errors.New("pelton: quick compose is changed in its own setting")

// the quick compose window. Its name puts it among the compose windows, so
// TrafficLights finds it by quickWindowToken.
const (
	quickWindowName  = "compose-quick"
	quickWindowToken = "quick"

	quickWindowWidth     = 560
	quickWindowHeight    = 440
	minQuickWindowWidth  = 420
	minQuickWindowHeight = 300
)

// defaultQuickComposeShortcut is Control and Option with M on macOS and
// Control and Alt with M elsewhere: free in the common desktops, and away from
// the Command shortcuts every Mac app uses.
func defaultQuickComposeShortcut() string {
	if goruntime.GOOS == "darwin" {
		return "Ctrl+Option+M"
	}
	return "Ctrl+Alt+M"
}

// QuickComposeStatusDTO is the state of the quick compose shortcut.
type QuickComposeStatusDTO struct {
	Enabled  bool   `json:"enabled"`
	Shortcut string `json:"shortcut"`
	// Registered is set while the operating system holds the shortcut.
	Registered bool `json:"registered"`
	// Wayland is set on a Wayland session, where the desktop decides through
	// its shortcuts portal whether and how the shortcut is bound.
	Wayland bool `json:"wayland"`
	// Error says why the shortcut could not be registered: "invalid" for a
	// shortcut that is not one, "taken" when the system refused it.
	Error string `json:"error,omitempty"`
}

// quickModifiers are the modifier names a shortcut may use, in the order they
// are written.
var quickModifiers = []string{"Ctrl", "Option", "Alt", "Shift", "Cmd", "Super"}

// normalizeQuickShortcut checks a shortcut and writes it in a canonical form:
// modifiers in a fixed order, then one key. It needs at least one modifier
// other than Shift, so the shortcut never swallows plain typing anywhere.
func normalizeQuickShortcut(s string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(s), "+")
	if len(parts) < 2 {
		return "", false
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	switch {
	case len(key) == 1 && key[0] > ' ' && key[0] < 0x7f:
		key = strings.ToUpper(key)
	case quickNamedKeys[strings.ToLower(key)]:
		key = strings.ToLower(key)
	default:
		return "", false
	}
	held := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		i := slices.IndexFunc(quickModifiers, func(m string) bool { return strings.EqualFold(m, strings.TrimSpace(p)) })
		if i < 0 {
			return "", false
		}
		held[quickModifiers[i]] = true
	}
	strong := held["Ctrl"] || held["Option"] || held["Alt"] || held["Cmd"] || held["Super"]
	if !strong {
		return "", false
	}
	var out []string
	for _, m := range quickModifiers {
		if held[m] {
			out = append(out, m)
		}
	}
	return strings.Join(append(out, key), "+"), true
}

// quickNamedKeys are the named keys a shortcut may end in, as Wails spells
// them.
var quickNamedKeys = func() map[string]bool {
	keys := map[string]bool{}
	for _, k := range []string{"space", "tab", "return", "escape", "backspace", "delete", "home", "end", "page up", "page down", "left", "right", "up", "down"} {
		keys[k] = true
	}
	for i := 1; i <= 12; i++ {
		keys["f"+strconv.Itoa(i)] = true
	}
	return keys
}()

// waylandSession reports whether this is a Wayland session.
func waylandSession() bool {
	if goruntime.GOOS != "linux" {
		return false
	}
	switch strings.ToLower(os.Getenv("XDG_SESSION_TYPE")) {
	case "wayland":
		return true
	case "x11":
		return false
	}
	return os.Getenv("WAYLAND_DISPLAY") != ""
}

// GetQuickComposeStatus reports the quick compose setting and whether its
// shortcut is registered.
func (a *App) GetQuickComposeStatus() QuickComposeStatusDTO {
	a.quickMu.Lock()
	registered := a.quickAccel != ""
	a.quickMu.Unlock()
	return QuickComposeStatusDTO{
		Enabled:    a.boolSetting(settingQuickCompose, false),
		Shortcut:   a.stringSetting(settingQuickComposeShortcut, defaultQuickComposeShortcut()),
		Registered: registered,
		Wayland:    waylandSession(),
	}
}

// SetQuickCompose turns quick compose on or off with the given shortcut. The
// shortcut is registered first and the setting stored only once that worked;
// when the system refuses it, the previous shortcut stays as it was and the
// status says why.
func (a *App) SetQuickCompose(enabled bool, shortcut string) (QuickComposeStatusDTO, error) {
	if err := a.ready(); err != nil {
		return QuickComposeStatusDTO{}, err
	}
	accel, ok := normalizeQuickShortcut(shortcut)
	if !ok {
		status := a.GetQuickComposeStatus()
		status.Error = "invalid"
		return status, nil
	}
	if enabled {
		if err := a.registerQuickCompose(accel); err != nil {
			a.log.Info("quick compose: register shortcut", "shortcut", accel, "err", err)
			status := a.GetQuickComposeStatus()
			status.Error = "taken"
			return status, nil
		}
	} else {
		a.unregisterQuickCompose()
	}
	if err := a.store.Set(a.ctx, settingQuickComposeShortcut, accel); err != nil {
		return QuickComposeStatusDTO{}, err
	}
	if err := a.store.SetBool(a.ctx, settingQuickCompose, enabled); err != nil {
		return QuickComposeStatusDTO{}, err
	}
	return a.GetQuickComposeStatus(), nil
}

// registerQuickCompose makes accel the quick compose shortcut, releasing the
// one held before. When accel is refused, the previous one is taken back.
func (a *App) registerQuickCompose(accel string) error {
	if a.wapp == nil || a.wapp.GlobalShortcut == nil {
		return errors.New("pelton: no app to register the shortcut with")
	}
	a.quickMu.Lock()
	defer a.quickMu.Unlock()
	previous := a.quickAccel
	if previous == accel {
		return nil
	}
	if previous != "" {
		if err := a.wapp.GlobalShortcut.Unregister(previous); err != nil {
			a.log.Info("quick compose: release shortcut", "shortcut", previous, "err", err)
		}
		a.quickAccel = ""
	}
	if err := a.wapp.GlobalShortcut.Register(accel, a.openQuickCompose); err != nil {
		if previous != "" && a.wapp.GlobalShortcut.Register(previous, a.openQuickCompose) == nil {
			a.quickAccel = previous
		}
		return err
	}
	a.quickAccel = accel
	return nil
}

// unregisterQuickCompose releases the quick compose shortcut, if one is held.
func (a *App) unregisterQuickCompose() {
	a.quickMu.Lock()
	defer a.quickMu.Unlock()
	if a.quickAccel == "" || a.wapp == nil || a.wapp.GlobalShortcut == nil {
		a.quickAccel = ""
		return
	}
	if err := a.wapp.GlobalShortcut.Unregister(a.quickAccel); err != nil {
		a.log.Info("quick compose: release shortcut", "shortcut", a.quickAccel, "err", err)
	}
	a.quickAccel = ""
}

// startQuickCompose registers the stored shortcut once the app is running,
// when quick compose is on, and opens the window when the app was launched to
// do so.
func (a *App) startQuickCompose(openNow bool) {
	if a.wapp == nil {
		return
	}
	a.wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		<-a.storeReady
		if a.boolSetting(settingQuickCompose, false) {
			accel, ok := normalizeQuickShortcut(a.stringSetting(settingQuickComposeShortcut, defaultQuickComposeShortcut()))
			if ok {
				if err := a.registerQuickCompose(accel); err != nil {
					a.log.Warn("quick compose: register shortcut", "shortcut", accel, "err", err)
				}
			}
		}
		if openNow {
			a.openQuickCompose()
		}
	})
}

// openQuickCompose shows the quick compose window, creating it the first
// time. It never shows the main window. It runs on the shortcut's own
// goroutine; every window call below goes through the main thread.
func (a *App) openQuickCompose() {
	if a.wapp == nil || a.store == nil {
		return
	}
	a.unhideWithoutMainWindow()
	if w, ok := a.wapp.Window.GetByName(quickWindowName); ok {
		w.Show()
		w.Focus()
		return
	}
	query := url.Values{}
	query.Set("window", quickWindowToken)
	opts := application.WebviewWindowOptions{
		Name:             quickWindowName,
		Title:            "Pelton",
		Width:            quickWindowWidth,
		Height:           quickWindowHeight,
		MinWidth:         minQuickWindowWidth,
		MinHeight:        minQuickWindowHeight,
		AlwaysOnTop:      true,
		InitialPosition:  application.WindowCentered,
		URL:              "/?" + query.Encode(),
		BackgroundColour: a.windowOptions.BackgroundColour,
		BackgroundType:   a.windowOptions.BackgroundType,
		Mac:              a.windowOptions.Mac,
		Windows:          a.windowOptions.Windows,
	}
	insetComposeTitleBar(&opts)
	cw := &composeWindow{}
	a.composeWindows.Store(quickWindowName, cw)
	w := a.wapp.Window.NewWithOptions(opts)
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		// while locked the window shows only the lock screen, with no
		// compose behind it to ask, so it simply closes.
		if !cw.closing.Load() && !a.quitRequested.Load() && !a.locked() {
			e.Cancel()
			w.DispatchWailsEvent(&application.CustomEvent{Name: EventComposeCloseRequest})
			return
		}
		a.composeWindows.Delete(quickWindowName)
		go a.afterQuickCompose()
	})
	w.Focus()
}

// unhideWithoutMainWindow makes the app visible for the quick compose window
// without bringing the main window along. On macOS a hidden app shows every
// window it had when unhidden, so a main window that is not on screen is
// ordered out first. Elsewhere windows are shown one by one and nothing needs
// doing.
func (a *App) unhideWithoutMainWindow() {
	if goruntime.GOOS != "darwin" {
		return
	}
	if a.win != nil && !a.win.IsVisible() {
		a.win.Hide()
	}
	application.InvokeSync(a.wapp.Show)
}

// afterQuickCompose hands the focus back to the app that had it when the
// quick compose window was the only one Pelton showed.
func (a *App) afterQuickCompose() {
	if goruntime.GOOS != "darwin" || a.win == nil || a.win.IsVisible() {
		return
	}
	// any other window, including a full composer the draft was just handed
	// to and which may not be on screen yet, keeps the app up.
	for _, w := range a.wapp.Window.GetAll() {
		if name := w.Name(); name != quickWindowName && name != a.win.Name() {
			return
		}
	}
	a.hideApp()
}
