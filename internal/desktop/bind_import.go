package desktop

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/peltonapp/Pelton/internal/mailimport"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Importing from another mail client (#196). Two separate things share this
// file because they share an entry point in the ui: account settings, which
// recreate mailboxes so the user does not retype six servers, and mail files,
// which land in the Local Folders account that sync never touches.
//
// Everything is user-initiated through a picker or an explicit button. The
// only directories read without being pointed at are Thunderbird's own profile
// locations, and only to answer "is there a profile here".

// importMu serializes mail-file imports. Two at once would hand out the same
// local uid twice, and the progress event has one running job's worth of state.
var importMu sync.Mutex

// errImportRunning is returned when an import is asked for while one is
// already going.
var errImportRunning = errors.New("pelton: an import is already running")

// importedFolderName is where single messages land when the user picks a batch
// of .eml files, which have no folder of their own to name.
const importedFolderName = "Imported"

// ThunderbirdAccountDTO is one account read out of a Thunderbird profile.
// Password is never read: Thunderbird keeps its own, and the user
// re-authenticates once after importing.
type ThunderbirdAccountDTO struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
	IMAPHost    string `json:"imapHost"`
	IMAPPort    int    `json:"imapPort"`
	SMTPHost    string `json:"smtpHost"`
	SMTPPort    int    `json:"smtpPort"`
	// Kind is "imap" or "pop3". Pelton speaks imap, so a pop3 account is listed
	// but cannot be imported; the ui says why instead of hiding it.
	Kind string `json:"kind"`
	// Exists is true when an account with this address is already configured,
	// so the ui can pre-deselect it rather than offering a duplicate.
	Exists bool `json:"exists"`
}

// ThunderbirdFolderDTO is one mail folder found on disk in a profile.
type ThunderbirdFolderDTO struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

// ThunderbirdProfileDTO is one discovered profile and what it holds.
type ThunderbirdProfileDTO struct {
	Name         string                  `json:"name"`
	Path         string                  `json:"path"`
	Accounts     []ThunderbirdAccountDTO `json:"accounts"`
	LocalFolders []ThunderbirdFolderDTO  `json:"localFolders"`
}

// FindThunderbirdProfiles looks for Thunderbird profiles in the locations it
// installs to on this platform and reads what each one holds. An empty result
// means Thunderbird is not installed, or keeps its profile somewhere else; the
// ui then offers to pick a profile directory by hand.
func (a *App) FindThunderbirdProfiles() ([]ThunderbirdProfileDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	profiles, err := mailimport.DiscoverThunderbird()
	if err != nil {
		return nil, err
	}
	return a.toProfileDTOs(profiles)
}

// ChooseThunderbirdProfile opens a directory picker and reads the chosen
// profile. An empty result means the dialog was cancelled.
func (a *App) ChooseThunderbirdProfile() ([]ThunderbirdProfileDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	dir, err := a.openDirectoryDialog(openDialogOptions{
		Title: "Choose a Thunderbird profile folder",
	})
	if err != nil || dir == "" {
		return nil, err
	}
	profile, err := mailimport.ReadProfile(dir)
	if err != nil {
		return nil, fmt.Errorf("pelton: %s does not look like a Thunderbird profile", filepath.Base(dir))
	}
	return a.toProfileDTOs([]mailimport.Profile{profile})
}

func (a *App) toProfileDTOs(profiles []mailimport.Profile) ([]ThunderbirdProfileDTO, error) {
	existing, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(existing))
	for _, acc := range existing {
		have[strings.ToLower(acc.Email)] = true
	}

	out := make([]ThunderbirdProfileDTO, 0, len(profiles))
	for _, p := range profiles {
		dto := ThunderbirdProfileDTO{
			Name:         p.Name,
			Path:         p.Path,
			Accounts:     make([]ThunderbirdAccountDTO, 0, len(p.Accounts)),
			LocalFolders: make([]ThunderbirdFolderDTO, 0, len(p.LocalFolders)),
		}
		for _, acc := range p.Accounts {
			dto.Accounts = append(dto.Accounts, ThunderbirdAccountDTO{
				Email:       acc.Email,
				DisplayName: acc.DisplayName,
				Username:    acc.Username,
				IMAPHost:    acc.IMAPHost,
				IMAPPort:    acc.IMAPPort,
				SMTPHost:    acc.SMTPHost,
				SMTPPort:    acc.SMTPPort,
				Kind:        acc.Kind,
				Exists:      have[strings.ToLower(acc.Email)],
			})
		}
		for _, f := range p.LocalFolders {
			dto.LocalFolders = append(dto.LocalFolders, ThunderbirdFolderDTO{
				Name: f.Name, Path: f.Path, SizeBytes: f.SizeBytes,
			})
		}
		out = append(out, dto)
	}
	return out, nil
}

// ImportThunderbirdAccounts recreates the listed addresses as Pelton accounts,
// reading their server settings from the profile at profilePath. Addresses
// already configured are skipped. Passwords are not importable, so each new
// account needs its password entered once before it can sync; the returned
// count is how many were created.
//
// Each account is started the same way the wizard starts one. Without that an
// imported account was inert: it never opened an idle connection, and nothing
// recorded that it could not log in, so it carried no marker and the mailbox
// looked healthy while no mail ever arrived in it.
func (a *App) ImportThunderbirdAccounts(profilePath string, emails []string) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	profile, err := mailimport.ReadProfile(profilePath)
	if err != nil {
		return 0, err
	}
	want := toSet(lowerAll(emails))

	existing, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return 0, err
	}
	have := make(map[string]bool, len(existing))
	for _, acc := range existing {
		have[strings.ToLower(acc.Email)] = true
	}

	created := 0
	var started []int64
	for _, acc := range profile.Accounts {
		key := strings.ToLower(acc.Email)
		if !want[key] || have[key] || acc.Kind != "imap" {
			continue
		}
		account := storage.Account{
			Email:       acc.Email,
			DisplayName: acc.DisplayName,
			Username:    acc.Username,
			IMAPHost:    acc.IMAPHost,
			IMAPPort:    acc.IMAPPort,
			SMTPHost:    acc.SMTPHost,
			SMTPPort:    acc.SMTPPort,
		}
		id, err := a.store.CreateAccount(a.ctx, &account)
		if err != nil {
			return created, err
		}
		have[key] = true
		started = append(started, id)
		created++
	}
	// started after the loop rather than inside it, so a failure halfway through
	// does not leave half the accounts syncing while the error says nothing was
	// imported.
	for _, id := range started {
		if err := a.startImportedAccount(id); err != nil {
			a.log.Error("start imported mailbox", "account", id, "err", err)
		}
	}
	return created, nil
}

// startImportedAccount starts one freshly imported account, through the
// App.startAccount seam so a test can see which accounts were started.
func (a *App) startImportedAccount(accountID int64) error {
	if a.startAccount != nil {
		return a.startAccount(accountID)
	}
	return a.StartAccountSync(accountID)
}

// ChooseMailFiles opens a file picker for messages and archives to import and
// returns the chosen paths. An empty result means the dialog was cancelled.
func (a *App) ChooseMailFiles() ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.openMultipleFilesDialog(openDialogOptions{
		Title: "Choose mail to import",
		Filters: []fileFilter{
			{DisplayName: "Mail files (*.eml, *.mbox)", Pattern: "*.eml;*.mbox;*.mbx"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
}

// ImportMailFiles imports the given .eml or .mbox files into Local Folders,
// creating that account on first use. It returns as soon as the job starts;
// progress and the final count arrive on EventImportProgress, so a multi-
// gigabyte archive never blocks the ui.
//
// Each mbox becomes a folder named after its file. Single messages are
// gathered into one "Imported" folder, since an .eml carries no folder of its
// own.
func (a *App) ImportMailFiles(paths []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	if !importMu.TryLock() {
		return errImportRunning
	}
	go func() {
		defer importMu.Unlock()
		a.runImport(sourcesForFiles(paths))
	}()
	return nil
}

// ImportThunderbirdFolders imports mail folders found in a Thunderbird profile,
// keeping the names they had there. Like ImportMailFiles it runs in the
// background and reports on EventImportProgress.
func (a *App) ImportThunderbirdFolders(paths []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	if !importMu.TryLock() {
		return errImportRunning
	}
	go func() {
		defer importMu.Unlock()
		a.runImport(sourcesForFolders(paths))
	}()
	return nil
}

// sourcesForFiles maps picked files onto local folders: an archive keeps its
// own name, a single message joins the shared Imported folder.
func sourcesForFiles(paths []string) []mailimport.Source {
	out := make([]mailimport.Source, 0, len(paths))
	for _, path := range paths {
		out = append(out, mailimport.Source{Path: path, Folder: folderNameFor(path)})
	}
	return out
}

// sourcesForFolders keeps a Thunderbird folder's own name, including the
// parent path for a nested one, so the imported tree reads the way it did
// there.
func sourcesForFolders(paths []string) []mailimport.Source {
	out := make([]mailimport.Source, 0, len(paths))
	for _, path := range paths {
		out = append(out, mailimport.Source{Path: path, Folder: filepath.Base(path)})
	}
	return out
}

func folderNameFor(path string) string {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	if ext == ".eml" {
		return importedFolderName
	}
	if ext != "" {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if base == "" {
		return importedFolderName
	}
	return base
}

// runImport does the work and reports it. It runs on its own goroutine with
// importMu held.
func (a *App) runImport(sources []mailimport.Source) {
	a.emit(EventImportProgress, ImportProgressEvent{Running: true})

	importer := mailimport.New(a.store, a.log)
	importer.OnProgress = func(p mailimport.Progress) {
		a.emit(EventImportProgress, ImportProgressEvent{
			Running:    true,
			Folder:     p.Folder,
			Imported:   p.Imported,
			Skipped:    p.Skipped,
			BytesDone:  p.BytesDone,
			BytesTotal: p.BytesTotal,
			FileIndex:  p.FileIndex,
			FileTotal:  p.FileTotal,
		})
	}

	result, err := importer.Import(a.ctx, sources)
	done := ImportProgressEvent{
		Running:  false,
		Imported: result.Imported,
		Skipped:  result.Skipped,
		Failed:   result.Failed,
		Folders:  result.Folders,
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		a.log.Error("import mail", "err", err)
		done.Error = err.Error()
	}
	a.emit(EventImportProgress, done)

	if result.Imported > 0 {
		// imported mail should be searchable and counted like anything else.
		go a.indexNewMessages()
		go a.refreshViewCounts()
	}
}

func lowerAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strings.ToLower(v))
	}
	return out
}
