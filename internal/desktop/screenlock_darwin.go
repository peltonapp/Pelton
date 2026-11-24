//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework Foundation
void peltonWatchScreenLock(void);
*/
import "C"

import "sync/atomic"

// screenLockTarget is the app the screen lock notification locks.
var screenLockTarget atomic.Pointer[App]

// watchScreenLock locks the app when the screen locks.
func (a *App) watchScreenLock() {
	if a.wapp == nil {
		return
	}
	screenLockTarget.Store(a)
	C.peltonWatchScreenLock()
}

//export peltonScreenLocked
func peltonScreenLocked() {
	a := screenLockTarget.Load()
	if a == nil {
		return
	}
	// off the main thread: locking hides windows, which waits on it.
	go a.lock.Lock()
	go a.lockMailVaults()
}
