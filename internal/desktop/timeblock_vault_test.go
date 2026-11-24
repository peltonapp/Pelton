package desktop

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

const (
	blockMarkSubject = "BLOCKMARKSUBJECTv8"
	blockMarkTask    = "BLOCKMARKTASKk2"
)

// blockVaultFixture is mail in clear with time blocks made for it, before
// any vault holds it.
type blockVaultFixture struct {
	app     *App
	dir     string
	account int64
	folder  int64
	// mailBlock shows the mail's subject, ownBlock has a title the user
	// typed, taskBlock is for a task made from the mail; all three are
	// written to the local calendar with the task title included.
	mailBlock, ownBlock, taskBlock int64
}

func newBlockVaultFixture(t *testing.T) *blockVaultFixture {
	t.Helper()
	ctx, stopBackground := testContext(t)
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler), dataDir: dir}
	a.mailVault.kdf = &testVaultKDF
	a.initMailVault()
	idx, err := openSearchIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.index = idx
	t.Cleanup(func() { _ = a.index.Close() })

	f := &blockVaultFixture{app: a, dir: dir}
	if f.account, err = store.CreateAccount(ctx, &storage.Account{Email: "me@example.com", IMAPHost: "h", IMAPPort: 993}); err != nil {
		t.Fatal(err)
	}
	if f.folder, err = store.CreateFolder(ctx, &storage.Folder{AccountID: f.account, Name: "Projects", IMAPPath: "Projects"}); err != nil {
		t.Fatal(err)
	}
	msg, err := store.InsertMessage(ctx, &storage.Message{AccountID: f.account, FolderID: f.folder, UID: 1,
		MessageID: "<blockmark@example.org>", Subject: blockMarkSubject, FromAddress: "ada@example.org",
		BodyPlain: "see the plan", Date: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	settings := a.blockSettings()
	settings.IncludeTitle = true
	if err := a.SetTimeBlockSettings(settings); err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(TaskRequest{Title: blockMarkTask, MailLinks: []string{midLink("blockmark@example.org")}})
	if err != nil {
		t.Fatal(err)
	}
	cal := localCalendar(t, a)
	make := func(req TimeBlockRequest, from, to string) int64 {
		t.Helper()
		req.Start, req.End = blockTimes("2030-01-07", from, to)
		req.CalendarID = cal
		b, err := a.createBlock(req)
		if err != nil {
			t.Fatal(err)
		}
		return b.ID
	}
	f.mailBlock = make(TimeBlockRequest{MessageID: msg, Title: blockMarkSubject}, "09:00", "09:30")
	f.ownBlock = make(TimeBlockRequest{MessageID: msg, Title: "Call Ada back"}, "10:00", "10:30")
	f.taskBlock = make(TimeBlockRequest{TaskUID: task.Task.UID}, "11:00", "11:30")
	if got := f.summaries(t); got[0] != blockMarkSubject || got[1] != "Call Ada back" || got[2] != blockMarkTask {
		t.Fatalf("events before the vault = %v", got)
	}
	if !scanDir(t, dir, strings.ToLower(blockMarkSubject)) {
		t.Fatal("the control found nothing: the scan cannot see the mail's subject")
	}
	return f
}

// summaries are the titles of the busy events, by start.
func (f *blockVaultFixture) summaries(t *testing.T) []string {
	t.Helper()
	return summaries(occurrences(t, f.app, "2030-01-06T00:00:00Z", "2030-01-09T00:00:00Z"))
}

// sealAndLock puts members in a new vault, waits for the sealing and locks it.
func (f *blockVaultFixture) sealAndLock(t *testing.T, members []MailVaultMemberDTO) {
	t.Helper()
	created, err := f.app.CreateMailVault(MailVaultCreateRequest{Passphrase: "correct horse battery", Members: members})
	if err != nil {
		t.Fatal(err)
	}
	waitForVaultState(t, f.app, created.ID, storage.MailVaultReady)
	f.app.LockAllMailVaults()
	if !waitForBackgroundWork(backgroundWorkGrace) {
		t.Fatal("the lock's work did not finish")
	}
}

func (f *blockVaultFixture) check(t *testing.T) {
	t.Helper()
	a := f.app
	for _, id := range []int64{f.mailBlock, f.taskBlock} {
		row, err := a.store.GetTimeBlock(a.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if row.Title != "" || row.TitleOwn {
			t.Errorf("block %d keeps a title taken from mail: %+v", id, row)
		}
	}
	own, _ := a.store.GetTimeBlock(a.ctx, f.ownBlock)
	if own.Title != "Call Ada back" || !own.TitleOwn {
		t.Errorf("the user's own title went: %+v", own)
	}
	if got := f.summaries(t); got[0] != "Focus time" || got[1] != "Call Ada back" || got[2] != "Focus time" {
		t.Errorf("events after the vault = %v", got)
	}
	for _, b := range listBlocks(t, a, "2030-01-07") {
		if !b.Masked || b.Title != "" {
			t.Errorf("block while locked = %+v", b)
		}
	}
	if scanDir(t, f.dir, strings.ToLower(blockMarkSubject)) {
		t.Error("the mail's subject is still on disk")
	}
	res, err := a.index.Search(search.Query{Text: blockMarkSubject, Limit: 10})
	if err != nil || res.Total != 0 {
		t.Errorf("the search index still finds the subject: %+v %v", res, err)
	}
}

// Mail moved into a vault takes its subject out of the blocks made for it,
// out of their busy events and off the disk; a title the user typed stays.
// A block for a task made from that mail is masked the same way.
func TestTimeBlocksForgetMailMovedIntoAVaultFolder(t *testing.T) {
	f := newBlockVaultFixture(t)
	f.sealAndLock(t, []MailVaultMemberDTO{{AccountID: f.account, FolderID: f.folder}})
	f.check(t)
}

func TestTimeBlocksForgetMailOfAVaultedAccount(t *testing.T) {
	f := newBlockVaultFixture(t)
	f.sealAndLock(t, []MailVaultMemberDTO{{AccountID: f.account}})
	f.check(t)
}

// A task made from vault mail lends its blocks no title once the task goes.
func TestReleasedBlocksOfVaultMailTaskTakeNoTitle(t *testing.T) {
	f := newBlockVaultFixture(t)
	f.sealAndLock(t, []MailVaultMemberDTO{{AccountID: f.account, FolderID: f.folder}})
	a := f.app
	row, _ := a.store.GetTimeBlock(a.ctx, f.taskBlock)
	if err := a.ReleaseTaskTimeBlocks(row.TaskUID, false); err != nil {
		t.Fatal(err)
	}
	if after, _ := a.store.GetTimeBlock(a.ctx, f.taskBlock); after.Title != "" || after.TaskUID != "" {
		t.Errorf("released block = %+v", after)
	}
}
