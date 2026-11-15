package desktop

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/caldav"
	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Free/busy for the scheduling assistant: the user's own busy time from the
// calendars stored here, and the attendees' from the user's own CalDAV server
// through its scheduling outbox (RFC 6638). Nothing else is asked.

// The busy kinds a span carries for the ui.
const (
	busyKindBusy        = "busy"
	busyKindTentative   = "tentative"
	busyKindUnavailable = "unavailable"
)

// Why an attendee's free time is not known.
const (
	freeBusyLocal       = "local"
	freeBusyUnsupported = "unsupported"
	freeBusyUnanswered  = "unanswered"
)

// maxFreeBusyWindow bounds one lookup.
const maxFreeBusyWindow = 31 * 24 * time.Hour

var errFreeBusyWindow = errors.New("pelton: the free/busy window is not valid")

// FreeBusyRequest asks for busy time between From and To, both rfc3339.
type FreeBusyRequest struct {
	// CalendarID is the calendar the event goes in; its CalDAV account is the
	// server asked about the attendees. A local calendar asks no server.
	CalendarID int64  `json:"calendarId"`
	From       string `json:"from"`
	To         string `json:"to"`
	// Organizer is the address the request is made as, empty for the first
	// one the server knows the user by.
	Organizer string   `json:"organizer"`
	Attendees []string `json:"attendees"`
	// ExcludeUID leaves the event being edited out of the user's own time.
	ExcludeUID string `json:"excludeUid"`
}

// BusySpanDTO is one busy stretch, rfc3339 bounds. Kind is busy, tentative or
// unavailable.
type BusySpanDTO struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Kind  string `json:"kind"`
}

// AttendeeFreeBusyDTO is what is known of one attendee's time. With Known
// false, Reason says why: local (the calendar has no server), unsupported (the
// server does not offer scheduling) or unanswered (the server could not answer
// for this address). Status is the server's request-status, when it gave one.
type AttendeeFreeBusyDTO struct {
	Email  string        `json:"email"`
	Known  bool          `json:"known"`
	Reason string        `json:"reason"`
	Status string        `json:"status"`
	Busy   []BusySpanDTO `json:"busy"`
}

// FreeBusyDTO is the result of a lookup: the user's own busy time and one
// entry per attendee asked about, in the order asked.
type FreeBusyDTO struct {
	Mine      []BusySpanDTO         `json:"mine"`
	Attendees []AttendeeFreeBusyDTO `json:"attendees"`
}

// LookupFreeBusy returns the user's busy time from the visible calendars here
// and the attendees' from the CalDAV server of the event's calendar. A server
// without scheduling support, and an attendee it cannot answer for, come back
// as unknown rather than as an error; a server that cannot be reached at all
// fails the lookup. It stops when the caller cancels the call, and after the
// calendar timeout.
func (a *App) LookupFreeBusy(ctx context.Context, req FreeBusyRequest) (FreeBusyDTO, error) {
	if err := a.ready(); err != nil {
		return FreeBusyDTO{}, err
	}
	from, err := time.Parse(time.RFC3339, req.From)
	if err != nil {
		return FreeBusyDTO{}, errFreeBusyWindow
	}
	to, err := time.Parse(time.RFC3339, req.To)
	if err != nil || !to.After(from) || to.Sub(from) > maxFreeBusyWindow {
		return FreeBusyDTO{}, errFreeBusyWindow
	}
	ctx, cancel := context.WithTimeout(ctx, calendarTimeout)
	defer cancel()

	attendees := uniqueAddresses(req.Attendees)
	out := FreeBusyDTO{Mine: []BusySpanDTO{}, Attendees: make([]AttendeeFreeBusyDTO, 0, len(attendees))}
	answers, reason, sched, err := a.attendeeFreeBusy(ctx, req, attendees, from, to)
	if err != nil {
		return FreeBusyDTO{}, err
	}
	for _, addr := range attendees {
		entry := AttendeeFreeBusyDTO{Email: addr, Reason: reason, Busy: []BusySpanDTO{}}
		if answer, ok := answers[strings.ToLower(addr)]; ok {
			entry.Status = answer.Status
			entry.Known = answer.Known
			if answer.Known {
				entry.Reason = ""
				entry.Busy = busyFromPeriods(answer.Busy, from, to)
			} else {
				entry.Reason = freeBusyUnanswered
			}
		}
		out.Attendees = append(out.Attendees, entry)
	}

	mine, err := a.selfAddresses(ctx, req.Organizer, sched.Addresses)
	if err != nil {
		return FreeBusyDTO{}, err
	}
	rows, err := a.store.QueryCalendarEvents(ctx, storage.EventQuery{From: from.Add(-24 * time.Hour), To: to.Add(24 * time.Hour), VisibleOnly: true})
	if err != nil {
		return FreeBusyDTO{}, err
	}
	cals, err := a.calendarIndex(ctx)
	if err != nil {
		return FreeBusyDTO{}, err
	}
	var objs []*ical.Object
	for _, row := range rows {
		if !cals[row.CalendarID].HoldsEvents() || (req.ExcludeUID != "" && row.UID == req.ExcludeUID) {
			continue
		}
		obj, err := parseStoredEvent(row)
		if err != nil {
			continue
		}
		objs = append(objs, obj)
	}
	out.Mine = ownBusy(objs, mine, from, to)
	return out, nil
}

// attendeeFreeBusy asks the server of the event's calendar about the
// attendees. reason is why none of them is known when the answers are empty.
func (a *App) attendeeFreeBusy(ctx context.Context, req FreeBusyRequest, attendees []string, from, to time.Time) (map[string]caldav.RecipientFreeBusy, string, caldav.Scheduling, error) {
	if len(attendees) == 0 {
		return nil, "", caldav.Scheduling{}, nil
	}
	cal, err := a.store.GetCalendar(ctx, req.CalendarID)
	if errors.Is(err, storage.ErrCalendarNotFound) {
		return nil, freeBusyLocal, caldav.Scheduling{}, nil
	}
	if err != nil {
		return nil, "", caldav.Scheduling{}, err
	}
	if cal.Local() {
		return nil, freeBusyLocal, caldav.Scheduling{}, nil
	}
	client, err := a.calendarClientFor(ctx, *cal)
	if err != nil {
		return nil, "", caldav.Scheduling{}, err
	}
	sched, err := client.Scheduling(ctx)
	if errors.Is(err, caldav.ErrNoScheduling) {
		return nil, freeBusyUnsupported, caldav.Scheduling{}, nil
	}
	if err != nil {
		return nil, "", caldav.Scheduling{}, err
	}
	organizer := pickOrganizer(req.Organizer, sched.Addresses)
	if organizer == "" {
		return nil, freeBusyUnsupported, sched, nil
	}
	answers, err := client.FreeBusy(ctx, sched.Outbox, caldav.FreeBusyQuery{Organizer: organizer, Attendees: attendees, Start: from, End: to})
	if errors.Is(err, caldav.ErrNoScheduling) {
		return nil, freeBusyUnsupported, sched, nil
	}
	if err != nil {
		return nil, "", sched, err
	}
	return answers, freeBusyUnanswered, sched, nil
}

// pickOrganizer is the address a request is made as: the asked one when the
// server knows the user by it, else the first address the server lists.
func pickOrganizer(asked string, known []string) string {
	for _, k := range known {
		if strings.EqualFold(k, asked) {
			return k
		}
	}
	if len(known) > 0 {
		return known[0]
	}
	return strings.TrimSpace(asked)
}

// selfAddresses are the addresses an invitation names the user by: their mail
// accounts, the organizer they invite as and what the server knows them by.
func (a *App) selfAddresses(ctx context.Context, organizer string, server []string) ([]string, error) {
	accounts, err := a.ownAddresses(ctx)
	if err != nil {
		return nil, err
	}
	out := append([]string{organizer}, server...)
	for addr := range accounts {
		out = append(out, addr)
	}
	return uniqueAddresses(out), nil
}

// uniqueAddresses trims, drops empty entries and keeps the first of each
// address regardless of case.
func uniqueAddresses(list []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, addr := range list {
		addr = ical.MailtoAddress(addr)
		if addr == "" || seen[strings.ToLower(addr)] {
			continue
		}
		seen[strings.ToLower(addr)] = true
		out = append(out, addr)
	}
	return out
}

// ownBusy is the user's busy time in [from, to) from their stored events. An
// event marked free (TRANSPARENT), a cancelled one and one the user declined
// keep nothing busy; an all-day event keeps its days busy only when it says
// OPAQUE outright, since a birthday or a holiday leaves the day open, which is
// how the composer's availability reads them too. A tentative event, or one
// the user has not answered or answered tentatively, is tentative.
func ownBusy(objs []*ical.Object, me []string, from, to time.Time) []BusySpanDTO {
	var spans []ical.BusyPeriod
	for _, obj := range objs {
		for _, occ := range obj.Expand(from, to) {
			ev := occ.Event
			if strings.EqualFold(ev.Status, ical.StatusCancelled) || strings.EqualFold(ev.Transparency, "TRANSPARENT") {
				continue
			}
			if occ.AllDay && !strings.EqualFold(ev.Transparency, "OPAQUE") {
				continue
			}
			kind := ical.FBTypeBusy
			if strings.EqualFold(ev.Status, ical.StatusTentative) {
				kind = ical.FBTypeBusyTentative
			}
			if self := attendeeOf(ev, me); self != nil {
				switch strings.ToUpper(self.PartStat) {
				case ical.PartStatDeclined:
					continue
				case ical.PartStatTentative, ical.PartStatNeedsAction:
					kind = ical.FBTypeBusyTentative
				}
			}
			spans = append(spans, ical.BusyPeriod{Start: occ.Start, End: occ.End, Type: kind})
		}
	}
	return busyFromPeriods(spans, from, to)
}

// attendeeOf finds the user among an event's attendees. The organizer of
// their own event is not an attendee, so nil there means busy.
func attendeeOf(ev *ical.Event, me []string) *ical.Attendee {
	for _, addr := range me {
		if a := ev.Attendee(addr); a != nil {
			return a
		}
	}
	return nil
}

// busyFromPeriods clips busy periods to [from, to) for the ui, sorted by
// start and then end.
func busyFromPeriods(periods []ical.BusyPeriod, from, to time.Time) []BusySpanDTO {
	var clipped []ical.BusyPeriod
	for _, p := range periods {
		p.Start, p.End = maxTime(p.Start, from), minTime(p.End, to)
		if p.End.After(p.Start) {
			clipped = append(clipped, p)
		}
	}
	slices.SortStableFunc(clipped, func(x, y ical.BusyPeriod) int {
		if c := x.Start.Compare(y.Start); c != 0 {
			return c
		}
		return x.End.Compare(y.End)
	})
	out := []BusySpanDTO{}
	for _, p := range clipped {
		kind := busyKindBusy
		switch p.Type {
		case ical.FBTypeBusyTentative:
			kind = busyKindTentative
		case ical.FBTypeBusyUnavailable:
			kind = busyKindUnavailable
		}
		out = append(out, BusySpanDTO{Start: p.Start.Format(time.RFC3339), End: p.End.Format(time.RFC3339), Kind: kind})
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
