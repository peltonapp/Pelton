package ical

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Todo is a VTODO read into the fields a task list shows and edits. Like
// Event, Component is the VTODO it came from and Apply writes the fields back
// into it, leaving every other property and subcomponent as it was.
type Todo struct {
	Component *Component

	UID         string
	Summary     string
	Description string
	URL         string
	// Status is NEEDS-ACTION, IN-PROCESS, COMPLETED or CANCELLED, or empty.
	Status string
	// Priority is 0 for none, 1 the highest through 9 the lowest.
	Priority int
	// Percent is PERCENT-COMPLETE, 0 to 100.
	Percent    int
	Categories []string
	Sequence   int

	Start DateTime
	Due   DateTime
	// Completed is when the task was done, a UTC instant, zero while open.
	Completed DateTime

	RRule *RRule
	// Parent is the UID of the task this one is a subtask of, from a
	// RELATED-TO with RELTYPE=PARENT (the default), empty for none.
	Parent string
	// Related are the UIDs of other components it relates to as a sibling,
	// which is how a task points at an event.
	Related []string
	// Attachments are the URIs of ATTACH properties, which is where a link
	// to a mail (mid:) goes. Inline attachments are kept in the component and
	// not listed.
	Attachments []string
	Contacts    []Contact
	// SortOrder is X-APPLE-SORT-ORDER, the manual order other task apps use
	// too. Zero means none.
	SortOrder int
	Alarms    []Alarm
}

// Contact is a CONTACT: a name, and the address it points at through its
// ALTREP, which Pelton writes as a mailto: URI.
type Contact struct {
	Name  string
	Email string
}

// The STATUS values for a VTODO.
const (
	TodoNeedsAction = "NEEDS-ACTION"
	TodoInProcess   = "IN-PROCESS"
	TodoCompleted   = "COMPLETED"
	TodoCancelled   = "CANCELLED"
)

// ErrNoTodo means the calendar data holds no VTODO.
var ErrNoTodo = errors.New("ical: no task in the calendar data")

// ParseTodo reads a VTODO. Malformed dates are an error; anything else that
// does not parse is left out of the fields and kept in the component.
func ParseTodo(c *Component) (*Todo, error) {
	t := &Todo{
		Component:   c,
		UID:         c.Text("UID"),
		Summary:     c.Text("SUMMARY"),
		Description: c.Text("DESCRIPTION"),
		URL:         strings.TrimSpace(c.Text("URL")),
		Status:      strings.ToUpper(strings.TrimSpace(c.Text("STATUS"))),
	}
	var err error
	if t.Start, err = propDateTime(c.Prop("DTSTART")); err != nil {
		return nil, err
	}
	if t.Due, err = propDateTime(c.Prop("DUE")); err != nil {
		return nil, err
	}
	if t.Due.IsZero() && !t.Start.IsZero() {
		if p := c.Prop("DURATION"); p != nil {
			if d, err := ParseDuration(p.Value); err == nil {
				t.Due = t.Start
				t.Due.Wall = t.Start.Wall.Add(d)
			}
		}
	}
	if p := c.Prop("COMPLETED"); p != nil {
		if d, err := propDateTime(p); err == nil {
			t.Completed = d
		}
	}
	if p := c.Prop("RRULE"); p != nil {
		if rule, err := ParseRRule(p.Value); err == nil {
			t.RRule = rule
		}
	}
	t.Priority = clampInt(atoi(c.Text("PRIORITY")), 0, 9)
	t.Percent = clampInt(atoi(c.Text("PERCENT-COMPLETE")), 0, 100)
	t.Sequence = atoi(c.Text("SEQUENCE"))
	t.SortOrder = atoi(c.Text("X-APPLE-SORT-ORDER"))
	for _, p := range c.PropsNamed("CATEGORIES") {
		for _, cat := range splitList(p.Value) {
			if cat = strings.TrimSpace(unescapeText(cat)); cat != "" {
				t.Categories = append(t.Categories, cat)
			}
		}
	}
	for _, p := range c.PropsNamed("RELATED-TO") {
		uid := strings.TrimSpace(p.Text())
		if uid == "" {
			continue
		}
		switch strings.ToUpper(p.Param("RELTYPE")) {
		case "", "PARENT":
			if t.Parent == "" {
				t.Parent = uid
			}
		case "SIBLING":
			t.Related = append(t.Related, uid)
		}
	}
	for _, p := range c.PropsNamed("ATTACH") {
		if strings.EqualFold(p.Param("VALUE"), "BINARY") || p.Param("ENCODING") != "" {
			continue
		}
		if v := strings.TrimSpace(p.Value); v != "" {
			t.Attachments = append(t.Attachments, v)
		}
	}
	for _, p := range c.PropsNamed("CONTACT") {
		ct := Contact{Name: strings.TrimSpace(p.Text()), Email: MailtoAddress(p.Param("ALTREP"))}
		if !strings.Contains(ct.Email, "@") {
			ct.Email = ""
		}
		if ct.Name != "" || ct.Email != "" {
			t.Contacts = append(t.Contacts, ct)
		}
	}
	for _, a := range c.ChildrenNamed("VALARM") {
		t.Alarms = append(t.Alarms, alarmFrom(a))
	}
	return t, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func clampInt(n, lo, hi int) int {
	return max(lo, min(hi, n))
}

// Done reports whether the task is completed.
func (t *Todo) Done() bool {
	return t.Status == TodoCompleted || (t.Status == "" && !t.Completed.IsZero())
}

// SetDone completes the task at the given instant, or opens it again.
func (t *Todo) SetDone(done bool, at time.Time) {
	if done {
		t.Status = TodoCompleted
		t.Percent = 100
		t.Completed = DateTime{Wall: at.UTC().Truncate(time.Second), UTC: true}
		return
	}
	t.Status = TodoNeedsAction
	if t.Percent == 100 {
		t.Percent = 0
	}
	t.Completed = DateTime{}
}

// Apply writes the fields back into the component, creating it when the task
// has none. Properties Pelton has no field for stay as they were.
func (t *Todo) Apply() *Component {
	c := t.Component
	if c == nil {
		c = &Component{Name: "VTODO"}
		t.Component = c
	}
	c.SetText("UID", t.UID)
	c.Set("DTSTAMP", time.Now().UTC().Format(layoutDateTime)+"Z")
	c.Set("LAST-MODIFIED", time.Now().UTC().Format(layoutDateTime)+"Z")
	setDate(c, "DTSTART", t.Start)
	setDate(c, "DUE", t.Due)
	if !t.Due.IsZero() {
		c.Remove("DURATION")
	}
	setDate(c, "COMPLETED", t.Completed)
	c.SetText("SUMMARY", t.Summary)
	c.SetText("DESCRIPTION", t.Description)
	c.Set("URL", t.URL)
	c.Set("STATUS", t.Status)
	setInt(c, "PRIORITY", t.Priority)
	setInt(c, "PERCENT-COMPLETE", t.Percent)
	setInt(c, "X-APPLE-SORT-ORDER", t.SortOrder)
	if t.Sequence > 0 {
		c.Set("SEQUENCE", strconv.Itoa(t.Sequence))
	}
	if len(t.Categories) > 0 {
		escaped := make([]string, len(t.Categories))
		for i, cat := range t.Categories {
			escaped[i] = escapeText(cat)
		}
		c.Set("CATEGORIES", strings.Join(escaped, ","))
	} else {
		c.Remove("CATEGORIES")
	}
	if t.RRule != nil {
		c.Set("RRULE", t.RRule.String())
	} else {
		c.Remove("RRULE")
	}
	t.applyRelations(c)
	t.applyAttachments(c)
	t.applyContacts(c)
	ev := &Event{Summary: t.Summary, Alarms: t.Alarms}
	ev.applyAlarms(c)
	return c
}

func setInt(c *Component, name string, n int) {
	if n == 0 {
		c.Remove(name)
		return
	}
	c.Set(name, strconv.Itoa(n))
}

// applyRelations rewrites the parent and sibling RELATED-TO lines, leaving
// any other relation type (a CHILD written by another app) alone.
func (t *Todo) applyRelations(c *Component) {
	var kept []*Prop
	for _, p := range c.Props {
		if strings.EqualFold(p.Name, "RELATED-TO") {
			switch strings.ToUpper(p.Param("RELTYPE")) {
			case "", "PARENT", "SIBLING":
				continue
			}
		}
		kept = append(kept, p)
	}
	c.Props = kept
	if t.Parent != "" {
		c.Add(&Prop{Name: "RELATED-TO", Params: []Param{{Name: "RELTYPE", Values: []string{"PARENT"}}}, Value: escapeText(t.Parent)})
	}
	for _, uid := range t.Related {
		c.Add(&Prop{Name: "RELATED-TO", Params: []Param{{Name: "RELTYPE", Values: []string{"SIBLING"}}}, Value: escapeText(uid)})
	}
}

// applyAttachments rewrites the URI attachments, keeping the parameters of
// one still listed and every inline attachment.
func (t *Todo) applyAttachments(c *Component) {
	existing := map[string]*Prop{}
	var kept []*Prop
	for _, p := range c.Props {
		if strings.EqualFold(p.Name, "ATTACH") && !strings.EqualFold(p.Param("VALUE"), "BINARY") && p.Param("ENCODING") == "" {
			existing[strings.TrimSpace(p.Value)] = p
			continue
		}
		kept = append(kept, p)
	}
	c.Props = kept
	for _, uri := range t.Attachments {
		p := existing[uri]
		if p == nil {
			p = &Prop{Name: "ATTACH", Value: uri}
			if strings.HasPrefix(strings.ToLower(uri), "mid:") {
				p.Params = []Param{{Name: "FMTTYPE", Values: []string{"message/rfc822"}}}
			}
		}
		c.Add(p)
	}
}

// applyContacts rewrites CONTACT, one line per person with the address as
// its ALTREP.
func (t *Todo) applyContacts(c *Component) {
	c.Remove("CONTACT")
	for _, ct := range t.Contacts {
		p := &Prop{Name: "CONTACT", Value: escapeText(ct.Name)}
		if ct.Name == "" {
			p.Value = escapeText(ct.Email)
		}
		if ct.Email != "" {
			p.Params = []Param{{Name: "ALTREP", Values: []string{"mailto:" + ct.Email}}}
		}
		c.Add(p)
	}
}

// anchor is the date the task's recurrence counts from: DTSTART when it has
// one, else DUE, which is what most task apps write a repeating task with.
func (t *Todo) anchor() DateTime {
	if !t.Start.IsZero() {
		return t.Start
	}
	return t.Due
}

// TodoObject is one calendar object resource holding a task: the VTODO and
// the VCALENDAR around it with the time zones it needs.
type TodoObject struct {
	Calendar *Component
	Todo     *Todo
	Zones    *Zones
}

// ParseTodoObject reads one calendar object resource holding a task. local is
// the zone floating times are read in; nil means time.Local.
func ParseTodoObject(data []byte, local *time.Location) (*TodoObject, error) {
	cal, err := ParseCalendar(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	objs := SplitTodos(cal, local)
	if len(objs) == 0 {
		return nil, ErrNoTodo
	}
	return objs[0], nil
}

// SplitTodos groups the VTODOs of a calendar by UID into objects, each with
// the calendar's own properties and the time zone definitions it uses. A
// task's overrides of single occurrences travel with it untouched.
func SplitTodos(cal *Component, local *time.Location) []*TodoObject {
	var order []string
	groups := map[string][]*Component{}
	for _, child := range cal.ChildrenNamed("VTODO") {
		uid := child.Text("UID")
		if _, seen := groups[uid]; !seen {
			order = append(order, uid)
		}
		groups[uid] = append(groups[uid], child)
	}
	var out []*TodoObject
	for _, uid := range order {
		wrapper := &Component{Name: "VCALENDAR"}
		for _, p := range cal.Props {
			wrapper.Props = append(wrapper.Props, &Prop{Name: p.Name, Params: p.Params, Value: p.Value})
		}
		var master *Todo
		var comps []*Component
		for _, comp := range groups[uid] {
			clone := comp.Clone()
			comps = append(comps, clone)
			if master == nil && clone.Prop("RECURRENCE-ID") == nil {
				if todo, err := ParseTodo(clone); err == nil {
					master = todo
				}
			}
		}
		if master == nil {
			continue
		}
		used := map[string]bool{}
		for _, comp := range comps {
			for _, p := range comp.Props {
				if id := p.Param("TZID"); id != "" {
					used[id] = true
				}
			}
		}
		for _, tz := range cal.ChildrenNamed("VTIMEZONE") {
			if used[tz.Text("TZID")] {
				wrapper.Children = append(wrapper.Children, tz.Clone())
			}
		}
		wrapper.Children = append(wrapper.Children, comps...)
		out = append(out, &TodoObject{Calendar: wrapper, Todo: master, Zones: NewZones(wrapper, local)})
	}
	return out
}

// NewTodoObject wraps a new task in a VCALENDAR of its own.
func NewTodoObject(t *Todo, local *time.Location) *TodoObject {
	cal := &Component{Name: "VCALENDAR"}
	cal.Set("VERSION", "2.0")
	cal.Set("PRODID", ProdID)
	comp := t.Apply()
	cal.Children = append(cal.Children, comp)
	obj := &TodoObject{Calendar: cal, Todo: t, Zones: NewZones(cal, local)}
	obj.EnsureTimezones()
	return obj
}

// Encode renders the object with the task's fields applied.
func (o *TodoObject) Encode() []byte {
	o.Todo.Apply()
	var b bytes.Buffer
	_ = Encode(&b, o.Calendar)
	return b.Bytes()
}

// EnsureTimezones adds a VTIMEZONE for every TZID the task uses that has no
// definition yet.
func (o *TodoObject) EnsureTimezones() {
	have := map[string]bool{}
	for _, tz := range o.Calendar.ChildrenNamed("VTIMEZONE") {
		have[tz.Text("TZID")] = true
	}
	var added []*Component
	year := time.Now().Year()
	if a := o.Todo.anchor(); !a.IsZero() {
		year = a.Wall.Year()
	}
	for _, d := range []DateTime{o.Todo.Start, o.Todo.Due} {
		if d.TZID == "" || d.UTC || have[d.TZID] {
			continue
		}
		loc, ok := lookupLocation(d.TZID)
		if !ok {
			continue
		}
		added = append(added, VTimezone(d.TZID, loc, year))
		have[d.TZID] = true
	}
	o.Calendar.Children = append(added, o.Calendar.Children...)
	o.Zones = NewZones(o.Calendar, o.Zones.Local)
}

// Advance moves a repeating task on to its next occurrence: DTSTART and DUE
// shift by as much as the anchor moves, COUNT goes down by the one that was
// done, and the task is open again. It reports false when the rule has no
// occurrence left, or the task does not repeat, and then changes nothing.
func (o *TodoObject) Advance() bool {
	t := o.Todo
	a := t.anchor()
	if t.RRule == nil || a.IsZero() {
		return false
	}
	var until time.Time
	if t.RRule.HasUntil {
		switch {
		case t.RRule.Until.AllDay && !a.AllDay:
			until = t.RRule.Until.Wall.Add(24*time.Hour - time.Second)
		case t.RRule.Until.AllDay:
			until = t.RRule.Until.Wall
		default:
			until = o.Zones.WallIn(o.Zones.Instant(t.RRule.Until), a)
		}
	}
	limit := a.Wall.AddDate(30, 0, 0)
	walls := t.RRule.instances(a.Wall, until, limit)
	if len(walls) < 2 {
		return false
	}
	shift := walls[1].Sub(a.Wall)
	if !t.Start.IsZero() {
		t.Start.Wall = shiftWall(t.Start, shift)
	}
	if !t.Due.IsZero() {
		t.Due.Wall = shiftWall(t.Due, shift)
	}
	if t.RRule.Count > 0 {
		t.RRule.Count--
	}
	t.SetDone(false, time.Time{})
	t.Percent = 0
	return true
}

// shiftWall moves a wall clock by a span measured on another wall clock,
// keeping a date a date.
func shiftWall(d DateTime, by time.Duration) time.Time {
	if d.AllDay {
		days := int(by.Round(24*time.Hour) / (24 * time.Hour))
		return d.Wall.AddDate(0, 0, days)
	}
	return d.Wall.Add(by)
}

// HasTodo reports whether calendar data holds a VTODO and no VEVENT, which is
// how a fetched resource is told apart from an event.
func HasTodo(data []byte) bool {
	upper := bytes.ToUpper(data)
	return bytes.Contains(upper, []byte("BEGIN:VTODO")) && !bytes.Contains(upper, []byte("BEGIN:VEVENT"))
}
