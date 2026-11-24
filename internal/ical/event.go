package ical

import (
	"strconv"
	"strings"
	"time"
)

// Event is a VEVENT read into the fields a calendar shows and edits.
// Component is the VEVENT it came from; Apply writes the fields back into it
// and leaves every other property and subcomponent as it was.
type Event struct {
	Component *Component

	UID          string
	Summary      string
	Description  string
	Location     string
	URL          string
	Status       string
	Transparency string
	Categories   []string
	Sequence     int

	Start DateTime
	// End is zero when the event gives a DURATION instead, or neither.
	End         DateTime
	Duration    time.Duration
	HasDuration bool

	RRule   *RRule
	RDates  []DateTime
	ExDates []DateTime
	// RecurrenceID is set on an override of one occurrence of a recurring
	// event, naming the original start of the occurrence it replaces.
	RecurrenceID DateTime

	Organizer *Attendee
	Attendees []Attendee
	Alarms    []Alarm
}

// Attendee is an ATTENDEE or ORGANIZER.
type Attendee struct {
	Email    string
	Name     string
	Role     string
	PartStat string
	RSVP     bool
}

// Alarm is a VALARM. Trigger is relative to the start, or to the end when
// RelatedEnd; an alarm with an absolute trigger has Absolute set instead.
type Alarm struct {
	Action      string
	Trigger     time.Duration
	RelatedEnd  bool
	Absolute    DateTime
	Description string
	// component is the VALARM it was read from, kept so a rewrite does not
	// drop what the alarm carries beyond these fields.
	component *Component
}

// The STATUS values for a VEVENT.
const (
	StatusTentative = "TENTATIVE"
	StatusConfirmed = "CONFIRMED"
	StatusCancelled = "CANCELLED"
)

// The PARTSTAT values an attendee answers with.
const (
	PartStatNeedsAction = "NEEDS-ACTION"
	PartStatAccepted    = "ACCEPTED"
	PartStatTentative   = "TENTATIVE"
	PartStatDeclined    = "DECLINED"
)

// ParseEvent reads a VEVENT. Malformed dates are an error; anything else that
// does not parse is left out of the fields and kept in the component.
func ParseEvent(c *Component) (*Event, error) {
	e := &Event{
		Component:    c,
		UID:          c.Text("UID"),
		Summary:      c.Text("SUMMARY"),
		Description:  c.Text("DESCRIPTION"),
		Location:     c.Text("LOCATION"),
		URL:          strings.TrimSpace(c.Text("URL")),
		Status:       strings.ToUpper(c.Text("STATUS")),
		Transparency: strings.ToUpper(c.Text("TRANSP")),
	}
	var err error
	if e.Start, err = propDateTime(c.Prop("DTSTART")); err != nil {
		return nil, err
	}
	if e.End, err = propDateTime(c.Prop("DTEND")); err != nil {
		return nil, err
	}
	if e.End.IsZero() {
		if p := c.Prop("DURATION"); p != nil {
			if d, err := ParseDuration(p.Value); err == nil {
				e.Duration, e.HasDuration = d, true
			}
		}
	}
	if e.RecurrenceID, err = propDateTime(c.Prop("RECURRENCE-ID")); err != nil {
		return nil, err
	}
	if p := c.Prop("RRULE"); p != nil {
		if rule, err := ParseRRule(p.Value); err == nil {
			e.RRule = rule
		}
	}
	e.RDates = propDateTimes(c.PropsNamed("RDATE"))
	e.ExDates = propDateTimes(c.PropsNamed("EXDATE"))
	if n, err := strconv.Atoi(strings.TrimSpace(c.Text("SEQUENCE"))); err == nil {
		e.Sequence = n
	}
	for _, p := range c.PropsNamed("CATEGORIES") {
		for _, cat := range splitList(p.Value) {
			if cat = strings.TrimSpace(unescapeText(cat)); cat != "" {
				e.Categories = append(e.Categories, cat)
			}
		}
	}
	if p := c.Prop("ORGANIZER"); p != nil {
		org := attendeeFrom(p)
		e.Organizer = &org
	}
	for _, p := range c.PropsNamed("ATTENDEE") {
		e.Attendees = append(e.Attendees, attendeeFrom(p))
	}
	for _, a := range c.ChildrenNamed("VALARM") {
		e.Alarms = append(e.Alarms, alarmFrom(a))
	}
	return e, nil
}

func attendeeFrom(p *Prop) Attendee {
	return Attendee{
		Email:    MailtoAddress(p.Value),
		Name:     p.Param("CN"),
		Role:     strings.ToUpper(p.Param("ROLE")),
		PartStat: strings.ToUpper(p.Param("PARTSTAT")),
		RSVP:     strings.EqualFold(p.Param("RSVP"), "TRUE"),
	}
}

func alarmFrom(c *Component) Alarm {
	a := Alarm{
		Action:      strings.ToUpper(c.Text("ACTION")),
		Description: c.Text("DESCRIPTION"),
		component:   c,
	}
	if p := c.Prop("TRIGGER"); p != nil {
		if strings.EqualFold(p.Param("VALUE"), "DATE-TIME") {
			a.Absolute, _ = ParseDateTime(p.Value, "", "")
		} else if d, err := ParseDuration(p.Value); err == nil {
			a.Trigger = d
			a.RelatedEnd = strings.EqualFold(p.Param("RELATED"), "END")
		}
	}
	return a
}

// MailtoAddress strips the mailto: scheme a calendar address carries.
func MailtoAddress(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 7 && strings.EqualFold(v[:7], "mailto:") {
		v = v[7:]
	}
	return strings.TrimSpace(v)
}

// Apply writes the fields back into the component, creating it when the event
// has none. Properties Pelton has no field for stay as they were.
func (e *Event) Apply() *Component {
	c := e.Component
	if c == nil {
		c = &Component{Name: "VEVENT"}
		e.Component = c
	}
	c.SetText("UID", e.UID)
	c.Set("DTSTAMP", time.Now().UTC().Format(layoutDateTime)+"Z")
	setDate(c, "DTSTART", e.Start)
	if !e.RecurrenceID.IsZero() {
		setDate(c, "RECURRENCE-ID", e.RecurrenceID)
	} else {
		c.Remove("RECURRENCE-ID")
	}
	if !e.End.IsZero() {
		setDate(c, "DTEND", e.End)
		c.Remove("DURATION")
	} else if e.HasDuration {
		c.Remove("DTEND")
		c.Set("DURATION", FormatDuration(e.Duration))
	} else {
		c.Remove("DTEND")
		c.Remove("DURATION")
	}
	c.SetText("SUMMARY", e.Summary)
	c.SetText("DESCRIPTION", e.Description)
	c.SetText("LOCATION", e.Location)
	c.Set("URL", e.URL)
	c.Set("STATUS", e.Status)
	c.Set("TRANSP", e.Transparency)
	if e.Sequence > 0 {
		c.Set("SEQUENCE", strconv.Itoa(e.Sequence))
	}
	if len(e.Categories) > 0 {
		escaped := make([]string, len(e.Categories))
		for i, cat := range e.Categories {
			escaped[i] = escapeText(cat)
		}
		c.Set("CATEGORIES", strings.Join(escaped, ","))
	} else {
		c.Remove("CATEGORIES")
	}
	if e.RRule != nil {
		c.Set("RRULE", e.RRule.String())
	} else {
		c.Remove("RRULE")
	}
	setDateList(c, "RDATE", e.RDates)
	setDateList(c, "EXDATE", e.ExDates)
	e.applyAttendees(c)
	e.applyAlarms(c)
	return c
}

func setDate(c *Component, name string, d DateTime) {
	if d.IsZero() {
		c.Remove(name)
		return
	}
	p := d.Prop(name)
	if existing := c.Prop(name); existing != nil {
		for _, param := range existing.Params {
			if !strings.EqualFold(param.Name, "TZID") && !strings.EqualFold(param.Name, "VALUE") {
				p.Params = append(p.Params, param)
			}
		}
	}
	c.Set(name, p.Value, p.Params...)
}

// setDateList writes one property per date, which every reader understands,
// rather than packing them onto one line.
func setDateList(c *Component, name string, dates []DateTime) {
	c.Remove(name)
	for _, d := range dates {
		c.Add(d.Prop(name))
	}
}

// applyAttendees rewrites ORGANIZER and ATTENDEE, keeping the parameters of an
// attendee who is still on the list (delegation, member, language) and
// changing only the ones Attendee carries.
func (e *Event) applyAttendees(c *Component) {
	existing := map[string]*Prop{}
	for _, p := range c.PropsNamed("ATTENDEE") {
		existing[strings.ToLower(MailtoAddress(p.Value))] = p
	}
	var org *Prop
	if e.Organizer != nil {
		org = c.Prop("ORGANIZER")
		if org == nil || !strings.EqualFold(MailtoAddress(org.Value), e.Organizer.Email) {
			org = &Prop{Name: "ORGANIZER"}
		}
		org.Value = "mailto:" + e.Organizer.Email
		org.SetParam("CN", e.Organizer.Name)
	}
	c.Remove("ORGANIZER")
	c.Remove("ATTENDEE")
	if org != nil {
		c.Add(org)
	}
	for _, a := range e.Attendees {
		p := existing[strings.ToLower(a.Email)]
		if p == nil {
			p = &Prop{Name: "ATTENDEE", Value: "mailto:" + a.Email}
		}
		p.SetParam("CN", a.Name)
		p.SetParam("ROLE", a.Role)
		p.SetParam("PARTSTAT", a.PartStat)
		if a.RSVP {
			p.SetParam("RSVP", "TRUE")
		} else {
			p.SetParam("RSVP", "")
		}
		c.Add(p)
	}
}

// applyAlarms rewrites the VALARM children from Alarms, reusing the component
// each alarm was read from.
func (e *Event) applyAlarms(c *Component) {
	c.RemoveChildren("VALARM")
	for _, a := range e.Alarms {
		comp := a.component
		if comp == nil {
			comp = &Component{Name: "VALARM"}
		}
		action := a.Action
		if action == "" {
			action = "DISPLAY"
		}
		comp.Set("ACTION", action)
		if !a.Absolute.IsZero() {
			comp.Set("TRIGGER", a.Absolute.Value(), Param{Name: "VALUE", Values: []string{"DATE-TIME"}})
		} else if a.RelatedEnd {
			comp.Set("TRIGGER", FormatDuration(a.Trigger), Param{Name: "RELATED", Values: []string{"END"}})
		} else {
			comp.Set("TRIGGER", FormatDuration(a.Trigger))
		}
		description := a.Description
		if description == "" && action == "DISPLAY" {
			description = e.Summary
			if description == "" {
				description = "Reminder"
			}
		}
		comp.SetText("DESCRIPTION", description)
		c.Children = append(c.Children, comp)
	}
}

// IsRecurring reports whether the event repeats.
func (e *Event) IsRecurring() bool {
	return e.RRule != nil || len(e.RDates) > 0
}

// endWall returns the wall-clock end of an occurrence starting at start, from
// DTEND, DURATION, or the RFC 5545 defaults: one day for a date, zero length
// for a date-time. An end written in another zone than the start is measured
// as elapsed time instead of on the wall clock.
func (e *Event) endWall(z *Zones, start time.Time) time.Time {
	switch {
	case !e.End.IsZero() && (e.End.TZID != e.Start.TZID || e.End.UTC != e.Start.UTC):
		return start.Add(z.Instant(e.End).Sub(z.Instant(e.Start)))
	case !e.End.IsZero():
		return start.Add(e.End.Wall.Sub(e.Start.Wall))
	case e.HasDuration:
		return start.Add(e.Duration)
	case e.Start.AllDay:
		return start.AddDate(0, 0, 1)
	}
	return start
}
