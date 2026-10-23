package desktop

import (
	"context"
	"sync"
	"time"
)

// syncProgressHeartbeat is how often a running account's latest progress
// event is sent again while its counted run is open, and the latest event of
// its folder check line while checks run. It must stay well under the time
// the ui keeps a silent progress line (2 min), or the ui drops the line
// during a long step that reports nothing (one big list, a slow body batch,
// the full check of a big folder).
const syncProgressHeartbeat = 45 * time.Second

func (a *App) progressHeartbeatInterval() time.Duration {
	if a.progressHeartbeatEvery > 0 {
		return a.progressHeartbeatEvery
	}
	return syncProgressHeartbeat
}

// startProgressHeartbeat re-sends the account's latest running progress event
// on a timer until stop is called or ctx ends. stop waits for the goroutine to
// exit, so calling it before the closing event means nothing running can
// follow the close. stop is safe to call more than once.
func (a *App) startProgressHeartbeat(ctx context.Context, accountID int64) (stop func()) {
	st := a.accountStateFor(accountID)
	quit := make(chan struct{})
	exited := make(chan struct{})
	every := a.progressHeartbeatInterval()
	go func() {
		defer close(exited)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ctx.Done():
				return
			case <-t.C:
			}
			st.progressMu.Lock()
			if ev := st.lastProgress; ev != nil {
				a.emit(EventSyncProgress, *ev)
			}
			st.progressMu.Unlock()
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(quit)
			<-exited
		})
	}
}

// startVerifyHeartbeat re-sends the account's latest "checking folders"
// event every heartbeat until a closing verify event clears it or the app
// quits. The caller holds st.progressMu and has set st.verifyBeating, so
// one heartbeat runs per account.
func (a *App) startVerifyHeartbeat(st *accountState) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	every := a.progressHeartbeatInterval()
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				st.progressMu.Lock()
				st.verifyBeating = false
				st.progressMu.Unlock()
				return
			case <-t.C:
			}
			st.progressMu.Lock()
			ev := st.lastVerify
			if ev == nil {
				st.verifyBeating = false
				st.progressMu.Unlock()
				return
			}
			a.emit(EventSyncProgress, *ev)
			st.progressMu.Unlock()
		}
	}()
}
