package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

var errScriptsUnavailable = errors.New("pelton: automations are not available")

// ScriptDTO is one script as the Automations space shows it. Blocks is the
// block tree as JSON text, empty for a Lua script.
type ScriptDTO struct {
	ID             int64             `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Kind           string            `json:"kind"`
	Source         string            `json:"source"`
	Blocks         string            `json:"blocks"`
	Trigger        ScriptTriggerDTO  `json:"trigger"`
	Permissions    []string          `json:"permissions"`
	Approved       []string          `json:"approved"`
	Enabled        bool              `json:"enabled"`
	Origin         string            `json:"origin"`
	SettingsSchema []scripting.Param `json:"settingsSchema"`
	Settings       map[string]any    `json:"settings"`
	CatchUp        string            `json:"catchUp"`
	CatchUpCap     int               `json:"catchUpCap"`
	AllowUnasked   []string          `json:"allowUnasked"`
	CreatedAt      string            `json:"createdAt"`
	UpdatedAt      string            `json:"updatedAt"`
	LastRunAt      string            `json:"lastRunAt"`
}

// ScriptTriggerDTO is a script's trigger and its parameters.
type ScriptTriggerDTO struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params"`
}

// ScriptInput is what the editor saves: everything about a script the user
// writes, but not its switch, approvals or origin.
type ScriptInput struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Kind           string            `json:"kind"`
	Source         string            `json:"source"`
	Blocks         string            `json:"blocks"`
	Trigger        ScriptTriggerDTO  `json:"trigger"`
	Permissions    []string          `json:"permissions"`
	SettingsSchema []scripting.Param `json:"settingsSchema"`
	CatchUp        string            `json:"catchUp"`
	CatchUpCap     int               `json:"catchUpCap"`
}

// ScriptRunDTO is one run, live or from the log.
type ScriptRunDTO struct {
	ID           int64            `json:"id"`
	ScriptID     int64            `json:"scriptId"`
	ScriptName   string           `json:"scriptName"`
	Trigger      string           `json:"trigger"`
	Dry          bool             `json:"dry"`
	Depth        int              `json:"depth"`
	Started      string           `json:"started"`
	DurationMs   int64            `json:"durationMs"`
	Status       string           `json:"status"`
	Error        string           `json:"error"`
	Steps        []scripting.Step `json:"steps"`
	Changes      int              `json:"changes"`
	Cancelled    bool             `json:"cancelled"`
	CancelReason string           `json:"cancelReason"`
	LogTruncated bool             `json:"logTruncated"`
	// Undoable is set while the run's mail changes can still be undone;
	// Messages are the messages they touched.
	Undoable bool    `json:"undoable"`
	Messages []int64 `json:"messages"`
}

// ScriptConfirmDTO asks about one risky call.
type ScriptConfirmDTO struct {
	ID         string `json:"id"`
	ScriptID   int64  `json:"scriptId"`
	ScriptName string `json:"scriptName"`
	Function   string `json:"function"`
	Summary    string `json:"summary"`
}

// ScriptCatchUpDTO asks whether a script runs over mail that arrived while
// the app was closed.
type ScriptCatchUpDTO struct {
	ScriptID int64  `json:"scriptId"`
	Name     string `json:"name"`
	Count    int    `json:"count"`
}

// ScriptToastDTO is a message a script shows.
type ScriptToastDTO struct {
	Script string `json:"script"`
	Text   string `json:"text"`
	Level  string `json:"level"`
}

// ScriptFileDTO is a script file read for import, shown before anything is
// stored: the script as it would arrive and the file's text.
type ScriptFileDTO struct {
	Script ScriptDTO `json:"script"`
	Text   string    `json:"text"`
}

func fmtStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func toScriptDTO(sc *scripting.Script) ScriptDTO {
	blocks := ""
	if sc.Blocks != nil {
		if b, err := json.Marshal(sc.Blocks); err == nil {
			blocks = string(b)
		}
	}
	settings := sc.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	params := sc.Trigger.Params
	if params == nil {
		params = map[string]any{}
	}
	return ScriptDTO{
		ID: sc.ID, Name: sc.Name, Description: sc.Description, Kind: string(sc.Kind),
		Source: sc.Source, Blocks: blocks,
		Trigger:        ScriptTriggerDTO{Type: sc.Trigger.Type, Params: params},
		Permissions:    orEmpty(sc.Permissions),
		Approved:       orEmpty(sc.Approved),
		Enabled:        sc.Enabled,
		Origin:         string(sc.Origin),
		SettingsSchema: orEmpty(sc.SettingsSchema),
		Settings:       settings,
		CatchUp:        string(sc.CatchUp),
		CatchUpCap:     sc.CatchUpCap,
		AllowUnasked:   orEmpty(sc.AllowUnasked),
		CreatedAt:      fmtStamp(sc.CreatedAt),
		UpdatedAt:      fmtStamp(sc.UpdatedAt),
		LastRunAt:      fmtStamp(sc.LastRunAt),
	}
}

func (in ScriptInput) script() (*scripting.Script, error) {
	sc := &scripting.Script{
		Name: strings.TrimSpace(in.Name), Description: in.Description,
		Kind: scripting.Kind(in.Kind), Source: in.Source,
		Trigger:        scripting.TriggerConfig{Type: in.Trigger.Type, Params: in.Trigger.Params},
		Permissions:    in.Permissions,
		SettingsSchema: in.SettingsSchema,
		CatchUp:        scripting.CatchUpMode(in.CatchUp),
		CatchUpCap:     in.CatchUpCap,
	}
	if sc.Kind == scripting.KindBlocks {
		raw := in.Blocks
		if strings.TrimSpace(raw) == "" {
			raw = `{"version":1,"blocks":[]}`
		}
		t, err := scripting.ParseTree([]byte(raw))
		if err != nil {
			return nil, err
		}
		sc.Blocks = &t
		sc.Source = ""
	}
	return sc, nil
}

func (a *App) runDTO(name string, r *scripting.Run) ScriptRunDTO {
	undoable, messages := a.runUndo(r.ID)
	return ScriptRunDTO{
		ID: r.ID, ScriptID: r.ScriptID, ScriptName: name, Trigger: r.Trigger, Dry: r.Dry, Depth: r.Depth,
		Started: fmtStamp(r.Started), DurationMs: r.Duration.Milliseconds(), Status: string(r.Status),
		Error: r.Error, Steps: orEmpty(r.Steps), Changes: r.Changes(), Cancelled: r.Cancelled,
		CancelReason: r.CancelReason, LogTruncated: r.LogTruncated,
		Undoable: undoable && !r.Dry, Messages: messages,
	}
}

func (a *App) scriptService() (*scripting.Service, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if a.scripts == nil {
		return nil, errScriptsUnavailable
	}
	return a.scripts, nil
}

// ScriptRegistry returns everything scripts can use, as data: triggers,
// conditions, actions, modules with their functions, value types,
// permissions and parameter types. The editor, the pickers and the API
// reference are built from it.
func (a *App) ScriptRegistry() (scripting.Catalog, error) {
	svc, err := a.scriptService()
	if err != nil {
		return scripting.Catalog{}, err
	}
	return svc.Registry().Catalog(), nil
}

// ListScripts returns every script by name.
func (a *App) ListScripts() ([]ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return nil, err
	}
	list, err := svc.Scripts(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScriptDTO, 0, len(list))
	for _, sc := range list {
		out = append(out, toScriptDTO(sc))
	}
	return out, nil
}

// GetScript returns one script.
func (a *App) GetScript(id int64) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	sc, err := svc.Script(a.ctx, id)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(sc), nil
}

// CreateScript stores a new script, switched off.
func (a *App) CreateScript(in ScriptInput) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	sc, err := in.script()
	if err != nil {
		return ScriptDTO{}, err
	}
	sc.Origin = scripting.OriginUser
	out, err := svc.Create(a.ctx, sc)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}

// UpdateScript saves a script's content. A newly declared permission
// switches it off until approved.
func (a *App) UpdateScript(id int64, in ScriptInput) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	sc, err := in.script()
	if err != nil {
		return ScriptDTO{}, err
	}
	sc.ID = id
	out, err := svc.Update(a.ctx, sc)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}

// DeleteScript removes a script with its log and storage.
func (a *App) DeleteScript(id int64) error {
	svc, err := a.scriptService()
	if err != nil {
		return err
	}
	if err := svc.Delete(a.ctx, id); err != nil {
		return err
	}
	return a.setScriptFolder(id, "")
}

// SetScriptEnabled switches a script on, approving the given permissions,
// or off. On needs every declared permission approved.
func (a *App) SetScriptEnabled(id int64, enabled bool, approve []string) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	out, err := svc.SetEnabled(a.ctx, id, enabled, approve)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}

// SetScriptAllowUnasked lets a script call a risky function, e.g.
// "mail.send", without asking each time, or takes that back.
func (a *App) SetScriptAllowUnasked(id int64, function string, allow bool) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	out, err := svc.SetAllowUnasked(a.ctx, id, function, allow)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}

// SetScriptSettings stores the values of a script's declared settings.
func (a *App) SetScriptSettings(id int64, values map[string]any) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	out, err := svc.SetSettings(a.ctx, id, values)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}

// SetScriptCatchUp sets what a mail script does with mail that arrived
// while the app was closed: "run", "live" or "ask", and the most messages
// it runs over.
func (a *App) SetScriptCatchUp(id int64, mode string, limit int) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	out, err := svc.SetCatchUp(a.ctx, id, scripting.CatchUpMode(mode), limit)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}

// CompileScriptBlocks compiles a block tree (JSON) for a trigger to Lua, as
// the editor shows it, with every problem found.
func (a *App) CompileScriptBlocks(blocks, trigger string) (scripting.CompileResult, error) {
	svc, err := a.scriptService()
	if err != nil {
		return scripting.CompileResult{}, err
	}
	t, err := scripting.ParseTree([]byte(blocks))
	if err != nil {
		return scripting.CompileResult{Errors: []scripting.CompileError{{Message: err.Error()}}, Permissions: []string{}}, nil
	}
	return scripting.Compile(svc.Registry(), t, trigger, false), nil
}

// CheckScriptLua reports a syntax error in Lua, or "" when it parses.
func (a *App) CheckScriptLua(source string) string {
	if err := scripting.CheckLua(source); err != nil {
		return strings.TrimPrefix(err.Error(), "scripting: ")
	}
	return ""
}

// ConvertScriptToLua turns a block script into a Lua script for good.
func (a *App) ConvertScriptToLua(id int64) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	out, err := svc.ConvertToLua(a.ctx, id)
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}

// manualEvent is the event a manual run gets: the picked message, if any.
func manualEvent(messageID int64) map[string]any {
	if messageID == 0 {
		return nil
	}
	return map[string]any{"message_id": messageID}
}

// RunScript runs an enabled script now, on the given message when its
// trigger works on one (0 for none), and returns the run.
func (a *App) RunScript(id, messageID int64) (ScriptRunDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptRunDTO{}, err
	}
	sc, err := svc.Script(a.ctx, id)
	if err != nil {
		return ScriptRunDTO{}, err
	}
	run, err := svc.RunNow(a.ctx, id, manualEvent(messageID))
	if err != nil {
		return ScriptRunDTO{}, err
	}
	return a.runDTO(sc.Name, run), nil
}

// TestScript dry runs a script as written in the editor, saved or not,
// against a message (0 for the sample message). Reads work; every change is
// listed as a planned step and nothing is changed.
func (a *App) TestScript(in ScriptInput, messageID int64) (ScriptRunDTO, error) {
	return a.testScript(a.ctx, in, messageID)
}

func (a *App) testScript(ctx context.Context, in ScriptInput, messageID int64) (ScriptRunDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptRunDTO{}, err
	}
	sc, err := in.script()
	if err != nil {
		return ScriptRunDTO{}, err
	}
	if sc.CatchUp == "" {
		sc.CatchUp = scripting.CatchUpLive
	}
	if err := sc.Validate(svc.Registry()); err != nil {
		return ScriptRunDTO{}, err
	}
	var data map[string]any
	if messageID != 0 {
		data = map[string]any{"message_id": messageID}
		if t, ok := svc.Registry().Trigger(sc.Trigger.Type); ok && t.Prepare == nil {
			return ScriptRunDTO{}, fmt.Errorf("pelton: this trigger does not run on a message")
		}
	}
	run, err := svc.TestRun(ctx, sc, data)
	if err != nil {
		return ScriptRunDTO{}, err
	}
	return a.runDTO(sc.Name, run), nil
}

// ScriptRuns returns the latest runs of a script, or of every script for 0,
// newest first.
func (a *App) ScriptRuns(scriptID int64, limit int) ([]ScriptRunDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return nil, err
	}
	runs, err := svc.Store().Runs(a.ctx, scriptID, limit)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	if list, err := svc.Scripts(a.ctx); err == nil {
		for _, sc := range list {
			names[sc.ID] = sc.Name
		}
	}
	out := make([]ScriptRunDTO, 0, len(runs))
	for _, r := range runs {
		out = append(out, a.runDTO(names[r.ScriptID], r))
	}
	return out, nil
}

// ClearScriptRuns empties the log of a script, or every log for 0.
func (a *App) ClearScriptRuns(scriptID int64) error {
	svc, err := a.scriptService()
	if err != nil {
		return err
	}
	return svc.Store().ClearRuns(a.ctx, scriptID)
}

// ScriptLogDays returns how many days the run log keeps.
func (a *App) ScriptLogDays() int {
	return a.intSetting(settingScriptLogDays, scripting.DefaultRetentionDays)
}

// SetScriptLogDays sets how many days the run log keeps, 1 to 365.
func (a *App) SetScriptLogDays(days int) error {
	if err := a.ready(); err != nil {
		return err
	}
	if days < 1 || days > 365 {
		return fmt.Errorf("pelton: keep runs for 1 to 365 days")
	}
	return a.store.Set(a.ctx, settingScriptLogDays, strconv.Itoa(days))
}

// UndoScriptRun reverses the mail changes a run made, newest first. Sends
// are never undone. A run can be undone once.
func (a *App) UndoScriptRun(runID int64) error {
	st := &a.scriptState
	st.mu.Lock()
	j := st.journal[runID]
	delete(st.journal, runID)
	st.order = slices.DeleteFunc(st.order, func(id int64) bool { return id == runID })
	st.mu.Unlock()
	if j == nil {
		return fmt.Errorf("pelton: this run can no longer be undone")
	}
	var errs []error
	for i := len(j.undo) - 1; i >= 0; i-- {
		if err := j.undo[i](); err != nil {
			errs = append(errs, err)
		}
	}
	goSafe("counting unread mail", a.refreshViewCounts)
	return errors.Join(errs...)
}

// AnswerScriptConfirm answers a confirmation a script asked for. always lets
// the script make that call without asking from now on.
func (a *App) AnswerScriptConfirm(id string, allow, always bool) {
	st := &a.scriptState
	st.mu.Lock()
	ch := st.confirms[id]
	st.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- scripting.ConfirmAnswer{Allow: allow, Always: allow && always}:
	default:
	}
}

// ResolveScriptCatchUp answers a catch-up question: run over the mail that
// arrived while the app was closed, or skip it.
func (a *App) ResolveScriptCatchUp(scriptID int64, run bool) error {
	svc, err := a.scriptService()
	if err != nil {
		return err
	}
	svc.ResolveCatchUp(scriptID, run)
	return nil
}

// PendingScriptCatchUps lists the catch-up questions still open, for a
// window that opened after they were asked.
func (a *App) PendingScriptCatchUps() ([]ScriptCatchUpDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return nil, err
	}
	out := []ScriptCatchUpDTO{}
	for id, n := range svc.PendingCatchUps() {
		if sc, err := svc.Script(a.ctx, id); err == nil {
			out = append(out, ScriptCatchUpDTO{ScriptID: id, Name: sc.Name, Count: n})
		}
	}
	return out, nil
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9 ._-]+`)

// ExportScript writes a script to a .peltonscript file the user picks and
// returns the path, "" when they cancelled.
func (a *App) ExportScript(id int64) (string, error) {
	svc, err := a.scriptService()
	if err != nil {
		return "", err
	}
	sc, err := svc.Script(a.ctx, id)
	if err != nil {
		return "", err
	}
	data, err := scripting.Export(svc.Registry(), sc)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(unsafeFileChars.ReplaceAllString(sc.Name, ""))
	if name == "" {
		name = "script"
	}
	path, err := a.saveFileDialog(saveDialogOptions{
		Title:           "Export script",
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

// PickScriptFile asks for a .peltonscript file and returns its path, ""
// when the user cancelled.
func (a *App) PickScriptFile() (string, error) {
	return a.openFileDialog(openDialogOptions{
		Title:   "Import script",
		Filters: []fileFilter{{DisplayName: "Pelton script", Pattern: "*" + scripting.FileExtension}},
	})
}

// ReadScriptFile reads a script file for the import dialog without storing
// or running anything.
func (a *App) ReadScriptFile(path string) (ScriptFileDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptFileDTO{}, err
	}
	if !strings.EqualFold(filepath.Ext(path), scripting.FileExtension) {
		return ScriptFileDTO{}, fmt.Errorf("pelton: not a %s file", scripting.FileExtension)
	}
	info, err := os.Stat(path)
	if err != nil {
		return ScriptFileDTO{}, err
	}
	if info.Size() > 1<<20 {
		return ScriptFileDTO{}, fmt.Errorf("pelton: the file is too large for a script")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ScriptFileDTO{}, err
	}
	sc, err := scripting.ParseFile(svc.Registry(), data)
	if err != nil {
		return ScriptFileDTO{}, err
	}
	return ScriptFileDTO{Script: toScriptDTO(sc), Text: string(data)}, nil
}

// ImportScript stores the script in a file's text, switched off and marked
// as imported.
func (a *App) ImportScript(text string) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	out, err := svc.Import(a.ctx, []byte(text))
	if err != nil {
		return ScriptDTO{}, err
	}
	return toScriptDTO(out), nil
}
