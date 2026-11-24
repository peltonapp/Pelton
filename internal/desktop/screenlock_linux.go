//go:build linux

package desktop

import (
	"os"

	"github.com/godbus/dbus/v5"
)

// watchScreenLock locks the app when the session locks. logind sends Lock on
// the system bus when it is asked to lock the session, and the desktop's
// screensaver announces ActiveChanged(true) on the session bus when the
// screen locks or blanks. Either is enough; a desktop offering neither leaves
// the idle time and system sleep to lock the app.
func (a *App) watchScreenLock() {
	if a.wapp == nil {
		return
	}
	goSafe("watching for the session lock", a.watchLogindLock)
	goSafe("watching for the screensaver", a.watchScreensaver)
}

// watchLogindLock waits for logind's Lock signal on this process's session.
func (a *App) watchLogindLock() {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		a.log.Info("app lock: no system bus", "err", err)
		return
	}
	defer conn.Close()
	var session dbus.ObjectPath
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		Call("org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).
		Store(&session)
	if err != nil {
		a.log.Info("app lock: no logind session", "err", err)
		return
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(session),
		dbus.WithMatchInterface("org.freedesktop.login1.Session"),
		dbus.WithMatchMember("Lock"),
	); err != nil {
		a.log.Info("app lock: watch logind", "err", err)
		return
	}
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	for sig := range signals {
		if sig.Path == session && sig.Name == "org.freedesktop.login1.Session.Lock" {
			a.lock.Lock()
		}
	}
}

// screensaverInterfaces are the screensaver services desktops announce on.
var screensaverInterfaces = []string{"org.freedesktop.ScreenSaver", "org.gnome.ScreenSaver"}

// watchScreensaver waits for a screensaver to become active. The session bus
// connection is shared and stays open.
func (a *App) watchScreensaver() {
	conn, err := dbus.SessionBus()
	if err != nil {
		a.log.Info("app lock: no session bus", "err", err)
		return
	}
	names := map[string]bool{}
	for _, iface := range screensaverInterfaces {
		if err := conn.AddMatchSignal(dbus.WithMatchInterface(iface), dbus.WithMatchMember("ActiveChanged")); err != nil {
			a.log.Info("app lock: watch screensaver", "iface", iface, "err", err)
			continue
		}
		names[iface+".ActiveChanged"] = true
	}
	if len(names) == 0 {
		return
	}
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	for sig := range signals {
		if !names[sig.Name] || len(sig.Body) == 0 {
			continue
		}
		if active, ok := sig.Body[0].(bool); ok && active {
			a.lock.Lock()
		}
	}
}
