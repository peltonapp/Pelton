package mailimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Plan is what the user picked in the guided import: the source, which of
// its parts to bring over, and where mail goes. Parts are named by the keys
// a scan gave them; accounts by address.
type Plan struct {
	Kind         string      `json:"kind"`
	Paths        []string    `json:"paths"`
	Accounts     []string    `json:"accounts"`
	Folders      []string    `json:"folders"`
	AddressBooks []string    `json:"addressBooks"`
	Calendars    []string    `json:"calendars"`
	Filters      []string    `json:"filters"`
	Destination  Destination `json:"destination"`
}

// The destination modes.
const (
	// DestinationLocal keeps mail on this computer, in an Imported folder of
	// Local Folders with the source's folder tree below it.
	DestinationLocal = "local"
	// DestinationAccount uploads mail into a folder of one of the user's
	// accounts.
	DestinationAccount = "account"
)

// Destination is where a plan's mail goes.
type Destination struct {
	Mode      string `json:"mode"`
	AccountID int64  `json:"accountId"`
	// Folder is the account folder the source's tree goes under, by its
	// server path; empty puts it at the top of the account.
	Folder string `json:"folder"`
}

// RunProgress is where a running guided import is.
type RunProgress struct {
	Phase    string `json:"phase"`
	Current  string `json:"current"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
}

// The phases of a run, in the order they happen.
const (
	PhaseAccounts  = "accounts"
	PhaseFilters   = "filters"
	PhaseContacts  = "contacts"
	PhaseCalendars = "calendars"
	PhaseMail      = "mail"
)

// Report says what a run imported, skipped and why. It names folders,
// address books, calendars, filters and accounts, never message content.
type Report struct {
	Lines []ReportLine `json:"lines"`
}

// ReportLine is one part of the source.
type ReportLine struct {
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Imported int            `json:"imported"`
	Skipped  int            `json:"skipped"`
	Failed   int            `json:"failed"`
	Reasons  map[string]int `json:"reasons,omitempty"`
	// Details lists what could not be brought over in the source's own words,
	// such as the parts of a filter with no Pelton counterpart.
	Details []string `json:"details,omitempty"`
}

func (r *Report) line(kind, name string) *ReportLine {
	for i := range r.Lines {
		if r.Lines[i].Kind == kind && r.Lines[i].Name == name {
			return &r.Lines[i]
		}
	}
	r.Lines = append(r.Lines, ReportLine{Kind: kind, Name: name})
	return &r.Lines[len(r.Lines)-1]
}

func (l *ReportLine) skip(reason string) {
	l.Skipped++
	l.reason(reason)
}

func (l *ReportLine) reason(reason string) {
	if l.Reasons == nil {
		l.Reasons = map[string]int{}
	}
	l.Reasons[reason]++
}

func (l *ReportLine) detail(text string) {
	if !slices.Contains(l.Details, text) {
		l.Details = append(l.Details, text)
	}
}

// The report line kinds.
const (
	LineAccount  = "account"
	LineFolder   = "folder"
	LineContacts = "contacts"
	LineCalendar = "calendar"
	LineFilter   = "filter"
	LineNotice   = "notice"
)

// Account outcomes a Hooks.CreateAccount returns besides creating one.
const (
	ReasonAccountExists = "accountExists"
	ReasonPOP3          = "pop3"
	ReasonUnmappable    = "unmappable"
	ReasonNoChange      = "unchanged"
)

// Hooks are the steps of an import that belong to the app rather than to
// this package: creating accounts and starting their sync, importing a
// calendar, storing an automation and feeding the search index.
type Hooks struct {
	// CreateAccount adds an account from its settings and returns "" or the
	// reason it was not added.
	CreateAccount func(ctx context.Context, acc Account) (string, error)
	// AccountID returns the Pelton account with this address, or 0.
	AccountID func(ctx context.Context, email string) int64
	// ImportCalendar imports iCalendar text into a local calendar of this
	// name and returns how many items it took and how many it left out.
	ImportCalendar func(ctx context.Context, name string, data []byte) (imported, skipped int, err error)
	// CreateAutomation stores a block script switched off.
	CreateAutomation func(ctx context.Context, name, description, trigger string, tree scripting.Tree) error
	// MailBatch runs after each batch of messages is stored.
	MailBatch func()
}

// MailDestination is where a run's mail goes.
type MailDestination interface {
	// Folder returns the destination folder for a folder of the source at
	// path, creating it when it is missing.
	Folder(ctx context.Context, path []string) (DestFolder, error)
	// Deliver stores one message and returns its local row, 0 when it went
	// to a server.
	Deliver(ctx context.Context, f *DestFolder, item Item) (int64, error)
}

// DestFolder is one folder a run delivers into.
type DestFolder struct {
	// Key names the folder in the delivery ledger.
	Key     string
	ID      int64
	Mailbox string
	nextUID uint32
}

// Runner carries out one import job.
type Runner struct {
	Store       *storage.DB
	Log         *slog.Logger
	Hooks       Hooks
	Destination MailDestination
	// OnProgress is told where the run is; it must not block.
	OnProgress func(RunProgress)
	// BatchSize is how many messages are stored between saved positions.
	BatchSize int
}

// batchInterval is the longest a run goes without saving its position.
const batchInterval = 2 * time.Second

type runState struct {
	job      *storage.ImportJob
	plan     Plan
	report   Report
	progress RunProgress
	parts    map[string]storage.ImportPosition
	saved    time.Time
}

// Run carries out a job from where it last stopped. It returns the report so
// far and ctx's error when it is stopped; the caller records whether that was
// a pause or a cancel. The job's progress and report are saved as it goes.
func (r *Runner) Run(ctx context.Context, jobID int64) (Report, error) {
	if r.Log == nil {
		r.Log = slog.New(slog.DiscardHandler)
	}
	if r.BatchSize <= 0 {
		r.BatchSize = 200
	}
	job, err := r.Store.GetImportJob(ctx, jobID)
	if err != nil {
		return Report{}, err
	}
	st := &runState{job: job}
	if err := json.Unmarshal([]byte(job.Plan), &st.plan); err != nil {
		return Report{}, fmt.Errorf("mailimport: read plan: %w", err)
	}
	_ = json.Unmarshal([]byte(job.Report), &st.report)
	_ = json.Unmarshal([]byte(job.Progress), &st.progress)
	if st.parts, err = r.Store.ImportPositions(ctx, jobID); err != nil {
		return st.report, err
	}
	src, err := Open(st.plan.Kind, st.plan.Paths)
	if err != nil {
		return st.report, err
	}
	inv, err := src.Scan(ctx)
	if err != nil {
		return st.report, err
	}
	folders := pick(inv.Folders, st.plan.Folders, func(f MailFolder) string { return f.Key })
	st.progress.Total = 0
	for _, f := range folders {
		st.progress.Total += f.Count
	}

	steps := []func(context.Context, *runState, MailSource, Inventory) error{
		r.accounts, r.filters, r.contacts, r.calendars,
	}
	for _, step := range steps {
		if err := step(ctx, st, src, inv); err != nil {
			r.save(ctx, st, true)
			return st.report, err
		}
	}
	for _, f := range folders {
		if err := r.folder(ctx, st, src, f); err != nil {
			r.save(ctx, st, true)
			return st.report, err
		}
	}
	r.save(ctx, st, true)
	return st.report, nil
}

func pick[T any](all []T, keys []string, key func(T) string) []T {
	var out []T
	for _, item := range all {
		if slices.Contains(keys, key(item)) {
			out = append(out, item)
		}
	}
	return out
}

func (r *Runner) emit(st *runState) {
	st.progress.Done = min(st.progress.Done, st.progress.Total)
	if r.OnProgress != nil {
		r.OnProgress(st.progress)
	}
}

// save writes the progress and report, at most every batchInterval unless
// forced.
func (r *Runner) save(ctx context.Context, st *runState, force bool) {
	if !force && time.Since(st.saved) < batchInterval {
		return
	}
	st.saved = time.Now()
	progress, _ := json.Marshal(st.progress)
	report, _ := json.Marshal(st.report)
	ctx = context.WithoutCancel(ctx)
	if err := r.Store.SaveImportJobState(ctx, st.job.ID, storage.ImportRunning, string(progress), string(report), ""); err != nil {
		r.Log.Warn("save import progress", "job", st.job.ID, "err", err)
	}
}

// part reports whether a part finished in an earlier run.
func (r *Runner) part(_ context.Context, st *runState, key string) bool {
	return st.parts[key].Done
}

// finish marks a part done.
func (r *Runner) finish(ctx context.Context, st *runState, key string, position int64) error {
	p := storage.ImportPosition{Position: position, Done: true}
	st.parts[key] = p
	return r.Store.SetImportPosition(context.WithoutCancel(ctx), st.job.ID, key, p)
}

func (r *Runner) accounts(ctx context.Context, st *runState, _ MailSource, inv Inventory) error {
	const key = "accounts"
	if r.part(ctx, st, key) || len(st.plan.Accounts) == 0 || r.Hooks.CreateAccount == nil {
		return nil
	}
	st.progress.Phase = PhaseAccounts
	r.emit(st)
	for _, acc := range inv.Accounts {
		if !slices.ContainsFunc(st.plan.Accounts, func(e string) bool { return strings.EqualFold(e, acc.Email) }) {
			continue
		}
		line := st.report.line(LineAccount, acc.Email)
		if acc.Kind != "imap" {
			line.skip(ReasonPOP3)
			continue
		}
		reason, err := r.Hooks.CreateAccount(ctx, acc)
		switch {
		case err != nil:
			line.Failed++
			line.detail(err.Error())
		case reason != "":
			line.skip(reason)
		default:
			line.Imported++
			if acc.SignInWithProvider {
				line.reason(NoticeOAuthAccount)
			}
		}
	}
	return r.finish(ctx, st, key, 0)
}

func (r *Runner) filters(ctx context.Context, st *runState, _ MailSource, inv Inventory) error {
	const key = "filters"
	if r.part(ctx, st, key) || len(st.plan.Filters) == 0 || r.Hooks.CreateAutomation == nil {
		return nil
	}
	st.progress.Phase = PhaseFilters
	r.emit(st)
	for _, f := range pick(inv.Filters, st.plan.Filters, func(f Filter) string { return f.Key }) {
		line := st.report.line(LineFilter, f.Account+": "+f.Name)
		var accountID int64
		if r.Hooks.AccountID != nil && f.Account != LocalFoldersName {
			accountID = r.Hooks.AccountID(ctx, f.Account)
		}
		m := MapFilter(f, accountID)
		for _, d := range m.Dropped {
			line.detail(d)
		}
		if m.Unmappable {
			line.skip(ReasonUnmappable)
			continue
		}
		name := "Thunderbird: " + f.Name
		description := "Imported from the Thunderbird filter of " + f.Account + "."
		if err := r.Hooks.CreateAutomation(ctx, name, description, m.Trigger, m.Tree); err != nil {
			line.Failed++
			line.detail(err.Error())
			continue
		}
		line.Imported++
	}
	return r.finish(ctx, st, key, 0)
}

func (r *Runner) contacts(ctx context.Context, st *runState, src MailSource, inv Inventory) error {
	books := pick(inv.AddressBooks, st.plan.AddressBooks, func(b ContactSource) string { return b.Key })
	for _, book := range books {
		key := "contacts:" + book.Key
		if r.part(ctx, st, key) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		st.progress.Phase, st.progress.Current = PhaseContacts, book.Name
		r.emit(st)
		line := st.report.line(LineContacts, book.Name)
		cards, err := src.Contacts(book)
		if err != nil {
			line.Failed++
			line.detail(err.Error())
		} else if err := r.storeCards(ctx, book.Name, cards, line); err != nil {
			return err
		}
		if err := r.finish(ctx, st, key, 0); err != nil {
			return err
		}
	}
	return nil
}

// storeCards puts cards into the local address book of this name, creating
// it on first use. A card already there, by its uid, is left as it is.
func (r *Runner) storeCards(ctx context.Context, name string, cards []string, line *ReportLine) error {
	books, err := r.Store.ListAddressBooks(ctx)
	if err != nil {
		return err
	}
	var bookID int64
	for _, b := range books {
		if b.Local() && b.Name == name {
			bookID = b.ID
		}
	}
	if bookID == 0 {
		book := storage.AddressBook{Name: name}
		if bookID, err = r.Store.CreateAddressBook(ctx, &book); err != nil {
			return err
		}
	}
	existing, err := r.Store.ListContacts(ctx, bookID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, c := range existing {
		have[c.Path] = true
	}
	for _, raw := range cards {
		contact, err := pcarddav.ParseCard(raw)
		if err != nil {
			line.skip(ReasonUnreadable)
			continue
		}
		uid := contact.UID
		if uid == "" {
			sum := sha256.Sum256([]byte(raw))
			uid = "import-" + hex.EncodeToString(sum[:12])
		}
		path := "/" + uid + ".vcf"
		if have[path] {
			line.skip(ReasonDuplicate)
			continue
		}
		card, err := pcarddav.EncodeCard(contact.Card)
		if err != nil {
			line.skip(ReasonUnreadable)
			continue
		}
		stored := storage.Contact{
			BookID: bookID, Path: path, UID: uid, FullName: contact.FullName, Organization: contact.Organization,
			Title: contact.Title, Note: contact.Note, Card: card,
		}
		for _, e := range contact.Emails {
			stored.Emails = append(stored.Emails, storage.ContactValue{Value: e.Value, Label: e.Label})
		}
		for _, p := range contact.Phones {
			stored.Phones = append(stored.Phones, storage.ContactValue{Value: p.Value, Label: p.Label})
		}
		if err := r.Store.SaveContact(ctx, &stored); err != nil {
			return err
		}
		have[path] = true
		line.Imported++
	}
	return nil
}

func (r *Runner) calendars(ctx context.Context, st *runState, src MailSource, inv Inventory) error {
	if r.Hooks.ImportCalendar == nil {
		return nil
	}
	for _, cal := range pick(inv.Calendars, st.plan.Calendars, func(c CalendarSource) string { return c.Key }) {
		key := "calendar:" + cal.Key
		if r.part(ctx, st, key) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		st.progress.Phase, st.progress.Current = PhaseCalendars, cal.Name
		r.emit(st)
		line := st.report.line(LineCalendar, cal.Name)
		data, err := src.Calendar(cal)
		if err == nil {
			var imported, skipped int
			imported, skipped, err = r.Hooks.ImportCalendar(ctx, cal.Name, data)
			line.Imported += imported
			for range skipped {
				line.skip(ReasonUnreadable)
			}
		}
		if err != nil {
			line.Failed++
			line.detail(err.Error())
		}
		if err := r.finish(ctx, st, key, 0); err != nil {
			return err
		}
	}
	return nil
}

// folder imports one folder of mail from its saved position. Each message is
// checked against the delivery ledger first, so one delivered before an
// interruption, or by an earlier run, is skipped rather than doubled.
func (r *Runner) folder(ctx context.Context, st *runState, src MailSource, f MailFolder) error {
	key := "mail:" + f.Key
	pos := st.parts[key]
	name := strings.Join(f.Path, "/")
	if name == "" {
		name = importedFolderName
	}
	line := st.report.line(LineFolder, name)
	if pos.Done {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	st.progress.Phase, st.progress.Current = PhaseMail, name
	r.emit(st)
	dest, err := r.Destination.Folder(ctx, f.Path)
	if err != nil {
		return err
	}
	reader, err := src.OpenFolder(f, pos.Position)
	if err != nil {
		line.Failed++
		line.detail(err.Error())
		return r.finish(ctx, st, key, 0)
	}
	defer reader.Close()

	position := pos.Position
	sinceBatch := 0
	commit := func(force bool) error {
		if sinceBatch == 0 && !force {
			return nil
		}
		sinceBatch = 0
		p := storage.ImportPosition{Position: position}
		st.parts[key] = p
		if err := r.Store.SetImportPosition(context.WithoutCancel(ctx), st.job.ID, key, p); err != nil {
			return err
		}
		r.save(ctx, st, true)
		if r.Hooks.MailBatch != nil {
			r.Hooks.MailBatch()
		}
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = commit(false)
			return err
		}
		item, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line.Failed++
			line.detail(err.Error())
			break
		}
		r.deliver(ctx, st, &dest, item, line)
		if err := ctx.Err(); err != nil {
			_ = commit(false)
			return err
		}
		position = item.Next
		st.progress.Done++
		sinceBatch++
		r.emit(st)
		if sinceBatch >= r.BatchSize {
			if err := commit(false); err != nil {
				return err
			}
		}
	}
	if err := commit(false); err != nil {
		return err
	}
	return r.finish(ctx, st, key, position)
}

func (r *Runner) deliver(ctx context.Context, st *runState, dest *DestFolder, item Item, line *ReportLine) {
	if item.Skip != "" {
		line.skip(item.Skip)
		st.progress.Skipped++
		return
	}
	fp := Fingerprint(item.Raw)
	done, err := r.Store.ImportDelivered(ctx, dest.Key, fp)
	if err == nil && done {
		line.skip(ReasonDuplicate)
		st.progress.Skipped++
		return
	}
	id, err := r.Destination.Deliver(ctx, dest, item)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.Log.Warn("import message", "folder", line.Name, "err", err)
		line.Failed++
		var reason string
		if errors.Is(err, errUpload) {
			reason = ReasonUploadFailed
		} else {
			reason = ReasonUnreadable
		}
		line.reason(reason)
		st.progress.Failed++
		return
	}
	if err := r.Store.RecordImportDelivery(context.WithoutCancel(ctx), dest.Key, fp, id); err != nil {
		r.Log.Warn("record import delivery", "err", err)
	}
	if item.Note != "" {
		line.reason(item.Note)
	}
	line.Imported++
	st.progress.Imported++
}

// Estimate is what a plan would bring over, for the summary before it runs.
type Estimate struct {
	Messages int   `json:"messages"`
	Bytes    int64 `json:"bytes"`
}

// EstimatePlan adds up the selected folders' messages and their size on disk,
// which is close to the space they take on a server.
func EstimatePlan(inv Inventory, plan Plan) Estimate {
	var e Estimate
	for _, f := range pick(inv.Folders, plan.Folders, func(f MailFolder) string { return f.Key }) {
		e.Messages += f.Count
		e.Bytes += f.SizeBytes
	}
	return e
}
