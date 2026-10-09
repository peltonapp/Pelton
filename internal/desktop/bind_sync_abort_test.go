package desktop

import (
	"sync"
	"testing"
	"time"
)

func TestAbortAccountSyncReleasesAccountLock(t *testing.T) {
	a := &App{}
	const accountID = 7
	mu := a.accountLock(accountID)
	holding := make(chan struct{})
	released := make(chan struct{})

	go func() {
		mu.Lock()
		defer mu.Unlock()
		unblock := make(chan struct{})
		as := &activeSync{
			cancel: func() {},
			close:  func() { close(unblock) },
		}
		a.setActiveSync(accountID, as)
		close(holding)
		<-unblock
		a.clearActiveSync(accountID, as)
		close(released)
	}()

	select {
	case <-holding:
	case <-time.After(2 * time.Second):
		t.Fatal("sync never took the lock")
	}

	a.abortAccountSync(accountID)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if mu.TryLock() {
			mu.Unlock()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("account lock still held after abort")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("holder never exited")
	}
}

func TestLockAccountAfterAbort(t *testing.T) {
	a := &App{}
	const accountID = 7
	var once sync.Once
	go func() {
		mu := a.accountLock(accountID)
		mu.Lock()
		defer mu.Unlock()
		done := make(chan struct{})
		a.setActiveSync(accountID, &activeSync{
			cancel: func() {},
			close:  func() { once.Do(func() { close(done) }) },
		})
		<-done
	}()

	// Give the holder time to acquire.
	time.Sleep(50 * time.Millisecond)
	a.abortAccountSync(accountID)
	if err := a.lockAccount(accountID, 2*time.Second); err != nil {
		t.Fatalf("lockAccount: %v", err)
	}
	a.accountLock(accountID).Unlock()
}
