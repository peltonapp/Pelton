package spaces

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The task permissions.
const (
	PermTasksRead  = "tasks.read"
	PermTasksWrite = "tasks.write"
)

// The task triggers.
const (
	TriggerTaskCreated   = "tasks.created"
	TriggerTaskCompleted = "tasks.completed"
	TriggerTaskOverdue   = "tasks.overdue"
)

// Task is a task as scripts see it.
type Task struct {
	ID         int64
	ListID     int64
	List       string
	UID        string
	Title      string
	Notes      string
	Due        time.Time
	Done       bool
	Completed  time.Time
	Priority   int
	Categories []string
}

// TaskInput is a new task. ListID 0 picks the default list. MessageID, when
// set, links the task to that message.
type TaskInput struct {
	ListID    int64
	Title     string
	Notes     string
	Due       time.Time
	Priority  int
	MessageID int64
}

// TaskChange is an edit; nil fields stay as they are. A zero Due clears it.
type TaskChange struct {
	Title    *string
	Notes    *string
	Due      *time.Time
	Priority *int
	Done     *bool
}

// TaskFilter narrows a task list.
type TaskFilter struct {
	// Query matches the title or notes, "" for all.
	Query    string
	OpenOnly bool
	// DueBefore keeps tasks due before it, zero for any.
	DueBefore time.Time
	Limit     int
}

// TasksHost is the tasks a script reads and changes.
type TasksHost interface {
	Tasks(ctx context.Context, f TaskFilter) ([]Task, error)
	Task(ctx context.Context, id int64) (Task, error)
	CreateTask(ctx context.Context, in TaskInput) (Task, error)
	UpdateTask(ctx context.Context, id int64, ch TaskChange) (Task, error)
	// TaskFromMessage drafts a task from a message, linked to it.
	TaskFromMessage(ctx context.Context, messageID int64) (TaskInput, error)
}

// TaskObject turns a task into the value scripts get.
func TaskObject(t Task) scripting.Object {
	return scripting.Object{Type: "task", Fields: map[string]any{
		"id": t.ID, "list_id": t.ListID, "list": t.List, "uid": t.UID, "title": t.Title,
		"notes": t.Notes, "due": unix(t.Due), "done": t.Done, "completed": unix(t.Completed),
		"priority": t.Priority, "categories": texts(t.Categories),
	}}
}

func sampleTask(now time.Time) map[string]any {
	return TaskObject(Task{List: "Inbox", Title: "Pay invoice 2026-114", Notes: "120.00 EUR", Due: now.Add(-time.Hour), Priority: 1}).Fields
}

func taskInput(c *scripting.Call, t map[string]any, in TaskInput) (TaskInput, error) {
	if s := strings.TrimSpace(textOf(t, "title")); s != "" {
		in.Title = s
	}
	if s := textOf(t, "notes"); s != "" {
		in.Notes = s
	}
	if id := numberOf(t, "list_id"); id != 0 {
		in.ListID = id
	}
	if p := numberOf(t, "priority"); p != 0 {
		in.Priority = int(p)
	}
	if t["due"] != nil {
		due, err := timeValue(c, t["due"])
		if err != nil {
			return in, err
		}
		in.Due = due
	}
	if in.Title == "" {
		return in, fmt.Errorf("%s: a task needs a title", c.Function)
	}
	if in.Priority < 0 || in.Priority > 9 {
		return in, fmt.Errorf("%s: a priority is 0 (none) to 9", c.Function)
	}
	return in, nil
}

func taskChange(c *scripting.Call, t map[string]any) (TaskChange, error) {
	var ch TaskChange
	if s, ok := t["title"].(string); ok {
		ch.Title = &s
	}
	if s, ok := t["notes"].(string); ok {
		ch.Notes = &s
	}
	if p, ok := t["priority"].(float64); ok {
		n := int(p)
		ch.Priority = &n
	}
	if b, ok := t["done"].(bool); ok {
		ch.Done = &b
	}
	if v, ok := t["due"]; ok {
		due, err := timeValue(c, v)
		if err != nil {
			return ch, err
		}
		ch.Due = &due
	}
	return ch, nil
}

func tasksEntries(h TasksHost) scripting.Entries {
	host := func() (TasksHost, error) {
		if h == nil {
			return nil, unavailable("tasks")
		}
		return h, nil
	}
	load := func(c *scripting.PrepareContext, id int64) (scripting.Object, error) {
		hh, err := host()
		if err != nil {
			return scripting.Object{}, err
		}
		t, err := hh.Task(c.Ctx, id)
		if err != nil {
			return scripting.Object{}, fmt.Errorf("spaces: load task %d: %w", id, err)
		}
		return TaskObject(t), nil
	}
	taskField := field("task", "task")
	listName := param("list", scripting.TypeString)
	matchList := func(params, event map[string]any) bool {
		t := objectFields(event, "task")
		name, _ := t["list"].(string)
		return t != nil && matchText(params, "list", name)
	}
	trigger := func(id string) scripting.Trigger {
		return scripting.Trigger{
			Meta: meta("trigger", id, "tasks"), Params: []scripting.Param{listName},
			Event: []scripting.Field{taskField}, Permission: PermTasksRead,
			Prepare: prepareObject("task", "task_id", load), Match: matchList,
		}
	}

	taskParam := param("task", scripting.TypeRef)
	taskParam.ValueType = "task"
	msg := param("message", scripting.TypeRef)
	msg.ValueType = "message"
	tbl := param("task", scripting.TypeRef)
	filter := param("filter", scripting.TypeRef)
	changes := param("changes", scripting.TypeRef)

	create := func(c *scripting.Call, hh TasksHost, in TaskInput) ([]any, error) {
		t, err := hh.CreateTask(c.Ctx, in)
		if err != nil {
			return nil, err
		}
		return []any{TaskObject(t)}, nil
	}
	update := func(c *scripting.Call, ch TaskChange) ([]any, error) {
		hh, err := host()
		if err != nil {
			return nil, err
		}
		id, err := idOf(c, 0, "a task")
		if err != nil {
			return nil, err
		}
		_, err = hh.UpdateTask(c.Ctx, id, ch)
		return nil, err
	}
	fns := []scripting.Function{
		fnDef("tasks", "list", PermTasksRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			t := c.Table(0)
			f := TaskFilter{Query: textOf(t, "query"), Limit: 200}
			f.OpenOnly, _ = t["open"].(bool)
			if n := numberOf(t, "limit"); n > 0 && n <= 500 {
				f.Limit = int(n)
			}
			if t["due_before"] != nil {
				if f.DueBefore, err = timeValue(c, t["due_before"]); err != nil {
					return nil, err
				}
			}
			out, err := hh.Tasks(c.Ctx, f)
			if err != nil {
				return nil, err
			}
			return []any{list(out, TaskObject)}, nil
		}, []scripting.Field{field("tasks", "list")}, filter),
		fnDef("tasks", "get", PermTasksRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "a task")
			if err != nil {
				return nil, err
			}
			t, err := hh.Task(c.Ctx, id)
			if err != nil {
				return []any{nil, err.Error()}, nil
			}
			return []any{TaskObject(t)}, nil
		}, []scripting.Field{taskField}, param("id", scripting.TypeNumber)),
		withDry(fnDef("tasks", "create", PermTasksWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			t, err := tableArg(c, 0, "a title")
			if err != nil {
				return nil, err
			}
			in, err := taskInput(c, t, TaskInput{})
			if err != nil {
				return nil, err
			}
			return create(c, hh, in)
		}, []scripting.Field{taskField}, tbl)),
		withDry(fnDef("tasks", "from_mail", PermTasksWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "a message")
			if err != nil {
				return nil, err
			}
			in, err := hh.TaskFromMessage(c.Ctx, id)
			if err != nil {
				return nil, err
			}
			if in, err = taskInput(c, c.Table(1), in); err != nil {
				return nil, err
			}
			return create(c, hh, in)
		}, []scripting.Field{taskField}, msg, changes)),
		fnDef("tasks", "update", PermTasksWrite, "effect", func(c *scripting.Call) ([]any, error) {
			t, err := tableArg(c, 1, "the fields to change")
			if err != nil {
				return nil, err
			}
			ch, err := taskChange(c, t)
			if err != nil {
				return nil, err
			}
			return update(c, ch)
		}, nil, taskParam, changes),
		fnDef("tasks", "complete", PermTasksWrite, "effect", func(c *scripting.Call) ([]any, error) {
			done := c.Bool(1, true)
			return update(c, TaskChange{Done: &done})
		}, nil, taskParam, param("on", scripting.TypeBool)),
	}

	taskRef := scripting.RefParam("task", "task", "event.task")
	msgRef := scripting.RefParam("message", "message", "event.message")
	conds := []scripting.Condition{
		scripting.TextCondition(meta("cond", "tasks.title", "tasks"), "{{task}}.title", taskRef),
		scripting.TextCondition(meta("cond", "tasks.list", "tasks"), "{{task}}.list", taskRef),
		{
			Meta: meta("cond", "tasks.overdue", "tasks"), Params: []scripting.Param{taskRef},
			Lua: "not {{task}}.done and ({{task}}.due or 0) > 0 and {{task}}.due < time.now()",
		},
		{Meta: meta("cond", "tasks.has_due", "tasks"), Params: []scripting.Param{taskRef}, Lua: "({{task}}.due or 0) > 0"},
	}

	title := required(param("title", scripting.TypeString))
	notes := param("notes", scripting.TypeText)
	due := param("due", scripting.TypeString)
	idOut := []scripting.Field{field("id", "number")}
	act := func(id, lua string, out []scripting.Field, params ...scripting.Param) scripting.Action {
		return scripting.Action{Meta: meta("action", id, "tasks"), Params: params, Outputs: out, Permission: PermTasksWrite, Lua: lua}
	}
	actions := []scripting.Action{
		act("tasks.create", "tasks.create({title = {{title}}, notes = {{notes}}, due = {{due}}})", idOut, title, notes, due),
		act("tasks.from_mail", "tasks.from_mail({{message}}, {due = {{due}}})", idOut, msgRef, due),
		act("tasks.complete", "tasks.complete({{task}})", nil, taskRef),
	}

	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermTasksRead, "tasks"), Risk: "low"},
			{Meta: meta("perm", PermTasksWrite, "tasks"), Risk: "medium"},
		},
		ValueTypes: []scripting.ValueType{{Meta: meta("type", "task", "tasks"), Sample: sampleTask, Fields: fields(
			"id:number", "list_id:number", "list:string", "uid:string", "title:string", "notes:text",
			"due:number", "done:bool", "completed:number", "priority:number", "categories:list:string")}},
		Triggers:   []scripting.Trigger{trigger(TriggerTaskCreated), trigger(TriggerTaskCompleted), trigger(TriggerTaskOverdue)},
		Modules:    []scripting.Module{{Meta: meta("module", "tasks", "tasks"), Functions: fns}},
		Conditions: conds,
		Actions:    actions,
		Placements: []scripting.Placement{menu("tasks.task", "tasks", "task", PermTasksRead, intLoad(load))},
		ResultActions: []scripting.ResultAction{{
			Meta: meta("result", "tasks.create", "tasks"), Icon: "task", Permission: PermTasksWrite,
			Run: func(ctx context.Context, r scripting.Result) (string, error) {
				hh, err := host()
				if err != nil {
					return "", err
				}
				title := r.Title
				if title == "" {
					title, _, _ = strings.Cut(strings.TrimSpace(r.Text), "\n")
				}
				t, err := hh.CreateTask(ctx, TaskInput{Title: clipTitle(title, r.ScriptName), Notes: r.Text})
				if err != nil {
					return "", err
				}
				return t.Title, nil
			},
		}},
	}
}

// clipTitle is a title of at most 200 bytes, the script's name when empty.
func clipTitle(title, fallback string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = fallback
	}
	if len(title) > 200 {
		cut := 200
		for cut > 0 && title[cut]&0xc0 == 0x80 {
			cut--
		}
		title = title[:cut]
	}
	return title
}
