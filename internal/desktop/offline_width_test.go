package desktop

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
)

// offlineInbox is wideInbox whose server also answers the offline
// download's SEARCH SINCE with every uid, and the download's plan.
func offlineInbox(t *testing.T, client *wideIMAP, n, par int) (*App, storage.Account, storage.Folder, *accountSync, []dlTask, []int64) {
	t.Helper()
	for uid := 1; uid <= n; uid++ {
		client.since = append(client.since, imap.UID(uid))
	}
	a, account, inbox, rt := wideInbox(t, client, n, par)
	tasks, pin, err := a.planDownload(a.ctx, time.Now().AddDate(-1, 0, 0))
	if err != nil {
		t.Fatalf("planDownload: %v", err)
	}
	if len(tasks) != n {
		t.Fatalf("download planned %d messages, want %d", len(tasks), n)
	}
	return a, account, inbox, rt, tasks, pin
}

// A sync plan's body copies and an offline download of the same folder
// fetch disjoint ids: no uid is fetched twice, and every message ends filled
// and pinned, the ones the plan filled included.
func TestPlanAndOfflineDownloadNeverFetchTwice(t *testing.T) {
	client := newWideIMAP(20 * time.Millisecond)
	a, account, inbox, rt, tasks, pin := offlineInbox(t, client, 40, 4)
	planDone := make(chan error, 1)
	enqueueSyncPlan(a.ctx, rt.sched, []storage.Folder{inbox}, syncPlan{
		step: bodiesOnly(a, account), bodyWidth: rt.bodyWidth, done: planDone,
	})
	dlDone := make(chan error, 1)
	go func() { dlDone <- a.runDownload(a.ctx, tasks, pin, true, len(tasks)) }()
	if err := recv(t, planDone, 20*time.Second, "the plan never ended"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if err := recv(t, dlDone, 20*time.Second, "the download never ended"); err != nil {
		t.Fatalf("download: %v", err)
	}
	_, _, served := client.stats()
	for uid := imap.UID(1); uid <= 40; uid++ {
		if served[uid] != 1 {
			t.Fatalf("uid %d fetched %d times, want once (served %v)", uid, served[uid], served)
		}
	}
	for uid := uint32(1); uid <= 40; uid++ {
		if m := mustGetMessage(t, a.store, stateIDByUID(t, a.store, inbox.ID, uid)); !m.BodyComplete || !m.Offline {
			t.Fatalf("uid %d complete %v offline %v", uid, m.BodyComplete, m.Offline)
		}
	}
	if h := rt.bodyClaims().Held(inbox.ID); h != 0 {
		t.Fatalf("%d ids still claimed", h)
	}
}

// A soft stop and an account removal's hard stop leave no claim behind
// once the body and offline units that held them returned.
func TestStopAndAbortFreeTheClaims(t *testing.T) {
	for _, hard := range []bool{false, true} {
		t.Run(fmt.Sprintf("hard=%v", hard), func(t *testing.T) {
			client := newWideIMAP(0)
			gate := make(chan struct{})
			var blocked atomic.Int32
			client.hook = func(int, []imap.UID) error {
				blocked.Add(1)
				<-gate
				return nil
			}
			a, account, inbox, rt, tasks, pin := offlineInbox(t, client, 40, 4)
			claims := rt.bodyClaims()
			enqueueSyncPlan(a.ctx, rt.sched, []storage.Folder{inbox}, syncPlan{
				step: bodiesOnly(a, account), bodyWidth: rt.bodyWidth,
			})
			go func() { _ = a.runDownload(a.ctx, tasks, pin, true, len(tasks)) }()
			waitUntil(t, 10*time.Second, "the body and offline units never fetched at once", func() bool { return blocked.Load() >= 2 })
			if claims.Held(inbox.ID) == 0 {
				t.Fatal("no claim held while units fetch")
			}
			stopped := make(chan struct{})
			go func() {
				a.stopAccountScheduler(account.ID, hard)
				close(stopped)
			}()
			waitUntil(t, 5*time.Second, "the stop never began", rt.sched.Stopped)
			close(gate)
			recv(t, stopped, 10*time.Second, "the scheduler did not stop")
			waitUntil(t, 5*time.Second, "claims left after the units returned", func() bool { return claims.Held(inbox.ID) == 0 })
		})
	}
}

// An offline unit whose stubs are all claimed by running body units
// fetches nothing and returns ErrSaturated, keeping them planned; once the
// claims are gone it fetches and pins them.
func TestOfflineUnitWaitsForClaimedStubs(t *testing.T) {
	client := newWideIMAP(0)
	a, account, inbox, rt := wideInbox(t, client, 3, 2)
	claims := rt.bodyClaims()
	tok, _ := claims.Take(inbox.ID, []string{"3", "2", "1"}, 3)
	ft := &folderTasks{folder: inbox, stubs: []string{"3", "2", "1"}}
	prog := &dlProgress{total: 3, start: time.Now()}
	if err := a.offlineUnit(a.ctx, client, account, ft, true, prog); !errors.Is(err, syncsched.ErrSaturated) {
		t.Fatalf("unit over claimed stubs: %v, want ErrSaturated", err)
	}
	if calls, _, _ := client.stats(); calls != 0 || len(ft.stubs) != 3 {
		t.Fatalf("%d FETCH, %d stubs planned; want none fetched and all kept", calls, len(ft.stubs))
	}
	claims.Release(inbox.ID, tok)
	if err := a.offlineUnit(a.ctx, client, account, ft, true, prog); err != nil {
		t.Fatalf("unit after the release: %v", err)
	}
	for uid := uint32(1); uid <= 3; uid++ {
		if m := mustGetMessage(t, a.store, stateIDByUID(t, a.store, inbox.ID, uid)); !m.BodyComplete || !m.Offline {
			t.Fatalf("uid %d complete %v offline %v", uid, m.BodyComplete, m.Offline)
		}
	}
}

// Messages the download planned with no row that a sync then listed and
// its body generator filled (while they were claimed, the offline unit
// waited) are only pinned, not fetched a second time.
func TestOfflineFreshFilledMeanwhileIsNotFetchedAgain(t *testing.T) {
	client := newWideIMAP(0)
	a, account, inbox, rt := wideInbox(t, client, 3, 2)
	claims := rt.bodyClaims()
	// Planned before the sync listed them: no row then.
	ft := &folderTasks{folder: inbox, fresh: []string{"3", "2", "1"}}
	prog := &dlProgress{total: 3, start: time.Now()}
	tok, _ := claims.Take(inbox.ID, []string{"3", "2", "1"}, 3)
	if err := a.offlineUnit(a.ctx, client, account, ft, true, prog); !errors.Is(err, syncsched.ErrSaturated) {
		t.Fatalf("unit over claimed messages: %v, want ErrSaturated", err)
	}
	claims.Release(inbox.ID, tok)
	// The body generator fills them.
	if err := a.imapBodyBatch(a.ctx, account, inbox); !errors.Is(err, syncsched.ErrAgain) {
		t.Fatalf("body unit: %v", err)
	}
	if n := missingBodies(t, a, inbox.ID); n != 0 {
		t.Fatalf("%d bodies missing after the body unit", n)
	}
	if err := a.offlineUnit(a.ctx, client, account, ft, true, prog); err != nil {
		t.Fatalf("unit after the fill: %v", err)
	}
	_, _, served := client.stats()
	for uid := imap.UID(1); uid <= 3; uid++ {
		if served[uid] != 1 {
			t.Fatalf("uid %d fetched %d times, want once (served %v)", uid, served[uid], served)
		}
	}
	for uid := uint32(1); uid <= 3; uid++ {
		if m := mustGetMessage(t, a.store, stateIDByUID(t, a.store, inbox.ID, uid)); !m.BodyComplete || !m.Offline {
			t.Fatalf("uid %d complete %v offline %v", uid, m.BodyComplete, m.Offline)
		}
	}
	if len(ft.fresh) != 0 {
		t.Fatalf("still planned %v", ft.fresh)
	}
}
