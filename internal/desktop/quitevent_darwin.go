//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework Cocoa
void peltonWatchQuitEvent(void);
*/
import "C"

import "sync/atomic"

// quitEventTarget is the app the quit Apple event is delivered to.
var quitEventTarget atomic.Pointer[App]

// watchQuitEvent makes the Dock's Quit, logout and shutdown quit the app. Wails
// sends them through the same OnBeforeClose as the window's close button, so
// with the close action on "keep running" they only hid the window (#501).
func (a *App) watchQuitEvent() {
	quitEventTarget.Store(a)
	C.peltonWatchQuitEvent()
}

//export peltonQuitEvent
func peltonQuitEvent() {
	a := quitEventTarget.Load()
	if a == nil {
		return
	}
	// off the main thread: the event arrives on it, and quitting waits on it.
	go a.quitApp()
}

//export peltonReopenEvent
func peltonReopenEvent() {
	a := quitEventTarget.Load()
	if a == nil {
		return
	}
	go a.showWindow()
}
