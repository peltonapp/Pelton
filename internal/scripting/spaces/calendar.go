package spaces

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The calendar permissions.
const (
	PermCalendarRead  = "calendar.read"
	PermCalendarWrite = "calendar.write"
)

// The calendar triggers.
const (
	TriggerEventStarts  = "calendar.event_starts"
	TriggerInvitation   = "calendar.invitation"
	TriggerEventCreated = "calendar.event_created"
	TriggerEventChanged = "calendar.event_changed"
)

// Event is a calendar event as scripts see it. For an occurrence of a
// repeating event, Start and End are that occurrence's.
type Event struct {
	ID          int64
	CalendarID  int64
	Calendar    string
	UID         string
	Title       string
	Description string
	Location    string
	URL         string
	Status      string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Recurring   bool
	Organizer   string
	Attendees   []string
}

// EventInput is a new event. CalendarID 0 picks the default calendar.
type EventInput struct {
	CalendarID  int64
	Title       string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         time.Time
	AllDay      bool
}

// EventChange is an edit; nil fields stay as they are.
type EventChange struct {
	Title       *string
	Description *string
	Location    *string
	Start       *time.Time
	End         *time.Time
	AllDay      *bool
}

// Invitation is a calendar invitation found in a message.
type Invitation struct {
	Method          string
	UID             string
	Title           string
	Location        string
	Organizer       string
	Start           time.Time
	End             time.Time
	AllDay          bool
	ExistingEventID int64
}

// CalendarHost is the calendar a script reads and changes, through the same
// paths the app uses, so synced calendars get the change on their server.
type CalendarHost interface {
	// Events lists event occurrences between from and to whose title,
	// location or description contains query ("" for all), at most limit.
	Events(ctx context.Context, from, to time.Time, query string, limit int) ([]Event, error)
	Event(ctx context.Context, id int64) (Event, error)
	CreateEvent(ctx context.Context, in EventInput) (Event, error)
	UpdateEvent(ctx context.Context, id int64, ch EventChange) (Event, error)
	// EventFromMessage drafts an event from a message: its subject, text and
	// any date it names.
	EventFromMessage(ctx context.Context, messageID int64) (EventInput, error)
	// Invitation returns the invitation a message carries, false when none.
	Invitation(ctx context.Context, messageID int64) (Invitation, bool, error)
}

// EventObject turns an event into the value scripts get.
func EventObject(e Event) scripting.Object {
	return scripting.Object{Type: "event", Fields: map[string]any{
		"id": e.ID, "calendar_id": e.CalendarID, "calendar": e.Calendar, "uid": e.UID,
		"title": e.Title, "description": e.Description, "location": e.Location, "url": e.URL,
		"status": e.Status, "start": unix(e.Start), "end": unix(e.End), "all_day": e.AllDay,
		"recurring": e.Recurring, "organizer": e.Organizer, "attendees": texts(e.Attendees),
	}}
}

// InvitationObject turns an invitation into the value scripts get.
func InvitationObject(in Invitation) scripting.Object {
	return scripting.Object{Type: "invitation", Fields: map[string]any{
		"method": in.Method, "uid": in.UID, "title": in.Title, "location": in.Location,
		"organizer": in.Organizer, "start": unix(in.Start), "end": unix(in.End), "all_day": in.AllDay,
		"event_id": in.ExistingEventID,
	}}
}

func sampleEvent(now time.Time) map[string]any {
	start := now.Add(time.Hour).Truncate(time.Hour)
	return EventObject(Event{
		CalendarID: 0, Calendar: "Work", Title: "Project review", Location: "Room 2",
		Description: "Go through the open points.", Start: start, End: start.Add(time.Hour),
		Organizer: "lead@example.org", Attendees: []string{"you@example.org", "lead@example.org"},
	}).Fields
}

func sampleInvitation(now time.Time) map[string]any {
	start := now.Add(24 * time.Hour).Truncate(time.Hour)
	return InvitationObject(Invitation{
		Method: "REQUEST", Title: "Planning", Organizer: "lead@example.org",
		Start: start, End: start.Add(30 * time.Minute),
	}).Fields
}

// eventInput reads a table describing an event. An event without an end
// lasts an hour, or a day when it is all day.
func eventInput(c *scripting.Call, t map[string]any) (EventInput, error) {
	in := EventInput{
		CalendarID: numberOf(t, "calendar_id"), Title: strings.TrimSpace(textOf(t, "title")),
		Description: textOf(t, "description"), Location: textOf(t, "location"), URL: textOf(t, "url"),
	}
	in.AllDay, _ = t["all_day"].(bool)
	var err error
	if in.Start, err = timeValue(c, t["start"]); err != nil {
		return in, err
	}
	if in.End, err = timeValue(c, t["end"]); err != nil {
		return in, err
	}
	if in.Title == "" {
		return in, fmt.Errorf("%s: an event needs a title", c.Function)
	}
	if in.Start.IsZero() {
		return in, fmt.Errorf("%s: an event needs a start", c.Function)
	}
	if secs, ok := t["duration"].(float64); ok && in.End.IsZero() && secs > 0 {
		in.End = in.Start.Add(time.Duration(secs * float64(time.Second)))
	}
	if in.End.IsZero() {
		if in.AllDay {
			in.End = in.Start.AddDate(0, 0, 1)
		} else {
			in.End = in.Start.Add(time.Hour)
		}
	}
	if in.End.Before(in.Start) {
		return in, fmt.Errorf("%s: an event cannot end before it starts", c.Function)
	}
	return in, nil
}

func eventChange(c *scripting.Call, t map[string]any) (EventChange, error) {
	var ch EventChange
	str := func(key string) *string {
		if s, ok := t[key].(string); ok {
			return &s
		}
		return nil
	}
	ch.Title, ch.Description, ch.Location = str("title"), str("description"), str("location")
	if b, ok := t["all_day"].(bool); ok {
		ch.AllDay = &b
	}
	for key, dst := range map[string]**time.Time{"start": &ch.Start, "end": &ch.End} {
		if t[key] == nil {
			continue
		}
		v, err := timeValue(c, t[key])
		if err != nil {
			return ch, err
		}
		*dst = &v
	}
	return ch, nil
}

func calendarEntries(h CalendarHost) scripting.Entries {
	host := func() (CalendarHost, error) {
		if h == nil {
			return nil, unavailable("calendar")
		}
		return h, nil
	}
	eventField := field("event", "event")
	eventRef := scripting.RefParam("event", "event", "event.event")
	msgRef := scripting.RefParam("message", "message", "event.message")
	idOut := []scripting.Field{field("id", "number")}
	calendarName := param("calendar", scripting.TypeString)

	load := func(c *scripting.PrepareContext, id int64) (scripting.Object, error) {
		hh, err := host()
		if err != nil {
			return scripting.Object{}, err
		}
		e, err := hh.Event(c.Ctx, id)
		if err != nil {
			return scripting.Object{}, fmt.Errorf("spaces: load event %d: %w", id, err)
		}
		return EventObject(e), nil
	}
	matchCalendar := func(params, event map[string]any) bool {
		ev := objectFields(event, "event")
		name, _ := ev["calendar"].(string)
		return ev != nil && matchText(params, "calendar", name)
	}
	minutes := required(withDef(param("minutes", scripting.TypeNumber), float64(15)))

	triggers := []scripting.Trigger{
		{
			Meta:       meta("trigger", TriggerEventStarts, "calendar"),
			Params:     []scripting.Param{minutes, calendarName},
			Event:      []scripting.Field{eventField, field("minutes", "number")},
			Permission: PermCalendarRead,
			Prepare:    prepareObject("event", "event_id", load),
			Match: func(params, event map[string]any) bool {
				return number(params["minutes"]) == number(event["minutes"]) && matchCalendar(params, event)
			},
		},
		{
			Meta:       meta("trigger", TriggerInvitation, "calendar"),
			Event:      []scripting.Field{field("message", "message"), field("invitation", "invitation")},
			Permission: PermCalendarRead,
		},
		{
			Meta: meta("trigger", TriggerEventCreated, "calendar"), Params: []scripting.Param{calendarName},
			Event: []scripting.Field{eventField}, Permission: PermCalendarRead,
			Prepare: prepareObject("event", "event_id", load), Match: matchCalendar,
		},
		{
			Meta: meta("trigger", TriggerEventChanged, "calendar"), Params: []scripting.Param{calendarName},
			Event: []scripting.Field{eventField}, Permission: PermCalendarRead,
			Prepare: prepareObject("event", "event_id", load), Match: matchCalendar,
		},
	}

	evParam := param("event", scripting.TypeRef)
	evParam.ValueType = "event"
	from := param("from", scripting.TypeString)
	to := param("to", scripting.TypeString)
	query := param("query", scripting.TypeString)
	limit := param("limit", scripting.TypeNumber)
	tbl := param("event", scripting.TypeRef)
	changes := param("changes", scripting.TypeRef)
	msg := param("message", scripting.TypeRef)
	msg.ValueType = "message"
	eventsOut := []scripting.Field{field("events", "list")}

	fns := []scripting.Function{
		fnDef("calendar", "events", PermCalendarRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			start, err := timeValue(c, c.Arg(0))
			if err != nil {
				return nil, err
			}
			if start.IsZero() {
				start = c.Now()
			}
			end, err := timeValue(c, c.Arg(1))
			if err != nil {
				return nil, err
			}
			if end.IsZero() {
				end = start.AddDate(0, 0, 7)
			}
			q, err := c.OptString(2, "")
			if err != nil {
				return nil, err
			}
			n, err := limitArg(c, 3)
			if err != nil {
				return nil, err
			}
			list0, err := hh.Events(c.Ctx, start, end, q, n)
			if err != nil {
				return nil, err
			}
			return []any{list(list0, EventObject)}, nil
		}, eventsOut, from, to, query, limit),
		fnDef("calendar", "get", PermCalendarRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "an event")
			if err != nil {
				return nil, err
			}
			e, err := hh.Event(c.Ctx, id)
			if err != nil {
				return []any{nil, err.Error()}, nil
			}
			return []any{EventObject(e)}, nil
		}, []scripting.Field{eventField}, param("id", scripting.TypeNumber)),
		withDry(fnDef("calendar", "create", PermCalendarWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			t, err := tableArg(c, 0, "title and start")
			if err != nil {
				return nil, err
			}
			in, err := eventInput(c, t)
			if err != nil {
				return nil, err
			}
			e, err := hh.CreateEvent(c.Ctx, in)
			if err != nil {
				return nil, err
			}
			return []any{EventObject(e)}, nil
		}, []scripting.Field{eventField}, tbl)),
		fnDef("calendar", "update", PermCalendarWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "an event")
			if err != nil {
				return nil, err
			}
			t, err := tableArg(c, 1, "the fields to change")
			if err != nil {
				return nil, err
			}
			ch, err := eventChange(c, t)
			if err != nil {
				return nil, err
			}
			_, err = hh.UpdateEvent(c.Ctx, id, ch)
			return nil, err
		}, nil, evParam, changes),
		withDry(fnDef("calendar", "from_mail", PermCalendarWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "a message")
			if err != nil {
				return nil, err
			}
			in, err := hh.EventFromMessage(c.Ctx, id)
			if err != nil {
				return nil, err
			}
			if t := c.Table(1); t != nil {
				if s := textOf(t, "title"); s != "" {
					in.Title = s
				}
				if t["start"] != nil {
					start, err := timeValue(c, t["start"])
					if err != nil {
						return nil, err
					}
					in.End = start.Add(in.End.Sub(in.Start))
					in.Start = start
				}
			}
			e, err := hh.CreateEvent(c.Ctx, in)
			if err != nil {
				return nil, err
			}
			return []any{EventObject(e)}, nil
		}, []scripting.Field{eventField}, msg, changes)),
		fnDef("calendar", "invitation", PermCalendarRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "a message")
			if err != nil {
				return nil, err
			}
			in, ok, err := hh.Invitation(c.Ctx, id)
			if err != nil || !ok {
				return []any{nil}, err
			}
			return []any{InvitationObject(in)}, nil
		}, []scripting.Field{field("invitation", "invitation")}, msg),
	}

	text := func(id, key string) scripting.Condition {
		return scripting.TextCondition(meta("cond", id, "calendar"), "{{event}}."+key, eventRef)
	}
	within := required(withDef(param("duration", scripting.TypeDuration), "1h"))
	conds := []scripting.Condition{
		text("calendar.title", "title"),
		text("calendar.location", "location"),
		text("calendar.calendar", "calendar"),
		{Meta: meta("cond", "calendar.all_day", "calendar"), Params: []scripting.Param{eventRef}, Lua: "{{event}}.all_day == true"},
		{
			Meta: meta("cond", "calendar.starts_within", "calendar"), Params: []scripting.Param{eventRef, within},
			Lua: "({{event}}.start or 0) - time.now() <= {{duration}} and ({{event}}.start or 0) >= time.now()",
		},
	}

	title := required(param("title", scripting.TypeString))
	start := required(withDef(param("start", scripting.TypeString), "1d"))
	length := required(withDef(param("duration", scripting.TypeDuration), "1h"))
	location := param("location", scripting.TypeString)
	description := param("description", scripting.TypeText)
	act := func(id, lua string, out []scripting.Field, params ...scripting.Param) scripting.Action {
		return scripting.Action{Meta: meta("action", id, "calendar"), Params: params, Outputs: out, Permission: PermCalendarWrite, Lua: lua}
	}
	actions := []scripting.Action{
		act("calendar.create", `calendar.create({title = {{title}}, start = {{start}}, duration = {{duration}}, location = {{location}}, description = {{description}}})`,
			idOut, title, start, length, location, description),
		act("calendar.from_mail", "calendar.from_mail({{message}})", idOut, msgRef),
		act("calendar.reschedule", "calendar.update({{event}}, {start = {{start}}})", nil, eventRef, start),
	}

	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermCalendarRead, "calendar"), Risk: "low"},
			{Meta: meta("perm", PermCalendarWrite, "calendar"), Risk: "medium"},
		},
		ValueTypes: []scripting.ValueType{
			{Meta: meta("type", "event", "calendar"), Sample: sampleEvent, Fields: fields(
				"id:number", "calendar_id:number", "calendar:string", "uid:string", "title:string",
				"description:text", "location:string", "url:string", "status:string", "start:number",
				"end:number", "all_day:bool", "recurring:bool", "organizer:address", "attendees:list")},
			{Meta: meta("type", "invitation", "calendar"), Sample: sampleInvitation, Fields: fields(
				"method:string", "uid:string", "title:string", "location:string", "organizer:address",
				"start:number", "end:number", "all_day:bool", "event_id:number")},
		},
		Triggers:   triggers,
		Modules:    []scripting.Module{{Meta: meta("module", "calendar", "calendar"), Functions: fns}},
		Conditions: conds,
		Actions:    actions,
	}
}

// withDry gives a call that creates something a dry run answer with id 0.
func withDry(f scripting.Function) scripting.Function {
	f.Dry = dryID
	return f
}
