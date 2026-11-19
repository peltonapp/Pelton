package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scriptref"
)

// Recent lists the newest stored messages of a folder, by path or role, in
// one account or in every account that has such a folder, newest first. It
// is how scripts and the match tester look through a folder.
func (s *scriptMail) Recent(ctx context.Context, folder string, accountID int64, limit int) ([]scripting.Message, error) {
	a := s.app
	if err := a.ready(); err != nil {
		return nil, err
	}
	accounts := []int64{accountID}
	if accountID == 0 {
		list, err := a.store.ListAccounts(ctx)
		if err != nil {
			return nil, err
		}
		accounts = accounts[:0]
		for _, acc := range list {
			accounts = append(accounts, acc.ID)
		}
	}
	var out []scripting.Message
	found := false
	for _, id := range accounts {
		f, err := s.folder(id, folder)
		if err != nil {
			continue
		}
		found = true
		rows, err := a.store.ListMessages(ctx, f.ID, limit)
		if err != nil {
			return nil, err
		}
		for _, m := range rows {
			msg, err := a.scriptMessage(m)
			if err != nil {
				return nil, err
			}
			out = append(out, msg)
		}
	}
	if !found {
		return nil, fmt.Errorf("pelton: no folder %q", folder)
	}
	slices.SortStableFunc(out, func(x, y scripting.Message) int { return y.Date.Compare(x.Date) })
	return out[:min(limit, len(out))], nil
}

// ScriptLibraryDTO is a shared library as the Automations space shows it.
type ScriptLibraryDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Origin      string `json:"origin"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// ScriptLibraryInput is what the editor saves of a library.
type ScriptLibraryInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

func toLibraryDTO(l *scripting.Library) ScriptLibraryDTO {
	return ScriptLibraryDTO{
		ID: l.ID, Name: l.Name, Description: l.Description, Source: l.Source, Origin: string(l.Origin),
		CreatedAt: fmtStamp(l.CreatedAt), UpdatedAt: fmtStamp(l.UpdatedAt),
	}
}

func (in ScriptLibraryInput) library() *scripting.Library {
	return &scripting.Library{Name: strings.TrimSpace(in.Name), Description: in.Description, Source: in.Source}
}

// ListScriptLibraries returns every shared library by name.
func (a *App) ListScriptLibraries() ([]ScriptLibraryDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return nil, err
	}
	list, err := svc.Libraries(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScriptLibraryDTO, 0, len(list))
	for _, l := range list {
		out = append(out, toLibraryDTO(l))
	}
	return out, nil
}

// ScriptRefDTO names a script a library change reaches.
type ScriptRefDTO struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// ScriptLibrarySavedDTO is a stored library with the scripts storing it
// switched off.
type ScriptLibrarySavedDTO struct {
	Library     ScriptLibraryDTO `json:"library"`
	SwitchedOff []ScriptRefDTO   `json:"switchedOff"`
}

// ScriptLibraryNoteDTO says that a script was switched off because a library
// it loads changed, with the change since the user last approved it. Exists
// is false when there is no such note.
type ScriptLibraryNoteDTO struct {
	ScriptID int64  `json:"scriptId"`
	Exists   bool   `json:"exists"`
	Library  string `json:"library"`
	When     string `json:"when"`
	// Deleted is set when the library was deleted.
	Deleted bool                `json:"deleted"`
	Diff    []ScriptDiffLineDTO `json:"diff"`
}

// settingScriptLibraryNotes holds, as json keyed by script id, why scripts
// were switched off by a change to a library they load, until each is
// switched on again.
const settingScriptLibraryNotes = "scripts_library_notes"

// libraryNote is the stored form of a ScriptLibraryNoteDTO: the library's
// source when the script was last approved and as it is now.
type libraryNote struct {
	Library string    `json:"library"`
	Before  string    `json:"before"`
	After   string    `json:"after"`
	Deleted bool      `json:"deleted"`
	At      time.Time `json:"at"`
}

func refs(list []*scripting.Script) []ScriptRefDTO {
	out := make([]ScriptRefDTO, 0, len(list))
	for _, sc := range list {
		out = append(out, ScriptRefDTO{ID: sc.ID, Name: sc.Name, Enabled: sc.Enabled})
	}
	return out
}

func (a *App) libraryNotes() map[int64]libraryNote {
	out := map[int64]libraryNote{}
	if raw := a.stringSetting(settingScriptLibraryNotes, ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			a.log.Error("read script library notes", "err", err)
		}
	}
	return out
}

func (a *App) saveLibraryNotes(notes map[int64]libraryNote) error {
	data, err := json.Marshal(notes)
	if err != nil {
		return err
	}
	return a.store.Set(a.ctx, settingScriptLibraryNotes, string(data))
}

// noteLibraryChange records on each script a change to the library oldName,
// now library, switched off what changed, and brings the notes already
// waiting about it up to date. A note keeps the source the script was
// approved with.
func (a *App) noteLibraryChange(oldName, library, before, after string, deleted bool, revoked []*scripting.Script) error {
	a.proposalMu.Lock()
	defer a.proposalMu.Unlock()
	notes := a.libraryNotes()
	touched := false
	for id, n := range notes {
		if n.Library == oldName || n.Library == library {
			n.Library, n.After, n.Deleted = library, after, deleted
			notes[id] = n
			touched = true
		}
	}
	now := time.Now().UTC()
	for _, sc := range revoked {
		n := libraryNote{Library: library, Before: before, After: after, Deleted: deleted, At: now}
		if old, ok := notes[sc.ID]; ok && old.Library == library {
			n.Before = old.Before
		}
		notes[sc.ID] = n
		touched = true
	}
	if !touched {
		return nil
	}
	return a.saveLibraryNotes(notes)
}

func (a *App) dropLibraryNote(id int64) {
	a.proposalMu.Lock()
	defer a.proposalMu.Unlock()
	notes := a.libraryNotes()
	if _, ok := notes[id]; !ok {
		return
	}
	delete(notes, id)
	if err := a.saveLibraryNotes(notes); err != nil {
		a.log.Error("drop script library note", "script", id, "err", err)
	}
}

// ScriptLibraryNote says whether a script was switched off because a library
// it loads changed, and what changed in it since the script was approved.
func (a *App) ScriptLibraryNote(scriptID int64) (ScriptLibraryNoteDTO, error) {
	out := ScriptLibraryNoteDTO{ScriptID: scriptID, Diff: []ScriptDiffLineDTO{}}
	if err := a.ready(); err != nil {
		return out, err
	}
	n, ok := a.libraryNotes()[scriptID]
	if !ok {
		return out, nil
	}
	out.Exists, out.Library, out.When, out.Deleted = true, n.Library, formatDate(n.At), n.Deleted
	out.Diff = lineDiff(sourceLines(n.Before), sourceLines(n.After))
	return out, nil
}

func sourceLines(src string) []string {
	if src == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(src, "\n"), "\n")
}

// ScriptLibraryImpact lists the scripts saving a library under name would
// switch off: those that load it, under its current name or the new one,
// and are on or hold an approval. id is 0 for a library not stored yet.
func (a *App) ScriptLibraryImpact(id int64, name string) ([]ScriptRefDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return nil, err
	}
	names := []string{strings.TrimSpace(name)}
	if id != 0 {
		cur, err := svc.Library(a.ctx, id)
		if err != nil {
			return nil, err
		}
		names = append(names, cur.Name)
	}
	list, err := svc.LibraryDependents(a.ctx, names...)
	if err != nil {
		return nil, err
	}
	return refs(list), nil
}

// CreateScriptLibrary stores a new library. Its name is what require takes
// and must be free. Scripts that already required that name are switched
// off and listed.
func (a *App) CreateScriptLibrary(in ScriptLibraryInput) (ScriptLibrarySavedDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	l, revoked, err := svc.CreateLibrary(a.ctx, in.library())
	if err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	return a.librarySaved(l, l.Name, "", revoked)
}

func (a *App) librarySaved(l *scripting.Library, oldName, before string, revoked []*scripting.Script) (ScriptLibrarySavedDTO, error) {
	if err := a.noteLibraryChange(oldName, l.Name, before, l.Source, false, revoked); err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	return ScriptLibrarySavedDTO{Library: toLibraryDTO(l), SwitchedOff: refs(revoked)}, nil
}

// UpdateScriptLibrary saves a library. Renaming it breaks the scripts that
// require the old name. A change to its code or name switches off the
// scripts that load it, which it lists.
func (a *App) UpdateScriptLibrary(id int64, in ScriptLibraryInput) (ScriptLibrarySavedDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	cur, err := svc.Library(a.ctx, id)
	if err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	l := in.library()
	l.ID = id
	out, revoked, err := svc.UpdateLibrary(a.ctx, l)
	if err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	return a.librarySaved(out, cur.Name, cur.Source, revoked)
}

// DeleteScriptLibrary removes a library and lists the scripts that load it,
// which are switched off.
func (a *App) DeleteScriptLibrary(id int64) ([]ScriptRefDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return nil, err
	}
	cur, err := svc.Library(a.ctx, id)
	if errors.Is(err, scripting.ErrNotFound) {
		return []ScriptRefDTO{}, nil
	}
	if err != nil {
		return nil, err
	}
	revoked, err := svc.DeleteLibrary(a.ctx, id)
	if err != nil {
		return nil, err
	}
	if err := a.noteLibraryChange(cur.Name, cur.Name, cur.Source, "", true, revoked); err != nil {
		return nil, err
	}
	return refs(revoked), nil
}

// ExportScriptLibrary writes a library to a .peltonscript file the user
// picks and returns the path, "" when they cancelled.
func (a *App) ExportScriptLibrary(id int64) (string, error) {
	svc, err := a.scriptService()
	if err != nil {
		return "", err
	}
	l, err := svc.Library(a.ctx, id)
	if err != nil {
		return "", err
	}
	data, err := scripting.ExportLibrary(l)
	if err != nil {
		return "", err
	}
	return a.writeScriptFile(l.Name, "Export library", data)
}

// writeScriptFile asks where to save a script file named after name and
// writes data there, returning the path or "" when the user cancelled.
func (a *App) writeScriptFile(name, title string, data []byte) (string, error) {
	name = strings.TrimSpace(unsafeFileChars.ReplaceAllString(name, ""))
	if name == "" {
		name = "script"
	}
	path, err := a.saveFileDialog(saveDialogOptions{
		Title:           title,
		DefaultFilename: name + scripting.FileExtension,
		Filters:         []fileFilter{{DisplayName: "Pelton script", Pattern: "*" + scripting.FileExtension}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += scripting.FileExtension
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ImportScriptLibrary stores the library in a file's text. A library of the
// same name is not replaced. Scripts that already required that name are
// switched off and listed.
func (a *App) ImportScriptLibrary(text string) (ScriptLibrarySavedDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	l, revoked, err := svc.ImportLibrary(a.ctx, []byte(text))
	if err != nil {
		return ScriptLibrarySavedDTO{}, err
	}
	return a.librarySaved(l, l.Name, "", revoked)
}

// UseScriptTemplate stores a copy of a gallery template as a new script,
// switched off, and returns it.
func (a *App) UseScriptTemplate(id string) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	sc, err := svc.FromTemplate(a.ctx, id)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(sc), nil
}

// TestScriptMatch tries a regular expression or a condition on the newest
// messages of a folder and reports which match. Nothing is changed.
func (a *App) TestScriptMatch(req scripting.MatchRequest) (*scripting.MatchReport, error) {
	svc, err := a.scriptService()
	if err != nil {
		return nil, err
	}
	return svc.TestMatch(a.ctx, req)
}

// ScriptReference returns the scripting reference as one markdown document
// in English, the same one an assistant reads over MCP.
func (a *App) ScriptReference() (string, error) {
	svc, err := a.scriptService()
	if err != nil {
		return "", err
	}
	return scriptref.Build(svc.Registry()).Text(), nil
}

// dropAIScriptOptions forgets a deleted script's AI switches.
func (a *App) dropAIScriptOptions(id int64) error {
	aiMu.Lock()
	defer aiMu.Unlock()
	all := a.aiScriptOptions()
	key := strconv.FormatInt(id, 10)
	if _, ok := all[key]; !ok {
		return nil
	}
	delete(all, key)
	return a.saveAIScriptOptions(all)
}

// scriptFileInfo reads what a script file holds for the import dialog: the
// script or library, and the libraries it needs that do not exist here.
func (a *App) scriptFileInfo(svc *scripting.Service, data []byte) (ScriptFileDTO, error) {
	kind, requires, err := scripting.ReadFileInfo(data)
	if err != nil {
		return ScriptFileDTO{}, err
	}
	out := ScriptFileDTO{Text: string(data), Kind: string(kind)}
	if kind == scripting.KindLibrary {
		l, _, err := scripting.ParseLibraryFile(data)
		if err != nil {
			return ScriptFileDTO{}, err
		}
		dto := toLibraryDTO(l)
		out.Library = &dto
	} else {
		sc, err := scripting.ParseFile(svc.Registry(), data)
		if err != nil {
			return ScriptFileDTO{}, err
		}
		out.Script = toScriptDTO(sc)
	}
	missing, err := svc.MissingLibraries(a.ctx, requires)
	if err != nil {
		return ScriptFileDTO{}, err
	}
	out.MissingLibraries = orEmpty(missing)
	return out, nil
}

var errNotScriptFile = errors.New("pelton: not a " + scripting.FileExtension + " file")

// scriptFileTooLarge is the most a script file read for import may hold.
const scriptFileTooLarge = 1 << 20

func readScriptFileData(path string) ([]byte, error) {
	if !strings.EqualFold(filepath.Ext(path), scripting.FileExtension) {
		return nil, errNotScriptFile
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > scriptFileTooLarge {
		return nil, fmt.Errorf("pelton: the file is too large for a script")
	}
	return os.ReadFile(path)
}
