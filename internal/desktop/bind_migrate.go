package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/mailimport"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The guided import: the user picks a source, Pelton scans it and shows what
// it holds, the user picks what to bring over and where mail goes, and a job
// runs in the background, reporting on EventGuidedImport. A job can be
// paused, cancelled and resumed, also after a restart.
//
// Accounts come over as settings only. No password or token is read from the
// other program and no credential store is touched; the user enters the
// password the usual way afterwards. The only network use is the optional
// upload into the user's own account.

// guidedImportState is the job that is running, if any.
type guidedImportState struct {
	mu     sync.Mutex
	job    int64
	cancel context.CancelCauseFunc
}

var (
	errImportPaused    = errors.New("pelton: import paused")
	errImportCancelled = errors.New("pelton: import cancelled")
	errImportNotPaused = errors.New("pelton: this import is not paused")
	errImportNoAccount = errors.New("pelton: choose an account to upload into")
)

// uploadRate and uploadGap limit how fast an upload sends mail, well inside
// what mail servers accept from one client.
const (
	uploadRate = 4 << 20
	uploadGap  = 25 * time.Millisecond
)

// ImportAccountDTO is an account found in a source.
type ImportAccountDTO struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	IMAPHost    string `json:"imapHost"`
	IMAPPort    int    `json:"imapPort"`
	SMTPHost    string `json:"smtpHost"`
	SMTPPort    int    `json:"smtpPort"`
	// Kind is "imap" or "pop3"; Pelton adds imap accounts only.
	Kind string `json:"kind"`
	// SignInWithProvider marks an account that signed in through its
	// provider's web login there; here it needs an app password.
	SignInWithProvider bool `json:"signInWithProvider"`
	// Exists is set when an account with this address is already here.
	Exists bool `json:"exists"`
}

// ImportFolderDTO is a mail folder found in a source.
type ImportFolderDTO struct {
	Key        string   `json:"key"`
	Path       []string `json:"path"`
	Count      int      `json:"count"`
	SizeBytes  int64    `json:"sizeBytes"`
	ServerCopy bool     `json:"serverCopy"`
}

// ImportPartDTO is an address book or a calendar found in a source.
type ImportPartDTO struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ImportFilterDTO is a message filter found in a source, with what of it
// cannot become an automation.
type ImportFilterDTO struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Account    string   `json:"account"`
	Enabled    bool     `json:"enabled"`
	Mappable   bool     `json:"mappable"`
	Unmappable []string `json:"unmappable"`
}

// ImportInventoryDTO is what a scanned source holds.
type ImportInventoryDTO struct {
	Kind         string              `json:"kind"`
	Paths        []string            `json:"paths"`
	Accounts     []ImportAccountDTO  `json:"accounts"`
	Folders      []ImportFolderDTO   `json:"folders"`
	AddressBooks []ImportPartDTO     `json:"addressBooks"`
	Calendars    []ImportPartDTO     `json:"calendars"`
	Filters      []ImportFilterDTO   `json:"filters"`
	Notices      []mailimport.Notice `json:"notices"`
}

// ImportEstimateDTO is what a plan would bring over: messages and their size,
// which is about the space they need on a server.
type ImportEstimateDTO struct {
	Messages int   `json:"messages"`
	Bytes    int64 `json:"bytes"`
}

// ImportTargetDTO is an account mail can be uploaded into, with its folders.
type ImportTargetDTO struct {
	AccountID int64    `json:"accountId"`
	Email     string   `json:"email"`
	Folders   []string `json:"folders"`
}

// ImportJobDTO is one guided import and where it stands.
type ImportJobDTO struct {
	ID         int64                  `json:"id"`
	Kind       string                 `json:"kind"`
	SourcePath string                 `json:"sourcePath"`
	State      string                 `json:"state"`
	Plan       mailimport.Plan        `json:"plan"`
	Progress   mailimport.RunProgress `json:"progress"`
	Report     mailimport.Report      `json:"report"`
	Error      string                 `json:"error"`
	CreatedAt  string                 `json:"createdAt"`
	UpdatedAt  string                 `json:"updatedAt"`
}

// ChooseImportSource opens the picker that fits a source: a profile or
// store folder for Thunderbird and Apple Mail, mail files or a folder of
// them, or an Outlook data file. An empty result means it was cancelled.
// Apple Mail's folder is protected by macOS, and picking it here is what
// lets Pelton read it.
func (a *App) ChooseImportSource(kind string) ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	opts := openDialogOptions{ShowHidden: true}
	switch kind {
	case mailimport.KindThunderbird:
		opts.Title = "Choose a Thunderbird profile folder"
	case mailimport.KindAppleMail:
		opts.Title = "Choose the Mail folder in your Library"
		if runtime.GOOS == "darwin" && home != "" {
			opts.Directory = filepath.Join(home, "Library")
		}
	case "folder":
		opts.Title = "Choose a folder of mail"
	case mailimport.KindFiles:
		return a.openMultipleFilesDialog(openDialogOptions{
			Title: "Choose mail to import",
			Filters: []fileFilter{
				{DisplayName: "Mail, contacts and calendars", Pattern: "*.eml;*.mbox;*.mbx;*.vcf;*.ics"},
				{DisplayName: "All files", Pattern: "*"},
			},
		})
	case mailimport.KindPST:
		path, err := a.openFileDialog(openDialogOptions{
			Title:   "Choose an Outlook data file",
			Filters: []fileFilter{{DisplayName: "Outlook data file (*.pst, *.ost)", Pattern: "*.pst;*.ost"}},
		})
		if err != nil || path == "" {
			return nil, err
		}
		return []string{path}, nil
	default:
		return nil, mailimport.ErrUnknownKind
	}
	dir, err := a.openDirectoryDialog(opts)
	if err != nil || dir == "" {
		return nil, err
	}
	return []string{dir}, nil
}

// sourceKind maps the picker kinds onto the source kinds: a picked folder of
// mail is read the same way as picked files.
func sourceKind(kind string) string {
	if kind == "folder" {
		return mailimport.KindFiles
	}
	return kind
}

// ScanImportSource reads what a source holds without changing anything in
// it: accounts, mail folders with their message counts, address books,
// calendars and filters, and what Pelton cannot bring over.
func (a *App) ScanImportSource(kind string, paths []string) (ImportInventoryDTO, error) {
	if err := a.ready(); err != nil {
		return ImportInventoryDTO{}, err
	}
	kind = sourceKind(kind)
	src, err := mailimport.Open(kind, paths)
	if err != nil {
		return ImportInventoryDTO{}, err
	}
	inv, err := src.Scan(a.ctx)
	if err != nil {
		return ImportInventoryDTO{}, err
	}
	return a.inventoryDTO(inv, kind, paths)
}

func (a *App) inventoryDTO(inv mailimport.Inventory, kind string, paths []string) (ImportInventoryDTO, error) {
	existing, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return ImportInventoryDTO{}, err
	}
	have := map[string]bool{}
	for _, acc := range existing {
		have[strings.ToLower(acc.Email)] = true
	}
	out := ImportInventoryDTO{
		Kind: kind, Paths: paths, Notices: inv.Notices,
		Accounts: []ImportAccountDTO{}, Folders: []ImportFolderDTO{}, AddressBooks: []ImportPartDTO{},
		Calendars: []ImportPartDTO{}, Filters: []ImportFilterDTO{},
	}
	if out.Notices == nil {
		out.Notices = []mailimport.Notice{}
	}
	for _, acc := range inv.Accounts {
		out.Accounts = append(out.Accounts, ImportAccountDTO{
			Email: acc.Email, DisplayName: acc.DisplayName, IMAPHost: acc.IMAPHost, IMAPPort: acc.IMAPPort,
			SMTPHost: acc.SMTPHost, SMTPPort: acc.SMTPPort, Kind: acc.Kind,
			SignInWithProvider: acc.SignInWithProvider, Exists: have[strings.ToLower(acc.Email)],
		})
	}
	for _, f := range inv.Folders {
		out.Folders = append(out.Folders, ImportFolderDTO{Key: f.Key, Path: f.Path, Count: f.Count,
			SizeBytes: f.SizeBytes, ServerCopy: f.ServerCopy})
	}
	for _, b := range inv.AddressBooks {
		out.AddressBooks = append(out.AddressBooks, ImportPartDTO{Key: b.Key, Name: b.Name, Count: b.Count})
	}
	for _, c := range inv.Calendars {
		out.Calendars = append(out.Calendars, ImportPartDTO{Key: c.Key, Name: c.Name, Count: c.Count})
	}
	for _, f := range inv.Filters {
		m := mailimport.MapFilter(f, 0)
		dropped := m.Dropped
		if dropped == nil {
			dropped = []string{}
		}
		out.Filters = append(out.Filters, ImportFilterDTO{Key: f.Key, Name: f.Name, Account: f.Account,
			Enabled: f.Enabled, Mappable: !m.Unmappable, Unmappable: dropped})
	}
	return out, nil
}

// EstimateImport says how many messages a plan brings over and how much
// space they take, which is what an upload needs free on the server.
func (a *App) EstimateImport(plan mailimport.Plan) (ImportEstimateDTO, error) {
	if err := a.ready(); err != nil {
		return ImportEstimateDTO{}, err
	}
	src, err := mailimport.Open(plan.Kind, plan.Paths)
	if err != nil {
		return ImportEstimateDTO{}, err
	}
	inv, err := src.Scan(a.ctx)
	if err != nil {
		return ImportEstimateDTO{}, err
	}
	e := mailimport.EstimatePlan(inv, plan)
	return ImportEstimateDTO{Messages: e.Messages, Bytes: e.Bytes}, nil
}

// ImportTargets lists the accounts mail can be uploaded into, with their
// folders by server path.
func (a *App) ImportTargets() ([]ImportTargetDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	out := []ImportTargetDTO{}
	for _, acc := range accounts {
		if acc.Local {
			continue
		}
		folders, err := a.store.ListFolders(a.ctx, acc.ID)
		if err != nil {
			return nil, err
		}
		t := ImportTargetDTO{AccountID: acc.ID, Email: acc.Email, Folders: []string{}}
		for _, f := range folders {
			t.Folders = append(t.Folders, f.IMAPPath)
		}
		out = append(out, t)
	}
	return out, nil
}

// StartImportJob stores a plan as a new job and starts it in the background.
// It returns the job's id; progress and the end arrive on EventGuidedImport.
func (a *App) StartImportJob(plan mailimport.Plan) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	plan.Kind = sourceKind(plan.Kind)
	if _, err := mailimport.Open(plan.Kind, plan.Paths); err != nil {
		return 0, err
	}
	if plan.Destination.Mode == mailimport.DestinationAccount {
		acc, err := a.store.GetAccount(a.ctx, plan.Destination.AccountID)
		if err != nil || acc.Local {
			return 0, errImportNoAccount
		}
	} else {
		plan.Destination = mailimport.Destination{Mode: mailimport.DestinationLocal}
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return 0, err
	}
	if !a.claimImport() {
		return 0, errImportRunning
	}
	job := &storage.ImportJob{SourceKind: plan.Kind, SourcePath: strings.Join(plan.Paths, "\n"), Plan: string(data)}
	id, err := a.store.CreateImportJob(a.ctx, job)
	if err != nil {
		a.releaseImport()
		return 0, err
	}
	a.launchImport(id)
	return id, nil
}

// ResumeImportJob carries on with a paused job from where it stopped.
func (a *App) ResumeImportJob(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.interruptStaleImports()
	job, err := a.store.GetImportJob(a.ctx, id)
	if err != nil {
		return err
	}
	if job.State != storage.ImportPaused && job.State != storage.ImportFailed {
		return errImportNotPaused
	}
	if !a.claimImport() {
		return errImportRunning
	}
	if err := a.store.SaveImportJobState(a.ctx, id, storage.ImportRunning, job.Progress, job.Report, ""); err != nil {
		a.releaseImport()
		return err
	}
	a.launchImport(id)
	return nil
}

// PauseImportJob stops a running job so it can be resumed later.
func (a *App) PauseImportJob(id int64) error {
	return a.stopImport(id, errImportPaused)
}

// CancelImportJob stops a job for good. What it already imported stays.
func (a *App) CancelImportJob(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.stopImport(id, errImportCancelled) == nil {
		return nil
	}
	job, err := a.store.GetImportJob(a.ctx, id)
	if err != nil {
		return err
	}
	if job.State != storage.ImportPaused && job.State != storage.ImportFailed {
		return nil
	}
	if err := a.store.SaveImportJobState(a.ctx, id, storage.ImportCancelled, job.Progress, job.Report, job.Error); err != nil {
		return err
	}
	if dto, err := toImportJobDTO(*job); err == nil {
		dto.State = storage.ImportCancelled
		a.emit(EventGuidedImport, dto)
	}
	return nil
}

func (a *App) stopImport(id int64, cause error) error {
	a.guided.mu.Lock()
	defer a.guided.mu.Unlock()
	if a.guided.job != id || a.guided.cancel == nil {
		return errImportNotPaused
	}
	a.guided.cancel(cause)
	return nil
}

// ImportJobs returns the most recent guided imports, newest first. A job
// that was running when the app last quit shows as paused.
func (a *App) ImportJobs() ([]ImportJobDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.interruptStaleImports()
	jobs, err := a.store.ListImportJobs(a.ctx, 10)
	if err != nil {
		return nil, err
	}
	out := make([]ImportJobDTO, 0, len(jobs))
	for _, j := range jobs {
		dto, err := toImportJobDTO(j)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// interruptStaleImports marks jobs left running by an earlier run of the app
// as paused, unless one is running now.
func (a *App) interruptStaleImports() {
	a.guided.mu.Lock()
	defer a.guided.mu.Unlock()
	if a.guided.job != 0 {
		return
	}
	if _, err := a.store.InterruptImportJobs(a.ctx); err != nil {
		a.log.Warn("mark interrupted imports", "err", err)
	}
}

func toImportJobDTO(j storage.ImportJob) (ImportJobDTO, error) {
	dto := ImportJobDTO{ID: j.ID, Kind: j.SourceKind, SourcePath: j.SourcePath, State: j.State, Error: j.Error,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt}
	if err := json.Unmarshal([]byte(j.Plan), &dto.Plan); err != nil {
		return dto, fmt.Errorf("pelton: read import plan: %w", err)
	}
	_ = json.Unmarshal([]byte(j.Progress), &dto.Progress)
	_ = json.Unmarshal([]byte(j.Report), &dto.Report)
	if dto.Report.Lines == nil {
		dto.Report.Lines = []mailimport.ReportLine{}
	}
	return dto, nil
}

// claimImport takes the one import slot, which the file import of the
// Classic dialog shares: two imports at once would hand out the same local
// uids.
func (a *App) claimImport() bool {
	if !importMu.TryLock() {
		return false
	}
	a.guided.mu.Lock()
	defer a.guided.mu.Unlock()
	if a.guided.job != 0 {
		importMu.Unlock()
		return false
	}
	a.guided.job = -1
	return true
}

func (a *App) releaseImport() {
	a.guided.mu.Lock()
	a.guided.job, a.guided.cancel = 0, nil
	a.guided.mu.Unlock()
	importMu.Unlock()
}

// launchImport runs a claimed job on its own goroutine.
func (a *App) launchImport(id int64) {
	ctx, cancel := context.WithCancelCause(a.ctx)
	a.guided.mu.Lock()
	a.guided.job, a.guided.cancel = id, cancel
	a.guided.mu.Unlock()
	go func() {
		defer a.releaseImport()
		defer cancel(nil)
		a.runImportJob(ctx, id)
	}()
}

func (a *App) runImportJob(ctx context.Context, id int64) {
	state, failure := storage.ImportDone, ""
	report, err := a.executeImportJob(ctx, id)
	switch cause := context.Cause(ctx); {
	case err == nil:
	case errors.Is(cause, errImportPaused):
		state = storage.ImportPaused
	case errors.Is(cause, errImportCancelled):
		state = storage.ImportCancelled
	default:
		state, failure = storage.ImportFailed, err.Error()
		a.log.Error("guided import", "job", id, "err", err)
	}
	bg := context.WithoutCancel(a.ctx)
	job, getErr := a.store.GetImportJob(bg, id)
	if getErr != nil {
		a.log.Error("guided import", "job", id, "err", getErr)
		return
	}
	reportJSON := job.Report
	if len(report.Lines) > 0 {
		if b, err := json.Marshal(report); err == nil {
			reportJSON = string(b)
		}
	}
	if err := a.store.SaveImportJobState(bg, id, state, job.Progress, reportJSON, failure); err != nil {
		a.log.Error("guided import", "job", id, "err", err)
	}
	if job, err = a.store.GetImportJob(bg, id); err == nil {
		if dto, err := toImportJobDTO(*job); err == nil {
			a.emit(EventGuidedImport, dto)
		}
	}
	go a.refreshViewCounts()
}

func (a *App) executeImportJob(ctx context.Context, id int64) (mailimport.Report, error) {
	job, err := a.store.GetImportJob(ctx, id)
	if err != nil {
		return mailimport.Report{}, err
	}
	var plan mailimport.Plan
	if err := json.Unmarshal([]byte(job.Plan), &plan); err != nil {
		return mailimport.Report{}, err
	}
	var dest mailimport.MailDestination
	if plan.Destination.Mode == mailimport.DestinationAccount && len(plan.Folders) > 0 {
		up, closeUp, err := a.openUploader(plan.Destination.AccountID)
		if err != nil {
			return mailimport.Report{}, err
		}
		defer closeUp()
		dest, err = mailimport.NewUploadDestination(ctx, up, plan.Destination.AccountID, plan.Destination.Folder,
			&mailimport.Limiter{Rate: uploadRate, Gap: uploadGap})
		if err != nil {
			return mailimport.Report{}, err
		}
	} else {
		local, err := mailimport.NewLocalDestination(ctx, a.store)
		if err != nil {
			return mailimport.Report{}, err
		}
		dest = local
	}

	dto, _ := toImportJobDTO(*job)
	var lastEmit time.Time
	r := &mailimport.Runner{
		Store: a.store, Log: a.log, Destination: dest, Hooks: a.importHooks(),
		OnProgress: func(p mailimport.RunProgress) {
			if time.Since(lastEmit) < 250*time.Millisecond {
				return
			}
			lastEmit = time.Now()
			dto.State, dto.Progress = storage.ImportRunning, p
			a.emit(EventGuidedImport, dto)
		},
	}
	return r.Run(ctx, id)
}

// importHooks connects a run to the rest of the app.
func (a *App) importHooks() mailimport.Hooks {
	return mailimport.Hooks{
		CreateAccount:    a.importAccount,
		AccountID:        a.accountIDByEmail,
		ImportCalendar:   a.importCalendarNamed,
		CreateAutomation: a.importAutomation,
		MailBatch: func() {
			if err := a.indexNewMessages(); err != nil {
				a.log.Warn("index imported mail", "err", err)
			}
		},
	}
}

// importAccount adds an account from imported settings and starts it the way
// the wizard does, so it asks for its password and then syncs.
func (a *App) importAccount(ctx context.Context, acc mailimport.Account) (string, error) {
	if a.accountIDByEmail(ctx, acc.Email) != 0 {
		return mailimport.ReasonAccountExists, nil
	}
	account := storage.Account{
		Email: acc.Email, DisplayName: acc.DisplayName, Username: acc.Username,
		IMAPHost: acc.IMAPHost, IMAPPort: acc.IMAPPort, SMTPHost: acc.SMTPHost, SMTPPort: acc.SMTPPort,
	}
	id, err := a.store.CreateAccount(ctx, &account)
	if err != nil {
		return "", err
	}
	if err := a.startImportedAccount(id); err != nil {
		a.log.Error("start imported mailbox", "account", id, "err", err)
	}
	return "", nil
}

func (a *App) accountIDByEmail(ctx context.Context, email string) int64 {
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return 0
	}
	for _, acc := range accounts {
		if !acc.Local && strings.EqualFold(acc.Email, email) {
			return acc.ID
		}
	}
	return 0
}

// importCalendarNamed imports into the local calendar of this name, made on
// first use. Events and tasks with a uid the calendar has are updated, so a
// second import adds nothing.
func (a *App) importCalendarNamed(ctx context.Context, name string, data []byte) (int, int, error) {
	cals, err := a.store.ListCalendars(ctx)
	if err != nil {
		return 0, 0, err
	}
	var id int64
	for _, c := range cals {
		if c.Local() && c.Name == name {
			id = c.ID
		}
	}
	if id == 0 {
		cal := storage.Calendar{Name: name, Visible: true, Components: storage.KindEvent + "," + storage.KindTask}
		if id, err = a.store.CreateCalendar(ctx, &cal); err != nil {
			return 0, 0, err
		}
	}
	res, err := a.importICS(ctx, id, data)
	if err != nil {
		return 0, 0, err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{id}})
	return res.Imported + res.Tasks, res.Skipped, nil
}

// importAutomation stores a filter's block script, switched off and marked
// as imported, for the user to look over before turning it on.
func (a *App) importAutomation(ctx context.Context, name, description, trigger string, tree scripting.Tree) error {
	svc, err := a.scriptService()
	if err != nil {
		return err
	}
	compiled := scripting.Compile(svc.Registry(), tree, trigger, false)
	if len(compiled.Errors) > 0 {
		return compiled.Errors[0]
	}
	perms := compiled.Permissions
	if t, ok := svc.Registry().Trigger(trigger); ok && t.Permission != "" && !slices.Contains(perms, t.Permission) {
		perms = append(perms, t.Permission)
	}
	sc := &scripting.Script{
		Name: name, Description: description, Kind: scripting.KindBlocks, Blocks: &tree,
		Trigger: scripting.TriggerConfig{Type: trigger}, Permissions: perms,
		Origin: scripting.OriginImport,
	}
	_, err = svc.Create(ctx, sc)
	return err
}

// imapUploader appends to one account over its own connection, so a long
// upload never holds up sync.
type imapUploader struct {
	client mailClient
}

func (a *App) openUploader(accountID int64) (*imapUploader, func(), error) {
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return nil, nil, err
	}
	client, err := a.connectIMAP(cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Login(); err != nil {
		client.Close()
		return nil, nil, err
	}
	return &imapUploader{client: client}, func() {
		_ = client.Logout()
		_ = client.Close()
	}, nil
}

func (u *imapUploader) Delimiter(context.Context) (string, error) {
	folders, err := u.client.ListFolders()
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		if f.Delimiter != 0 {
			return string(f.Delimiter), nil
		}
	}
	return "/", nil
}

func (u *imapUploader) EnsureMailbox(_ context.Context, name string) error {
	folders, err := u.client.ListFolders()
	if err != nil {
		return err
	}
	for _, f := range folders {
		if f.Name == name {
			return nil
		}
	}
	return u.client.CreateFolder(name)
}

func (u *imapUploader) Append(_ context.Context, mailbox string, raw []byte, flags storage.Flag, date time.Time) error {
	var out []imap.Flag
	if flags.Has(storage.FlagSeen) {
		out = append(out, imap.FlagSeen)
	}
	if flags.Has(storage.FlagFlagged) {
		out = append(out, imap.FlagFlagged)
	}
	if flags.Has(storage.FlagAnswered) {
		out = append(out, imap.FlagAnswered)
	}
	if flags.Has(storage.FlagForwarded) {
		out = append(out, imap.Flag("$Forwarded"))
	}
	return u.client.AppendMessage(mailbox, raw, out, date)
}
