package scripting

import (
	"cmp"
	"fmt"
	"strings"
)

// ReferenceFormat is how Reference renders the API.
type ReferenceFormat string

// The reference formats.
const (
	ReferenceText     ReferenceFormat = "text"
	ReferenceMarkdown ReferenceFormat = "markdown"
)

// Reference renders the scripting API a catalog describes as a document in
// English: the globals a run has, every module function with its signature,
// what it needs and whether it asks first, the triggers with their event
// fields, the value types, the block actions and conditions, and the
// permissions with their risk. It is built from the catalog alone, so it
// always matches what scripts can use.
func Reference(c Catalog, format ReferenceFormat) string {
	w := &refWriter{md: format == ReferenceMarkdown}
	w.heading(1, "Pelton scripting reference")
	w.para("Scripts are Lua 5.1 run in a sandbox: no files, no network and no OS access except through the modules below, and only with the permissions a script declares and the user approves. Lua string patterns are not available; the text module takes RE2 regular expressions instead.")

	w.heading(2, "Globals")
	w.item("event", "what started the run; its fields depend on the trigger, listed under Triggers")
	w.item("script", "the running script: id, name, trigger, dry_run")
	w.item("stop()", "ends the run at once")
	w.item("print(...)", "writes a line to the run log")
	w.item(`require("name")`, "loads a shared library the user wrote, by name; a library runs with the calling script's permissions and is loaded once per run")
	w.item(`settings.get("name")`, "a value of the script's settings, which a Lua script declares in a SETTINGS table")
	w.end()

	w.heading(2, "Modules")
	for _, m := range c.Modules {
		w.heading(3, m.ID)
		w.para(m.Description)
		for _, f := range m.Functions {
			w.function(m.ID, f)
		}
	}

	w.heading(2, "Triggers")
	for _, t := range c.Triggers {
		w.heading(3, t.ID+" ("+label(t.Meta)+")")
		w.para(t.Description)
		var notes []string
		if t.Permission != "" {
			notes = append(notes, "needs "+t.Permission)
		}
		if t.Mutable {
			notes = append(notes, "the script may change the event")
		}
		if t.Missing != "" {
			notes = append(notes, "not available: "+t.Missing)
		}
		w.para(strings.Join(notes, "; "))
		if len(t.Params) > 0 {
			w.para("Settings: " + paramList(t.Params))
		}
		for _, f := range t.Event {
			w.item("event."+f.Name, fieldType(f)+describe(f.Label))
		}
		w.end()
	}

	w.heading(2, "Value types")
	for _, v := range c.ValueTypes {
		w.heading(3, v.ID+" ("+label(v.Meta)+")")
		w.para(v.Description)
		for _, f := range v.Fields {
			extra := ""
			if fn, ok := v.Lazy[f.Name]; ok {
				extra = ", read on first use through " + fn
			}
			w.item(f.Name, fieldType(f)+extra+describe(f.Label))
		}
		w.end()
	}

	w.heading(2, "Block actions")
	for _, a := range c.Actions {
		w.item(a.ID, blockEntry(a.Meta, a.Params, a.Permissions, a.Risky, a.Missing))
	}
	w.end()

	w.heading(2, "Block conditions")
	for _, x := range c.Conditions {
		w.item(x.ID, blockEntry(x.Meta, x.Params, x.Permissions, false, ""))
	}
	w.end()

	w.heading(2, "Permissions")
	w.para(`A script declares what it needs. A permission with a target narrows it, e.g. "net:example.org" for one web host or "ai:<provider>" for one AI provider.`)
	for _, p := range c.Permissions {
		w.item(p.ID, label(p.Meta)+", "+p.Risk+" risk"+describe(p.Description))
	}
	w.end()
	return strings.TrimRight(w.b.String(), "\n") + "\n"
}

func label(m Meta) string {
	return cmp.Or(m.Label, m.ID)
}

func describe(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

func fieldType(f Field) string {
	if f.Of != "" {
		return f.Type + " of " + f.Of
	}
	return f.Type
}

func paramType(p Param) string {
	switch {
	case p.Type == TypeList && p.Of != "":
		return "list of " + string(p.Of)
	case p.Type == TypeRef && p.ValueType != "":
		return p.ValueType
	case p.Type == TypeEnum:
		vals := make([]string, len(p.Options))
		for i, o := range p.Options {
			vals[i] = o.Value
		}
		return strings.Join(vals, "|")
	}
	return string(p.Type)
}

func paramList(params []Param) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		s := p.Name + ": " + paramType(p)
		if p.Default != nil {
			s += fmt.Sprintf(" = %v", p.Default)
		} else if !p.Required {
			s += "?"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// signature renders a module function's call, e.g. "mail.move(message:
// message?, folder: folder?)" with its results after an arrow.
func signature(module string, f Function) string {
	s := module + "." + f.Name + "(" + paramList(f.Params) + ")"
	if len(f.Returns) > 0 {
		parts := make([]string, len(f.Returns))
		for i, r := range f.Returns {
			parts[i] = r.Name + ": " + fieldType(r)
		}
		s += " -> {" + strings.Join(parts, ", ") + "}"
	}
	return s
}

func blockEntry(m Meta, params []Param, perms []string, risky bool, missing string) string {
	s := label(m) + describe(m.Description)
	if len(params) > 0 {
		s += " Parameters: " + paramList(params) + "."
	}
	if len(perms) > 0 {
		s += " Needs " + strings.Join(perms, ", ") + "."
	}
	if risky {
		s += " Asks first."
	}
	if missing != "" {
		s += " Not available: " + missing + "."
	}
	return s
}

type refWriter struct {
	md     bool
	b      strings.Builder
	inList bool
}

func (w *refWriter) heading(level int, text string) {
	w.end()
	if w.md {
		fmt.Fprintf(&w.b, "%s %s\n\n", strings.Repeat("#", level), text)
		return
	}
	under := map[int]string{1: "=", 2: "-"}[level]
	fmt.Fprintf(&w.b, "%s\n", text)
	if under != "" {
		fmt.Fprintf(&w.b, "%s\n", strings.Repeat(under, len(text)))
	}
	w.b.WriteByte('\n')
}

func (w *refWriter) para(text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	w.end()
	w.b.WriteString(text + "\n\n")
}

func (w *refWriter) item(name, text string) {
	w.inList = true
	if w.md {
		fmt.Fprintf(&w.b, "- `%s` %s\n", name, text)
		return
	}
	fmt.Fprintf(&w.b, "  %s  %s\n", name, text)
}

func (w *refWriter) end() {
	if w.inList {
		w.b.WriteByte('\n')
		w.inList = false
	}
}

func (w *refWriter) function(module string, f Function) {
	sig := signature(module, f)
	if w.md {
		fmt.Fprintf(&w.b, "- `%s`", sig)
	} else {
		fmt.Fprintf(&w.b, "  %s", sig)
	}
	w.inList = true
	var notes []string
	if len(f.Permissions) > 0 {
		notes = append(notes, "needs "+strings.Join(f.Permissions, ", "))
	}
	if f.Risky {
		notes = append(notes, "asks the user first")
	}
	if f.Effect {
		notes = append(notes, "changes something; a test run only plans it")
	}
	text := f.Description
	if len(notes) > 0 {
		text = strings.TrimSpace(text + " (" + strings.Join(notes, "; ") + ")")
	}
	if text != "" {
		if w.md {
			w.b.WriteString(": " + text)
		} else {
			w.b.WriteString("\n      " + text)
		}
	}
	w.b.WriteByte('\n')
	if !w.md {
		w.b.WriteByte('\n')
	}
}
