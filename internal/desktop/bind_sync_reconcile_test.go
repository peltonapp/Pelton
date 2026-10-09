package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
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
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
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
	a := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}
	t.Cleanup(func() { a.stopAccountScheduler(id) })
	return a, *account, out
}

func TestEnqueueFullReconcileDedupsPerFolder(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	release := make(chan struct{})
	var mu sync.Mutex
	calls := map[int64]int{}
	a.fullReconcileForTest = func(_ context.Context, _ storage.Account, f storage.Folder) error {
		mu.Lock()
		calls[f.ID]++
		mu.Unlock()
		<-release
		return nil
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	a.enqueueFullReconcile(a.ctx, account, folders)
	time.Sleep(100 * time.Millisecond)
	close(release)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls[folders[0].ID] != 1 {
		t.Fatalf("reconcile ran %d times for one folder, want 1", calls[folders[0].ID])
	}
	// After it finished the folder can be queued again.
	a.enqueueFullReconcile(a.ctx, account, folders)
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
	select {
	case id := <-got:
		if id != folders[0].ID {
			t.Fatalf("reconciled folder %d, want INBOX", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("due folder never reconciled")
	}
	select {
	case id := <-got:
		t.Fatalf("fresh folder %d reconciled too", id)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestReconcilePauseYieldsOnlyToLiveAtNOne(t *testing.T) {
	a, account, _ := reconcileTestApp(t)
	if err := a.store.SetInt(a.ctx, settingSyncMaxParallel, 1); err != nil {
		t.Fatal(err)
	}
	rt, err := a.ensureAccountSync(a.ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	pause := a.reconcilePause(account.ID)
	rt.sched.RequestSoftPause() // what IMAP IDLE does at N=1
	if pause() {
		t.Fatal("a soft-pause alone (IDLE) must not make the reconcile yield")
	}
	rt.sched.ClearSoftPause()
	if pause() {
		t.Fatal("nothing live is waiting")
	}
	release := holdLiveJob(t, rt)
	if !pause() {
		t.Fatal("a live job at N=1 must make the reconcile yield")
	}
	release()
}

// holdLiveJob queues a live job that runs until the returned func is called,
// and waits until the scheduler has it.
func holdLiveJob(t *testing.T, rt *accountSync) func() {
	t.Helper()
	hold := make(chan struct{})
	running := make(chan struct{})
	rt.sched.Enqueue(syncsched.Job{
		Priority: syncsched.PriorityLive,
		Kind:     syncsched.JobManualSync,
		Run: func(context.Context) error {
			close(running)
			<-hold
			return nil
		},
	})
	select {
	case <-running:
	case <-time.After(2 * time.Second):
		t.Fatal("live job never ran")
	}
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	return release
}

func TestReconcilePauseIgnoresLiveAtNTwo(t *testing.T) {
	a, account, _ := reconcileTestApp(t)
	if err := a.store.SetInt(a.ctx, settingSyncMaxParallel, 2); err != nil {
		t.Fatal(err)
	}
	rt, err := a.ensureAccountSync(a.ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	pause := a.reconcilePause(account.ID)
	holdLiveJob(t, rt)
	if !rt.sched.LiveBusy() {
		t.Fatal("live job is not running")
	}
	if pause() {
		t.Fatal("at N=2 the reconcile has its own slot and must not yield to live work")
	}
}

func TestManualSyncDoesNotWaitForReconcile(t *testing.T) {
	a, account, _ := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	useFake(t, a, account.ID, &fakeIMAP{})
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
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("manual sync: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("manual sync waited for the full reconcile")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("manual sync never queued the reconcile")
	}
}

// reconcileRecorder points the account's manual sync at an empty fake server
// and reports every folder a full reconcile ran for.
func reconcileRecorder(t *testing.T, a *App, accountID int64) <-chan int64 {
	useFake(t, a, accountID, &fakeIMAP{})
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
	select {
	case id := <-got:
		if id != folders[0].ID {
			t.Fatalf("auto-sync reconciled folder %d, want the stale INBOX", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("auto-sync queued no reconcile for a folder past the setting")
	}
	select {
	case id := <-got:
		t.Fatalf("auto-sync queued a full reconcile of fresh folder %d", id)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestUserSyncQueuesReconcile(t *testing.T) {
	for name, run := range map[string]func(*App, storage.Account) error{
		"TriggerSync":    func(a *App, _ storage.Account) error { return a.TriggerSync() },
		"SyncAccountNow": func(a *App, acc storage.Account) error { return a.SyncAccountNow(acc.ID) },
	} {
		t.Run(name, func(t *testing.T) {
			a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
			got := reconcileRecorder(t, a, account.ID)
			if err := run(a, account); err != nil {
				t.Fatalf("sync: %v", err)
			}
			select {
			case id := <-got:
				if id != folders[0].ID {
					t.Fatalf("reconciled folder %d, want INBOX", id)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("user-initiated sync queued no full reconcile")
			}
		})
	}
}

// verifyEvents records the "checking folders" events of one account.
func verifyEvents(a *App, accountID int64) func() []SyncProgressEvent {
	var mu sync.Mutex
	var got []SyncProgressEvent
	a.syncProgressEmitForTest = func(e SyncProgressEvent) {
		if e.AccountID != accountID || e.Phase != SyncPhaseVerify {
			return
		}
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}
	return func() []SyncProgressEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]SyncProgressEvent(nil), got...)
	}
}

// Stopping the scheduler mid-sweep cancels the running check and drops the
// queued ones, so nothing would ever close the line: the stop has to.
func TestVerifyLineClosesWhenSchedulerStopsMidSweep(t *testing.T) {
	a, account, folders := reconcileTestApp(t,
		storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
		storage.Folder{Name: "Archive", IMAPPath: "Archive"},
	)
	if err := a.store.SetInt(a.ctx, settingSyncMaxParallel, 1); err != nil {
		t.Fatal(err)
	}
	events := verifyEvents(a, account.ID)
	running := make(chan struct{}, 2)
	a.fullReconcileForTest = func(ctx context.Context, _ storage.Account, _ storage.Folder) error {
		running <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	a.enqueueFullReconcile(a.ctx, account, folders)
	select {
	case <-running:
	case <-time.After(2 * time.Second):
		t.Fatal("no reconcile started")
	}
	rt := a.accountSync(account.ID)
	a.stopAccountScheduler(account.ID)

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
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Fatal("reconcile never ran")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := events()
		closes := 0
		for _, e := range got {
			if e.Folder == "" {
				closes++
			}
		}
		if len(got) > 0 && got[len(got)-1].Folder == "" {
			if closes != 1 {
				t.Fatalf("verify events = %+v, want exactly one close", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("verify events = %+v, want the line closed", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	useFake(t, a, account.ID, &fakeIMAP{})
	var mu sync.Mutex
	var lines []SyncProgressEvent
	a.syncProgressEmitForTest = func(e SyncProgressEvent) {
		if e.Phase == SyncPhaseVerify {
			return
		}
		mu.Lock()
		lines = append(lines, e)
		mu.Unlock()
	}
	if err := a.execFullReconcile(a.ctx, account, folders[0]); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
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
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"})
	if err := a.store.SetBool(a.ctx, settingNotifyNewMail, true); err != nil {
		t.Fatal(err)
	}
	useFake(t, a, account.ID, &fakeIMAP{})
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return &oneMailIMAP{}, nil }
	notified := make(chan int64, 4)
	a.notificationForTest = func(n notification) { notified <- n.messageID }
	announced := make(chan MailNewEvent, 4)
	a.emitForTest = func(name string, payload any) {
		if e, ok := payload.(MailNewEvent); ok && name == EventMailNew {
			announced <- e
		}
	}

	if err := a.execFullReconcile(a.ctx, account, folders[0]); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	select {
	case <-announced:
	case <-time.After(2 * time.Second):
		t.Fatal("healed message was never announced to the list")
	}
	select {
	case id := <-notified:
		t.Fatalf("full reconcile notified for message %d", id)
	case <-time.After(300 * time.Millisecond):
	}

	// The other body paths still notify.
	msgs, err := a.store.ListMessages(a.ctx, folders[0].ID, 0)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages = %d (%v), want the healed one", len(msgs), err)
	}
	a.announceNewBodies(folders[0], psync.FolderSyncResult{New: 1, NewIDs: []int64{msgs[0].ID}})
	select {
	case id := <-notified:
		if id != msgs[0].ID {
			t.Fatalf("notified %d, want %d", id, msgs[0].ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("announceNewBodies no longer notifies")
	}
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

// The check lists only. The bodies it finds go to an ordinary background
// body job, which soft-pauses for live work, so an opened message does not
// wait for them.
func TestFullReconcileQueuesBodiesAsBackgroundJob(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"})
	useFake(t, a, account.ID, &fakeIMAP{})
	client := &recordingOneMailIMAP{fetched: make(chan []imap.UID, 4)}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	rt, err := a.ensureAccountSync(a.ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.sched.RequestSoftPause()

	if err := a.execFullReconcile(a.ctx, account, folders[0]); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	select {
	case uids := <-client.fetched:
		t.Fatalf("full reconcile fetched bodies %v inline", uids)
	default:
	}
	if !rt.sched.BackgroundBusy() {
		t.Fatal("full reconcile queued no body job")
	}

	rt.sched.ClearSoftPause()
	select {
	case uids := <-client.fetched:
		if len(uids) != 1 || uids[0] != 5 {
			t.Fatalf("body job fetched %v, want [5]", uids)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued body job never fetched the found message")
	}
}

// A folder check hands its bodies to a background job. A second check of the
// folder must not queue another one beside the job still waiting.
func TestReconcileBodyJobsDedupPerFolder(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"})
	useFake(t, a, account.ID, &fakeIMAP{})
	client := &recordingOneMailIMAP{fetched: make(chan []imap.UID, 4)}
	var sessions atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		sessions.Add(1)
		return client, nil
	}
	rt, err := a.ensureAccountSync(a.ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.sched.RequestSoftPause()
	if err := a.execFullReconcile(a.ctx, account, folders[0]); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	// A second check of the folder hands off the bodies it found while the
	// first one's job still waits.
	a.enqueueReconcileBodies(a.ctx, account, folders[0], []string{"5"})

	sessions.Store(0)
	rt.sched.ClearSoftPause()
	deadline := time.Now().Add(2 * time.Second)
	for rt.sched.BackgroundBusy() {
		if time.Now().After(deadline) {
			t.Fatal("body jobs never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := sessions.Load(); got != 1 {
		t.Fatalf("body jobs run = %d, want 1", got)
	}
	if !rt.claimReconcileBodies(folders[0].ID) {
		t.Fatal("the finished body job kept its folder claimed")
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

// runningWorker registers a stand-in worker, which is what makes an account
// count as running.
func runningWorker(a *App, accountID int64) {
	a.workersMu.Lock()
	defer a.workersMu.Unlock()
	if a.workers == nil {
		a.workers = make(map[int64]*accountWorker)
	}
	a.workers[accountID] = &accountWorker{cancel: func() {}, done: make(chan struct{})}
}

// Timed auto-sync, which is what queues due checks, can be switched off. The
// hourly check has to queue them on its own.
func TestDueReconcileLoopQueuesWithoutAutoSync(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX"})
	runningWorker(a, account.ID)
	got := reconcileRecorder(t, a, account.ID)
	old := dueReconcileInterval
	dueReconcileInterval = 10 * time.Millisecond
	t.Cleanup(func() { dueReconcileInterval = old })

	goSafe("checking folders", a.runDueReconcileLoop)
	select {
	case id := <-got:
		if id != folders[0].ID {
			t.Fatalf("reconciled folder %d, want INBOX", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no due folder check without an auto-sync tick")
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

	runningWorker(a, account.ID)
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
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"})
	useFake(t, a, account.ID, &fakeIMAP{})
	// the check queues a body job after it; stop it before the password goes.
	t.Cleanup(func() { a.stopAccountScheduler(account.ID) })
	client := &gatedListIMAP{firstIn: make(chan struct{}), gate: make(chan struct{})}
	close(client.gate)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	if _, err := a.ensureAccountSync(a.ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	prev := folderListPoll
	folderListPoll = 5 * time.Millisecond
	t.Cleanup(func() { folderListPoll = prev })

	live := newCountingList()
	liveDone := make(chan error, 1)
	go func() { liveDone <- a.listFolderOnce(a.ctx, account.ID, folders[0].ID, live.run) }()
	<-live.firstIn

	reconciled := make(chan error, 1)
	go func() { reconciled <- a.execFullReconcile(a.ctx, account, folders[0]) }()
	time.Sleep(100 * time.Millisecond)
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
	select {
	case err := <-reconciled:
		if err != nil {
			t.Fatalf("full reconcile: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("full check never ran after the list ended")
	}
	if got := client.lists.Load(); got != 1 {
		t.Fatalf("full check listed %d times, want 1", got)
	}
}

// A list asked for while the full check runs is not lost: it runs right after
// the check, so the newest flags and cursor are the ones stored last.
func TestFullReconcileRunsCoalescedListAfterIt(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"})
	useFake(t, a, account.ID, &fakeIMAP{})
	// the check queues a body job after it; stop it before the password goes.
	t.Cleanup(func() { a.stopAccountScheduler(account.ID) })
	client := &gatedListIMAP{firstIn: make(chan struct{}), gate: make(chan struct{})}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	if _, err := a.ensureAccountSync(a.ctx, account.ID); err != nil {
		t.Fatal(err)
	}

	reconciled := make(chan error, 1)
	go func() { reconciled <- a.execFullReconcile(a.ctx, account, folders[0]) }()
	<-client.firstIn

	live := newCountingList()
	close(live.release)
	if err := a.listFolderOnce(a.ctx, account.ID, folders[0].ID, live.run); err != nil {
		t.Fatalf("live list: %v", err)
	}
	if got := live.runs.Load(); got != 0 {
		t.Fatalf("live list ran %d times beside the full check, want 0", got)
	}

	close(client.gate)
	select {
	case err := <-reconciled:
		if err != nil {
			t.Fatalf("full reconcile: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("full reconcile never ended")
	}
	if got := client.lists.Load(); got != 2 {
		t.Fatalf("folder listed %d times, want 2 (the check, then the coalesced list)", got)
	}
}
