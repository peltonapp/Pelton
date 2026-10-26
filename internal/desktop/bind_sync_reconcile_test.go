package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func TestFoldersDueForReconcile(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	folders := []storage.Folder{
		{ID: 1, Name: "INBOX", IMAPPath: "INBOX"},
		{ID: 2, Name: "Fresh", IMAPPath: "Fresh", LastFullSyncAt: now.Add(-24 * time.Hour)},
		{ID: 3, Name: "Old", IMAPPath: "Old", LastFullSyncAt: now.Add(-8 * 24 * time.Hour)},
		{ID: 4, Name: "Skip", IMAPPath: "Skip", SyncExcluded: true},
		{ID: 5, Name: "Box", IMAPPath: "Box", Attributes: []string{`\Noselect`}},
	}
	got := foldersDueForReconcile(folders, 7, now)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("due = %+v, want INBOX (never) and Old", got)
	}
	if due := foldersDueForReconcile(folders, 0, now); len(due) != 0 {
		t.Fatalf("days 0 = manual only, got %+v", due)
	}
}

func TestClampFullReconcileDays(t *testing.T) {
	for in, want := range map[int]int{-3: 0, 0: 0, 7: 7, 365: 365, 9000: 365} {
		if got := clampFullReconcileDays(in); got != want {
			t.Fatalf("clamp(%d) = %d, want %d", in, got, want)
		}
	}
}

// reconcileTestApp is an App with a store, one account and its folders.
func reconcileTestApp(t *testing.T, folders ...storage.Folder) (*App, storage.Account, []storage.Folder) {
	t.Helper()
	ctx, stop := testContext(t)
	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// after the store's cleanup, so it runs first: goroutines still using the
	// store finish before it closes (see testContext).
	t.Cleanup(stop)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateAccount(ctx, &storage.Account{Email: "user@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []storage.Folder
	for _, f := range folders {
		folder := f
		folder.AccountID = id
		if _, err := db.CreateFolder(ctx, &folder); err != nil {
			t.Fatal(err)
		}
		out = append(out, folder)
	}
	a := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler), workers: map[int64]*accountWorker{}}
	useFake(t, a, id, &fakeIMAP{})
	t.Cleanup(func() { a.stopAccountScheduler(id, false) })
	return a, *account, out
}

// reconcileInbox is a reconcileTestApp with one INBOX whose IMAP sessions all
// use client. The scheduler stops before the account's password goes, so a
// body job the check queues ends first.
func reconcileInbox(t *testing.T, client mailClient) (*App, storage.Account, storage.Folder) {
	t.Helper()
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"})
	t.Cleanup(func() { a.stopAccountScheduler(account.ID, false) })
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	return a, account, folders[0]
}

func TestEnqueueFullReconcileDedupsPerFolder(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	release := make(chan struct{})
	ran := make(chan int64, 4)
	a.fullReconcileForTest = func(_ context.Context, _ storage.Account, f storage.Folder) error {
		ran <- f.ID
		<-release
		return nil
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	a.enqueueFullReconcile(a.ctx, account, folders)
	recv(t, ran, 2*time.Second, "reconcile never ran")
	close(release)
	// A deduped second task was never queued; a duplicate would keep the
	// queue busy until it had run (and reported in ran).
	sched := a.accountSync(account.ID).sched
	waitUntil(t, 2*time.Second, "reconcile tasks never finished", func() bool { st := sched.Stats(); return st.Normal+st.Running == 0 })
	if extra := len(ran); extra != 0 {
		t.Fatalf("reconcile ran %d times for one folder, want 1", 1+extra)
	}
	// After it finished the folder can be queued again.
	a.enqueueFullReconcile(a.ctx, account, folders)
	if id := recv(t, ran, 2*time.Second, "a finished folder could not be queued again"); id != folders[0].ID {
		t.Fatalf("reconciled folder %d, want INBOX", id)
	}
}

func TestEnqueueDueReconcileSkipsFreshFolders(t *testing.T) {
	a, account, folders := reconcileTestApp(t,
		storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
		storage.Folder{Name: "Fresh", IMAPPath: "Fresh"},
	)
	if err := a.store.SetFolderFullSyncAt(a.ctx, folders[1].ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := make(chan int64, 4)
	a.fullReconcileForTest = func(_ context.Context, _ storage.Account, f storage.Folder) error {
		got <- f.ID
		return nil
	}
	a.enqueueDueReconcile(a.ctx, account)
	if id := recv(t, got, 2*time.Second, "due folder never reconciled"); id != folders[0].ID {
		t.Fatalf("reconciled folder %d, want INBOX", id)
	}
	noRecv(t, got, 200*time.Millisecond, "fresh folder reconciled too")
}

func TestManualSyncDoesNotWaitForReconcile(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	deltaOnly(t, a, folders)
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var once sync.Once
	a.fullReconcileForTest = func(context.Context, storage.Account, storage.Folder) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- a.syncAccount(account, true) }()
	if err := recv(t, done, time.Second, "manual sync waited for the full reconcile"); err != nil {
		t.Fatalf("manual sync: %v", err)
	}
	recv(t, started, 2*time.Second, "manual sync never queued the reconcile")
}

// reconcileRecorder points the account's manual sync at an empty fake server
// and reports every folder a full reconcile ran for.
func reconcileRecorder(t *testing.T, a *App, _ int64) <-chan int64 {
	// reconcileTestApp stored the password; a second useFake would delete it
	// before the scheduler stops, under a running dial.
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &fakeIMAP{}, nil }
	got := make(chan int64, 8)
	a.fullReconcileForTest = func(_ context.Context, _ storage.Account, f storage.Folder) error {
		got <- f.ID
		return nil
	}
	return got
}

// unchangedIMAP is a CONDSTORE server on which nothing changed since
// unchangedIMAPCursor, so a sync of a folder holding that cursor lists nothing.
type unchangedIMAP struct{ fakeIMAP }

const unchangedIMAPCursor = "v1:9:100:51:50"

func (*unchangedIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: mailbox, UIDValidity: 9, HighestModSeq: 100, UIDNext: 51, NumMessages: 50}, nil
}

// Timed auto-sync never queues every folder, only those whose last full
// check is older than the setting, so the interval holds without a restart.
func TestAutoSyncPassQueuesOnlyDueReconcile(t *testing.T) {
	a, account, folders := reconcileTestApp(t,
		storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
		storage.Folder{Name: "Fresh", IMAPPath: "Fresh"},
	)
	stale := time.Now().Add(-time.Duration(a.fullReconcileDays()+1) * 24 * time.Hour)
	if err := a.store.SetFolderFullSyncAt(a.ctx, folders[0].ID, stale); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetFolderFullSyncAt(a.ctx, folders[1].ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	// a clean full list stamps the folder as checked, so the timed sync has to
	// find nothing changed through its CONDSTORE cursor, as a synced folder does.
	for _, f := range folders {
		if err := a.store.SetFolderStateToken(a.ctx, f.ID, unchangedIMAPCursor); err != nil {
			t.Fatal(err)
		}
		if err := a.store.SetFolderSyncWindow(a.ctx, f.ID, "", true); err != nil {
			t.Fatal(err)
		}
	}
	got := reconcileRecorder(t, a, account.ID)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &unchangedIMAP{}, nil }
	if err := a.runAutoSyncPass(); err != nil {
		t.Fatalf("auto-sync pass: %v", err)
	}
	if id := recv(t, got, 2*time.Second, "auto-sync queued no reconcile for a folder past the setting"); id != folders[0].ID {
		t.Fatalf("auto-sync reconciled folder %d, want the stale INBOX", id)
	}
	noRecv(t, got, 300*time.Millisecond, "auto-sync queued a full reconcile of the fresh folder")
}

// deltaOnly gives the folders a CONDSTORE cursor on an unchanged server, so
// a sync lists them as an empty delta and stamps no full check.
func deltaOnly(t *testing.T, a *App, folders []storage.Folder) {
	t.Helper()
	for _, f := range folders {
		if err := a.store.SetFolderStateToken(a.ctx, f.ID, unchangedIMAPCursor); err != nil {
			t.Fatal(err)
		}
		if err := a.store.SetFolderSyncWindow(a.ctx, f.ID, "", true); err != nil {
			t.Fatal(err)
		}
	}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &unchangedIMAP{}, nil }
}

// A user's Sync queues the full check of the folders not checked in a day:
// never checked, checked long ago, or stamped in the future by a clock that
// was ahead. One checked an hour ago is left alone.
func TestUserSyncQueuesReconcile(t *testing.T) {
	for name, run := range map[string]func(*App, storage.Account) error{
		"TriggerSync":    func(a *App, _ storage.Account) error { return a.TriggerSync() },
		"SyncAccountNow": func(a *App, acc storage.Account) error { return a.SyncAccountNow(acc.ID) },
	} {
		t.Run(name, func(t *testing.T) {
			a, account, folders := reconcileTestApp(t,
				storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
				storage.Folder{Name: "Old", IMAPPath: "Old"},
				storage.Folder{Name: "Ahead", IMAPPath: "Ahead"},
				storage.Folder{Name: "Recent", IMAPPath: "Recent"},
			)
			for i, at := range []time.Time{{}, time.Now().Add(-25 * time.Hour), time.Now().Add(48 * time.Hour), time.Now().Add(-time.Hour)} {
				if !at.IsZero() {
					if err := a.store.SetFolderFullSyncAt(a.ctx, folders[i].ID, at); err != nil {
						t.Fatal(err)
					}
				}
			}
			got := reconcileRecorder(t, a, account.ID)
			deltaOnly(t, a, folders)
			if err := run(a, account); err != nil {
				t.Fatalf("sync: %v", err)
			}
			var checked []int64
			for range 3 {
				checked = append(checked, recv(t, got, 2*time.Second, "user-initiated sync queued too few full checks"))
			}
			noRecv(t, got, 300*time.Millisecond, "the folder checked an hour ago was checked again")
			slices.Sort(checked)
			if want := []int64{folders[0].ID, folders[1].ID, folders[2].ID}; !slices.Equal(checked, want) {
				t.Fatalf("checked %v, want %v", checked, want)
			}
		})
	}
}

// Low-power mode queues no full check at all, not only the hourly ones.
func TestLowPowerModeQueuesNoReconcile(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	got := reconcileRecorder(t, a, account.ID)
	if err := a.store.SetBool(a.ctx, settingLowPower, true); err != nil {
		t.Fatal(err)
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	a.enqueueDueReconcile(a.ctx, account)
	noRecv(t, got, 200*time.Millisecond, "low-power mode queued a full check")
}

// A failed check is not a failed sync: the account's sync state stays ok.
func TestFailedReconcileLeavesTheSyncStateAlone(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	failed := make(chan struct{})
	a.fullReconcileForTest = func(context.Context, storage.Account, storage.Folder) error {
		defer close(failed)
		return errors.New("NO folder too big")
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	recv(t, failed, 2*time.Second, "check never ran")
	waitUntil(t, 2*time.Second, "check never ended", func() bool { return a.accountSync(account.ID).sched.Stats().Running == 0 })
	states, err := a.store.AccountSyncStates(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.AccountID == account.ID && st.Failing() {
			t.Fatalf("a failed check marked the account failed: %+v", st)
		}
	}
}

// verifyEvents records the progress events of one account and returns its
// "checking folders" (verify) events so far.
func verifyEvents(a *App, accountID int64) func() []SyncProgressEvent {
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(accountID)
	return func() []SyncProgressEvent {
		var got []SyncProgressEvent
		for _, e := range rec.snapshot() {
			if e.Phase == SyncPhaseVerify {
				got = append(got, e)
			}
		}
		return got
	}
}

// Stopping the scheduler mid-sweep cancels the running check and drops the
// queued ones, so nothing would ever close the line: the stop has to.
func TestVerifyLineClosesWhenSchedulerStopsMidSweep(t *testing.T) {
	a, account, folders := reconcileTestApp(t,
		storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
		storage.Folder{Name: "Archive", IMAPPath: "Archive"},
	)
	setParallel(t, a, 1)
	events := verifyEvents(a, account.ID)
	running := make(chan struct{}, 2)
	a.fullReconcileForTest = func(ctx context.Context, _ storage.Account, _ storage.Folder) error {
		running <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	recv(t, running, 2*time.Second, "no reconcile started")
	rt := a.accountSync(account.ID)
	a.stopAccountScheduler(account.ID, true)

	got := events()
	if len(got) == 0 || got[len(got)-1].Folder != "" {
		t.Fatalf("verify events = %+v, want the last one to close the line", got)
	}
	rt.mu.Lock()
	left := rt.reconciling.len()
	rt.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d reconcile claims left after the scheduler stopped", left)
	}
}

// The last check to finish closes the line.
func TestVerifyLineClosesAfterLastReconcile(t *testing.T) {
	a, account, folders := reconcileTestApp(t,
		storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
		storage.Folder{Name: "Archive", IMAPPath: "Archive"},
	)
	events := verifyEvents(a, account.ID)
	finished := make(chan struct{}, 2)
	a.fullReconcileForTest = func(context.Context, storage.Account, storage.Folder) error {
		finished <- struct{}{}
		return nil
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	for range folders {
		recv(t, finished, 2*time.Second, "reconcile never ran")
	}
	waitUntil(t, 2*time.Second, "verify line never closed", func() bool {
		got := events()
		return len(got) > 0 && got[len(got)-1].Folder == ""
	})
	assertClosedOnce(t, events())
}

// The heartbeat re-sends the running sync's line. The verify line is a
// separate line, so neither its events nor its close may replace or clear
// what the heartbeat re-sends.
func TestVerifyEventsLeaveTheHeartbeatLineAlone(t *testing.T) {
	a, account, _ := reconcileTestApp(t)
	a.emitSyncProgress(account.ID, account.Email, "", syncCounts{Folder: "INBOX"})
	a.emitSyncProgress(account.ID, account.Email, "", syncCounts{Folder: "Archive", Phase: SyncPhaseVerify})
	a.emitSyncProgress(account.ID, account.Email, "", syncCounts{Phase: SyncPhaseVerify})
	st := a.accountStateFor(account.ID)
	st.progressMu.Lock()
	last := st.lastProgress
	st.progressMu.Unlock()
	if last == nil || last.Folder != "INBOX" {
		t.Fatalf("heartbeat line = %+v, want the INBOX sync line", last)
	}
}

// The check shows only its own calm line. The engine's per-folder progress
// would open a normal sync line (spinner, bar) that nothing closes.
func TestFullReconcileEmitsNoSyncLine(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	rec := &progressRecorder{}
	a.emitForTest = rec.hook(account.ID)
	if err := a.execFullReconcile(a.ctx, account, folders[0]); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	var lines []SyncProgressEvent
	for _, e := range rec.snapshot() {
		if e.Phase != SyncPhaseVerify {
			lines = append(lines, e)
		}
	}
	if len(lines) != 0 {
		t.Fatalf("full reconcile emitted sync lines %+v, want none", lines)
	}
}

// oneMailIMAP is an INBOX holding one unseen message, UID 5, that the cache
// does not have yet.
type oneMailIMAP struct{ fakeIMAP }

func (*oneMailIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: 6, NumMessages: 1}, nil
}

func (*oneMailIMAP) FetchAllFlags() ([]pimap.MessageHeader, error) {
	return []pimap.MessageHeader{{UID: 5}}, nil
}

func (*oneMailIMAP) FetchHeaders([]imap.UID, *imap.FetchOptions) ([]pimap.MessageHeader, error) {
	return []pimap.MessageHeader{{UID: 5, Subject: "old", From: "ada@example.com", Date: time.Now(), HasEnvelope: true}}, nil
}

func (*oneMailIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	for _, uid := range uids {
		if err := fn(uid, &pimap.Message{UID: uid, Subject: "old", From: "ada@example.com", Date: time.Now(), Text: "hi"}, nil); err != nil {
			return err
		}
	}
	return nil
}

// A message the background check heals into Inbox is old mail the user may
// never have read. It goes into the list, but raises no new-mail notification.
func TestFullReconcileAnnouncesHealedMailWithoutNotifying(t *testing.T) {
	a, account, inbox := reconcileInbox(t, &oneMailIMAP{})
	if err := a.store.SetBool(a.ctx, settingNotifyNewMail, true); err != nil {
		t.Fatal(err)
	}
	notified := make(chan int64, 4)
	a.notificationForTest = func(n notification) { notified <- n.messageID }
	announced := make(chan MailNewEvent, 4)
	a.emitForTest = func(name string, payload any) {
		if e, ok := payload.(MailNewEvent); ok && name == EventMailNew {
			announced <- e
		}
	}

	if err := a.execFullReconcile(a.ctx, account, inbox); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	recv(t, announced, 2*time.Second, "healed message was never announced to the list")
	noRecv(t, notified, 300*time.Millisecond, "full reconcile notified for the healed message")

}

// recordingOneMailIMAP is oneMailIMAP that reports every body fetch.
type recordingOneMailIMAP struct {
	oneMailIMAP
	fetched chan []imap.UID
}

func (c *recordingOneMailIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.fetched <- append([]imap.UID(nil), uids...)
	return c.oneMailIMAP.FetchMessages(uids, fn)
}

// holdFolderList runs a list task of the folder until the returned func is
// called. Body tasks of the folder wait for it.
func holdFolderList(t *testing.T, rt *accountSync, folderID int64) func() {
	t.Helper()
	hold := make(chan struct{})
	recv(t, signalJob(rt.sched, syncsched.KindListStubs, folderID, hold), 2*time.Second, "list task never started")
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	return release
}

// The check lists only. The bodies it finds go to the folder's body
// generator in the normal lane, so an opened message does not wait for them.
func TestFullReconcileQueuesBodiesAsNormalTask(t *testing.T) {
	client := &recordingOneMailIMAP{fetched: make(chan []imap.UID, 4)}
	a, account, inbox := reconcileInbox(t, client)
	rt := mustAccountSync(t, a, account.ID)
	release := holdFolderList(t, rt, inbox.ID)

	if err := a.execFullReconcile(a.ctx, account, inbox); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	select {
	case uids := <-client.fetched:
		t.Fatalf("full reconcile fetched bodies %v inline", uids)
	default:
	}
	if st := rt.sched.Stats(); st.Normal != 1 {
		t.Fatalf("stats %+v, want one queued body task", st)
	}

	release()
	if uids := recv(t, client.fetched, 2*time.Second, "queued body task never fetched the found message"); len(uids) != 1 || uids[0] != 5 {
		t.Fatalf("body task fetched %v, want [5]", uids)
	}
}

// A folder check hands its bodies to the folder's body generator. A second
// check of the folder joins the one still waiting.
func TestReconcileBodyTasksDedupPerFolder(t *testing.T) {
	client := &recordingOneMailIMAP{fetched: make(chan []imap.UID, 4)}
	a, account, inbox := reconcileInbox(t, client)
	rt := mustAccountSync(t, a, account.ID)
	release := holdFolderList(t, rt, inbox.ID)
	if err := a.execFullReconcile(a.ctx, account, inbox); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	if err := a.execFullReconcile(a.ctx, account, inbox); err != nil {
		t.Fatalf("second full reconcile: %v", err)
	}
	if st := rt.sched.Stats(); st.Normal != 1 {
		t.Fatalf("stats %+v, want the second check joined to one body task", st)
	}
	release()
	recv(t, client.fetched, 2*time.Second, "body task never ran")
	waitUntil(t, 2*time.Second, "body tasks never finished", func() bool { st := rt.sched.Stats(); return st.Normal+st.Running == 0 })
	if extra := len(client.fetched); extra != 0 {
		t.Fatalf("bodies fetched %d more times, want once", extra)
	}
}

// Two folder checks can finish at the same moment. Each one used to release
// its claim and then look at the totals, so both saw none left and both
// closed the line. Only the release that ends the last check may close it.
func TestOnlyTheLastReconcileReleaseClosesTheLine(t *testing.T) {
	rt := &accountSync{}
	if !rt.claimReconcile(1) || !rt.claimReconcile(2) {
		t.Fatal("claims refused")
	}
	if rt.releaseReconcile(1) {
		t.Fatal("releasing the first of two checks reported the last one")
	}
	if !rt.releaseReconcile(2) {
		t.Fatal("releasing the second check did not report the last one")
	}
	if rt.releaseReconcile(2) {
		t.Fatal("releasing a check twice reported the last one again")
	}
}

// Timed auto-sync, which is what queues due checks, can be switched off. The
// hourly check has to queue them on its own.
func TestDueReconcileLoopQueuesWithoutAutoSync(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	plantQuiescentWorker(a, account.ID) // a registered worker makes the account count as running
	got := reconcileRecorder(t, a, account.ID)
	old := dueReconcileInterval
	dueReconcileInterval = 10 * time.Millisecond
	t.Cleanup(func() { dueReconcileInterval = old })

	goSafe("checking folders", a.runDueReconcileLoop)
	if id := recv(t, got, 2*time.Second, "no due folder check without an auto-sync tick"); id != folders[0].ID {
		t.Fatalf("reconciled folder %d, want INBOX", id)
	}
}

// A held account (removal) and one without a running worker
// get no check, and no scheduler is started for them either.
func TestDueReconcileSkipsHeldAndStoppedAccounts(t *testing.T) {
	a, account, _ := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	reconcileRecorder(t, a, account.ID)

	a.enqueueDueReconcileRunning()
	if a.accountSync(account.ID) != nil {
		t.Fatal("an account without a running worker got a check")
	}

	plantQuiescentWorker(a, account.ID)
	release := a.holdAccountSync(account.ID)
	defer release()
	a.enqueueDueReconcileRunning()
	if a.accountSync(account.ID) != nil {
		t.Fatal("a held account got a check")
	}

	release()
	a.enqueueDueReconcileRunning()
	if a.accountSync(account.ID) == nil {
		t.Fatal("a running account that is not held got no check")
	}
}

// gatedListIMAP is oneMailIMAP whose first full flag listing waits for gate,
// counting every flag listing.
type gatedListIMAP struct {
	oneMailIMAP
	lists   atomic.Int32
	firstIn chan struct{}
	gate    chan struct{}
}

func (c *gatedListIMAP) FetchAllFlags() ([]pimap.MessageHeader, error) {
	if c.lists.Add(1) == 1 {
		close(c.firstIn)
		<-c.gate
	}
	return c.oneMailIMAP.FetchAllFlags()
}

// A full check run beside a delta of the same folder would store the flags and
// cursor it read before the delta's newer ones. It waits for a list already
// running instead.
func TestFullReconcileWaitsForRunningList(t *testing.T) {
	client := &gatedListIMAP{firstIn: make(chan struct{}), gate: make(chan struct{})}
	close(client.gate)
	a, account, inbox := reconcileInbox(t, client)
	mustAccountSync(t, a, account.ID)
	prev := folderListPoll
	folderListPoll = 5 * time.Millisecond
	t.Cleanup(func() { folderListPoll = prev })

	live := newCountingList()
	liveDone := make(chan error, 1)
	go func() { liveDone <- a.listFolderOnce(a.ctx, account.ID, inbox.ID, live.run) }()
	<-live.firstIn

	reconciled := make(chan error, 1)
	go func() { reconciled <- a.execFullReconcile(a.ctx, account, inbox) }()
	time.Sleep(100 * time.Millisecond) // window: the check must not list beside the running list
	if got := client.lists.Load(); got != 0 {
		t.Fatalf("full check listed %d times while a list of the folder ran, want 0", got)
	}

	close(live.release)
	if err := <-liveDone; err != nil {
		t.Fatalf("live list: %v", err)
	}
	if got := live.runs.Load(); got != 1 {
		t.Fatalf("live lists = %d, want 1 (a waiting check asks for no follow-up)", got)
	}
	if err := recv(t, reconciled, 2*time.Second, "full check never ran after the list ended"); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	if got := client.lists.Load(); got != 1 {
		t.Fatalf("full check listed %d times, want 1", got)
	}
}

// A list asked for while the full check runs is not lost: it runs right after
// the check, so the newest flags and cursor are the ones stored last.
func TestFullReconcileRunsCoalescedListAfterIt(t *testing.T) {
	client := &gatedListIMAP{firstIn: make(chan struct{}), gate: make(chan struct{})}
	a, account, inbox := reconcileInbox(t, client)
	mustAccountSync(t, a, account.ID)

	reconciled := make(chan error, 1)
	go func() { reconciled <- a.execFullReconcile(a.ctx, account, inbox) }()
	<-client.firstIn

	live := newCountingList()
	close(live.release)
	if err := a.listFolderOnce(a.ctx, account.ID, inbox.ID, live.run); err != nil {
		t.Fatalf("live list: %v", err)
	}
	if got := live.runs.Load(); got != 0 {
		t.Fatalf("live list ran %d times beside the full check, want 0", got)
	}

	close(client.gate)
	if err := recv(t, reconciled, 2*time.Second, "full reconcile never ended"); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	if got := client.lists.Load(); got != 2 {
		t.Fatalf("folder listed %d times, want 2 (the check, then the coalesced list)", got)
	}
}

// refusingIMAP is a server that refuses every login, counting them.
type refusingIMAP struct {
	fakeIMAP
	logins *atomic.Int32
}

func (c *refusingIMAP) Login() error {
	c.logins.Add(1)
	return fmt.Errorf("%w: NO [AUTHENTICATIONFAILED] Invalid credentials", pimap.ErrAuthFailed)
}

// A wrong password costs one failed login, not one per folder check: the
// first check's refused login drops the rest, and the account's checks back
// off. A burst of refused logins is what gets an address banned (fail2ban).
func TestBadPasswordCausesNoBurstOfLogins(t *testing.T) {
	var folders []storage.Folder
	for i := range 10 {
		folders = append(folders, storage.Folder{Name: fmt.Sprint("F", i), IMAPPath: fmt.Sprint("F", i)})
	}
	a, account, created := reconcileTestApp(t, folders...)
	var logins atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &refusingIMAP{logins: &logins}, nil }
	a.enqueueFullReconcile(a.ctx, account, created)
	sched := mustAccountSync(t, a, account.ID).sched
	waitUntil(t, 5*time.Second, "checks never ended", func() bool {
		st := sched.Stats()
		return logins.Load() > 0 && st.Normal+st.Running == 0
	})
	a.enqueueFullReconcile(a.ctx, account, created)
	time.Sleep(100 * time.Millisecond)
	if n := logins.Load(); n != 1 {
		t.Fatalf("%d logins for 10 folder checks with a wrong password, want 1", n)
	}
}

// The hourly pass queues no checks for an account that cannot sync now: its
// last sync failed, its login was refused, or it has no password.
func TestHourlyChecksSkipAccountsThatCannotSync(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(a *App, id int64)
	}{
		{"last sync failed", func(a *App, id int64) {
			_ = a.store.RecordSyncFailure(a.ctx, id, syncFailNetwork, "connection refused")
		}},
		{"login refused", func(a *App, id int64) {
			a.noteLoginResult(id, fmt.Errorf("%w: NO", pimap.ErrAuthFailed))
		}},
		{"no password", func(_ *App, id int64) { _ = credentials.Delete(id) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, account, _ := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
			reconcileRecorder(t, a, account.ID)
			plantQuiescentWorker(a, account.ID)
			tc.setup(a, account.ID)
			a.enqueueDueReconcileRunning()
			if a.accountSync(account.ID) != nil {
				t.Fatal("an account that cannot sync got a check")
			}
		})
	}
}

// A folder whose check failed is not checked again on the next trigger: it
// backs off, so a broken folder is not re-listed in full every time.
func TestFailedCheckBacksOff(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &fakeIMAP{}, nil }
	var runs atomic.Int32
	a.fullReconcileForTest = func(context.Context, storage.Account, storage.Folder) error {
		runs.Add(1)
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "mailbox is corrupt"}
	}
	sched := mustAccountSync(t, a, account.ID).sched
	a.enqueueFullReconcile(a.ctx, account, folders)
	waitUntil(t, 2*time.Second, "check never ran", func() bool { st := sched.Stats(); return runs.Load() == 1 && st.Normal+st.Running == 0 })
	a.enqueueFullReconcile(a.ctx, account, folders)
	time.Sleep(50 * time.Millisecond)
	if n := runs.Load(); n != 1 {
		t.Fatalf("the failed folder was checked %d times, want once until its backoff ends", n)
	}
}

// Low-power mode turned on while checks wait stops them: a queued check
// lists nothing, and the rest are dropped.
func TestLowPowerStopsQueuedChecks(t *testing.T) {
	a, account, folders := reconcileTestApp(t,
		storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
		storage.Folder{Name: "Work", IMAPPath: "Work"},
	)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &fakeIMAP{}, nil }
	gate := make(chan struct{})
	ran := make(chan int64, 4)
	a.fullReconcileForTest = func(_ context.Context, _ storage.Account, f storage.Folder) error {
		ran <- f.ID
		<-gate
		return nil
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	recv(t, ran, 2*time.Second, "first check never ran")
	if err := a.store.SetBool(a.ctx, settingLowPower, true); err != nil {
		t.Fatal(err)
	}
	close(gate)
	noRecv(t, ran, 200*time.Millisecond, "a check ran in low-power mode")
}

// A check that low-power mode stops gives its session back as it got it:
// it ran no command, so the session is not closed as broken and the next
// task reuses it instead of dialing.
func TestLowPowerCheckKeepsItsSession(t *testing.T) {
	a, account, folders := reconcileTestApp(t,
		storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
		storage.Folder{Name: "Work", IMAPPath: "Work"},
	)
	setParallel(t, a, 1)
	var dials atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		dials.Add(1)
		return &fakeIMAP{}, nil
	}
	gate := make(chan struct{})
	ran := make(chan int64, 4)
	a.fullReconcileForTest = func(_ context.Context, _ storage.Account, f storage.Folder) error {
		ran <- f.ID
		<-gate
		return nil
	}
	sched := mustAccountSync(t, a, account.ID).sched
	a.enqueueFullReconcile(a.ctx, account, folders)
	recv(t, ran, 2*time.Second, "first check never ran")
	if err := a.store.SetBool(a.ctx, settingLowPower, true); err != nil {
		t.Fatal(err)
	}
	close(gate)
	waitUntil(t, 2*time.Second, "the checks never ended", func() bool { st := sched.Stats(); return st.Normal+st.Running == 0 })
	if err := a.runNow(a.ctx, account, syncsched.KindSource, 0, func(context.Context, mailClient) error { return nil }); err != nil {
		t.Fatalf("runNow: %v", err)
	}
	if n := dials.Load(); n != 1 {
		t.Fatalf("%d dials, want the one session kept", n)
	}
}
