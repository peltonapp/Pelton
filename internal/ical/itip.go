package ical

import (
	"bytes"
	"strings"
	"time"
)

// The iTIP (RFC 5546) methods Pelton acts on.
const (
	MethodRequest = "REQUEST"
	MethodReply   = "REPLY"
	MethodCancel  = "CANCEL"
)

// Invite is the scheduling message in a text/calendar part.
type Invite struct {
	// Method is REQUEST, CANCEL, REPLY or PUBLISH; empty when the part named
	// none, which is a plain event shared by mail.
	Method string
	Object *Object
}

// ParseInvite reads the calendar data of an invitation. Only the first event
// UID is taken: an iTIP message is about one event.
func ParseInvite(data []byte, local *time.Location) (*Invite, error) {
	cal, err := ParseCalendar(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	objs, err := Split(cal, local)
	if err != nil {
		return nil, err
	}
	return &Invite{
		Method: strings.ToUpper(strings.TrimSpace(cal.Text("METHOD"))),
		Object: objs[0],
	}, nil
}

// Attendee returns the attendee with the given address on the event, or nil.
func (e *Event) Attendee(email string) *Attendee {
	for i := range e.Attendees {
		if strings.EqualFold(e.Attendees[i].Email, email) {
			return &e.Attendees[i]
		}
	}
	return nil
}

// SetPartStat records an attendee's answer on every event in the object,
// adding the attendee when the invitation did not list them by that address.
func (o *Object) SetPartStat(email, name, partstat string) {
	for _, ev := range o.Events() {
		if a := ev.Attendee(email); a != nil {
			a.PartStat = partstat
			a.RSVP = false
		} else {
			ev.Attendees = append(ev.Attendees, Attendee{Email: email, Name: name, PartStat: partstat, Role: "REQ-PARTICIPANT"})
		}
		ev.Apply()
	}
}

// Reply builds the iTIP REPLY an attendee sends the organizer: each event in
// the object reduced to what identifies it plus that one attendee and their
// answer, as RFC 5546 asks.
func (o *Object) Reply(email, name, partstat string) *Object {
	cal := &Component{Name: "VCALENDAR"}
	cal.Set("VERSION", "2.0")
	cal.Set("PRODID", ProdID)
	cal.Set("METHOD", MethodReply)
	for _, tz := range o.Calendar.ChildrenNamed("VTIMEZONE") {
		cal.Children = append(cal.Children, tz.Clone())
	}
	reply := &Object{Calendar: cal, Zones: NewZones(cal, o.Zones.Local)}
	for _, ev := range o.Events() {
		out := &Event{
			UID:          ev.UID,
			Summary:      ev.Summary,
			Start:        ev.Start,
			End:          ev.End,
			Duration:     ev.Duration,
			HasDuration:  ev.HasDuration,
			RecurrenceID: ev.RecurrenceID,
			Sequence:     ev.Sequence,
			Organizer:    ev.Organizer,
			Attendees:    []Attendee{{Email: email, Name: name, PartStat: partstat}},
		}
		if a := ev.Attendee(email); a != nil {
			out.Attendees[0].Role = a.Role
			if out.Attendees[0].Name == "" {
				out.Attendees[0].Name = a.Name
			}
		}
		out.Apply()
		if ev == o.Master {
			reply.Master = out
		} else {
			reply.Overrides = append(reply.Overrides, out)
		}
	}
	return reply
}
