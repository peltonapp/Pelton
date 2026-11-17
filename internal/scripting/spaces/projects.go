package spaces

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The project permissions.
const (
	PermProjectsRead  = "projects.read"
	PermProjectsWrite = "projects.write"
)

// The project triggers.
const (
	TriggerCardCreated = "projects.card_created"
	TriggerCardMoved   = "projects.card_moved"
)

// Project is a project as scripts see it.
type Project struct {
	ID      int64
	Name    string
	Status  string
	Columns []string
}

// Card is a card on a project board. A card is a task, named by the task's
// uid.
type Card struct {
	TaskID    int64
	UID       string
	Title     string
	Notes     string
	ProjectID int64
	Project   string
	Column    string
	Due       time.Time
	Done      bool
}

// CardInput is a new card. An empty Column is the board's first column.
type CardInput struct {
	ProjectID int64
	Column    string
	Title     string
	Notes     string
	Due       time.Time
}

// ProjectsHost is the project boards a script reads and changes. Columns are
// named by their title, matched ignoring case.
type ProjectsHost interface {
	Projects(ctx context.Context) ([]Project, error)
	Cards(ctx context.Context, projectID int64) ([]Card, error)
	Card(ctx context.Context, uid string) (Card, error)
	CreateCard(ctx context.Context, in CardInput) (Card, error)
	MoveCard(ctx context.Context, uid, column string) (Card, error)
	// LinkMail links a message to a project, or to one of its cards when
	// cardUID is set.
	LinkMail(ctx context.Context, projectID, messageID int64, cardUID string) error
}

// ProjectObject turns a project into the value scripts get.
func ProjectObject(p Project) scripting.Object {
	return scripting.Object{Type: "project", Fields: map[string]any{
		"id": p.ID, "name": p.Name, "status": p.Status, "columns": texts(p.Columns),
	}}
}

// CardObject turns a card into the value scripts get. Its id is the task's,
// so the tasks module works on it too.
func CardObject(c Card) scripting.Object {
	return scripting.Object{Type: "card", Fields: map[string]any{
		"id": c.TaskID, "uid": c.UID, "title": c.Title, "notes": c.Notes, "project_id": c.ProjectID,
		"project": c.Project, "column": c.Column, "due": unix(c.Due), "done": c.Done,
	}}
}

func sampleCard(time.Time) map[string]any {
	return CardObject(Card{Title: "Draft the launch notes", Project: "Website", Column: "Doing"}).Fields
}

// projectArg resolves argument i, a project table, id or name, to an id.
func projectArg(c *scripting.Call, h ProjectsHost, i int) (int64, error) {
	switch v := c.Arg(i).(type) {
	case float64:
		return int64(v), nil
	case map[string]any:
		if id, ok := v["id"].(float64); ok {
			return int64(id), nil
		}
	case string:
		list, err := h.Projects(c.Ctx)
		if err != nil {
			return 0, err
		}
		for _, p := range list {
			if strings.EqualFold(p.Name, strings.TrimSpace(v)) {
				return p.ID, nil
			}
		}
		return 0, fmt.Errorf("%s: no project named %q", c.Function, v)
	}
	return 0, fmt.Errorf("%s: argument %d must be a project, its id or its name", c.Function, i+1)
}

// cardArg reads argument i, a card table or a task uid.
func cardArg(c *scripting.Call, i int) (string, error) {
	switch v := c.Arg(i).(type) {
	case string:
		if v != "" {
			return v, nil
		}
	case map[string]any:
		if uid, ok := v["uid"].(string); ok && uid != "" {
			return uid, nil
		}
	}
	return "", fmt.Errorf("%s: argument %d must be a card or its uid", c.Function, i+1)
}

func projectsEntries(h ProjectsHost) scripting.Entries {
	host := func() (ProjectsHost, error) {
		if h == nil {
			return nil, unavailable("projects")
		}
		return h, nil
	}
	cardField := field("card", "card")
	projectName := param("project", scripting.TypeString)
	column := param("column", scripting.TypeString)
	load := func(c *scripting.PrepareContext, uid string) (scripting.Object, error) {
		hh, err := host()
		if err != nil {
			return scripting.Object{}, err
		}
		card, err := hh.Card(c.Ctx, uid)
		if err != nil {
			return scripting.Object{}, fmt.Errorf("spaces: load card %s: %w", uid, err)
		}
		return CardObject(card), nil
	}
	match := func(params, event map[string]any) bool {
		card := objectFields(event, "card")
		p, _ := card["project"].(string)
		col, _ := card["column"].(string)
		return card != nil && matchText(params, "project", p) && matchText(params, "column", col)
	}

	cardParam := param("card", scripting.TypeRef)
	cardParam.ValueType = "card"
	projectParam := required(param("project", scripting.TypeString))
	msg := param("message", scripting.TypeRef)
	msg.ValueType = "message"
	tbl := param("card", scripting.TypeRef)

	fns := []scripting.Function{
		fnDef("projects", "list", PermProjectsRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			out, err := hh.Projects(c.Ctx)
			if err != nil {
				return nil, err
			}
			return []any{list(out, ProjectObject)}, nil
		}, []scripting.Field{field("projects", "list")}),
		fnDef("projects", "cards", PermProjectsRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := projectArg(c, hh, 0)
			if err != nil {
				return nil, err
			}
			out, err := hh.Cards(c.Ctx, id)
			if err != nil {
				return nil, err
			}
			return []any{list(out, CardObject)}, nil
		}, []scripting.Field{field("cards", "list")}, projectParam),
		withDry(fnDef("projects", "add_card", PermProjectsWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := projectArg(c, hh, 0)
			if err != nil {
				return nil, err
			}
			t, err := tableArg(c, 1, "a title")
			if err != nil {
				return nil, err
			}
			in := CardInput{ProjectID: id, Column: textOf(t, "column"), Title: strings.TrimSpace(textOf(t, "title")), Notes: textOf(t, "notes")}
			if in.Title == "" {
				return nil, fmt.Errorf("%s: a card needs a title", c.Function)
			}
			if in.Due, err = timeValue(c, t["due"]); err != nil {
				return nil, err
			}
			card, err := hh.CreateCard(c.Ctx, in)
			if err != nil {
				return nil, err
			}
			return []any{CardObject(card)}, nil
		}, []scripting.Field{cardField}, projectParam, tbl)),
		fnDef("projects", "move_card", PermProjectsWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			uid, err := cardArg(c, 0)
			if err != nil {
				return nil, err
			}
			col, err := c.String(1)
			if err != nil {
				return nil, err
			}
			_, err = hh.MoveCard(c.Ctx, uid, col)
			return nil, err
		}, nil, cardParam, required(column)),
		fnDef("projects", "link_mail", PermProjectsWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := projectArg(c, hh, 0)
			if err != nil {
				return nil, err
			}
			msgID, err := idOf(c, 1, "a message")
			if err != nil {
				return nil, err
			}
			uid := ""
			if c.Arg(2) != nil {
				if uid, err = cardArg(c, 2); err != nil {
					return nil, err
				}
			}
			return nil, hh.LinkMail(c.Ctx, id, msgID, uid)
		}, nil, projectParam, msg, cardParam),
	}

	cardRef := scripting.RefParam("card", "card", "event.card")
	msgRef := scripting.RefParam("message", "message", "event.message")
	conds := []scripting.Condition{
		scripting.TextCondition(meta("cond", "projects.card_title", "projects"), "{{card}}.title", cardRef),
		scripting.TextCondition(meta("cond", "projects.column", "projects"), "{{card}}.column", cardRef),
		scripting.TextCondition(meta("cond", "projects.project", "projects"), "{{card}}.project", cardRef),
	}
	title := required(param("title", scripting.TypeString))
	act := func(id, lua string, out []scripting.Field, params ...scripting.Param) scripting.Action {
		return scripting.Action{Meta: meta("action", id, "projects"), Params: params, Outputs: out, Permission: PermProjectsWrite, Lua: lua}
	}
	actions := []scripting.Action{
		act("projects.add_card", "projects.add_card({{project}}, {title = {{title}}, column = {{column}}, notes = {{notes}}})",
			[]scripting.Field{field("id", "number"), field("uid", "string")}, projectParam, title, column, param("notes", scripting.TypeText)),
		act("projects.move_card", "projects.move_card({{card}}, {{column}})", nil, cardRef, required(column)),
		act("projects.link_mail", "projects.link_mail({{project}}, {{message}})", nil, projectParam, msgRef),
	}

	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermProjectsRead, "projects"), Risk: "low"},
			{Meta: meta("perm", PermProjectsWrite, "projects"), Risk: "medium"},
		},
		ValueTypes: []scripting.ValueType{
			{Meta: meta("type", "project", "projects"), Fields: fields("id:number", "name:string", "status:string", "columns:list:string")},
			{Meta: meta("type", "card", "projects"), Sample: sampleCard, Fields: fields(
				"id:number", "uid:string", "title:string", "notes:text", "project_id:number", "project:string",
				"column:string", "due:number", "done:bool")},
		},
		Triggers: []scripting.Trigger{
			{
				Meta: meta("trigger", TriggerCardCreated, "projects"), Params: []scripting.Param{projectName, column},
				Event: []scripting.Field{cardField}, Permission: PermProjectsRead,
				Prepare: prepareObject("card", "card_uid", load), Match: match,
			},
			{
				Meta: meta("trigger", TriggerCardMoved, "projects"), Params: []scripting.Param{projectName, column},
				Event: []scripting.Field{cardField, field("from_column", "string")}, Permission: PermProjectsRead,
				Prepare: prepareObject("card", "card_uid", load), Match: match,
			},
		},
		Modules:    []scripting.Module{{Meta: meta("module", "projects", "projects"), Functions: fns}},
		Conditions: conds,
		Actions:    actions,
		Placements: []scripting.Placement{menu("projects.card", "projects", "card", PermProjectsRead, load)},
	}
}
