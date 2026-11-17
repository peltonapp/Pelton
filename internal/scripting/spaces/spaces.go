// Package spaces lets scripts reach Pelton's other spaces: the calendar,
// tasks, contacts, projects, notes, feeds and chat. Each space registers its
// permissions, value types, triggers, conditions, actions and Lua module
// through the public registry API, and reaches the app through the small host
// interface in its own file, so this package never imports the app.
//
// Times are unix seconds, as message dates are. A time argument also takes a
// span from now such as "2h" or "3d", or a date "2006-01-02" with an optional
// " 15:04".
package spaces

import (
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// Hosts is how the spaces reach the app. A nil member leaves that space's
// entries registered, with calls failing as unavailable.
type Hosts struct {
	Calendar CalendarHost
	Tasks    TasksHost
	Contacts ContactsHost
	Projects ProjectsHost
	Notes    NotesHost
	Feeds    FeedsHost
	Chat     ChatHost
}

// Register adds every space's entries.
func Register(r *scripting.Registry, h Hosts) error {
	for _, e := range []scripting.Entries{
		calendarEntries(h.Calendar),
		tasksEntries(h.Tasks),
		contactsEntries(h.Contacts),
		projectsEntries(h.Projects),
		notesEntries(h.Notes),
		feedsEntries(h.Feeds),
		chatEntries(h.Chat),
	} {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}

var (
	meta     = scripting.EntryMeta
	param    = scripting.NewParam
	field    = scripting.NewField
	required = scripting.Required
	withDef  = scripting.WithDefault
)

// fields builds a field list from "name:type" pairs.
func fields(specs ...string) []scripting.Field {
	out := make([]scripting.Field, 0, len(specs))
	for _, s := range specs {
		name, typ, _ := strings.Cut(s, ":")
		out = append(out, field(name, typ))
	}
	return out
}

// number reads a number of any Go numeric type, 0 otherwise.
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// unix turns a time into the number scripts see, 0 for none.
func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// timeValue reads a time a script gave: unix seconds, a span from now such as
// "2h", or a date "2006-01-02" with an optional " 15:04" in local time. nil
// and 0 are no time.
func timeValue(c *scripting.Call, v any) (time.Time, error) {
	switch x := v.(type) {
	case nil:
		return time.Time{}, nil
	case float64:
		if x == 0 {
			return time.Time{}, nil
		}
		return time.Unix(int64(x), 0), nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return time.Time{}, nil
		}
		loc := c.Now().Location()
		for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, s, loc); err == nil {
				return t, nil
			}
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, nil
		}
		secs, err := scripting.ParseDuration(s)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s: %q is not a time, a date or a span such as \"2h\"", c.Function, s)
		}
		return c.Now().Add(time.Duration(secs * float64(time.Second))), nil
	}
	return time.Time{}, fmt.Errorf("%s: a time is a number, a date or a span such as \"2h\"", c.Function)
}

// idOf reads an id from a number or a table with an "id" field.
func idOf(c *scripting.Call, i int, what string) (int64, error) {
	switch v := c.Arg(i).(type) {
	case float64:
		return int64(v), nil
	case map[string]any:
		if id, ok := v["id"].(float64); ok {
			return int64(id), nil
		}
	}
	return 0, fmt.Errorf("%s: argument %d must be %s or its id", c.Function, i+1, what)
}

// textOf reads a text field of a table, "" when missing.
func textOf(t map[string]any, key string) string {
	s, _ := t[key].(string)
	return s
}

// numberOf reads a number field of a table, 0 when missing.
func numberOf(t map[string]any, key string) int64 {
	f, _ := t[key].(float64)
	return int64(f)
}

// tableArg returns argument i as a table, or an error naming what it holds.
func tableArg(c *scripting.Call, i int, what string) (map[string]any, error) {
	t := c.Table(i)
	if t == nil {
		return nil, fmt.Errorf("%s: argument %d must be a table with %s", c.Function, i+1, what)
	}
	return t, nil
}

// list turns objects into a Lua list.
func list[T any](items []T, conv func(T) scripting.Object) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, conv(it))
	}
	return out
}

// strings turns texts into a Lua list.
func texts(items []string) []any {
	out := make([]any, 0, len(items))
	for _, s := range items {
		out = append(out, s)
	}
	return out
}

// limitArg reads an optional result count, 1 to 500, 50 when missing.
func limitArg(c *scripting.Call, i int) (int, error) {
	if c.Arg(i) == nil {
		return 50, nil
	}
	n, err := c.Int(i)
	if err != nil || n < 1 || n > 500 {
		return 0, fmt.Errorf("%s: a count is 1 to 500", c.Function)
	}
	return int(n), nil
}

// fnDef builds a module function. kind is "read" for a call that only reads,
// "effect" for one that changes something, which a dry run records as a
// planned step.
func fnDef(module, name, perm, kind string, h scripting.Handler, returns []scripting.Field, params ...scripting.Param) scripting.Function {
	f := scripting.NewFunction(module, name, h, params...)
	f.Permission = perm
	f.Returns = returns
	f.Effect = kind == "effect"
	return f
}

// dryID answers a dry run of a call that creates something with id 0, so a
// script that uses the result keeps going.
func dryID(*scripting.Call) ([]any, error) {
	return []any{map[string]any{"id": 0}}, nil
}

// unavailable is the error of a space the app did not provide.
func unavailable(space string) error {
	return fmt.Errorf("%w: %s", scripting.ErrUnavailable, space)
}

// matchText reports whether a trigger's text param, when set, is contained
// in got, ignoring case.
func matchText(params map[string]any, key, got string) bool {
	want, _ := params[key].(string)
	want = strings.TrimSpace(want)
	return want == "" || strings.Contains(strings.ToLower(got), strings.ToLower(want))
}

// objectField returns a field of the event's object under key.
func objectFields(event map[string]any, key string) map[string]any {
	switch o := event[key].(type) {
	case scripting.Object:
		return o.Fields
	case map[string]any:
		return o
	}
	return nil
}

// prepareObject builds a trigger's Prepare that loads the object behind
// data[idKey] into data[key] with load, unless the hook sent it along.
func prepareObject[K int64 | string](key, idKey string, load func(c *scripting.PrepareContext, id K) (scripting.Object, error)) func(*scripting.PrepareContext, map[string]any) (map[string]any, error) {
	return func(c *scripting.PrepareContext, data map[string]any) (map[string]any, error) {
		out := map[string]any{}
		for k, v := range data {
			out[k] = v
		}
		if _, ok := out[key]; ok {
			return out, nil
		}
		id, ok := out[idKey].(K)
		if !ok {
			return out, nil
		}
		obj, err := load(c, id)
		if err != nil {
			return nil, err
		}
		out[key] = obj
		delete(out, idKey)
		return out, nil
	}
}
