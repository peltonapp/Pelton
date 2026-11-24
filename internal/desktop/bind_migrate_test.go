package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/mailimport"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

const migrateMbox = "From wren@example.org Tue Jan  7 10:00:00 2020\r\n" +
	"Message-ID: <vaulted-import@example.org>\r\nFrom: Wren <wren@example.org>\r\nTo: juno@example.org\r\n" +
	"Date: Tue, 07 Jan 2020 10:00:00 +0000\r\nSubject: " + vaultMarkSubject + "\r\nX-Mozilla-Status: 0005\r\n\r\n" +
	vaultMarkBody + "\r\n\r\n"

func waitForImport(t *testing.T, a *App, id int64) *storage.ImportJob {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		a.guided.mu.Lock()
		running := a.guided.job != 0
		a.guided.mu.Unlock()
		job, err := a.store.GetImportJob(a.ctx, id)
		if err == nil && job.State != storage.ImportRunning && !running {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("import %d never finished", id)
	return nil
}

func scanAll(t *testing.T, a *App, kind string, paths []string) mailimport.Plan {
	t.Helper()
	inv, err := a.ScanImportSource(kind, paths)
	if err != nil {
		t.Fatal(err)
	}
	plan := mailimport.Plan{Kind: kind, Paths: paths}
	for _, f := range inv.Folders {
		plan.Folders = append(plan.Folders, f.Key)
	}
	return plan
}

// mail imported into a local folder that sits in a vault is sealed on the
// way in, and stays absent while the vault is locked.
func TestGuidedImportIntoVaultedLocalFolderIsSealed(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	local, err := a.store.EnsureLocalAccount(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := a.store.EnsureLocalFolderPath(a.ctx, local.ID, []string{"Imported", "Private"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := a.CreateMailVault(MailVaultCreateRequest{Name: "Imported", Passphrase: "another long phrase",
		Members: []MailVaultMemberDTO{{AccountID: local.ID, FolderID: folder.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	waitForVaultState(t, a, created.ID, storage.MailVaultReady)
	a.LockAllMailVaults()

	dir := t.TempDir()
	path := filepath.Join(dir, "Private.mbox")
	if err := os.WriteFile(path, []byte(migrateMbox), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := scanAll(t, a, mailimport.KindFiles, []string{path})
	id, err := a.StartImportJob(plan)
	if err != nil {
		t.Fatal(err)
	}
	job := waitForImport(t, a, id)
	if job.State != storage.ImportDone {
		t.Fatalf("job = %+v", job)
	}

	list, err := a.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: folder.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Messages) != 0 {
		t.Fatalf("a locked vault folder listed %d imported messages", len(list.Messages))
	}
	leak(t, "ListMessages", list)
	jobs, err := a.ImportJobs()
	if err != nil {
		t.Fatal(err)
	}
	leak(t, "ImportJobs", jobs)

	if res := a.UnlockMailVault(created.ID, "another long phrase"); !res.OK {
		t.Fatalf("unlock: %+v", res)
	}
	list, err = a.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: folder.ID, Limit: 50})
	if err != nil || len(list.Messages) != 1 || !strings.Contains(list.Messages[0].Subject, vaultMarkSubject) {
		t.Fatalf("unlocked folder = %+v %v", list, err)
	}

	again, err := a.StartImportJob(plan)
	if err != nil {
		t.Fatal(err)
	}
	waitForImport(t, a, again)
	list, _ = a.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: folder.ID, Limit: 50})
	if len(list.Messages) != 1 {
		t.Fatalf("a second import into the vault left %d messages", len(list.Messages))
	}
}

func TestGuidedImportUploadsToAccount(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	client := &fakeIMAP{folders: []pimap.Folder{{Name: "INBOX", Delimiter: '/'}, {Name: "Archive", Delimiter: '/'}}}
	useFake(t, a, f.account, client)

	dir := t.TempDir()
	path := filepath.Join(dir, "Old.mbox")
	msg := strings.ReplaceAll(migrateMbox, vaultMarkSubject, "plain subject")
	if err := os.WriteFile(path, []byte(msg), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := scanAll(t, a, mailimport.KindFiles, []string{path})
	est, err := a.EstimateImport(plan)
	if err != nil || est.Messages != 1 || est.Bytes != int64(len(msg)) {
		t.Fatalf("estimate = %+v %v", est, err)
	}
	plan.Destination = mailimport.Destination{Mode: mailimport.DestinationAccount, AccountID: f.account, Folder: "Archive"}
	id, err := a.StartImportJob(plan)
	if err != nil {
		t.Fatal(err)
	}
	job := waitForImport(t, a, id)
	if job.State != storage.ImportDone {
		t.Fatalf("job = %+v", job)
	}
	if len(client.created) != 1 || client.created[0] != "Archive/Old" {
		t.Fatalf("created = %v", client.created)
	}
	if len(client.appends) != 1 {
		t.Fatalf("appends = %d", len(client.appends))
	}
	got := client.appends[0]
	if got.mailbox != "Archive/Old" || got.date.Year() != 2020 {
		t.Fatalf("append = %s %v", got.mailbox, got.date)
	}
	if len(got.flags) != 2 || got.flags[0] != imap.FlagSeen || got.flags[1] != imap.FlagFlagged {
		t.Fatalf("flags = %v", got.flags)
	}

	if _, err := a.StartImportJob(mailimport.Plan{Kind: mailimport.KindFiles, Paths: []string{path},
		Destination: mailimport.Destination{Mode: mailimport.DestinationAccount, AccountID: 999}}); err != errImportNoAccount {
		t.Fatalf("upload to a missing account = %v", err)
	}
}

// a job left running by a quit app shows as paused and resumes to the end.
func TestGuidedImportResumesAfterRestart(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	dir := t.TempDir()
	path := filepath.Join(dir, "Box.mbox")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(migrateMbox, vaultMarkSubject, "s")), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := scanAll(t, a, mailimport.KindFiles, []string{path})
	plan.Destination = mailimport.Destination{Mode: mailimport.DestinationLocal}
	data, _ := json.Marshal(plan)
	job := &storage.ImportJob{SourceKind: plan.Kind, SourcePath: path, Plan: string(data)}
	id, err := a.store.CreateImportJob(a.ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := a.ImportJobs()
	if err != nil || len(jobs) != 1 || jobs[0].State != storage.ImportPaused {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
	if err := a.ResumeImportJob(id); err != nil {
		t.Fatal(err)
	}
	done := waitForImport(t, a, id)
	if done.State != storage.ImportDone {
		t.Fatalf("resumed job = %+v", done)
	}
	if err := a.ResumeImportJob(id); err != errImportNotPaused {
		t.Fatalf("resuming a finished job = %v", err)
	}
	if err := a.CancelImportJob(id); err != nil {
		t.Fatal(err)
	}
	if j, _ := a.store.GetImportJob(a.ctx, id); j.State != storage.ImportDone {
		t.Fatalf("cancelling a finished job changed it to %s", j.State)
	}
}

// a Thunderbird filter becomes a block automation that is switched off and
// marked as imported.
func TestImportedFiltersStartSwitchedOff(t *testing.T) {
	a, _ := newScriptTestApp(t)
	filters := []mailimport.Filter{{
		Name: "News", Account: "wren@example.org", Enabled: true, Type: 17,
		Condition: "AND (subject,contains,newsletter)",
		Actions: []mailimport.FilterAction{
			{Type: "Move to folder", Value: "imap://wren%40example.org@imap.example.org/INBOX/News"},
			{Type: "Mark read"},
		},
	}}
	m := mailimport.MapFilter(filters[0], 0)
	if err := a.importAutomation(a.ctx, "Thunderbird: News", "from a test", m.Trigger, m.Tree); err != nil {
		t.Fatal(err)
	}
	scripts, err := a.ListScripts()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, sc := range scripts {
		if sc.Name != "Thunderbird: News" {
			continue
		}
		found = true
		if sc.Enabled || sc.Origin != string(scripting.OriginImport) || sc.Kind != string(scripting.KindBlocks) {
			t.Fatalf("imported automation = %+v", sc)
		}
		if !strings.Contains(sc.Blocks, "INBOX/News") {
			t.Fatalf("blocks = %s", sc.Blocks)
		}
	}
	if !found {
		t.Fatalf("no imported automation in %+v", scripts)
	}
}

func TestLocalAddressBookEditsStayLocal(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	book := storage.AddressBook{Name: "Imported"}
	id, err := a.store.CreateAddressBook(a.ctx, &book)
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.SaveContact(ContactRequest{BookID: id, FullName: "Ida Example", Emails: []ContactValueDTO{{Value: "ida@example.org"}}})
	if err != nil || res.Saved.ID == 0 {
		t.Fatalf("save = %+v %v", res, err)
	}
	books, err := a.ListAddressBooks()
	if err != nil || len(books) != 1 || !books[0].HasPassword || books[0].ContactCount != 1 {
		t.Fatalf("books = %+v %v", books, err)
	}
	if _, err := a.DeleteContact(res.Saved.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := a.syncAllAddressBooks(a.ctx); err != nil {
		t.Fatalf("sync tried a local book: %v", err)
	}
}
