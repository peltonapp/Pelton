package spaces

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The note permissions.
const (
	PermNotesRead  = "notes.read"
	PermNotesWrite = "notes.write"
)

// TriggerNoteSaved fires when a note in the notes space is saved.
const TriggerNoteSaved = "notes.saved"

// maxNoteBytes caps the text a script writes to a note in one call.
const maxNoteBytes = 1 << 20

// Note is a note of the notes space, named by its path in the vault.
type Note struct {
	Path     string
	Title    string
	Content  string
	Modified time.Time
}

// NotesHost is the notes space a script reads and writes. Paths are
// relative to the vault, with "/" between folders.
type NotesHost interface {
	Note(ctx context.Context, path string) (Note, error)
	// SearchNotes returns the notes matching query, without their content.
	SearchNotes(ctx context.Context, query string, limit int) ([]Note, error)
	// CreateNote creates a note, at a free path near path when it is taken.
	CreateNote(ctx context.Context, path, content string) (Note, error)
	// AppendNote adds text to the end of a note, creating it when missing.
	AppendNote(ctx context.Context, path, text string) (Note, error)
	// DailyNotePath is the path of a day's daily note, as the notes space
	// names it.
	DailyNotePath(ctx context.Context, day time.Time) (string, error)
}

// NoteObject turns a note into the value scripts get.
func NoteObject(n Note) scripting.Object {
	return scripting.Object{Type: "note", Fields: map[string]any{
		"path": n.Path, "title": n.Title, "content": n.Content, "modified": unix(n.Modified),
	}}
}

func sampleNote(now time.Time) map[string]any {
	return NoteObject(Note{Path: "Projects/Website.md", Title: "Website", Content: "# Website\n\n- launch notes\n", Modified: now}).Fields
}

// notePath reads argument i as a note path in the vault, adding ".md" when
// it has no extension.
func notePath(c *scripting.Call, i int) (string, error) {
	p, err := c.String(i)
	if err != nil {
		return "", err
	}
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || strings.Contains(p, ":") {
		return "", fmt.Errorf("%s: give a note path such as \"Ideas/today.md\"", c.Function)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", fmt.Errorf("%s: a path cannot leave the notes folder", c.Function)
		}
	}
	p = path.Clean(p)
	if path.Ext(p) == "" {
		p += ".md"
	}
	return p, nil
}

func noteText(c *scripting.Call, i int) (string, error) {
	s, err := c.OptString(i, "")
	if err != nil {
		return "", err
	}
	if len(s) > maxNoteBytes {
		return "", fmt.Errorf("%s: at most %d bytes at a time", c.Function, maxNoteBytes)
	}
	return s, nil
}

func notesEntries(h NotesHost) scripting.Entries {
	host := func() (NotesHost, error) {
		if h == nil {
			return nil, unavailable("notes")
		}
		return h, nil
	}
	noteField := field("note", "note")
	pathParam := required(param("path", scripting.TypeString))
	text := scripting.Required(param("text", scripting.TypeText))
	written := func(n Note, err error) ([]any, error) {
		if err != nil {
			return nil, err
		}
		n.Content = ""
		return []any{NoteObject(n)}, nil
	}
	dryNote := func(c *scripting.Call) ([]any, error) {
		p, _ := c.Arg(0).(string)
		return []any{map[string]any{"path": p}}, nil
	}

	fns := []scripting.Function{
		fnDef("notes", "read", PermNotesRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			p, err := notePath(c, 0)
			if err != nil {
				return nil, err
			}
			n, err := hh.Note(c.Ctx, p)
			if err != nil {
				return []any{nil, err.Error()}, nil
			}
			return []any{NoteObject(n)}, nil
		}, []scripting.Field{noteField}, pathParam),
		fnDef("notes", "search", PermNotesRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			q, err := c.String(0)
			if err != nil {
				return nil, err
			}
			n, err := limitArg(c, 1)
			if err != nil {
				return nil, err
			}
			out, err := hh.SearchNotes(c.Ctx, q, n)
			if err != nil {
				return nil, err
			}
			return []any{list(out, NoteObject)}, nil
		}, []scripting.Field{field("notes", "list")}, required(param("query", scripting.TypeString)), param("limit", scripting.TypeNumber)),
		fnDef("notes", "daily_path", PermNotesRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			day, err := timeValue(c, c.Arg(0))
			if err != nil {
				return nil, err
			}
			if day.IsZero() {
				day = c.Now()
			}
			p, err := hh.DailyNotePath(c.Ctx, day)
			if err != nil {
				return nil, err
			}
			return []any{p}, nil
		}, []scripting.Field{field("path", "string")}, param("day", scripting.TypeString)),
		dryAs(fnDef("notes", "create", PermNotesWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			p, err := notePath(c, 0)
			if err != nil {
				return nil, err
			}
			body, err := noteText(c, 1)
			if err != nil {
				return nil, err
			}
			n, err := hh.CreateNote(c.Ctx, p, body)
			return written(n, err)
		}, []scripting.Field{noteField}, pathParam, text), dryNote),
		dryAs(fnDef("notes", "append", PermNotesWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			p, err := notePath(c, 0)
			if err != nil {
				return nil, err
			}
			body, err := noteText(c, 1)
			if err != nil {
				return nil, err
			}
			n, err := hh.AppendNote(c.Ctx, p, body)
			return written(n, err)
		}, []scripting.Field{noteField}, pathParam, text), dryNote),
		dryAs(fnDef("notes", "append_daily", PermNotesWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			body, err := noteText(c, 0)
			if err != nil {
				return nil, err
			}
			p, err := hh.DailyNotePath(c.Ctx, c.Now())
			if err != nil {
				return nil, err
			}
			n, err := hh.AppendNote(c.Ctx, p, body)
			return written(n, err)
		}, []scripting.Field{noteField}, text), func(*scripting.Call) ([]any, error) {
			return []any{map[string]any{"path": ""}}, nil
		}),
	}

	noteRef := scripting.RefParam("note", "note", "event.note")
	conds := []scripting.Condition{
		scripting.TextCondition(meta("cond", "notes.title", "notes"), "{{note}}.title", noteRef),
		scripting.TextCondition(meta("cond", "notes.path", "notes"), "{{note}}.path", noteRef),
	}
	act := func(id, lua string, params ...scripting.Param) scripting.Action {
		return scripting.Action{Meta: meta("action", id, "notes"), Params: params, Permission: PermNotesWrite,
			Outputs: []scripting.Field{field("path", "string")}, Lua: lua}
	}
	actions := []scripting.Action{
		act("notes.create", "notes.create({{path}}, {{text}})", pathParam, text),
		act("notes.append", "notes.append({{path}}, {{text}})", pathParam, text),
		act("notes.append_daily", "notes.append_daily({{text}})", text),
	}

	folder := param("folder", scripting.TypeString)
	loadNote := func(c *scripting.PrepareContext, p string) (scripting.Object, error) {
		hh, err := host()
		if err != nil {
			return scripting.Object{}, err
		}
		n, err := hh.Note(c.Ctx, p)
		if err != nil {
			return scripting.Object{}, fmt.Errorf("spaces: load note %s: %w", p, err)
		}
		return NoteObject(n), nil
	}
	return scripting.Entries{
		Placements: []scripting.Placement{menu("notes.note", "notes", "note", PermNotesRead, loadNote)},
		ResultActions: []scripting.ResultAction{{
			Meta: meta("result", "notes.save", "notes"), Icon: "note", Permission: PermNotesWrite,
			Run: func(ctx context.Context, r scripting.Result) (string, error) {
				hh, err := host()
				if err != nil {
					return "", err
				}
				name := clipTitle(strings.Map(func(r rune) rune {
					if strings.ContainsRune(`/\:*?"<>|`, r) {
						return '-'
					}
					return r
				}, r.Title), r.ScriptName)
				n, err := hh.CreateNote(ctx, name+".md", r.Text)
				if err != nil {
					return "", err
				}
				return n.Path, nil
			},
		}},
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermNotesRead, "notes"), Risk: "low"},
			{Meta: meta("perm", PermNotesWrite, "notes"), Risk: "medium"},
		},
		ValueTypes: []scripting.ValueType{{Meta: meta("type", "note", "notes"), Sample: sampleNote,
			Fields: fields("path:string", "title:string", "content:text", "modified:number")}},
		Triggers: []scripting.Trigger{{
			Meta: meta("trigger", TriggerNoteSaved, "notes"), Params: []scripting.Param{folder},
			Event: []scripting.Field{noteField}, Permission: PermNotesRead,
			Prepare: prepareObject("note", "note_path", loadNote),
			Match: func(params, event map[string]any) bool {
				n := objectFields(event, "note")
				p, _ := n["path"].(string)
				want, _ := params["folder"].(string)
				want = strings.Trim(strings.TrimSpace(want), "/")
				return n != nil && (want == "" || strings.HasPrefix(strings.ToLower(p), strings.ToLower(want)+"/"))
			},
		}},
		Modules:    []scripting.Module{{Meta: meta("module", "notes", "notes"), Functions: fns}},
		Conditions: conds,
		Actions:    actions,
	}
}

// dryAs gives a call its own dry run answer.
func dryAs(f scripting.Function, dry scripting.Handler) scripting.Function {
	f.Dry = dry
	return f
}
