package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/peltonapp/Pelton/internal/promises"
	"github.com/peltonapp/Pelton/internal/scripting"
)

// What scripts see of promises: a promise being found or falling overdue,
// conditions on its fields, and accepting or dismissing it.

const (
	triggerPromiseFound   = "promises.found"
	triggerPromiseOverdue = "promises.overdue"
	permPromisesRead      = "promises.read"
	permPromisesWrite     = "promises.write"
)

// promiseScriptHost is what the scripts reach promises through.
type promiseScriptHost interface {
	Promise(ctx context.Context, id int64) (promises.Promise, error)
	Accept(ctx context.Context, id int64) error
	Dismiss(ctx context.Context, id int64) error
}

// promiseObject is a promise as scripts see it.
func promiseObject(p promises.Promise) scripting.Object {
	due := int64(0)
	if d := taskDueTime(p.Due); !d.IsZero() {
		due = d.Unix()
	}
	return scripting.Object{Type: "promise", Fields: map[string]any{
		"id": p.ID, "title": p.Title, "quote": p.Quote, "direction": string(p.Direction),
		"person": p.PersonName, "email": p.PersonEmail, "due": due, "state": string(p.State),
		"mail_id": p.MailID, "confidence": p.Confidence, "source": p.Source,
	}}
}

func samplePromise(now time.Time) map[string]any {
	return promiseObject(promises.Promise{
		ID: 1, Title: "I'll send the signed contract by Friday.", Quote: "I'll send the signed contract by Friday.",
		Direction: promises.Theirs, PersonName: "Ana Example", PersonEmail: "ana@example.com",
		Due: now.AddDate(0, 0, 2).Format(dateLayout), State: promises.StateSuggested, Confidence: 0.85, Source: "rules",
	}).Fields
}

func promiseScriptEntries(h promiseScriptHost) scripting.Entries {
	meta := scripting.EntryMeta
	load := func(c *scripting.PrepareContext, data map[string]any) (map[string]any, error) {
		out := map[string]any{}
		for k, v := range data {
			out[k] = v
		}
		id, ok := out["promise_id"].(int64)
		if !ok {
			return out, nil
		}
		p, err := h.Promise(c.Ctx, id)
		if err != nil {
			return nil, fmt.Errorf("promises: load %d: %w", id, err)
		}
		out["promise"] = promiseObject(p)
		delete(out, "promise_id")
		return out, nil
	}
	trigger := func(id string) scripting.Trigger {
		return scripting.Trigger{
			Meta: meta("trigger", id, "promises"), Params: []scripting.Param{},
			Event:      []scripting.Field{scripting.NewField("promise", "promise")},
			Permission: permPromisesRead, Prepare: load,
		}
	}
	idArg := func(c *scripting.Call) (int64, error) {
		switch v := c.Arg(0).(type) {
		case float64:
			return int64(v), nil
		case map[string]any:
			if id, ok := v["id"].(float64); ok {
				return int64(id), nil
			}
		}
		return 0, fmt.Errorf("%s: argument 1 must be a promise or its id", c.Function)
	}
	change := func(name string, run func(context.Context, int64) error) scripting.Function {
		f := scripting.NewFunction("promises", name, func(c *scripting.Call) ([]any, error) {
			id, err := idArg(c)
			if err != nil {
				return nil, err
			}
			return nil, run(c.Ctx, id)
		}, scripting.NewParam("promise", scripting.TypeRef))
		f.Permission, f.Effect = permPromisesWrite, true
		return f
	}
	ref := scripting.RefParam("promise", "promise", "event.promise")
	is := func(id, lua string) scripting.Condition {
		return scripting.Condition{Meta: meta("cond", id, "promises"), Params: []scripting.Param{ref}, Lua: lua}
	}
	act := func(id, lua string) scripting.Action {
		return scripting.Action{Meta: meta("action", id, "promises"), Params: []scripting.Param{ref}, Permission: permPromisesWrite, Lua: lua}
	}
	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", permPromisesRead, "promises"), Risk: "low"},
			{Meta: meta("perm", permPromisesWrite, "promises"), Risk: "medium"},
		},
		ValueTypes: []scripting.ValueType{{Meta: meta("type", "promise", "promises"), Sample: samplePromise, Fields: []scripting.Field{
			scripting.NewField("id", "number"), scripting.NewField("title", "string"), scripting.NewField("quote", "text"),
			scripting.NewField("direction", "string"), scripting.NewField("person", "string"), scripting.NewField("email", "string"),
			scripting.NewField("due", "number"), scripting.NewField("state", "string"), scripting.NewField("mail_id", "string"),
			scripting.NewField("confidence", "number"), scripting.NewField("source", "string"),
		}}},
		Triggers: []scripting.Trigger{trigger(triggerPromiseFound), trigger(triggerPromiseOverdue)},
		Modules: []scripting.Module{{Meta: meta("module", "promises", "promises"), Functions: []scripting.Function{
			change("accept", h.Accept), change("dismiss", h.Dismiss),
		}}},
		Conditions: []scripting.Condition{
			scripting.TextCondition(meta("cond", "promises.title", "promises"), "{{promise}}.title", ref),
			scripting.TextCondition(meta("cond", "promises.person", "promises"), "({{promise}}.person .. ' ' .. {{promise}}.email)", ref),
			is("promises.i_owe", `{{promise}}.direction == "mine"`),
			is("promises.owed_to_me", `{{promise}}.direction == "theirs"`),
			is("promises.has_due", `({{promise}}.due or 0) > 0`),
		},
		Actions: []scripting.Action{
			act("promises.accept", "promises.accept({{promise}})"),
			act("promises.dismiss", "promises.dismiss({{promise}})"),
		},
	}
}

// registerScriptPromises adds the promise entries to the scripts' registry.
func (a *App) registerScriptPromises(r *scripting.Registry) error {
	return r.Register(promiseScriptEntries(scriptPromises{app: a}))
}

type scriptPromises struct {
	app *App
}

func (s scriptPromises) Promise(ctx context.Context, id int64) (promises.Promise, error) {
	return s.app.promiseStore().Get(ctx, id)
}

func (s scriptPromises) Accept(ctx context.Context, id int64) error {
	p, err := s.app.promiseStore().Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.app.acceptPromise(ctx, PromiseEditDTO{ID: id, Title: p.Title, Due: p.Due}); err != nil {
		return err
	}
	s.app.emit(EventPromisesChanged, nil)
	return nil
}

func (s scriptPromises) Dismiss(ctx context.Context, id int64) error {
	return s.app.DismissPromise(id)
}

// runPromiseClock reports the accepted promises whose task fell due, for the
// scripts that listen for it. Each tick covers the time since the last.
func (a *App) runPromiseClock() {
	last := time.Now()
	t := time.NewTicker(scriptClockEvery)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-t.C:
			a.scriptPromisesDue(last, now)
			last = now
		}
	}
}

func (a *App) scriptPromisesDue(last, now time.Time) {
	if a.scripts == nil || !a.scripts.Wants(triggerPromiseOverdue) || !a.promisesActive() {
		return
	}
	open, err := a.promiseStore().List(a.ctx, promises.StateAccepted)
	if err != nil || len(open) == 0 {
		return
	}
	tasks, err := a.ListTasks()
	if err != nil {
		return
	}
	byUID := map[string]TaskDTO{}
	for _, t := range tasks {
		byUID[t.UID] = t
	}
	ctx := withScriptBudget(a.ctx)
	for _, p := range open {
		t, ok := byUID[p.TaskUID]
		if !ok || t.Done {
			continue
		}
		if due := taskDueTime(t.Due); due.After(last) && !due.After(now) {
			a.scriptEventBudgeted(ctx, triggerPromiseOverdue, map[string]any{"promise_id": p.ID})
		}
	}
}
