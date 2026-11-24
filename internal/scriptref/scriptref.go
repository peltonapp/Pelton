// Package scriptref renders the scripting registry as a plain text reference
// for someone writing a script without the editor in front of them, such as an
// assistant connected over MCP. Everything it lists comes from the live
// registry, so a trigger, module or action another package registers shows up
// without an edit here. The fixed parts (the block tree format, the SETTINGS
// convention, limits, the file format) describe the scripting package's own
// behaviour.
package scriptref

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// Section is one part of the reference, fetched on its own so a reader need
// not load the whole thing.
type Section struct {
	ID    string
	Title string
	Body  string
}

// Entry is one searchable item: a function, trigger, condition, action,
// value type, permission, placement, or a section's introduction.
type Entry struct {
	Section string `json:"section"`
	ID      string `json:"id"`
	Text    string `json:"text"`
}

// Reference is a rendered registry.
type Reference struct {
	sections []Section
	entries  []Entry
}

// builder collects one section's introduction and entries.
type builder struct {
	section string
	title   string
	intro   string
	entries []Entry
}

func (b *builder) add(id, text string) {
	b.entries = append(b.entries, Entry{Section: b.section, ID: id, Text: strings.TrimRight(text, "\n")})
}

func (b *builder) done() (Section, []Entry) {
	var body strings.Builder
	fmt.Fprintf(&body, "## %s\n\n", b.title)
	if b.intro != "" {
		body.WriteString(strings.TrimSpace(b.intro))
		body.WriteString("\n\n")
	}
	for _, e := range b.entries {
		body.WriteString(e.Text)
		body.WriteString("\n\n")
	}
	all := b.entries
	if b.intro != "" {
		all = append([]Entry{{Section: b.section, ID: b.section, Text: strings.TrimSpace(b.intro)}}, all...)
	}
	return Section{ID: b.section, Title: b.title, Body: strings.TrimRight(body.String(), "\n") + "\n"}, all
}

// Build renders r. The English text comes from the entries themselves, which
// registration fills from the scripting package's English catalog.
func Build(r *scripting.Registry) *Reference {
	cat := r.Catalog()
	ref := &Reference{}
	for _, f := range []func(*builder, scripting.Catalog){
		overview, luaSection, modules, triggers, conditions, actions, valueTypes,
		permissions, blocksSection, settingsSection, uiSection, placements, limitsSection, formatSection,
	} {
		b := &builder{}
		f(b, cat)
		s, entries := b.done()
		ref.sections = append(ref.sections, s)
		ref.entries = append(ref.entries, entries...)
	}
	return ref
}

// Sections lists the sections in order, without their bodies.
func (ref *Reference) Sections() []Section {
	out := make([]Section, 0, len(ref.sections))
	for _, s := range ref.sections {
		out = append(out, Section{ID: s.ID, Title: s.Title})
	}
	return out
}

// Section returns one section with its body.
func (ref *Reference) Section(id string) (Section, bool) {
	for _, s := range ref.sections {
		if s.ID == id {
			return s, true
		}
	}
	return Section{}, false
}

// Text returns the whole reference.
func (ref *Reference) Text() string {
	var b strings.Builder
	b.WriteString("# Pelton scripting reference\n\n")
	for _, s := range ref.sections {
		b.WriteString(s.Body)
		b.WriteString("\n")
	}
	return b.String()
}

// Search returns up to limit entries containing every word of query, ignoring
// case, best first: a hit in the entry's id ranks above one in its text only.
func (ref *Reference) Search(query string, limit int) []Entry {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	type hit struct {
		e     Entry
		score int
		order int
	}
	var hits []hit
	for i, e := range ref.entries {
		id, body := strings.ToLower(e.ID), strings.ToLower(e.Text)
		score := 0
		matched := true
		for _, w := range words {
			switch {
			case id == w:
				score += 4
			case strings.Contains(id, w):
				score += 2
			case strings.Contains(body, w):
				score++
			default:
				matched = false
			}
			if !matched {
				break
			}
		}
		if matched {
			hits = append(hits, hit{e, score, i})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Entry, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.e)
	}
	return out
}

func (b *builder) label(m scripting.Meta) string { return m.Label }

func (b *builder) desc(m scripting.Meta) string { return m.Description }

// heading names an entry with its label when there is one.
func (b *builder) heading(id string, m scripting.Meta) string {
	if l := b.label(m); l != "" && l != id {
		return fmt.Sprintf("### %s (%s)", id, l)
	}
	return "### " + id
}

func literal(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func (b *builder) param(p scripting.Param) string {
	typ := string(p.Type)
	if p.Of != "" {
		typ += " of " + string(p.Of)
	}
	if p.ValueType != "" {
		typ += " to a " + p.ValueType
	}
	var notes []string
	if p.Required {
		notes = append(notes, "required")
	} else {
		notes = append(notes, "optional")
	}
	if p.Default != nil {
		notes = append(notes, "default "+literal(p.Default))
	}
	if p.DefaultRef != "" {
		notes = append(notes, "default ref "+p.DefaultRef)
	}
	if len(p.Options) > 0 {
		vals := make([]string, 0, len(p.Options))
		for _, o := range p.Options {
			vals = append(vals, o.Value)
		}
		notes = append(notes, "one of "+strings.Join(vals, ", "))
	}
	line := fmt.Sprintf("- %s: %s; %s", p.Name, typ, strings.Join(notes, "; "))
	label := p.Label
	if strings.EqualFold(label, p.Name) || strings.EqualFold(label, string(p.Type)) {
		label = ""
	}
	if extra := strings.TrimSpace(strings.Join(nonEmpty(label, p.Description), ". ")); extra != "" {
		line += ". " + extra
	}
	return line
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func (b *builder) params(title string, ps []scripting.Param) string {
	if len(ps) == 0 {
		return ""
	}
	lines := []string{title + ":"}
	for _, p := range ps {
		lines = append(lines, b.param(p))
	}
	return strings.Join(lines, "\n") + "\n"
}

func (b *builder) fields(title string, fs []scripting.Field) string {
	if len(fs) == 0 {
		return ""
	}
	lines := []string{title + ":"}
	for _, f := range fs {
		typ := f.Type
		if f.Of != "" {
			typ += " of " + f.Of
		}
		line := fmt.Sprintf("- %s: %s", f.Name, typ)
		if f.Label != "" {
			line += ". " + f.Label
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

func paragraph(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimRight(p, "\n"); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

func overview(b *builder, _ scripting.Catalog) {
	b.section, b.title = "overview", "Overview"
	b.intro = `A script is an automation the user runs in Pelton. It is written either in Lua (kind "lua", a "source" string) or as a block tree (kind "blocks", a JSON tree that Pelton compiles to Lua). A trigger starts it: mail arriving, a schedule, the user running it by hand, and so on (see "triggers").

A script declares the permissions it needs (see "permissions"). Every host function and trigger checks its permission at run time: a call needing a permission the script did not declare fails the run. The user must approve every declared permission before switching a script on.

Functions marked risky ask the user before each call unless the user allows that function for that script. Functions marked as changes are only recorded as planned steps in a dry run, which is how a test run works: reads work, nothing is changed.

Lua scripts declare their settings in a SETTINGS table (see "settings"); block scripts declare a settings schema. Manual scripts can be offered in menus, the palette, toolbars and shortcuts through placements, which the user sets up in Pelton.

Scripts an assistant creates or changes arrive switched off, marked as written by an assistant, for the user to review. An assistant cannot switch a script on, approve permissions, allow a risky function without asking, or run a script for real.`
}

func luaSection(b *builder, _ scripting.Catalog) {
	b.section, b.title = "lua", "Lua environment"
	b.intro = `Scripts run in Lua 5.1 (gopher-lua) in a sandbox. Available standard globals: assert, error, ipairs, next, pairs, pcall, rawequal, rawget, rawset, select, setmetatable, getmetatable, tonumber, tostring, type, unpack, xpcall, _G, _VERSION, and the string, table and math libraries. There is no os, io, debug, load, loadstring, dofile or coroutine. require("name") loads a shared library the user wrote, by its name written out in the code; it runs with the calling script's permissions, once per run.

string.dump is removed. string.find only searches plain text (pass true as the 4th argument). string.match, gmatch, gfind and gsub are refused: Lua patterns are not available; use text.match, text.find_all and text.replace, which take RE2 regular expressions. string.rep and string.format results are capped.

Globals a run gets:
- event: the event table of the trigger (see "triggers" for its fields). Values of a value type are tables; their lazy fields (e.g. a message's body) are fetched on first use and need the fetching function's permission.
- script: { id, name, trigger, dry_run }.
- stop(): ends the run as stopped.
- print(...): writes an info line to the run log.
- vars, steps: used by compiled block scripts.
- SETTINGS: see "settings"; read the user's values with the settings module.
- Every module in "modules" as a global table, e.g. mail.move(msg, "Archive").

Errors: a Lua error ends the run with status error and the line it happened on. pcall and xpcall work but cannot catch a stop, a timeout, a limit or a permission denial.`
}

func modules(b *builder, cat scripting.Catalog) {
	b.section, b.title = "modules", "Lua modules and functions"
	b.intro = `Each module is a global table of host functions. Arguments are positional in the order listed. "ref to a message" means a message table (e.g. event.message); a message id number also works where the function takes a message.`
	for _, m := range cat.Modules {
		head := fmt.Sprintf("### module %s", m.ID)
		if l := b.label(m.Meta); l != "" {
			head += " (" + l + ")"
		}
		b.add(m.ID, paragraph(head, b.desc(m.Meta)))
		for _, f := range m.Functions {
			full := m.ID + "." + f.Name
			names := make([]string, 0, len(f.Params))
			for _, p := range f.Params {
				names = append(names, p.Name)
			}
			var flags []string
			if f.Permission != "" {
				flags = append(flags, "needs permission "+f.Permission)
			}
			if f.Effect {
				flags = append(flags, "changes something (a dry run records it as planned)")
			}
			if f.Risky {
				flags = append(flags, "risky: asks the user first unless allowed for this script")
			}
			flagLine := ""
			if len(flags) > 0 {
				flagLine = strings.ToUpper(flags[0][:1]) + flags[0][1:]
				if len(flags) > 1 {
					flagLine += "; " + strings.Join(flags[1:], "; ")
				}
				flagLine += "."
			}
			b.add(full, paragraph(
				fmt.Sprintf("#### %s(%s)", full, strings.Join(names, ", ")),
				f.Description,
				b.params("Parameters", f.Params),
				b.fields("Returns", f.Returns),
				flagLine,
			))
		}
	}
}

func triggers(b *builder, cat scripting.Catalog) {
	b.section, b.title = "triggers", "Triggers"
	b.intro = `A script has one trigger: {"type": "<id>", "params": {...}}. The event fields listed are the keys of the global event table. A trigger with a permission needs that permission declared.`
	for _, t := range cat.Triggers {
		var notes []string
		if t.Permission != "" {
			notes = append(notes, "Needs permission "+t.Permission+".")
		}
		if t.Mutable {
			notes = append(notes, "The script may change the event and Pelton reads it back.")
		}
		if t.Missing != "" {
			notes = append(notes, "Not available in this app: "+t.Missing+".")
		}
		b.add(t.ID, paragraph(
			b.heading(t.ID, t.Meta),
			b.desc(t.Meta),
			b.params("Params", t.Params),
			b.fields("Event fields", t.Event),
			strings.Join(notes, " "),
		))
	}
}

func conditions(b *builder, cat scripting.Catalog) {
	b.section, b.title = "conditions", "Conditions (block scripts)"
	b.intro = `Conditions are used in the "when" of an if block: {"kind": "condition", "type": "<id>", "args": {...}}. A parameter with a default ref (e.g. message, default ref event.message) can be left out.`
	for _, c := range cat.Conditions {
		perm := ""
		if c.Permission != "" {
			perm = "Needs permission " + c.Permission + "."
		}
		b.add(c.ID, paragraph(
			b.heading(c.ID, c.Meta),
			b.desc(c.Meta),
			b.params("Args", c.Params),
			"Compiles to: "+c.Lua,
			perm,
		))
	}
}

func actions(b *builder, cat scripting.Catalog) {
	b.section, b.title = "actions", "Actions (block scripts)"
	b.intro = `Actions are blocks: {"id": "b1", "kind": "action", "type": "<id>", "args": {...}}. Outputs can be referred to by later blocks as step:<block id>.<field>.`
	for _, a := range cat.Actions {
		var notes []string
		if a.Permission != "" {
			notes = append(notes, "Needs permission "+a.Permission+".")
		}
		if a.Risky {
			notes = append(notes, "Risky: asks the user first unless allowed.")
		}
		if len(a.Triggers) > 0 {
			notes = append(notes, "Only for scripts started by "+strings.Join(a.Triggers, ", ")+".")
		}
		if a.Missing != "" {
			notes = append(notes, "Not available in this app: "+a.Missing+".")
		}
		b.add(a.ID, paragraph(
			b.heading(a.ID, a.Meta),
			b.desc(a.Meta),
			b.params("Args", a.Params),
			b.fields("Outputs", a.Outputs),
			"Compiles to: "+a.Lua,
			strings.Join(notes, " "),
		))
	}
}

func valueTypes(b *builder, cat scripting.Catalog) {
	b.section, b.title = "values", "Value types"
	b.intro = "Parameter types: " + joinTypes(cat.ParamTypes) + `. Strings are text; numbers are Lua numbers; dates are Unix seconds; durations are "15m", "2h", "3d", "1w" or seconds; times of day are "HH:MM"; a folder is a path or a role (inbox, archive, trash, junk, sent, drafts); colours are 0 (none) to 8.

Structured values a script handles are tables with the fields below.`
	for _, v := range cat.ValueTypes {
		lazy := ""
		if len(v.Lazy) > 0 {
			keys := make([]string, 0, len(v.Lazy))
			for k := range v.Lazy {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+" (via "+v.Lazy[k]+")")
			}
			lazy = "Fetched on first use: " + strings.Join(parts, ", ") + "."
		}
		b.add(v.ID, paragraph(b.heading(v.ID, v.Meta), b.desc(v.Meta), b.fields("Fields", v.Fields), lazy))
	}
}

func joinTypes(ts []scripting.ParamType) string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, string(t))
	}
	return strings.Join(out, ", ")
}

func permissions(b *builder, cat scripting.Catalog) {
	b.section, b.title = "permissions", "Permissions"
	b.intro = `Declare permissions as a list of ids, e.g. ["mail.read", "mail.modify"]. Some take a target after a colon, such as a host for network access ("net:example.org"); the function then checks its own target. Risk is how strongly Pelton warns the user when they approve it. Declare only what the script uses.`
	for _, p := range cat.Permissions {
		b.add(p.ID, paragraph(b.heading(p.ID, p.Meta), b.desc(p.Meta), "Risk: "+p.Risk+"."))
	}
}

// BlockExample is a worked block tree for a script started by mail.arrived.
const BlockExample = `{
  "version": 1,
  "blocks": [
    {"id": "b1", "kind": "if", "branches": [
      {"when": {"kind": "all", "items": [
        {"kind": "condition", "type": "mail.from", "args": {"op": {"value": "contains"}, "value": {"value": "billing"}}},
        {"kind": "not", "items": [
          {"kind": "condition", "type": "mail.subject", "args": {"op": {"value": "matches"}, "value": {"value": "(?i)reminder"}}}
        ]}
      ]}, "body": [
        {"id": "b2", "kind": "action", "type": "mail.move", "args": {"folder": {"ref": "setting:folder"}}},
        {"id": "b3", "kind": "action", "type": "mail.color", "args": {"color": {"value": 3}}}
      ]}
    ], "else": [
      {"id": "b4", "kind": "stop"}
    ]},
    {"id": "b5", "kind": "set", "as": "who", "value": {"ref": "event.message.from"}},
    {"id": "b6", "kind": "foreach", "as": "addr", "list": {"value": ["x@example.org", "y@example.org"]}, "body": [
      {"id": "b7", "kind": "action", "type": "log.write", "args": {"text": {"ref": "var:addr"}}}
    ]},
    {"id": "b8", "kind": "lua", "code": "print(vars.who)"}
  ]
}`

// BlockExampleSettings is the settings schema BlockExample refers to.
const BlockExampleSettings = `[{"name": "folder", "type": "folder", "label": "Target folder", "default": "Receipts"}]`

func blocksSection(b *builder, _ scripting.Catalog) {
	b.section, b.title = "blocks", "Block tree format"
	b.intro = `A block script's "blocks" is a JSON tree:

{"version": 1, "blocks": [<block>, ...]}

A block has an "id" (1 to 64 of A-Z a-z 0-9 _ -, unique in the tree) and a "kind":
- "action": {"id", "kind": "action", "type": "<action id>", "args": {"<param>": <arg>}}
- "if": {"id", "kind": "if", "branches": [{"when": <cond>, "body": [<block>...]}, ...], "else": [<block>...]}. The first branch is the if, the rest are else-ifs.
- "foreach": {"id", "kind": "foreach", "list": <arg>, "as": "<var name>", "body": [<block>...]}
- "set": {"id", "kind": "set", "as": "<var name>", "value": <arg>}
- "stop": {"id", "kind": "stop"} ends the run.
- "lua": {"id", "kind": "lua", "code": "<Lua>"} runs a piece of Lua.

A cond is {"kind": "condition", "type": "<condition id>", "args": {...}}, or a group {"kind": "all"|"any", "items": [<cond>...]}, or {"kind": "not", "items": [<one cond>]}.

An arg is a literal {"value": <json value>} or a reference {"ref": "<ref>"}. References: "event.<field>" and deeper paths such as "event.message.from"; "var:<name>" for a variable set by a set or foreach block; "step:<block id>.<output>" for an earlier action's output; "setting:<name>" for a script setting.

Worked example, for a script with trigger mail.arrived, permissions ["mail.read", "mail.modify"] and settings ` + BlockExampleSettings + `:

` + BlockExample + `

It moves mail from a billing sender whose subject does not match "reminder" to the folder in the setting and colours it yellow, stops for anything else, then logs two addresses and prints the sender. Validate a tree before saving it: errors name the block id.`
}

func settingsSection(b *builder, cat scripting.Catalog) {
	b.section, b.title = "settings", "Script settings"
	b.intro = `Settings let the user configure a script without editing it. Setting types: ` + joinTypes(cat.SettingTypes) + `. Settings are stored in plain text: never use one for a password or key.

A Lua script declares them in a top level SETTINGS table written out as constants (no calls or variables):

SETTINGS = {
  { name = "folder", type = "folder", label = "Target folder", default = "Archive" },
  { name = "mode", type = "enum", options = { "fast", "slow" }, default = "fast" },
}

Each entry takes name, type, label, description, default, required and, for an enum, options as values or { value = ..., label = ... } tables. Read a value with settings.get("folder"); it falls back to the default.

A block script declares a settings schema instead: a list of {"name", "type", "label", "description", "default", "required", "options": [{"value", "label"}]}, and refers to a setting as {"ref": "setting:<name>"}.

The user fills in the values in Pelton. An assistant never sees them.`
}

func uiSection(b *builder, cat scripting.Catalog) {
	b.section, b.title = "ui", "Asking the user and showing results"
	b.intro = `The ui module (see "modules") asks the user questions while a script runs and shows longer output in a result panel. ui.ask(kind, prompt, options) asks a question of one of the kinds below; ui.text, ui.textarea, ui.choose, ui.confirm, ui.folder, ui.account and ui.datetime are shorthands. Options may hold "default" and "timeout" (seconds). A question of a run the user did not start times out and takes its default, or stops the run when it has none. A cancelled question ends the run as cancelled.

ui.show(text, { title = "...", format = "text" | "markdown" }) shows a result. Result panel buttons are offered by Pelton, not chosen by the script.`
	for _, k := range cat.InputKinds {
		b.add("input."+k.ID, paragraph(b.heading("question kind "+k.ID, k.Meta), b.desc(k.Meta), "Answer type: "+k.Value+".", b.params("Options", k.Params)))
	}
	for _, a := range cat.ResultActions {
		perm := ""
		if a.Permission != "" {
			perm = "Offered when the script holds " + a.Permission + "."
		}
		b.add("result."+a.ID, paragraph(b.heading("result button "+a.ID, a.Meta), b.desc(a.Meta), perm))
	}
}

func placements(b *builder, cat scripting.Catalog) {
	b.section, b.title = "placements", "Placements"
	b.intro = `A script with the manual trigger can be offered at places in the app: menus, the command palette, toolbars, keyboard shortcuts and mail row actions. The place hands the script the value it was used on as the manual trigger's event (event.message, or event.messages for a selection). The user chooses placements in Pelton; an assistant cannot set them. Write a manual script so it works with event.message and, where a place hands a selection, with event.messages.`
	for _, p := range cat.Placements {
		var notes []string
		notes = append(notes, "Surface: "+p.Surface+".")
		if p.ValueType != "" {
			v := "Hands over a " + p.ValueType
			if p.List != "" {
				v += ", or a selection in event." + p.List
			}
			if p.Optional {
				v += ", when there is one"
			}
			notes = append(notes, v+".")
		}
		if p.Permission != "" {
			notes = append(notes, "Needs "+p.Permission+" approved.")
		}
		b.add("place."+p.ID, paragraph(b.heading("place "+p.ID, p.Meta), b.desc(p.Meta), strings.Join(notes, " "), b.params("Options", p.Options)))
	}
}

func limitsSection(b *builder, _ scripting.Catalog) {
	l := scripting.DefaultLimits()
	b.section, b.title = "limits", "Limits"
	b.intro = fmt.Sprintf(`- Time: a run may take %s of wall clock time; waiting for the user does not count.
- Memory: a run that grows the heap by more than %d MiB is stopped.
- Steps: at most %d recorded steps (host calls, checks, log lines); more stops the run with status limit.
- Log: at most %d KiB of log text per run; later lines are dropped.
- Call depth: %d nested Lua calls.
- Chains: scripts set off by events other scripts caused stop after %d levels.
- Regular expressions are RE2 (Go regexp syntax): no backreferences or lookaround. Patterns are capped in length and number per run.
- No file system, process, network or OS access except through the modules listed, each behind its permission.
- Run statuses: ok, stopped, error, denied (missing permission or the user said no), timeout, limit, skipped, cancelled.`,
		l.Timeout, l.Memory>>20, l.Steps, l.LogBytes>>10, l.CallStack, scripting.MaxDepth)
}

func formatSection(b *builder, _ scripting.Catalog) {
	b.section, b.title = "format", "The .peltonscript file format"
	b.intro = fmt.Sprintf(`A script exported from Pelton is a %s file: JSON with what the script is and does, and nothing about who used it (no account ids, setting values, approvals or run history). Fields: format (%d), name, description, kind ("lua" or "blocks"), trigger {type, params}, permissions [...], blocks (block scripts) or source (Lua scripts), settings (the settings schema), catchUp ("live", "run" or "ask", for mail that arrived while Pelton was closed), placements [{id, options}].

An imported script arrives switched off with nothing approved.`, scripting.FileExtension, scripting.FileFormat)
}
