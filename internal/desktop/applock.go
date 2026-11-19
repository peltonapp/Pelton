package desktop

import (
	"errors"
	"time"
	"unicode/utf8"

	"github.com/peltonapp/Pelton/internal/applock"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The app lock: Pelton locks after an idle time or on demand, and the user
// unlocks it with the system's own prompt (Touch ID or the account password on
// macOS) or, where there is none, a passphrase of Pelton's own. It is a screen
// lock for someone at an unlocked computer. It does not encrypt the mail
// database, and nothing about it touches the system keychain: the system
// prompt is a yes or no answer with no stored item behind it, and the
// passphrase is kept only as a salted scrypt hash in the settings table.
//
// While locked, the bound methods refuse (lockgate.go), the secondary windows
// are hidden, and notifications can be reduced to a line naming no sender or
// subject. Sync and the outbox keep running.

const (
	// settingAppLock turns the lock on. It is set and cleared only through
	// EnableAppLock and DisableAppLock, which authenticate first.
	settingAppLock = "app_lock"
	// settingAppLockIdle is the idle time in minutes after which the app
	// locks by itself; 0 locks only on demand and at launch.
	settingAppLockIdle = "app_lock_idle_minutes"
	// settingAppLockOnLaunch starts the app locked.
	settingAppLockOnLaunch = "app_lock_on_launch"
	// settingAppLockHideNotifications keeps senders and subjects out of
	// notifications while locked.
	settingAppLockHideNotifications = "app_lock_hide_notifications"
	// settingAppLockMethod is how the lock is opened: lockMethodSystem or
	// lockMethodPassphrase.
	settingAppLockMethod = "app_lock_method"
	// settingAppLockPassphrase is the salted scrypt hash of the passphrase,
	// never the passphrase itself.
	settingAppLockPassphrase = "app_lock_passphrase"
	// settingAppLockFailures counts wrong passphrases in a row, so quitting
	// does not skip the wait after too many.
	settingAppLockFailures = "app_lock_failures"
)

const (
	lockMethodSystem     = "system"
	lockMethodPassphrase = "passphrase"
)

// defaultAppLockIdle is the idle time, in minutes, a new lock starts with.
const defaultAppLockIdle = 5

// minLockPassphrase is the shortest passphrase accepted, in characters.
const minLockPassphrase = 6

// lockTick is how often the idle time is checked.
const lockTick = 15 * time.Second

// protectedLockSettings are the lock settings SetSetting refuses to write:
// turning the lock off or replacing the passphrase has to authenticate first.
var protectedLockSettings = map[string]bool{
	settingAppLock:           true,
	settingAppLockMethod:     true,
	settingAppLockPassphrase: true,
	settingAppLockFailures:   true,
}

// errLockSetting is returned when SetSetting is asked to write a protected
// lock setting.
var errLockSetting = errors.New("pelton: the app lock is changed in its own settings")

// EventAppLock is emitted when the app locks or unlocks.
const EventAppLock = "applock:changed"

// AppLockEvent is the payload of EventAppLock.
type AppLockEvent struct {
	Locked bool `json:"locked"`
}

// LockedError is what a bound call gets while the app is locked, from the gate
// in lockgate.go.
type LockedError struct{}

func (LockedError) Error() string { return "pelton is locked" }

// systemAuth is the operating system's own authentication prompt. On macOS it
// is LocalAuthentication; elsewhere there is none and the lock uses a
// passphrase. It is an interface so tests never raise a real prompt.
type systemAuth interface {
	// available reports whether the prompt can be shown on this machine.
	available() bool
	// authenticate shows the prompt with reason and reports whether the
	// user passed it. It blocks until the prompt is answered.
	authenticate(reason string) (bool, error)
}

// LockStateDTO is what the lock screen and the settings need to know.
type LockStateDTO struct {
	Enabled bool `json:"enabled"`
	Locked  bool `json:"locked"`
	// Method is "system" or "passphrase", or empty while the lock is off.
	Method string `json:"method"`
	// SystemAvailable reports whether the system prompt exists here, which
	// is what turning the lock on uses when no passphrase is given.
	SystemAvailable bool `json:"systemAvailable"`
	// RetryAfterMs is how long the next passphrase attempt has to wait.
	RetryAfterMs int64 `json:"retryAfterMs"`
	// the look of the lock screen, which cannot read the full preferences
	// while locked.
	Theme    string `json:"theme"`
	Accent   string `json:"accent"`
	Language string `json:"language"`
}

// LockAuthResultDTO is the outcome of an unlock or of a lock setting change
// that had to authenticate first.
type LockAuthResultDTO struct {
	OK bool `json:"ok"`
	// Reason says why it failed: "wrong" passphrase, "wait" for the backoff,
	// "cancelled" system prompt, "unavailable" method, "busy" with another
	// prompt, "short" passphrase, "off" when the lock is not on, or "on"
	// when it already is.
	Reason       string `json:"reason,omitempty"`
	RetryAfterMs int64  `json:"retryAfterMs,omitempty"`
}

// initLock configures the lock from the stored settings once the store is
// open, locks at once when it should start locked, and starts the idle check.
// It runs before any window exists, so nothing can read mail first.
func (a *App) initLock() {
	a.lock.RestoreFailures(a.intSetting(settingAppLockFailures, 0))
	a.applyLockSettings()
	if a.lock.Enabled() && a.boolSetting(settingAppLockOnLaunch, true) {
		a.lock.Lock()
	}
	goSafe("watching for an idle app lock", a.watchIdle)
	if a.wapp != nil {
		a.wapp.Event.OnApplicationEvent(events.Common.SystemWillSleep, func(*application.ApplicationEvent) {
			a.lock.Lock()
		})
	}
	a.watchScreenLock()
}

// applyLockSettings hands the stored lock settings to the locker.
func (a *App) applyLockSettings() {
	enabled := a.boolSetting(settingAppLock, false) && a.lockMethod() != ""
	idle := max(a.intSetting(settingAppLockIdle, defaultAppLockIdle), 0)
	a.lock.Configure(enabled, time.Duration(idle)*time.Minute)
}

// lockMethod returns the stored unlock method, or "" when none is usable.
func (a *App) lockMethod() string {
	switch a.stringSetting(settingAppLockMethod, "") {
	case lockMethodSystem:
		return lockMethodSystem
	case lockMethodPassphrase:
		if a.stringSetting(settingAppLockPassphrase, "") != "" {
			return lockMethodPassphrase
		}
	}
	return ""
}

// watchIdle locks the app once it has gone unused for the idle time.
func (a *App) watchIdle() {
	ticker := time.NewTicker(lockTick)
	defer ticker.Stop()
	for range ticker.C {
		a.lock.Tick()
	}
}

// onLockChanged tells every window and hides the secondary ones while locked,
// bringing back the ones it hid once unlocked.
func (a *App) onLockChanged(locked bool) {
	a.emit(EventAppLock, AppLockEvent{Locked: locked})
	if locked {
		a.hideWindowsForLock()
	} else {
		a.restoreWindowsAfterLock()
	}
}

// hideWindowsForLock hides every visible window but the main one and
// remembers which, so unlocking brings them back.
func (a *App) hideWindowsForLock() {
	if a.wapp == nil {
		return
	}
	var hidden []string
	for _, w := range a.wapp.Window.GetAll() {
		if a.win != nil && w.Name() == a.win.Name() {
			continue
		}
		if w.IsVisible() {
			w.Hide()
			hidden = append(hidden, w.Name())
		}
	}
	a.lockWinMu.Lock()
	a.lockHidden = append(a.lockHidden, hidden...)
	a.lockWinMu.Unlock()
}

// restoreWindowsAfterLock shows the windows the lock hid that are still open.
func (a *App) restoreWindowsAfterLock() {
	a.lockWinMu.Lock()
	names := a.lockHidden
	a.lockHidden = nil
	a.lockWinMu.Unlock()
	if a.wapp == nil {
		return
	}
	for _, name := range names {
		if w, ok := a.wapp.Window.GetByName(name); ok {
			w.Show()
		}
	}
}

// maskForLock replaces a notification with one naming nothing while the app
// is locked and the setting asks for it. A click still opens the app.
func (a *App) maskForLock(n notification) notification {
	if !a.locked() || !a.boolSetting(settingAppLockHideNotifications, true) {
		return n
	}
	s := notifyStringsFor(a.stringSetting(settingLanguage, "en"))
	return notification{title: "Pelton", body: s.locked}
}

// lockSystemAuth returns the system prompt, or nil where there is none.
func (a *App) lockSystemAuth() systemAuth {
	if a.sysAuth == nil || !a.sysAuth.available() {
		return nil
	}
	return a.sysAuth
}

// GetLockState reports whether the lock is on and the app locked, how it is
// opened, and the look the lock screen takes. It is allowed while locked.
func (a *App) GetLockState() LockStateDTO {
	state := LockStateDTO{
		Enabled:         a.lock.Enabled(),
		Locked:          a.lock.Locked(),
		SystemAvailable: a.lockSystemAuth() != nil,
		RetryAfterMs:    a.lock.Wait().Milliseconds(),
		Theme:           a.stringSetting(storage.SettingTheme, defaultTheme),
		Accent:          a.stringSetting(settingAccent, defaultAccent),
		Language:        a.stringSetting(settingLanguage, "en"),
	}
	if state.Enabled {
		state.Method = a.lockMethod()
	}
	return state
}

// LockNow locks the app, when the lock is on.
func (a *App) LockNow() {
	a.lock.Lock()
}

// NoteActivity tells the idle check the app is being used. Every window calls
// it, throttled, on input.
func (a *App) NoteActivity() {
	a.lock.Touch()
}

// UnlockWithSystem shows the system prompt and unlocks when it passes. reason
// is the line the prompt shows under its title.
func (a *App) UnlockWithSystem(reason string) LockAuthResultDTO {
	if !a.lock.Locked() {
		return LockAuthResultDTO{OK: true}
	}
	if a.lockMethod() != lockMethodSystem {
		return LockAuthResultDTO{Reason: "unavailable"}
	}
	res := a.authenticateSystem(reason)
	if res.OK {
		a.lock.Unlock()
	}
	return res
}

// UnlockWithPassphrase unlocks when passphrase matches. Wrong attempts past
// the first few wait longer each time, also across restarts.
func (a *App) UnlockWithPassphrase(passphrase string) LockAuthResultDTO {
	if !a.lock.Locked() {
		return LockAuthResultDTO{OK: true}
	}
	res := a.checkPassphrase(passphrase)
	if res.OK {
		a.lock.Unlock()
		a.saveLockFailures()
	}
	return res
}

// EnableAppLock turns the lock on. With a passphrase it uses that; without
// one it uses the system prompt, which has to pass once first so the user
// knows it works. The app is not locked by turning it on.
func (a *App) EnableAppLock(passphrase, reason string) (LockAuthResultDTO, error) {
	if err := a.ready(); err != nil {
		return LockAuthResultDTO{}, err
	}
	if a.lock.Enabled() {
		return LockAuthResultDTO{Reason: "on"}, nil
	}
	method := lockMethodPassphrase
	hash := ""
	if passphrase == "" {
		if a.lockSystemAuth() == nil {
			return LockAuthResultDTO{Reason: "short"}, nil
		}
		if res := a.authenticateSystem(reason); !res.OK {
			return res, nil
		}
		method = lockMethodSystem
	} else {
		if utf8.RuneCountInString(passphrase) < minLockPassphrase {
			return LockAuthResultDTO{Reason: "short"}, nil
		}
		h, err := applock.HashPassphrase(passphrase)
		if err != nil {
			return LockAuthResultDTO{}, err
		}
		hash = h
	}
	// the switch itself goes last, so a failed write never leaves the lock on
	// without a way to open it.
	for _, kv := range [][2]string{
		{settingAppLockMethod, method},
		{settingAppLockPassphrase, hash},
		{settingAppLockFailures, "0"},
		{settingAppLock, "true"},
	} {
		if err := a.store.Set(a.ctx, kv[0], kv[1]); err != nil {
			return LockAuthResultDTO{}, err
		}
	}
	a.lock.Succeed()
	a.applyLockSettings()
	return LockAuthResultDTO{OK: true}, nil
}

// DisableAppLock turns the lock off once the user has authenticated: with the
// system prompt, or with passphrase when the lock uses one.
func (a *App) DisableAppLock(passphrase, reason string) (LockAuthResultDTO, error) {
	if err := a.ready(); err != nil {
		return LockAuthResultDTO{}, err
	}
	if !a.lock.Enabled() {
		return LockAuthResultDTO{Reason: "off"}, nil
	}
	if res := a.authenticateForChange(passphrase, reason); !res.OK {
		return res, nil
	}
	for _, key := range []string{settingAppLock, settingAppLockMethod, settingAppLockPassphrase, settingAppLockFailures} {
		if err := a.store.Set(a.ctx, key, ""); err != nil {
			return LockAuthResultDTO{}, err
		}
	}
	a.applyLockSettings()
	return LockAuthResultDTO{OK: true}, nil
}

// ChangeLockPassphrase replaces the passphrase once current has been checked.
func (a *App) ChangeLockPassphrase(current, next string) (LockAuthResultDTO, error) {
	if err := a.ready(); err != nil {
		return LockAuthResultDTO{}, err
	}
	if a.lockMethod() != lockMethodPassphrase {
		return LockAuthResultDTO{Reason: "unavailable"}, nil
	}
	if utf8.RuneCountInString(next) < minLockPassphrase {
		return LockAuthResultDTO{Reason: "short"}, nil
	}
	if res := a.checkPassphrase(current); !res.OK {
		return res, nil
	}
	hash, err := applock.HashPassphrase(next)
	if err != nil {
		return LockAuthResultDTO{}, err
	}
	if err := a.store.Set(a.ctx, settingAppLockPassphrase, hash); err != nil {
		return LockAuthResultDTO{}, err
	}
	a.saveLockFailures()
	return LockAuthResultDTO{OK: true}, nil
}

// authenticateForChange proves the user before a lock setting changes, the
// same way unlocking does.
func (a *App) authenticateForChange(passphrase, reason string) LockAuthResultDTO {
	switch a.lockMethod() {
	case lockMethodSystem:
		return a.authenticateSystem(reason)
	case lockMethodPassphrase:
		res := a.checkPassphrase(passphrase)
		if res.OK {
			a.saveLockFailures()
		}
		return res
	}
	return LockAuthResultDTO{Reason: "unavailable"}
}

// authenticateSystem shows the system prompt, one at a time.
func (a *App) authenticateSystem(reason string) LockAuthResultDTO {
	auth := a.lockSystemAuth()
	if auth == nil {
		return LockAuthResultDTO{Reason: "unavailable"}
	}
	if !a.authMu.TryLock() {
		return LockAuthResultDTO{Reason: "busy"}
	}
	defer a.authMu.Unlock()
	if reason == "" {
		reason = "unlock Pelton"
	}
	ok, err := auth.authenticate(reason)
	if err != nil {
		a.log.Info("app lock: system prompt", "err", err)
	}
	if !ok {
		return LockAuthResultDTO{Reason: "cancelled"}
	}
	return LockAuthResultDTO{OK: true}
}

// checkPassphrase checks passphrase against the stored hash, honouring and
// extending the wait after wrong attempts.
func (a *App) checkPassphrase(passphrase string) LockAuthResultDTO {
	if wait := a.lock.Wait(); wait > 0 {
		return LockAuthResultDTO{Reason: "wait", RetryAfterMs: wait.Milliseconds()}
	}
	hash := a.stringSetting(settingAppLockPassphrase, "")
	if hash == "" {
		return LockAuthResultDTO{Reason: "unavailable"}
	}
	ok, err := applock.VerifyPassphrase(passphrase, hash)
	if err != nil {
		a.log.Error("app lock: stored passphrase", "err", err)
		return LockAuthResultDTO{Reason: "unavailable"}
	}
	if !ok {
		wait := a.lock.Fail()
		a.saveLockFailures()
		return LockAuthResultDTO{Reason: "wrong", RetryAfterMs: wait.Milliseconds()}
	}
	a.lock.Succeed()
	return LockAuthResultDTO{OK: true}
}

// saveLockFailures stores the wrong passphrase count.
func (a *App) saveLockFailures() {
	if a.store == nil {
		return
	}
	if err := a.store.SetInt(a.ctx, settingAppLockFailures, a.lock.Failures()); err != nil {
		a.log.Error("app lock: save failed attempts", "err", err)
	}
}
