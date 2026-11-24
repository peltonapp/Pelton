package mailimport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

func newJob(t *testing.T, db *storage.DB, plan Plan) int64 {
	t.Helper()
	data, _ := json.Marshal(plan)
	id, err := db.CreateImportJob(context.Background(), &storage.ImportJob{SourceKind: plan.Kind, SourcePath: plan.Paths[0], Plan: string(data)})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func allKeys(inv Inventory) Plan {
	var p Plan
	for _, f := range inv.Folders {
		p.Folders = append(p.Folders, f.Key)
	}
	for _, b := range inv.AddressBooks {
		p.AddressBooks = append(p.AddressBooks, b.Key)
	}
	for _, c := range inv.Calendars {
		p.Calendars = append(p.Calendars, c.Key)
	}
	for _, f := range inv.Filters {
		p.Filters = append(p.Filters, f.Key)
	}
	for _, a := range inv.Accounts {
		p.Accounts = append(p.Accounts, a.Email)
	}
	return p
}

func scanPlan(t *testing.T, kind string, paths ...string) (Inventory, Plan) {
	t.Helper()
	src, err := Open(kind, paths)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plan := allKeys(inv)
	plan.Kind, plan.Paths = kind, paths
	plan.Destination = Destination{Mode: DestinationLocal}
	return inv, plan
}

type recordingHooks struct {
	mu        sync.Mutex
	accounts  []Account
	calendars map[string]string
	scripts   []string
	batches   int
}

func (h *recordingHooks) hooks() Hooks {
	h.calendars = map[string]string{}
	return Hooks{
		CreateAccount: func(_ context.Context, acc Account) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.accounts = append(h.accounts, acc)
			return "", nil
		},
		AccountID: func(context.Context, string) int64 { return 3 },
		ImportCalendar: func(_ context.Context, name string, data []byte) (int, int, error) {
			h.calendars[name] = string(data)
			return strings.Count(string(data), "BEGIN:VEVENT") + strings.Count(string(data), "BEGIN:VTODO"), 0, nil
		},
		CreateAutomation: func(_ context.Context, name, _, trigger string, tree scripting.Tree) error {
			h.scripts = append(h.scripts, name+"|"+trigger)
			return nil
		},
		MailBatch: func() { h.batches++ },
	}
}

func runner(t *testing.T, db *storage.DB, h Hooks) *Runner {
	t.Helper()
	dest, err := NewLocalDestination(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{Store: db, Hooks: h, Destination: dest, BatchSize: 2}
}

func localMessages(t *testing.T, db *storage.DB, path string) []storage.Message {
	t.Helper()
	ctx := context.Background()
	acc, err := db.LocalAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := db.ListFolders(ctx, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range folders {
		if f.IMAPPath == path {
			msgs, err := db.ListMessages(ctx, f.ID, 1000)
			if err != nil {
				t.Fatal(err)
			}
			return msgs
		}
	}
	t.Fatalf("no local folder %q", path)
	return nil
}

func reportLine(r Report, kind, name string) ReportLine {
	for _, l := range r.Lines {
		if l.Kind == kind && l.Name == name {
			return l
		}
	}
	return ReportLine{}
}

func TestRunThunderbirdEndToEnd(t *testing.T) {
	db := newTestStore(t)
	dir := makeThunderbirdProfile(t)
	_, plan := scanPlan(t, KindThunderbird, dir)
	rec := &recordingHooks{}
	r := runner(t, db, rec.hooks())
	report, err := r.Run(context.Background(), newJob(t, db, plan))
	if err != nil {
		t.Fatal(err)
	}

	inbox := localMessages(t, db, "Imported/Local Folders/Inbox")
	if len(inbox) != 2 {
		t.Fatalf("inbox holds %d, want 2 (the expunged one stays out)", len(inbox))
	}
	byID := map[string]storage.Message{}
	for _, m := range inbox {
		byID[m.MessageID] = m
	}
	if !byID["tb-1@example.org"].Flags.Has(storage.FlagSeen) {
		t.Error("read state was lost")
	}
	if f := byID["tb-2@example.org"].Flags; f.Has(storage.FlagSeen) || !f.Has(storage.FlagFlagged) {
		t.Errorf("flagged unread came in as %v", f)
	}
	if got := localMessages(t, db, "Imported/Local Folders/Archive/2019"); len(got) != 1 {
		t.Fatalf("nested folder holds %d", len(got))
	}
	line := reportLine(report, LineFolder, "Local Folders/Inbox")
	if line.Imported != 2 || line.Reasons[ReasonDeletedInSource] != 1 {
		t.Fatalf("inbox report = %+v", line)
	}

	if len(rec.accounts) != 1 || rec.accounts[0].Email != "wren@example.org" {
		t.Fatalf("accounts created = %+v", rec.accounts)
	}
	if l := reportLine(report, LineAccount, "juno@example.net"); l.Reasons[ReasonPOP3] != 1 {
		t.Fatalf("pop3 account report = %+v", l)
	}
	if !strings.Contains(rec.calendars["Home"], "Choir practice") {
		t.Fatalf("calendar = %q", rec.calendars["Home"])
	}
	if len(rec.scripts) != 2 {
		t.Fatalf("automations = %v", rec.scripts)
	}
	if l := reportLine(report, LineFilter, "wren@example.org: Tag it"); l.Skipped != 1 || len(l.Details) == 0 {
		t.Fatalf("unmapped filter report = %+v", l)
	}
	if l := reportLine(report, LineFilter, "wren@example.org: Forward and flag"); l.Imported != 1 || l.Details[0] != "Forward: else@example.com" {
		t.Fatalf("partly mapped filter report = %+v", l)
	}
	books, _ := db.ListAddressBooks(context.Background())
	if len(books) != 1 || !books[0].Local() || books[0].Name != "Personal Address Book" {
		t.Fatalf("books = %+v", books)
	}
	contacts, _ := db.ListContacts(context.Background(), books[0].ID)
	if len(contacts) != 2 {
		t.Fatalf("%d contacts", len(contacts))
	}
	if rec.batches == 0 {
		t.Fatal("the search index was never fed")
	}

	// a second run of the same plan adds nothing anywhere.
	again, err := r.Run(context.Background(), newJob(t, db, plan))
	if err != nil {
		t.Fatal(err)
	}
	if got := localMessages(t, db, "Imported/Local Folders/Inbox"); len(got) != 2 {
		t.Fatalf("second run left %d in the inbox", len(got))
	}
	if l := reportLine(again, LineFolder, "Local Folders/Inbox"); l.Imported != 0 || l.Reasons[ReasonDuplicate] != 2 {
		t.Fatalf("second run inbox report = %+v", l)
	}
	if l := reportLine(again, LineContacts, "Personal Address Book"); l.Imported != 0 || l.Skipped != 2 {
		t.Fatalf("second run contacts = %+v", l)
	}
}

// duplicate Message-IDs with different content are two messages; the same
// message twice is one.
func TestRunDeduplicatesByContent(t *testing.T) {
	db := newTestStore(t)
	dir := t.TempDir()
	content := mboxEntry(fixtureMessage("dup@x", "first", "one text")) +
		mboxEntry(fixtureMessage("dup@x", "second", "another text")) +
		mboxEntry(fixtureMessage("dup@x", "first", "one text"))
	path := writeFile(t, dir, "Dups.mbox", content)
	_, plan := scanPlan(t, KindFiles, path)
	report, err := runner(t, db, Hooks{}).Run(context.Background(), newJob(t, db, plan))
	if err != nil {
		t.Fatal(err)
	}
	if got := localMessages(t, db, "Imported/Dups"); len(got) != 2 {
		t.Fatalf("%d messages, want 2", len(got))
	}
	if l := reportLine(report, LineFolder, "Dups"); l.Imported != 2 || l.Reasons[ReasonDuplicate] != 1 {
		t.Fatalf("report = %+v", l)
	}
}

// a run stopped part way and started again ends with every message once.
func TestRunResumesAfterInterruption(t *testing.T) {
	db := newTestStore(t)
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 9; i++ {
		b.WriteString(mboxEntry(fixtureMessage("m"+itoa(i)+"@x", "n"+itoa(i), "body "+itoa(i))))
	}
	path := writeFile(t, dir, "Big.mbox", b.String())
	_, plan := scanPlan(t, KindFiles, path)
	job := newJob(t, db, plan)

	ctx, cancel := context.WithCancel(context.Background())
	r := runner(t, db, Hooks{})
	r.OnProgress = func(p RunProgress) {
		if p.Done >= 5 {
			cancel()
		}
	}
	if _, err := r.Run(ctx, job); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted run = %v", err)
	}
	stored := len(localMessages(t, db, "Imported/Big"))
	if stored == 0 || stored >= 9 {
		t.Fatalf("interrupted run stored %d", stored)
	}
	positions, _ := db.ImportPositions(context.Background(), job)
	if positions["mail:"+plan.Folders[0]].Position == 0 {
		t.Fatal("no position was saved")
	}

	r.OnProgress = nil
	report, err := r.Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if got := localMessages(t, db, "Imported/Big"); len(got) != 9 {
		t.Fatalf("after resuming %d messages, want 9", len(got))
	}
	if l := reportLine(report, LineFolder, "Big"); l.Imported != 9 {
		t.Fatalf("report after resume = %+v", l)
	}
}

func TestRunKeepsAttachmentsAndBadEncodings(t *testing.T) {
	db := newTestStore(t)
	dir := t.TempDir()
	payload := make([]byte, 3<<20)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	var lines []string
	for len(encoded) > 76 {
		lines = append(lines, encoded[:76])
		encoded = encoded[76:]
	}
	lines = append(lines, encoded)
	withAttachment := "Message-ID: <att@x>\r\nFrom: wren@example.org\r\nSubject: big\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=b1\r\n\r\n--b1\r\nContent-Type: text/plain\r\n\r\nsee file\r\n" +
		"--b1\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"scan.bin\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" + strings.Join(lines, "\r\n") + "\r\n--b1--\r\n"
	latin := "Message-ID: <latin@x>\r\nFrom: =?iso-8859-1?q?J=F6rg?= <j@example.org>\r\nSubject: Gr\xfc\xdfe\r\n" +
		"Content-Type: text/plain; charset=bogus-charset\r\n\r\nSch\xf6ne Gr\xfc\xdfe\r\n"
	noDate := "Message-ID: <nodate@x>\r\nFrom: a@example.org\r\nSubject: no date\r\n\r\nbody\r\n"
	path := writeFile(t, dir, "Mixed.mbox", mboxEntry(withAttachment)+mboxEntry(latin)+mboxEntry(noDate))
	_, plan := scanPlan(t, KindFiles, path)
	if _, err := runner(t, db, Hooks{}).Run(context.Background(), newJob(t, db, plan)); err != nil {
		t.Fatal(err)
	}
	msgs := localMessages(t, db, "Imported/Mixed")
	if len(msgs) != 3 {
		t.Fatalf("%d messages", len(msgs))
	}
	for _, m := range msgs {
		if m.MessageID != "att@x" {
			continue
		}
		atts, err := db.ListAttachments(context.Background(), m.ID)
		if err != nil || len(atts) != 1 || atts[0].SizeBytes != int64(len(payload)) {
			t.Fatalf("attachment = %+v %v", atts, err)
		}
	}
}

type fakeUploader struct {
	mu        sync.Mutex
	mailboxes map[string]bool
	appended  []string
	flags     []storage.Flag
	dates     []time.Time
	failOn    string
}

func (f *fakeUploader) Delimiter(context.Context) (string, error) { return ".", nil }

func (f *fakeUploader) EnsureMailbox(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mailboxes == nil {
		f.mailboxes = map[string]bool{}
	}
	f.mailboxes[name] = true
	return nil
}

func (f *fakeUploader) Append(_ context.Context, mailbox string, raw []byte, flags storage.Flag, date time.Time) error {
	if f.failOn != "" && strings.Contains(string(raw), f.failOn) {
		return errors.New("NO [OVERQUOTA]")
	}
	f.appended = append(f.appended, mailbox)
	f.flags = append(f.flags, flags)
	f.dates = append(f.dates, date)
	return nil
}

func TestRunUploadsToAnAccount(t *testing.T) {
	db := newTestStore(t)
	dir := makeThunderbirdProfile(t)
	inv, plan := scanPlan(t, KindThunderbird, dir)
	plan.Folders = nil
	for _, f := range inv.Folders {
		if !f.ServerCopy {
			plan.Folders = append(plan.Folders, f.Key)
		}
	}
	plan.Accounts, plan.Filters, plan.AddressBooks, plan.Calendars = nil, nil, nil, nil
	up := &fakeUploader{failOn: "tb-4@example.org"}
	dest, err := NewUploadDestination(context.Background(), up, 4, "Archive.Old", &Limiter{Gap: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{Store: db, Destination: dest}
	report, err := r.Run(context.Background(), newJob(t, db, plan))
	if err != nil {
		t.Fatal(err)
	}
	if !up.mailboxes["Archive.Old.Local Folders.Archive.2019"] || !up.mailboxes["Archive.Old.Local Folders.Inbox"] {
		t.Fatalf("mailboxes = %v", up.mailboxes)
	}
	if len(up.appended) != 3 {
		t.Fatalf("appended %v", up.appended)
	}
	if up.dates[0].IsZero() || up.dates[0].Year() != 2020 {
		t.Fatalf("date = %v", up.dates[0])
	}
	if l := reportLine(report, LineFolder, "Local Folders/Archive/2019"); l.Failed != 1 || l.Reasons[ReasonUploadFailed] != 1 {
		t.Fatalf("failed upload report = %+v", l)
	}

	up.failOn = ""
	report, err = r.Run(context.Background(), newJob(t, db, plan))
	if err != nil {
		t.Fatal(err)
	}
	if len(up.appended) != 4 {
		t.Fatalf("second run appended %d in total, want only the one that failed", len(up.appended))
	}
	if l := reportLine(report, LineFolder, "Local Folders/Inbox"); l.Reasons[ReasonDuplicate] != 2 {
		t.Fatalf("second upload run = %+v", l)
	}
	if e := EstimatePlan(inv, plan); e.Messages != 5 || e.Bytes == 0 {
		t.Fatalf("estimate = %+v", e)
	}
}

func TestLimiterSpacesUploads(t *testing.T) {
	l := &Limiter{Rate: 1000}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.Wait(context.Background(), 50); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took < 90*time.Millisecond {
		t.Fatalf("three 50 byte sends at 1000 B/s took %v", took)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.next = time.Now().Add(time.Hour)
	if err := l.Wait(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait on a cancelled context = %v", err)
	}
}

func TestRunNeverChangesTheSource(t *testing.T) {
	for _, tc := range []struct {
		kind string
		root func(*testing.T) string
	}{
		{KindThunderbird, makeThunderbirdProfile},
		{KindAppleMail, makeAppleMail},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			root := tc.root(t)
			before := snap(t, root)
			db := newTestStore(t)
			_, plan := scanPlan(t, tc.kind, root)
			rec := &recordingHooks{}
			if _, err := runner(t, db, rec.hooks()).Run(context.Background(), newJob(t, db, plan)); err != nil {
				t.Fatal(err)
			}
			assertUnchanged(t, before, snap(t, root))
			if len(snap(t, root)) != len(before) {
				t.Fatal("the import created files in the source")
			}
		})
	}
}
