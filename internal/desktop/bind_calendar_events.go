package desktop

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/peltonapp/Pelton/internal/caldav"
	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Events: reading occurrences for a window, and creating, editing and deleting
// events. An edit is stored here first and then written to the server; when
// the server cannot be reached the change waits in the queue the calendar
// sync drains, and when the server's copy moved on since the local one was
// fetched the change is kept and marked as a conflict for the user to settle.

// The scopes an edit or deletion of a recurring event's occurrence takes.
const (
	ScopeThis   = "this"
	ScopeFuture = "future"
	ScopeAll    = "all"
)

// dateLayout is how an all-day date travels to and from the ui.
const dateLayout = "2006-01-02"

// OccurrenceDTO is one occurrence in a window, already expanded, with the
// calendar's color so the ui can draw it without a second lookup. Start and
// End are rfc3339; for an all-day occurrence they are dates, End exclusive.
type OccurrenceDTO struct {
	EventID       int64  `json:"eventId"`
	CalendarID    int64  `json:"calendarId"`
	CalendarColor string `json:"calendarColor"`
	UID           string `json:"uid"`
	Summary       string `json:"summary"`
	Location      string `json:"location"`
	Start         string `json:"start"`
	End           string `json:"end"`
	AllDay        bool   `json:"allDay"`
	Recurring     bool   `json:"recurring"`
	// RecurrenceID names the occurrence for GetEvent, UpdateEvent and
	// DeleteEvent: the rfc3339 instant it originally started at. Empty for an
	// event that does not repeat.
	RecurrenceID string `json:"recurrenceId"`
	// Override is true when this occurrence was edited on its own.
	Override     bool   `json:"override"`
	Status       string `json:"status"`
	Transparency string `json:"transparency"`
	ReadOnly     bool   `json:"readOnly"`
	HasReminders bool   `json:"hasReminders"`
	// HasAttendees is true when others are invited, which is when preparing
	// for the meeting has something to show.
	HasAttendees bool `json:"hasAttendees"`
	// Pending is true while a change made here has not reached the server.
	Pending  bool `json:"pending"`
	Conflict bool `json:"conflict"`
}

// ReminderDTO is one reminder. MinutesBefore counts back from the start, or
// from the end with RelatedEnd; negative means after. Absolute, when set, is
// an rfc3339 time that replaces both.
type ReminderDTO struct {
	MinutesBefore int    `json:"minutesBefore"`
	RelatedEnd    bool   `json:"relatedEnd"`
	Absolute      string `json:"absolute"`
	// Action is DISPLAY, AUDIO or EMAIL; empty means DISPLAY.
	Action string `json:"action"`
}

// AttendeeDTO is an attendee or the organizer. PartStat is NEEDS-ACTION,
// ACCEPTED, TENTATIVE, DECLINED or DELEGATED; Role is REQ-PARTICIPANT,
// OPT-PARTICIPANT, NON-PARTICIPANT or CHAIR.
type AttendeeDTO struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	PartStat string `json:"partStat"`
	RSVP     bool   `json:"rsvp"`
}

// EventDTO is one event, or one occurrence of a recurring one, for the editor.
type EventDTO struct {
	ID            int64    `json:"id"`
	CalendarID    int64    `json:"calendarId"`
	CalendarName  string   `json:"calendarName"`
	CalendarColor string   `json:"calendarColor"`
	UID           string   `json:"uid"`
	Summary       string   `json:"summary"`
	Description   string   `json:"description"`
	Location      string   `json:"location"`
	URL           string   `json:"url"`
	Status        string   `json:"status"`
	Transparency  string   `json:"transparency"`
	Categories    []string `json:"categories"`
	// Start and End are rfc3339, or dates for an all-day event with End
	// exclusive.
	Start  string `json:"start"`
	End    string `json:"end"`
	AllDay bool   `json:"allDay"`
	// TimeZone is the IANA zone the event was written in, empty for UTC or a
	// floating time.
	TimeZone string `json:"timeZone"`
	// RRule is the series' rule as an RRULE value, empty for none.
	RRule     string `json:"rrule"`
	Recurring bool   `json:"recurring"`
	// ExDates are the rfc3339 starts of occurrences removed from the series.
	ExDates []string `json:"exDates"`
	// RecurrenceID is set when this is one occurrence of a series.
	RecurrenceID string        `json:"recurrenceId"`
	Reminders    []ReminderDTO `json:"reminders"`
	Organizer    AttendeeDTO   `json:"organizer"`
	Attendees    []AttendeeDTO `json:"attendees"`
	ReadOnly     bool          `json:"readOnly"`
	Pending      bool          `json:"pending"`
	Conflict     bool          `json:"conflict"`
	SyncError    string        `json:"syncError"`
	// Extra lists the iCalendar properties Pelton keeps but has no field for,
	// so the editor can say the event carries more than the form.
	Extra []string `json:"extra"`
}

// EventRequest is the editor's event. An ID of 0 creates one in CalendarID.
// For an occurrence of a recurring event, RecurrenceID names it and Scope says
// whether the change is for this occurrence, this and the following ones, or
// the whole series.
type EventRequest struct {
	ID           int64         `json:"id"`
	CalendarID   int64         `json:"calendarId"`
	Summary      string        `json:"summary"`
	Description  string        `json:"description"`
	Location     string        `json:"location"`
	URL          string        `json:"url"`
	Status       string        `json:"status"`
	Transparency string        `json:"transparency"`
	Categories   []string      `json:"categories"`
	Start        string        `json:"start"`
	End          string        `json:"end"`
	AllDay       bool          `json:"allDay"`
	TimeZone     string        `json:"timeZone"`
	RRule        string        `json:"rrule"`
	Reminders    []ReminderDTO `json:"reminders"`
	Organizer    AttendeeDTO   `json:"organizer"`
	Attendees    []AttendeeDTO `json:"attendees"`
	Scope        string        `json:"scope"`
	RecurrenceID string        `json:"recurrenceId"`
	// Force writes over the server's copy. Only the "keep mine" choice on a
	// conflict sets it.
	Force bool `json:"force"`
}

// EventSaveResultDTO is the outcome of a save or delete.
type EventSaveResultDTO struct {
	// Event is the saved event, or the edited occurrence of it.
	Event EventDTO `json:"event"`
	// Queued is true when the server could not be reached and the change
	// waits for the next sync.
	Queued bool `json:"queued"`
	// Conflict is true when the server refused the change because its copy
	// moved on. The change is kept, Server holds the server's version, and
	// ResolveEventConflict settles it.
	Conflict bool     `json:"conflict"`
	Server   EventDTO `json:"server"`
}

// ListOccurrences returns the occurrences overlapping [from, to), both
// rfc3339, sorted by start. calendarIDs limits them to those calendars; empty
// means every visible calendar.
func (a *App) ListOccurrences(from, to string, calendarIDs []int64) ([]OccurrenceDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	start, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return nil, err
	}
	cals, err := a.calendarIndex(a.ctx)
	if err != nil {
		return nil, err
	}
	// a day either side covers all-day and floating events, which are stored
	// against the zone the app ran in when they were written.
	rows, err := a.store.QueryCalendarEvents(a.ctx, storage.EventQuery{
		From:        start.Add(-24 * time.Hour),
		To:          end.Add(24 * time.Hour),
		CalendarIDs: calendarIDs,
		VisibleOnly: len(calendarIDs) == 0,
	})
	if err != nil {
		return nil, err
	}
	out := []OccurrenceDTO{}
	for _, row := range rows {
		obj, err := parseStoredEvent(row)
		if err != nil {
			a.log.Warn("unreadable calendar event", "event", row.ID, "err", err)
			continue
		}
		cal := cals[row.CalendarID]
		recurring := obj.Master != nil && obj.Master.IsRecurring() || len(obj.Overrides) > 0
		for _, occ := range obj.Expand(start, end) {
			out = append(out, OccurrenceDTO{
				EventID:       row.ID,
				CalendarID:    row.CalendarID,
				CalendarColor: cal.Color,
				UID:           row.UID,
				Summary:       occ.Event.Summary,
				Location:      occ.Event.Location,
				Start:         formatEventTime(occ.Start, occ.AllDay),
				End:           formatEventTime(occ.End, occ.AllDay),
				AllDay:        occ.AllDay,
				Recurring:     recurring,
				RecurrenceID:  formatRecurrenceID(occ.RecurrenceID),
				Override:      occ.Override,
				Status:        occ.Event.Status,
				Transparency:  occ.Event.Transparency,
				ReadOnly:      cal.ReadOnly,
				HasReminders:  len(occ.Event.Alarms) > 0,
				HasAttendees:  len(occ.Event.Attendees) > 0,
				Pending:       row.Pending != "",
				Conflict:      row.Conflict,
			})
		}
	}
	slices.SortStableFunc(out, func(x, y OccurrenceDTO) int { return strings.Compare(sortKey(x), sortKey(y)) })
	return out, nil
}

// sortKey orders all-day occurrences before timed ones on the same day.
func sortKey(o OccurrenceDTO) string {
	if o.AllDay {
		t, _ := time.ParseInLocation(dateLayout, o.Start, time.Local)
		return t.UTC().Format(time.RFC3339) + "0"
	}
	t, _ := time.Parse(time.RFC3339, o.Start)
	return t.UTC().Format(time.RFC3339) + "1"
}

// GetEvent returns an event for the editor. With a recurrenceID it returns
// that occurrence: its override when it has one, else the series' fields at
// the occurrence's time.
func (a *App) GetEvent(id int64, recurrenceID string) (EventDTO, error) {
	if err := a.ready(); err != nil {
		return EventDTO{}, err
	}
	row, err := a.store.GetCalendarEvent(a.ctx, id)
	if err != nil {
		return EventDTO{}, err
	}
	obj, err := parseStoredEvent(*row)
	if err != nil {
		return EventDTO{}, err
	}
	cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
	if err != nil {
		return EventDTO{}, err
	}
	rid, err := parseRecurrenceID(recurrenceID)
	if err != nil {
		return EventDTO{}, err
	}
	return occurrenceDTO(*row, *cal, obj, rid)
}

// CreateEvent stores a new event and writes it to its calendar's server.
func (a *App) CreateEvent(req EventRequest) (EventSaveResultDTO, error) {
	res, err := a.createEvent(req)
	if err == nil {
		a.scriptEventSaved(a.ctx, res.Event.ID, true)
	}
	return res, err
}

func (a *App) createEvent(req EventRequest) (EventSaveResultDTO, error) {
	if err := a.ready(); err != nil {
		return EventSaveResultDTO{}, err
	}
	if req.CalendarID == 0 {
		return EventSaveResultDTO{}, errEventNeedsCalendar
	}
	cal, err := a.store.GetCalendar(a.ctx, req.CalendarID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	if cal.ReadOnly {
		return EventSaveResultDTO{}, errCalendarReadOnly
	}
	start, end, err := requestTimes(req)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	ev := &ical.Event{UID: uuid.NewString()}
	if err := applyEventRequest(ev, req, start, end, true); err != nil {
		return EventSaveResultDTO{}, err
	}
	obj := ical.NewObject(ev, time.Local)
	row := newEventRow(*cal, obj)
	return a.commitEvent(a.ctx, *cal, row, obj, time.Time{}, false)
}

// UpdateEvent saves an edit. For an occurrence of a recurring event, Scope
// decides what changes: ScopeThis overrides that occurrence alone, ScopeFuture
// splits the series there and changes the part from it on, ScopeAll changes
// the series, moving every occurrence by as much as this one moved.
func (a *App) UpdateEvent(req EventRequest) (EventSaveResultDTO, error) {
	res, err := a.updateEvent(req)
	if err == nil {
		a.scriptEventSaved(a.ctx, res.Event.ID, false)
	}
	return res, err
}

func (a *App) updateEvent(req EventRequest) (EventSaveResultDTO, error) {
	if err := a.ready(); err != nil {
		return EventSaveResultDTO{}, err
	}
	row, err := a.store.GetCalendarEvent(a.ctx, req.ID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	if cal.ReadOnly {
		return EventSaveResultDTO{}, errCalendarReadOnly
	}
	obj, err := parseStoredEvent(*row)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	start, end, err := requestTimes(req)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	rid, err := parseRecurrenceID(req.RecurrenceID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	recurring := obj.Master != nil && obj.Master.IsRecurring()
	scope := req.Scope
	if !recurring || rid.IsZero() || scope == "" {
		scope = ScopeAll
	}
	if scope == ScopeFuture && obj.Master != nil && !rid.After(obj.Zones.Instant(obj.Master.Start)) {
		scope = ScopeAll
	}

	switch scope {
	case ScopeThis:
		if obj.Master == nil && obj.FindOverride(rid) == nil {
			return EventSaveResultDTO{}, errEventNoOccurrence
		}
		var ov *ical.Event
		if obj.Master != nil {
			ov = obj.Override(rid)
		} else {
			ov = obj.FindOverride(rid)
		}
		if err := applyEventRequest(ov, req, start, end, false); err != nil {
			return EventSaveResultDTO{}, err
		}
		ov.Sequence++
		ov.Apply()
		obj.EnsureTimezones()
		return a.commitEvent(a.ctx, *cal, *row, obj, rid, req.Force)

	case ScopeFuture:
		original := ""
		if obj.Master.RRule != nil {
			original = obj.Master.RRule.String()
		}
		future := obj.SplitAt(rid, uuid.NewString())
		kept := future.Master.RRule
		if err := applyEventRequest(future.Master, req, start, end, true); err != nil {
			return EventSaveResultDTO{}, err
		}
		// the editor sends back the rule it was shown; unchanged, the split's
		// rule stands, with its COUNT reduced by what stayed behind.
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(req.RRule), "RRULE:"), original) {
			future.Master.RRule = kept
		}
		future.Master.Apply()
		future.EnsureTimezones()
		if _, err := a.commitEvent(a.ctx, *cal, *row, obj, time.Time{}, req.Force); err != nil {
			return EventSaveResultDTO{}, err
		}
		return a.commitEvent(a.ctx, *cal, newEventRow(*cal, future), future, time.Time{}, false)
	}

	target := obj.First()
	if obj.Master != nil && !rid.IsZero() {
		shiftSeries(obj, rid, start)
	}
	if err := applyEventRequest(target, req, startFor(obj, target, rid, start), endFor(obj, target, rid, start, end), true); err != nil {
		return EventSaveResultDTO{}, err
	}
	target.Sequence++
	target.Apply()
	obj.EnsureTimezones()

	if req.CalendarID != 0 && req.CalendarID != row.CalendarID {
		return a.moveEvent(*row, *cal, req.CalendarID, obj)
	}
	return a.commitEvent(a.ctx, *cal, *row, obj, time.Time{}, req.Force)
}

// DeleteEvent deletes an event, or for an occurrence of a recurring one, that
// occurrence (ScopeThis), it and the ones after (ScopeFuture), or the series
// (ScopeAll).
func (a *App) DeleteEvent(id int64, scope, recurrenceID string, force bool) (EventSaveResultDTO, error) {
	if err := a.ready(); err != nil {
		return EventSaveResultDTO{}, err
	}
	row, err := a.store.GetCalendarEvent(a.ctx, id)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	if cal.ReadOnly {
		return EventSaveResultDTO{}, errCalendarReadOnly
	}
	obj, err := parseStoredEvent(*row)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	rid, err := parseRecurrenceID(recurrenceID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	if !rid.IsZero() && obj.Master != nil && obj.Master.IsRecurring() {
		switch scope {
		case ScopeThis:
			obj.ExcludeOccurrence(rid)
			return a.commitEvent(a.ctx, *cal, *row, obj, time.Time{}, force)
		case ScopeFuture:
			if obj.TruncateBefore(rid) {
				return a.commitEvent(a.ctx, *cal, *row, obj, time.Time{}, force)
			}
		}
	}
	return a.deleteEventRow(a.ctx, *cal, *row, force)
}

// ResolveEventConflict settles a conflict: keepMine writes the local version
// over the server's, otherwise the server's version replaces the local one.
func (a *App) ResolveEventConflict(id int64, keepMine bool) (EventDTO, error) {
	if err := a.ready(); err != nil {
		return EventDTO{}, err
	}
	cal, fresh, err := a.resolveRowConflict(id, keepMine)
	if err != nil || fresh == nil {
		return EventDTO{}, err
	}
	obj, err := parseStoredEvent(*fresh)
	if err != nil {
		return EventDTO{}, err
	}
	return occurrenceDTO(*fresh, *cal, obj, time.Time{})
}

// resolveRowConflict settles the conflict of an event or a task and returns
// its calendar and the row as it now stands, nil when the server no longer
// had it and the local copy went too.
func (a *App) resolveRowConflict(id int64, keepMine bool) (*storage.Calendar, *storage.CalendarEvent, error) {
	row, err := a.store.GetCalendarEvent(a.ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !row.Conflict {
		return nil, nil, errEventNotInConflict
	}
	cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
	if err != nil {
		return nil, nil, err
	}
	acct, err := a.store.GetCalendarAccount(a.ctx, cal.AccountID)
	if err != nil {
		return nil, nil, err
	}
	client, err := a.calendarClient(*acct)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, calendarTimeout)
	defer cancel()

	if keepMine {
		row.Conflict = false
		if err := a.pushEvent(ctx, client, *cal, row, true); err != nil {
			return nil, nil, err
		}
	} else {
		server, err := client.Get(ctx, row.Path)
		switch {
		case errors.Is(err, caldav.ErrNotFound):
			if err := a.store.DeleteCalendarEvent(a.ctx, row.ID); err != nil {
				return nil, nil, err
			}
			a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
			return cal, nil, nil
		case err != nil:
			return nil, nil, err
		}
		row.Pending, row.Conflict = "", false
		if err := a.store.SaveCalendarEvent(a.ctx, row); err != nil {
			return nil, nil, err
		}
		if _, err := a.storeResource(a.ctx, *cal, server); err != nil {
			return nil, nil, err
		}
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	fresh, err := a.store.GetCalendarEventByUID(a.ctx, cal.ID, row.UID)
	if err != nil {
		return nil, nil, err
	}
	return cal, fresh, nil
}

// CreateEventDraftFromMessage prefills an event from a mail, for dropping a
// message onto the calendar: the subject without its Re: and Fwd: as the
// title, the message's opening as the description, and everyone on it but the
// user as attendees. It starts at the next full hour for an hour; the ui moves
// it to where the message was dropped. Nothing is stored.
func (a *App) CreateEventDraftFromMessage(messageID int64) (EventRequest, error) {
	if err := a.ready(); err != nil {
		return EventRequest{}, err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return EventRequest{}, err
	}
	own := ""
	if account, err := a.store.GetAccount(a.ctx, m.AccountID); err == nil {
		own = strings.ToLower(account.Email)
	}
	cals, err := a.store.ListCalendars(a.ctx)
	if err != nil {
		return EventRequest{}, err
	}
	start := time.Now().Truncate(time.Hour).Add(time.Hour)
	req := EventRequest{
		CalendarID:  a.defaultCalendarID(cals),
		Summary:     stripSubjectPrefixes(m.Subject),
		Description: mailview.Snippet(m.BodyPlain, m.BodyHTML),
		Start:       start.Format(time.RFC3339),
		End:         start.Add(time.Hour).Format(time.RFC3339),
		Attendees:   []AttendeeDTO{},
		Reminders:   a.defaultReminders(),
	}
	seen := map[string]bool{own: true}
	for _, list := range []string{m.FromName + " <" + m.FromAddress + ">", m.ToAddresses, m.CcAddresses} {
		for _, addr := range parseAddresses(list) {
			email := strings.ToLower(addr.Address)
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			req.Attendees = append(req.Attendees, AttendeeDTO{
				Email: addr.Address, Name: addr.Name, Role: "REQ-PARTICIPANT", PartStat: ical.PartStatNeedsAction, RSVP: true,
			})
		}
	}
	return req, nil
}

// defaultReminders is the reminder setting as a list for a new event.
func (a *App) defaultReminders() []ReminderDTO {
	minutes := a.intSetting(settingCalendarDefault, 15)
	if minutes < 0 {
		return []ReminderDTO{}
	}
	return []ReminderDTO{{MinutesBefore: minutes}}
}

var subjectPrefix = regexp.MustCompile(`(?i)^\s*((re|fw|fwd|aw|wg|sv|vs|tr|rv)\s*(\[\d+\])?\s*:\s*)+`)

func stripSubjectPrefixes(s string) string {
	return strings.TrimSpace(subjectPrefix.ReplaceAllString(s, ""))
}

// parseAddresses reads a stored address list, falling back to splitting on
// commas for one net/mail refuses.
func parseAddresses(list string) []*mail.Address {
	list = strings.TrimSpace(list)
	if list == "" || list == "<>" {
		return nil
	}
	if parsed, err := mail.ParseAddressList(list); err == nil {
		return parsed
	}
	var out []*mail.Address
	for _, part := range splitAddresses(list) {
		if addr := bareAddress(part); strings.Contains(addr, "@") {
			out = append(out, &mail.Address{Address: addr})
		}
	}
	return out
}

// requestTimes reads the request's start and end in the form they are stored:
// a date for an all-day event, a wall time in the request's zone, or UTC. A
// missing end is an hour after the start, or the next day for an all-day
// event.
func requestTimes(req EventRequest) (ical.DateTime, ical.DateTime, error) {
	start, err := requestTime(req.Start, req.AllDay, req.TimeZone)
	if err != nil {
		return start, start, err
	}
	if start.IsZero() {
		return start, start, errEventNeedsStart
	}
	end, err := requestTime(req.End, req.AllDay, req.TimeZone)
	if err != nil {
		return start, end, err
	}
	switch {
	case end.IsZero() && req.AllDay:
		end = ical.DateTime{Wall: start.Wall.AddDate(0, 0, 1), AllDay: true}
	case end.IsZero():
		end = start
		end.Wall = start.Wall.Add(time.Hour)
	case end.Wall.Before(start.Wall):
		return start, end, errEventEndBeforeStart
	case req.AllDay && !end.Wall.After(start.Wall):
		end.Wall = start.Wall.AddDate(0, 0, 1)
	}
	return start, end, nil
}

func requestTime(s string, allDay bool, tz string) (ical.DateTime, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ical.DateTime{}, nil
	}
	if allDay {
		if len(s) > len(dateLayout) {
			s = s[:len(dateLayout)]
		}
		t, err := time.Parse(dateLayout, s)
		if err != nil {
			return ical.DateTime{}, err
		}
		return ical.DateTime{Wall: t, AllDay: true}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ical.DateTime{}, err
	}
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			in := t.In(loc)
			wall := time.Date(in.Year(), in.Month(), in.Day(), in.Hour(), in.Minute(), in.Second(), 0, time.UTC)
			return ical.DateTime{Wall: wall, TZID: tz}, nil
		}
	}
	return ical.DateTime{Wall: t.UTC().Truncate(time.Second), UTC: true}, nil
}

// applyEventRequest writes the editor's fields into an event. The rule is only
// taken for a series or a plain event, never for one occurrence.
func applyEventRequest(ev *ical.Event, req EventRequest, start, end ical.DateTime, withRule bool) error {
	ev.Summary = strings.TrimSpace(req.Summary)
	ev.Description = req.Description
	ev.Location = strings.TrimSpace(req.Location)
	ev.URL = strings.TrimSpace(req.URL)
	ev.Status = strings.ToUpper(strings.TrimSpace(req.Status))
	ev.Transparency = strings.ToUpper(strings.TrimSpace(req.Transparency))
	ev.Categories = nil
	for _, c := range req.Categories {
		if c = strings.TrimSpace(c); c != "" {
			ev.Categories = append(ev.Categories, c)
		}
	}
	ev.Start, ev.End = start, end
	ev.HasDuration = false
	if withRule {
		ev.RRule = nil
		if rule := strings.TrimSpace(strings.TrimPrefix(req.RRule, "RRULE:")); rule != "" {
			parsed, err := ical.ParseRRule(rule)
			if err != nil {
				return err
			}
			ev.RRule = parsed
		}
	}
	ev.Alarms = remindersToAlarms(ev.Alarms, req.Reminders)
	ev.Organizer = nil
	if req.Organizer.Email != "" {
		ev.Organizer = &ical.Attendee{Email: req.Organizer.Email, Name: req.Organizer.Name}
	}
	ev.Attendees = nil
	for _, at := range req.Attendees {
		if strings.TrimSpace(at.Email) == "" {
			continue
		}
		ev.Attendees = append(ev.Attendees, ical.Attendee{
			Email:    strings.TrimSpace(at.Email),
			Name:     at.Name,
			Role:     strings.ToUpper(at.Role),
			PartStat: strings.ToUpper(at.PartStat),
			RSVP:     at.RSVP,
		})
	}
	return nil
}

// remindersToAlarms turns the editor's reminders into alarms, reusing the
// existing alarms in order so what they carry beyond the trigger survives.
func remindersToAlarms(existing []ical.Alarm, reminders []ReminderDTO) []ical.Alarm {
	out := make([]ical.Alarm, 0, len(reminders))
	for i, r := range reminders {
		var alarm ical.Alarm
		if i < len(existing) {
			alarm = existing[i]
		}
		alarm.Action = strings.ToUpper(r.Action)
		alarm.Absolute = ical.DateTime{}
		alarm.Trigger = -time.Duration(r.MinutesBefore) * time.Minute
		alarm.RelatedEnd = r.RelatedEnd
		if r.Absolute != "" {
			if t, err := time.Parse(time.RFC3339, r.Absolute); err == nil {
				alarm.Absolute = ical.DateTime{Wall: t.UTC(), UTC: true}
			}
		}
		out = append(out, alarm)
	}
	return out
}

// shiftSeries moves a series by as much as the occurrence at rid moves to
// newStart, including its exceptions and overrides, so they still pair with
// the occurrences they name.
func shiftSeries(obj *ical.Object, rid time.Time, newStart ical.DateTime) {
	shift := obj.Zones.Instant(newStart).Sub(rid)
	if shift == 0 {
		return
	}
	m := obj.Master
	for i, ex := range m.ExDates {
		m.ExDates[i] = obj.DateLike(obj.Zones.Instant(ex).Add(shift), m.Start)
	}
	for _, ov := range obj.Overrides {
		ov.RecurrenceID = obj.DateLike(obj.Zones.Instant(ov.RecurrenceID).Add(shift), m.Start)
		ov.Apply()
	}
}

// startFor is the series start a ScopeAll edit gives the master: the request's
// start for a plain event, or the master's start moved by as much as the
// edited occurrence moved.
func startFor(obj *ical.Object, target *ical.Event, rid time.Time, start ical.DateTime) ical.DateTime {
	if rid.IsZero() || target != obj.Master {
		return start
	}
	shift := obj.Zones.Instant(start).Sub(rid)
	moved := obj.Zones.Instant(target.Start).Add(shift)
	probe := ical.NewZones(nil, obj.Zones.Local)
	if start.AllDay {
		w := probe.WallIn(moved, start)
		return ical.DateTime{Wall: time.Date(w.Year(), w.Month(), w.Day(), 0, 0, 0, 0, time.UTC), AllDay: true}
	}
	if start.UTC {
		return ical.DateTime{Wall: moved.UTC(), UTC: true}
	}
	return ical.DateTime{Wall: probe.WallIn(moved, start), TZID: start.TZID}
}

// endFor keeps the request's duration from the series start startFor picks.
func endFor(obj *ical.Object, target *ical.Event, rid time.Time, start, end ical.DateTime) ical.DateTime {
	s := startFor(obj, target, rid, start)
	out := end
	out.Wall = s.Wall.Add(end.Wall.Sub(start.Wall))
	return out
}

// parseStoredEvent reads a stored event's iCalendar text.
func parseStoredEvent(row storage.CalendarEvent) (*ical.Object, error) {
	return ical.ParseObject([]byte(row.Data), time.Local)
}

// newEventRow is the row for an object not stored yet.
func newEventRow(cal storage.Calendar, obj *ical.Object) storage.CalendarEvent {
	return storage.CalendarEvent{CalendarID: cal.ID, UID: obj.UID()}
}

// fillEventRow writes the object and its index columns into a row.
func fillEventRow(row *storage.CalendarEvent, obj *ical.Object) {
	row.UID = obj.UID()
	row.Data = string(obj.Encode())
	first := obj.First()
	if first != nil {
		row.Summary = first.Summary
		row.Location = first.Location
	}
	start, end, open := obj.Bounds()
	row.RangeStart, row.RangeEnd = start, end
	if open {
		row.RangeEnd = time.Time{}
	} else if end.IsZero() {
		row.RangeEnd = start
	}
	row.Recurring = obj.Master != nil && obj.Master.IsRecurring()
	row.HasAlarms = false
	for _, ev := range obj.Events() {
		if len(ev.Alarms) > 0 {
			row.HasAlarms = true
		}
	}
}

// occurrenceDTO builds the editor's view of an event, or of the occurrence at
// rid when it is set.
func occurrenceDTO(row storage.CalendarEvent, cal storage.Calendar, obj *ical.Object, rid time.Time) (EventDTO, error) {
	ev := obj.First()
	if ev == nil {
		return EventDTO{}, ical.ErrNoEvent
	}
	start := obj.Zones.Instant(ev.Start)
	occs := obj.Expand(start, start.Add(time.Second))
	if !rid.IsZero() {
		if ov := obj.FindOverride(rid); ov != nil {
			ev = ov
		} else if obj.Master != nil {
			ev = obj.Master
		}
		occs = obj.Expand(rid.Add(-time.Second), rid.Add(time.Second))
	}
	dto := eventDTO(row, cal, obj, ev)
	for _, occ := range occs {
		if occ.Event == ev && (rid.IsZero() || occ.RecurrenceID.Equal(rid)) {
			dto.Start = formatEventTime(occ.Start, occ.AllDay)
			dto.End = formatEventTime(occ.End, occ.AllDay)
			dto.RecurrenceID = formatRecurrenceID(occ.RecurrenceID)
			return dto, nil
		}
	}
	if !rid.IsZero() && obj.FindOverride(rid) == nil {
		return EventDTO{}, errEventNoOccurrence
	}
	return dto, nil
}

func eventDTO(row storage.CalendarEvent, cal storage.Calendar, obj *ical.Object, ev *ical.Event) EventDTO {
	start := obj.Zones.Instant(ev.Start)
	dto := EventDTO{
		ID:            row.ID,
		CalendarID:    cal.ID,
		CalendarName:  cal.Name,
		CalendarColor: cal.Color,
		UID:           ev.UID,
		Summary:       ev.Summary,
		Description:   ev.Description,
		Location:      ev.Location,
		URL:           ev.URL,
		Status:        ev.Status,
		Transparency:  ev.Transparency,
		Categories:    append([]string{}, ev.Categories...),
		Start:         formatEventTime(start, ev.Start.AllDay),
		AllDay:        ev.Start.AllDay,
		ExDates:       []string{},
		Reminders:     []ReminderDTO{},
		Attendees:     []AttendeeDTO{},
		ReadOnly:      cal.ReadOnly,
		Pending:       row.Pending != "",
		Conflict:      row.Conflict,
		SyncError:     row.SyncError,
		Extra:         extraEventProperties(ev),
	}
	if loc, ok := obj.Zones.Location(ev.Start.TZID); ok && !ev.Start.UTC {
		dto.TimeZone = loc.String()
	}
	for _, occ := range obj.Expand(start, start.Add(time.Second)) {
		if occ.Event == ev {
			dto.End = formatEventTime(occ.End, occ.AllDay)
		}
	}
	if dto.End == "" {
		dto.End = dto.Start
	}
	if m := obj.Master; m != nil {
		dto.Recurring = m.IsRecurring()
		if m.RRule != nil {
			dto.RRule = m.RRule.String()
		}
		for _, ex := range m.ExDates {
			dto.ExDates = append(dto.ExDates, formatRecurrenceID(obj.Zones.Instant(ex)))
		}
	}
	if !ev.RecurrenceID.IsZero() {
		dto.RecurrenceID = formatRecurrenceID(obj.Zones.Instant(ev.RecurrenceID))
	}
	for _, al := range ev.Alarms {
		r := ReminderDTO{MinutesBefore: int(-al.Trigger / time.Minute), RelatedEnd: al.RelatedEnd, Action: al.Action}
		if !al.Absolute.IsZero() {
			r.Absolute = obj.Zones.Instant(al.Absolute).Format(time.RFC3339)
		}
		dto.Reminders = append(dto.Reminders, r)
	}
	if ev.Organizer != nil {
		dto.Organizer = toAttendeeDTO(*ev.Organizer)
	}
	for _, at := range ev.Attendees {
		dto.Attendees = append(dto.Attendees, toAttendeeDTO(at))
	}
	return dto
}

func toAttendeeDTO(a ical.Attendee) AttendeeDTO {
	return AttendeeDTO{Email: a.Email, Name: a.Name, Role: a.Role, PartStat: a.PartStat, RSVP: a.RSVP}
}

// extraEventProperties lists the properties an event carries that the editor
// has no field for.
func extraEventProperties(ev *ical.Event) []string {
	out := []string{}
	for _, p := range ev.Component.Props {
		switch p.Name {
		case "UID", "DTSTAMP", "DTSTART", "DTEND", "DURATION", "SUMMARY", "DESCRIPTION", "LOCATION",
			"URL", "STATUS", "TRANSP", "CATEGORIES", "SEQUENCE", "RRULE", "RDATE", "EXDATE",
			"RECURRENCE-ID", "ORGANIZER", "ATTENDEE", "CREATED", "LAST-MODIFIED", "CLASS", "PRIORITY":
			continue
		}
		if !slices.Contains(out, p.Name) {
			out = append(out, p.Name)
		}
	}
	return out
}

// formatEventTime renders an instant for the ui: a date for an all-day
// occurrence, read in the user's zone, else rfc3339 in the user's zone.
func formatEventTime(t time.Time, allDay bool) string {
	if allDay {
		return t.In(time.Local).Format(dateLayout)
	}
	return t.In(time.Local).Format(time.RFC3339)
}

func formatRecurrenceID(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseRecurrenceID(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}
