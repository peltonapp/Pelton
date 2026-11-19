package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/peltonapp/Pelton/internal/scripting"
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

// CreateScriptLibrary stores a new library. Its name is what require takes
// and must be free.
func (a *App) CreateScriptLibrary(in ScriptLibraryInput) (ScriptLibraryDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptLibraryDTO{}, err
	}
	l, err := svc.CreateLibrary(a.ctx, in.library())
	if err != nil {
		return ScriptLibraryDTO{}, err
	}
	return toLibraryDTO(l), nil
}

// UpdateScriptLibrary saves a library. Renaming it breaks the scripts that
// require the old name.
func (a *App) UpdateScriptLibrary(id int64, in ScriptLibraryInput) (ScriptLibraryDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptLibraryDTO{}, err
	}
	l := in.library()
	l.ID = id
	out, err := svc.UpdateLibrary(a.ctx, l)
	if err != nil {
		return ScriptLibraryDTO{}, err
	}
	return toLibraryDTO(out), nil
}

// DeleteScriptLibrary removes a library.
func (a *App) DeleteScriptLibrary(id int64) error {
	svc, err := a.scriptService()
	if err != nil {
		return err
	}
	return svc.DeleteLibrary(a.ctx, id)
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
// same name is not replaced.
func (a *App) ImportScriptLibrary(text string) (ScriptLibraryDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptLibraryDTO{}, err
	}
	l, err := svc.ImportLibrary(a.ctx, []byte(text))
	if err != nil {
		return ScriptLibraryDTO{}, err
	}
	return toLibraryDTO(l), nil
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

// ScriptReference returns the scripting API as a document in English,
// markdown when markdown is set and plain text otherwise.
func (a *App) ScriptReference(markdown bool) (string, error) {
	svc, err := a.scriptService()
	if err != nil {
		return "", err
	}
	format := scripting.ReferenceText
	if markdown {
		format = scripting.ReferenceMarkdown
	}
	return scripting.Reference(svc.Registry().Catalog(), format), nil
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
