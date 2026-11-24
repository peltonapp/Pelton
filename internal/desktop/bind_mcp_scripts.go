package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mcpserver"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scriptref"
	"github.com/peltonapp/Pelton/internal/scriptsandbox"
)

const (
	// EventAssistantScript fires when an agent creates, changes or proposes
	// a change to a script, and when the user settles a proposal, with an
	// AssistantScriptDTO.
	EventAssistantScript = "scripts:assistant"

	// settingScriptProposals holds the changes agents proposed to scripts,
	// waiting for the user, as json keyed by script id.
	settingScriptProposals = "mcp_script_proposals"

	// maxScriptProposals caps the proposals kept; the oldest goes first.
	maxScriptProposals = 50
)

// AssistantScriptDTO says what an agent did to a script, or what the user
// decided about its proposal. Action is created, updated, proposed, accepted
// or rejected.
type AssistantScriptDTO struct {
	ScriptID int64  `json:"scriptId"`
	Name     string `json:"name"`
	Action   string `json:"action"`
}

// scriptProposal is a change an agent proposed, stored until the user
// accepts or rejects it. File is the whole proposed script in the export
// format, unscrubbed.
type scriptProposal struct {
	ScriptID int64           `json:"scriptId"`
	File     json.RawMessage `json:"file"`
	At       time.Time       `json:"at"`
}

// mcpScripts adapts the scripting service to mcpserver.Scripts. Every path
// that stores a script forces it off and marks it as an assistant's, and the
// only run it offers is the sandboxed dry run.
type mcpScripts struct {
	app *App
}

// scriptLimiter returns the one limiter every server run shares.
func (a *App) scriptLimiter() *mcpserver.ScriptLimiter {
	if a.mcpScriptLimits == nil {
		a.mcpScriptLimits = mcpserver.NewScriptLimiter(mcpserver.DefaultScriptLimits())
	}
	return a.mcpScriptLimits
}

func (m *mcpScripts) Reference(context.Context) (*scriptref.Reference, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	return scriptref.Build(svc.Registry()), nil
}

func templateSummary(t scripting.Template) mcpserver.ScriptTemplate {
	return mcpserver.ScriptTemplate{ID: t.ID, Name: t.Label, Description: t.Description, Kind: string(t.Kind), Trigger: t.Trigger.Type}
}

// ListScriptTemplates lists the gallery's templates that compile with what
// this app offers now.
func (m *mcpScripts) ListScriptTemplates(context.Context) ([]mcpserver.ScriptTemplate, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	out := []mcpserver.ScriptTemplate{}
	for _, t := range svc.Registry().Templates() {
		if t.Missing == "" {
			out = append(out, templateSummary(t))
		}
	}
	return out, nil
}

// GetScriptTemplate hands out one template as a draft for create_script.
func (m *mcpScripts) GetScriptTemplate(_ context.Context, id string) (*mcpserver.ScriptTemplateDetail, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	t, ok := svc.Registry().Template(id)
	if !ok {
		return nil, fmt.Errorf("there is no template %q; list_script_templates lists them", id)
	}
	d := mcpserver.ScriptDraft{
		Name: t.Label, Description: t.Description, Kind: string(t.Kind), Source: t.Source,
		Trigger:     mcpserver.ScriptTrigger{Type: t.Trigger.Type, Params: t.Trigger.Params},
		Permissions: orEmpty(t.Permissions),
	}
	if t.Blocks != nil {
		d.Blocks = t.Blocks
	}
	if len(t.Settings) > 0 {
		d.Settings = anyList(t.Settings)
	}
	return &mcpserver.ScriptTemplateDetail{ScriptTemplate: templateSummary(t), Script: d}, nil
}

func (m *mcpScripts) ListScripts(ctx context.Context) ([]mcpserver.ScriptSummary, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	list, err := svc.Scripts(ctx)
	if err != nil {
		return nil, err
	}
	proposals := m.app.scriptProposals()
	out := make([]mcpserver.ScriptSummary, 0, len(list))
	for _, sc := range list {
		s := mcpserver.ScriptSummary{
			ID: sc.ID, Name: sc.Name, Description: sc.Description, Kind: string(sc.Kind),
			Trigger: sc.Trigger.Type, Enabled: sc.Enabled, Origin: string(sc.Origin),
			Permissions: orEmpty(sc.Permissions), Proposed: proposals[sc.ID] != nil,
		}
		if runs, err := svc.Store().Runs(ctx, sc.ID, 1); err == nil && len(runs) > 0 {
			s.LastRun = string(runs[0].Status)
			s.LastRunAt = runs[0].Started.UTC().Format(time.RFC3339)
		}
		out = append(out, s)
	}
	return out, nil
}

// GetScript hands out the script as an export would: account ids, setting
// values, approvals and run history stay behind.
func (m *mcpScripts) GetScript(ctx context.Context, id int64) (*mcpserver.ScriptDetail, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	sc, err := svc.Script(ctx, id)
	if err != nil {
		return nil, err
	}
	data, err := scripting.Export(svc.Registry(), sc)
	if err != nil {
		return nil, err
	}
	var f scripting.File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	out := &mcpserver.ScriptDetail{
		ID: sc.ID, Name: f.Name, Description: f.Description, Kind: string(f.Kind), Source: f.Source,
		Trigger:     mcpserver.ScriptTrigger{Type: f.Trigger.Type, Params: f.Trigger.Params},
		Permissions: orEmpty(f.Permissions), CatchUp: string(f.CatchUp),
		Enabled: sc.Enabled, Origin: string(sc.Origin), Proposed: m.app.scriptProposals()[id] != nil,
	}
	if f.Blocks != nil {
		out.Blocks = f.Blocks
	}
	if sc.Kind == scripting.KindBlocks && len(f.Settings) > 0 {
		out.Settings = anyList(f.Settings)
	}
	return out, nil
}

func anyList[T any](list []T) []any {
	out := make([]any, 0, len(list))
	for _, v := range list {
		out = append(out, v)
	}
	return out
}

// draftScript turns an agent's draft into a script marked as an assistant's,
// switched off, with nothing approved. It reports malformed json only; the
// rest is for checkDraft.
func draftScript(d mcpserver.ScriptDraft) (*scripting.Script, error) {
	sc := &scripting.Script{
		Name: strings.TrimSpace(d.Name), Description: d.Description, Kind: scripting.Kind(d.Kind),
		Trigger:     scripting.TriggerConfig{Type: d.Trigger.Type, Params: d.Trigger.Params},
		Permissions: d.Permissions, CatchUp: scripting.CatchUpMode(d.CatchUp),
		Origin: scripting.OriginAssistant,
	}
	if sc.CatchUp == "" {
		sc.CatchUp = scripting.CatchUpLive
	}
	switch sc.Kind {
	case scripting.KindBlocks:
		if d.Blocks != nil {
			data, err := json.Marshal(d.Blocks)
			if err != nil {
				return nil, err
			}
			tree, err := scripting.ParseTree(data)
			if err != nil {
				return nil, err
			}
			sc.Blocks = &tree
		}
		if len(d.Settings) > 0 {
			data, err := json.Marshal(d.Settings)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(data, &sc.SettingsSchema); err != nil {
				return nil, fmt.Errorf("settings: %w", err)
			}
		}
	default:
		sc.Source = d.Source
		if schema, err := scripting.LuaSettings(d.Source); err == nil {
			sc.SettingsSchema = schema
		}
	}
	return sc, nil
}

var luaErrorLine = regexp.MustCompile(`line:(\d+)`)

// checkDraft finds what is wrong with a script and what it needs.
func checkDraft(reg *scripting.Registry, sc *scripting.Script) *mcpserver.ScriptValidation {
	v := &mcpserver.ScriptValidation{Problems: []mcpserver.ScriptProblem{}}
	var needed []string
	switch sc.Kind {
	case scripting.KindLua:
		if err := scripting.CheckLua(sc.Source); err != nil {
			p := mcpserver.ScriptProblem{Message: err.Error()}
			if m := luaErrorLine.FindStringSubmatch(err.Error()); m != nil {
				p.Line, _ = strconv.Atoi(m[1])
			}
			v.Problems = append(v.Problems, p)
		} else if _, err := scripting.LuaSettings(sc.Source); err != nil {
			v.Problems = append(v.Problems, mcpserver.ScriptProblem{Message: err.Error()})
		}
		needed = luaPermissions(reg, sc.Source)
	case scripting.KindBlocks:
		if sc.Blocks != nil {
			res := scripting.Compile(reg, *sc.Blocks, sc.Trigger.Type, false)
			for _, e := range res.Errors {
				v.Problems = append(v.Problems, mcpserver.ScriptProblem{Message: e.Message, Block: e.Block})
			}
			if len(res.Errors) == 0 {
				if err := scripting.CheckLua(res.Lua); err != nil {
					v.Problems = append(v.Problems, mcpserver.ScriptProblem{Message: err.Error()})
				}
			}
			needed, v.Lua = res.Permissions, res.Lua
		}
	}
	if err := sc.Validate(reg); err != nil {
		v.Problems = append(v.Problems, mcpserver.ScriptProblem{Message: err.Error()})
	}
	if t, ok := reg.Trigger(sc.Trigger.Type); ok && t.Permission != "" {
		needed = append(needed, t.Permission)
	}
	slices.Sort(needed)
	v.Needed = slices.Compact(needed)
	v.Missing, v.Unused = []string{}, []string{}
	for _, p := range v.Needed {
		if !declares(sc.Permissions, p) {
			v.Missing = append(v.Missing, p)
		}
	}
	for _, p := range sc.Permissions {
		base, _, _ := strings.Cut(p, ":")
		if !slices.Contains(v.Needed, base) && !slices.Contains(v.Needed, p) {
			v.Unused = append(v.Unused, p)
		}
	}
	v.Valid = len(v.Problems) == 0 && len(v.Missing) == 0
	return v
}

// declares reports whether perm is declared, a targeted form such as
// "net:example.org" meeting its base.
func declares(list []string, perm string) bool {
	return slices.ContainsFunc(list, func(p string) bool {
		return p == perm || strings.HasPrefix(p, perm+":")
	})
}

// luaPermissions reads the permissions Lua source appears to need from the
// host functions it names and the lazy fields it reads. A call built at run
// time is not seen; this is a hint, the run enforces the real thing.
func luaPermissions(reg *scripting.Registry, src string) []string {
	var out []string
	for _, mod := range reg.Modules() {
		if !strings.Contains(src, mod.ID+".") {
			continue
		}
		for _, f := range mod.Functions {
			if f.Permission == "" {
				continue
			}
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(mod.ID+"."+f.Name) + `\b`).MatchString(src) {
				out = append(out, f.Permission)
			}
		}
	}
	for _, vt := range reg.Catalog().ValueTypes {
		for field, fn := range vt.Lazy {
			if !regexp.MustCompile(`\.` + regexp.QuoteMeta(field) + `\b`).MatchString(src) {
				continue
			}
			if f, ok := reg.Function(fn); ok && f.Permission != "" {
				out = append(out, f.Permission)
			}
		}
	}
	return out
}

func (m *mcpScripts) ValidateScript(_ context.Context, d mcpserver.ScriptDraft) (*mcpserver.ScriptValidation, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	sc, err := draftScript(d)
	if err != nil {
		return &mcpserver.ScriptValidation{
			Problems: []mcpserver.ScriptProblem{{Message: err.Error()}},
			Needed:   []string{}, Missing: []string{}, Unused: []string{},
		}, nil
	}
	return checkDraft(svc.Registry(), sc), nil
}

// readyScript is a draft that passed every check, or the error that says
// what to fix.
func readyScript(reg *scripting.Registry, d mcpserver.ScriptDraft) (*scripting.Script, error) {
	sc, err := draftScript(d)
	if err != nil {
		return nil, err
	}
	v := checkDraft(reg, sc)
	if len(v.Problems) > 0 || len(v.Missing) > 0 {
		var parts []string
		for _, p := range v.Problems {
			switch {
			case p.Line > 0:
				parts = append(parts, fmt.Sprintf("line %d: %s", p.Line, p.Message))
			case p.Block != "":
				parts = append(parts, fmt.Sprintf("block %s: %s", p.Block, p.Message))
			default:
				parts = append(parts, p.Message)
			}
		}
		if len(v.Missing) > 0 {
			parts = append(parts, "declare the permissions "+strings.Join(v.Missing, ", "))
		}
		return nil, fmt.Errorf("not saved, fix this first (validate_script lists everything): %s", strings.Join(parts, "; "))
	}
	return sc, nil
}

func (m *mcpScripts) CreateScript(ctx context.Context, d mcpserver.ScriptDraft) (*mcpserver.ScriptSaved, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	sc, err := readyScript(svc.Registry(), d)
	if err != nil {
		return nil, err
	}
	out, err := svc.Create(ctx, sc)
	if err != nil {
		return nil, err
	}
	m.app.recordAgentAction(mcpserver.ToolCreateScript, 0, fmt.Sprintf("created the script %q, switched off for you to review", out.Name), nil)
	m.app.emit(EventAssistantScript, AssistantScriptDTO{ScriptID: out.ID, Name: out.Name, Action: "created"})
	return &mcpserver.ScriptSaved{
		ID: out.ID, Status: "created",
		Message: "Saved switched off and marked as written by an assistant. The user reviews the code and permissions in Pelton and decides whether to switch it on.",
	}, nil
}

// changesDirectly reports whether an agent may change sc without asking: an
// assistant's script that is off and holds no approval or allowance.
func changesDirectly(sc *scripting.Script) bool {
	return sc.Origin == scripting.OriginAssistant && !sc.Enabled && len(sc.Approved) == 0 && len(sc.AllowUnasked) == 0
}

// keepPersonal carries the account parameters of cur's trigger over to next
// when next leaves them out: an agent never sees them, so leaving one out
// is not a wish to drop it.
func keepPersonal(reg *scripting.Registry, cur, next *scripting.Script) {
	if cur.Trigger.Type != next.Trigger.Type {
		return
	}
	t, _ := reg.Trigger(cur.Trigger.Type)
	for _, p := range t.Params {
		if p.Type != scripting.TypeAccount {
			continue
		}
		v, ok := cur.Trigger.Params[p.Name]
		if _, set := next.Trigger.Params[p.Name]; ok && !set {
			if next.Trigger.Params == nil {
				next.Trigger.Params = map[string]any{}
			}
			next.Trigger.Params[p.Name] = v
		}
	}
}

func (m *mcpScripts) UpdateScript(ctx context.Context, id int64, d mcpserver.ScriptDraft) (*mcpserver.ScriptSaved, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	cur, err := svc.Script(ctx, id)
	if err != nil {
		return nil, err
	}
	next, err := readyScript(svc.Registry(), d)
	if err != nil {
		return nil, err
	}
	keepPersonal(svc.Registry(), cur, next)
	next.ID = id
	if changesDirectly(cur) {
		out, err := svc.Update(ctx, next)
		if err != nil {
			return nil, err
		}
		m.app.dropScriptProposal(id)
		m.app.recordAgentAction(mcpserver.ToolUpdateScript, 0, fmt.Sprintf("changed its script %q, still switched off", out.Name), nil)
		m.app.emit(EventAssistantScript, AssistantScriptDTO{ScriptID: id, Name: out.Name, Action: "updated"})
		return &mcpserver.ScriptSaved{ID: id, Status: "updated", Message: "Changed. It stays switched off until the user reviews it and switches it on."}, nil
	}
	if err := m.app.storeScriptProposal(id, next); err != nil {
		return nil, err
	}
	m.app.recordAgentAction(mcpserver.ToolUpdateScript, 0, fmt.Sprintf("proposed a change to the script %q", cur.Name), nil)
	m.app.emit(EventAssistantScript, AssistantScriptDTO{ScriptID: id, Name: cur.Name, Action: "proposed"})
	return &mcpserver.ScriptSaved{
		ID: id, Status: "proposed", Enabled: cur.Enabled,
		Message: "Not changed. This script was written by the user, or is switched on or approved, so the change is stored as a proposal the user accepts or rejects in Pelton after reading it. A later proposal replaces this one.",
	}, nil
}

func (m *mcpScripts) TestScript(ctx context.Context, id int64, d *mcpserver.ScriptDraft) (*mcpserver.ScriptTestRun, error) {
	svc, err := m.app.scriptService()
	if err != nil {
		return nil, err
	}
	var sc *scripting.Script
	if d != nil {
		if sc, err = draftScript(*d); err != nil {
			return nil, err
		}
	} else if sc, err = svc.Script(ctx, id); err != nil {
		return nil, err
	}
	res, err := scriptsandbox.Run(ctx, svc.Registry(), sc, time.Now())
	if err != nil {
		return nil, err
	}
	r := res.Run
	out := &mcpserver.ScriptTestRun{
		Status: string(r.Status), Error: r.Error, ErrorLine: r.ErrorLine, ErrorBlock: r.ErrorBlock,
		Planned: r.Changes(), Shown: res.Shown, NotReached: r.NotReached,
		Steps: make([]mcpserver.ScriptStep, 0, len(r.Steps)),
	}
	for _, s := range r.Steps {
		out.Steps = append(out.Steps, mcpserver.ScriptStep{
			Kind: string(s.Kind), Block: s.Block, Function: s.Function, Summary: s.Summary, Level: s.Level, Error: s.Error,
		})
	}
	return out, nil
}

// scriptProposals reads the stored proposals by script id.
func (a *App) scriptProposals() map[int64]*scriptProposal {
	out := map[int64]*scriptProposal{}
	var list []*scriptProposal
	if raw := a.stringSetting(settingScriptProposals, ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			a.log.Error("read script proposals", "err", err)
		}
	}
	for _, p := range list {
		out[p.ScriptID] = p
	}
	return out
}

func (a *App) saveScriptProposals(byID map[int64]*scriptProposal) error {
	list := make([]*scriptProposal, 0, len(byID))
	for _, p := range byID {
		list = append(list, p)
	}
	slices.SortFunc(list, func(x, y *scriptProposal) int { return x.At.Compare(y.At) })
	if len(list) > maxScriptProposals {
		list = list[len(list)-maxScriptProposals:]
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return a.store.Set(a.ctx, settingScriptProposals, string(data))
}

// proposalFile is the whole of a proposed script, nothing scrubbed.
func proposalFile(sc *scripting.Script) ([]byte, error) {
	f := scripting.File{
		Format: scripting.FileFormat, Name: sc.Name, Description: sc.Description, Kind: sc.Kind,
		Trigger: sc.Trigger, Permissions: orEmpty(sc.Permissions), Blocks: sc.Blocks, Source: sc.Source,
		CatchUp: sc.CatchUp,
	}
	if sc.Kind == scripting.KindBlocks {
		f.Settings = sc.SettingsSchema
	}
	return json.Marshal(f)
}

func (a *App) storeScriptProposal(id int64, next *scripting.Script) error {
	data, err := proposalFile(next)
	if err != nil {
		return err
	}
	a.proposalMu.Lock()
	defer a.proposalMu.Unlock()
	all := a.scriptProposals()
	all[id] = &scriptProposal{ScriptID: id, File: data, At: time.Now().UTC()}
	return a.saveScriptProposals(all)
}

func (a *App) dropScriptProposal(id int64) {
	a.proposalMu.Lock()
	defer a.proposalMu.Unlock()
	all := a.scriptProposals()
	if all[id] == nil {
		return
	}
	delete(all, id)
	if err := a.saveScriptProposals(all); err != nil {
		a.log.Error("drop script proposal", "script", id, "err", err)
	}
}

// ScriptDiffLineDTO is one line of a proposal's diff. Op is " " for a line
// both versions have, "-" for one only the current has, "+" for one only the
// proposal has.
type ScriptDiffLineDTO struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// ScriptProposalDTO is a change an agent proposed to a script, for the user
// to read before accepting or rejecting it.
type ScriptProposalDTO struct {
	ScriptID int64 `json:"scriptId"`
	// Exists is false when nothing is waiting.
	Exists bool                `json:"exists"`
	When   string              `json:"when"`
	Diff   []ScriptDiffLineDTO `json:"diff"`
	// AddedPermissions and RemovedPermissions compare the declared lists.
	AddedPermissions   []string `json:"addedPermissions"`
	RemovedPermissions []string `json:"removedPermissions"`
	// ResetsApproval says accepting switches the script off and clears its
	// approvals and allowances, and ResetReasons says which changes do that:
	// code, trigger, permissions, triggerParams or placements.
	ResetsApproval bool     `json:"resetsApproval"`
	ResetReasons   []string `json:"resetReasons"`
	// Problem is set when the proposal can no longer be applied, e.g. it
	// names a trigger that is gone; it can still be rejected.
	Problem string `json:"problem"`
}

// loadProposal returns the stored proposal of a script parsed back into a
// script, or nil when there is none.
func (a *App) loadProposal(svc *scripting.Service, id int64) (*scriptProposal, *scripting.Script, error) {
	p := a.scriptProposals()[id]
	if p == nil {
		return nil, nil, nil
	}
	next, err := scripting.ParseFile(svc.Registry(), p.File)
	if err != nil {
		return p, nil, fmt.Errorf("the proposed change no longer fits Pelton's scripting: %w", err)
	}
	next.ID = id
	return p, next, nil
}

// ScriptProposal returns the change an agent proposed to a script, with a
// line diff against the script as it is.
func (a *App) ScriptProposal(id int64) (ScriptProposalDTO, error) {
	out := ScriptProposalDTO{ScriptID: id, Diff: []ScriptDiffLineDTO{}, AddedPermissions: []string{}, RemovedPermissions: []string{}, ResetReasons: []string{}}
	svc, err := a.scriptService()
	if err != nil {
		return out, err
	}
	cur, err := svc.Script(a.ctx, id)
	if err != nil {
		return out, err
	}
	p, next, err := a.loadProposal(svc, id)
	if p == nil {
		return out, nil
	}
	out.Exists, out.When = true, formatDate(p.At)
	if err != nil {
		out.Problem = err.Error()
		return out, nil
	}
	out.Diff = lineDiff(scriptLines(cur), scriptLines(next))
	for _, perm := range next.Permissions {
		if !slices.Contains(cur.Permissions, perm) {
			out.AddedPermissions = append(out.AddedPermissions, perm)
		}
	}
	for _, perm := range cur.Permissions {
		if !slices.Contains(next.Permissions, perm) {
			out.RemovedPermissions = append(out.RemovedPermissions, perm)
		}
	}
	if cur.Enabled || len(cur.Approved) > 0 || len(cur.AllowUnasked) > 0 {
		reasons, err := svc.AssistantChanges(a.ctx, next)
		if err != nil {
			out.Problem = err.Error()
			return out, nil
		}
		out.ResetReasons = orEmpty(reasons)
		out.ResetsApproval = len(reasons) > 0
	}
	return out, nil
}

// AcceptScriptProposal applies a proposed change. When it changes the code,
// the trigger or its parameters, the declared permissions or where the
// script is placed, the update switches the script off and clears its
// approvals and allowances, so the user approves the new version afresh.
func (a *App) AcceptScriptProposal(id int64) (ScriptDTO, error) {
	svc, err := a.scriptService()
	if err != nil {
		return ScriptDTO{}, err
	}
	p, next, err := a.loadProposal(svc, id)
	if err != nil {
		return ScriptDTO{}, err
	}
	if p == nil {
		return ScriptDTO{}, fmt.Errorf("pelton: there is no proposed change to this script")
	}
	out, _, err := svc.UpdateFromAssistant(a.ctx, next)
	if err != nil {
		return ScriptDTO{}, err
	}
	a.dropScriptProposal(id)
	a.recordAgentAction(mcpserver.ToolUpdateScript, 0, fmt.Sprintf("you accepted a proposed change to the script %q", out.Name), nil)
	a.emit(EventAssistantScript, AssistantScriptDTO{ScriptID: id, Name: out.Name, Action: "accepted"})
	return toScriptDTO(out), nil
}

// RejectScriptProposal throws a proposed change away.
func (a *App) RejectScriptProposal(id int64) error {
	if a.scriptProposals()[id] == nil {
		return nil
	}
	a.dropScriptProposal(id)
	a.recordAgentAction(mcpserver.ToolUpdateScript, 0, "you rejected a proposed change to a script", nil)
	a.emit(EventAssistantScript, AssistantScriptDTO{ScriptID: id, Action: "rejected"})
	return nil
}

func mustJSONText(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// scriptLines renders a script as lines for a diff: its settings first, then
// its code.
func scriptLines(sc *scripting.Script) []string {
	lines := []string{
		"name: " + sc.Name,
		"description: " + sc.Description,
		"kind: " + string(sc.Kind),
		"trigger: " + sc.Trigger.Type + " " + mustJSONText(sc.Trigger.Params),
		"permissions: " + strings.Join(sc.Permissions, ", "),
		"catch_up: " + string(sc.CatchUp),
	}
	if sc.Kind == scripting.KindBlocks {
		lines = append(lines, "settings: "+mustJSONText(sc.SettingsSchema), "")
		data, _ := json.MarshalIndent(sc.Blocks, "", "  ")
		return append(lines, strings.Split(string(data), "\n")...)
	}
	lines = append(lines, "")
	return append(lines, strings.Split(strings.TrimRight(sc.Source, "\n"), "\n")...)
}

// maxDiffCells bounds the table lineDiff fills; a larger change is shown as
// everything removed and everything added.
const maxDiffCells = 4 << 20

// lineDiff returns the lines of a and b in order, marking those only one has.
func lineDiff(a, b []string) []ScriptDiffLineDTO {
	var out []ScriptDiffLineDTO
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		out = append(out, ScriptDiffLineDTO{Op: " ", Text: a[pre]})
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if len(ma) == 0 || len(mb) == 0 || len(ma) > maxDiffCells/len(mb) {
		for _, l := range ma {
			out = append(out, ScriptDiffLineDTO{Op: "-", Text: l})
		}
		for _, l := range mb {
			out = append(out, ScriptDiffLineDTO{Op: "+", Text: l})
		}
	} else {
		lcs := make([][]int32, len(ma)+1)
		for i := range lcs {
			lcs[i] = make([]int32, len(mb)+1)
		}
		for i := len(ma) - 1; i >= 0; i-- {
			for j := len(mb) - 1; j >= 0; j-- {
				if ma[i] == mb[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(ma) || j < len(mb) {
			switch {
			case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
				out = append(out, ScriptDiffLineDTO{Op: " ", Text: ma[i]})
				i++
				j++
			case i < len(ma) && (j == len(mb) || lcs[i+1][j] >= lcs[i][j+1]):
				out = append(out, ScriptDiffLineDTO{Op: "-", Text: ma[i]})
				i++
			default:
				out = append(out, ScriptDiffLineDTO{Op: "+", Text: mb[j]})
				j++
			}
		}
	}
	for _, l := range a[len(a)-suf:] {
		out = append(out, ScriptDiffLineDTO{Op: " ", Text: l})
	}
	return out
}
