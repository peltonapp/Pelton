package desktop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/scripting/spaces"
	"github.com/peltonapp/Pelton/internal/storage"
)

// scriptCalendar is the calendar and the tasks a script reaches, through the
// same paths the bindings use.
type scriptCalendar struct {
	app *App
}

// parseEventTime reads a time the calendar bindings write: a date for an all
// day event, else rfc3339.
func parseEventTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if len(s) == len(dateLayout) {
		t, _ := time.ParseInLocation(dateLayout, s, time.Local)
		return t
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// taskDueTime is when a task falls due: its instant, or for a due date the
// end of that day.
func taskDueTime(s string) time.Time {
	t := parseEventTime(s)
	if !t.IsZero() && len(strings.TrimSpace(s)) == len(dateLayout) {
		return t.AddDate(0, 0, 1)
	}
	return t
}

func scriptEventOf(dto EventDTO) spaces.Event {
	attendees := make([]string, 0, len(dto.Attendees))
	for _, at := range dto.Attendees {
		attendees = append(attendees, at.Email)
	}
	return spaces.Event{
		ID: dto.ID, CalendarID: dto.CalendarID, Calendar: dto.CalendarName, UID: dto.UID, Title: dto.Summary,
		Description: dto.Description, Location: dto.Location, URL: dto.URL, Status: dto.Status,
		Start: parseEventTime(dto.Start), End: parseEventTime(dto.End), AllDay: dto.AllDay,
		Recurring: dto.Recurring, Organizer: dto.Organizer.Email, Attendees: attendees,
	}
}

func (s *scriptCalendar) calendarNames() map[int64]string {
	out := map[int64]string{}
	cals, err := s.app.store.ListCalendars(s.app.ctx)
	if err != nil {
		return out
	}
	for _, c := range cals {
		out[c.ID] = c.Name
	}
	return out
}

func (s *scriptCalendar) occurrences(from, to time.Time) ([]spaces.Event, error) {
	occ, err := s.app.ListOccurrences(from.Format(time.RFC3339), to.Format(time.RFC3339), nil)
	if err != nil {
		return nil, err
	}
	names := s.calendarNames()
	out := make([]spaces.Event, 0, len(occ))
	for _, o := range occ {
		out = append(out, spaces.Event{
			ID: o.EventID, CalendarID: o.CalendarID, Calendar: names[o.CalendarID], UID: o.UID, Title: o.Summary,
			Location: o.Location, Status: o.Status, Start: parseEventTime(o.Start), End: parseEventTime(o.End),
			AllDay: o.AllDay, Recurring: o.Recurring,
		})
	}
	return out, nil
}

func (s *scriptCalendar) Events(_ context.Context, from, to time.Time, query string, limit int) ([]spaces.Event, error) {
	all, err := s.occurrences(from, to)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var out []spaces.Event
	for _, e := range all {
		if q != "" && !strings.Contains(strings.ToLower(e.Title+"\n"+e.Location), q) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *scriptCalendar) Event(_ context.Context, id int64) (spaces.Event, error) {
	dto, err := s.app.GetEvent(id, "")
	if err != nil {
		return spaces.Event{}, err
	}
	return scriptEventOf(dto), nil
}

func (s *scriptCalendar) CreateEvent(ctx context.Context, in spaces.EventInput) (spaces.Event, error) {
	a := s.app
	if in.CalendarID == 0 {
		cals, err := a.store.ListCalendars(a.ctx)
		if err != nil {
			return spaces.Event{}, err
		}
		in.CalendarID = a.defaultCalendarID(cals)
	}
	res, err := a.createEvent(EventRequest{
		CalendarID: in.CalendarID, Summary: in.Title, Description: in.Description, Location: in.Location, URL: in.URL,
		Start: formatEventTime(in.Start, in.AllDay), End: formatEventTime(in.End, in.AllDay), AllDay: in.AllDay,
		Attendees: []AttendeeDTO{}, Reminders: a.defaultReminders(),
	})
	if err != nil {
		return spaces.Event{}, err
	}
	a.scriptEventSaved(ctx, res.Event.ID, true)
	return scriptEventOf(res.Event), nil
}

func (s *scriptCalendar) UpdateEvent(ctx context.Context, id int64, ch spaces.EventChange) (spaces.Event, error) {
	a := s.app
	dto, err := a.GetEvent(id, "")
	if err != nil {
		return spaces.Event{}, err
	}
	req := EventRequest{
		ID: dto.ID, CalendarID: dto.CalendarID, Summary: dto.Summary, Description: dto.Description,
		Location: dto.Location, URL: dto.URL, Status: dto.Status, Transparency: dto.Transparency,
		Categories: dto.Categories, Start: dto.Start, End: dto.End, AllDay: dto.AllDay, TimeZone: dto.TimeZone,
		RRule: dto.RRule, Reminders: dto.Reminders, Organizer: dto.Organizer, Attendees: dto.Attendees, Scope: ScopeAll,
	}
	if ch.Title != nil {
		req.Summary = *ch.Title
	}
	if ch.Description != nil {
		req.Description = *ch.Description
	}
	if ch.Location != nil {
		req.Location = *ch.Location
	}
	if ch.AllDay != nil {
		req.AllDay = *ch.AllDay
	}
	start, end := parseEventTime(dto.Start), parseEventTime(dto.End)
	if ch.Start != nil {
		length := end.Sub(start)
		start = *ch.Start
		if ch.End == nil {
			end = start.Add(length)
		}
	}
	if ch.End != nil {
		end = *ch.End
	}
	req.Start, req.End = formatEventTime(start, req.AllDay), formatEventTime(end, req.AllDay)
	res, err := a.updateEvent(req)
	if err != nil {
		return spaces.Event{}, err
	}
	a.scriptEventSaved(ctx, res.Event.ID, false)
	return scriptEventOf(res.Event), nil
}

// EventFromMessage drafts an event from a message without its attendees, so
// a script never sends invitations by turning mail into an event.
func (s *scriptCalendar) EventFromMessage(_ context.Context, messageID int64) (spaces.EventInput, error) {
	req, err := s.app.CreateEventDraftFromMessage(messageID)
	if err != nil {
		return spaces.EventInput{}, err
	}
	return spaces.EventInput{
		CalendarID: req.CalendarID, Title: req.Summary, Description: req.Description, Location: req.Location,
		Start: parseEventTime(req.Start), End: parseEventTime(req.End), AllDay: req.AllDay,
	}, nil
}

func (s *scriptCalendar) Invitation(_ context.Context, messageID int64) (spaces.Invitation, bool, error) {
	inv, err := s.app.ParseInvite(messageID)
	if err != nil || !inv.Found {
		return spaces.Invitation{}, false, err
	}
	return scriptInvitationOf(inv), true, nil
}

func scriptInvitationOf(inv InviteDTO) spaces.Invitation {
	return spaces.Invitation{
		Method: inv.Method, UID: inv.UID, Title: inv.Summary, Location: inv.Location, Organizer: inv.Organizer.Email,
		Start: parseEventTime(inv.Start), End: parseEventTime(inv.End), AllDay: inv.AllDay, ExistingEventID: inv.ExistingEventID,
	}
}

func scriptTaskOf(dto TaskDTO) spaces.Task {
	return spaces.Task{
		ID: dto.ID, ListID: dto.ListID, List: dto.ListName, UID: dto.UID, Title: dto.Title, Notes: dto.Notes,
		Due: taskDueTime(dto.Due), Done: dto.Done, Completed: parseEventTime(dto.Completed),
		Priority: dto.Priority, Categories: dto.Categories,
	}
}

// taskDueText writes a due time the task bindings read: a date when it is
// midnight, else rfc3339.
func taskDueText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	l := t.In(time.Local)
	if l.Hour() == 0 && l.Minute() == 0 && l.Second() == 0 {
		return l.Format(dateLayout)
	}
	return l.Format(time.RFC3339)
}

func (s *scriptCalendar) Tasks(_ context.Context, f spaces.TaskFilter) ([]spaces.Task, error) {
	list, err := s.app.ListTasks()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	var out []spaces.Task
	for _, dto := range list {
		t := scriptTaskOf(dto)
		switch {
		case f.OpenOnly && t.Done:
			continue
		case !f.DueBefore.IsZero() && (t.Due.IsZero() || !t.Due.Before(f.DueBefore)):
			continue
		case q != "" && !strings.Contains(strings.ToLower(t.Title+"\n"+t.Notes), q):
			continue
		}
		out = append(out, t)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func (s *scriptCalendar) Task(_ context.Context, id int64) (spaces.Task, error) {
	dto, err := s.app.GetTask(id)
	if err != nil {
		return spaces.Task{}, err
	}
	return scriptTaskOf(dto), nil
}

func (s *scriptCalendar) CreateTask(ctx context.Context, in spaces.TaskInput) (spaces.Task, error) {
	a := s.app
	req := TaskRequest{ListID: in.ListID, Title: in.Title, Notes: in.Notes, Due: taskDueText(in.Due), Priority: in.Priority, MailLinks: []string{}}
	if in.MessageID != 0 {
		m, err := a.store.GetMessage(scriptsCtx(a.ctx), in.MessageID)
		if err != nil {
			return spaces.Task{}, err
		}
		if link := midLink(m.MessageID); link != "" {
			req.MailLinks = []string{link}
		}
	}
	res, err := a.createTask(req)
	if err != nil {
		return spaces.Task{}, err
	}
	a.scriptTaskSaved(ctx, res, true, false)
	return scriptTaskOf(res.Task), nil
}

func (s *scriptCalendar) UpdateTask(ctx context.Context, id int64, ch spaces.TaskChange) (spaces.Task, error) {
	a := s.app
	dto, err := a.GetTask(id)
	if err != nil {
		return spaces.Task{}, err
	}
	wasDone := dto.Done
	var res TaskSaveResultDTO
	if ch.Title != nil || ch.Notes != nil || ch.Due != nil || ch.Priority != nil {
		req := TaskRequest{
			ID: dto.ID, ListID: dto.ListID, UID: dto.UID, Title: dto.Title, Notes: dto.Notes, URL: dto.URL, Due: dto.Due,
			Start: dto.Start, TimeZone: dto.TimeZone, Status: dto.Status, Percent: dto.Percent, Priority: dto.Priority,
			RRule: dto.RRule, Reminders: dto.Reminders, Parent: dto.Parent, Categories: dto.Categories,
			MailLinks: dto.MailLinks, Contacts: dto.Contacts, EventUIDs: dto.EventUIDs, SortOrder: dto.SortOrder,
			Estimate: dto.Estimate,
		}
		if ch.Title != nil {
			req.Title = *ch.Title
		}
		if ch.Notes != nil {
			req.Notes = *ch.Notes
		}
		if ch.Due != nil {
			req.Due = taskDueText(*ch.Due)
		}
		if ch.Priority != nil {
			req.Priority = *ch.Priority
		}
		if res, err = a.updateTask(req); err != nil {
			return spaces.Task{}, err
		}
	}
	if ch.Done != nil && *ch.Done != dto.Done {
		if res, err = a.setTaskDone(id, *ch.Done); err != nil {
			return spaces.Task{}, err
		}
	}
	if res.Task.ID == 0 {
		return scriptTaskOf(dto), nil
	}
	a.scriptTaskSaved(ctx, res, false, wasDone)
	return scriptTaskOf(res.Task), nil
}

func (s *scriptCalendar) TaskFromMessage(_ context.Context, messageID int64) (spaces.TaskInput, error) {
	a := s.app
	m, err := a.store.GetMessage(scriptsCtx(a.ctx), messageID)
	if err != nil {
		return spaces.TaskInput{}, err
	}
	title := stripSubjectPrefixes(m.Subject)
	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("Mail from %s", m.FromAddress)
	}
	return spaces.TaskInput{Title: title, Notes: mailview.Snippet(m.BodyPlain, m.BodyHTML), MessageID: messageID}, nil
}

// taskDone reports whether a task is done now, false when it cannot be read.
func (a *App) taskDone(id int64) bool {
	row, err := a.store.GetCalendarEvent(a.ctx, id)
	return err == nil && row.TaskCompleted != ""
}

// scriptTaskSaved reports a saved task: created, or completed when it was
// not done before and is now, or moved on to its next occurrence.
func (a *App) scriptTaskSaved(ctx context.Context, res TaskSaveResultDTO, created, wasDone bool) {
	if res.Task.ID == 0 {
		return
	}
	if created {
		a.scriptEvent(ctx, spaces.TriggerTaskCreated, map[string]any{"task_id": res.Task.ID})
	}
	if (res.Task.Done && !wasDone) || res.Advanced {
		a.scriptEvent(ctx, spaces.TriggerTaskCompleted, map[string]any{"task_id": res.Task.ID})
	}
}

// scriptEventSaved reports an event saved here.
func (a *App) scriptEventSaved(ctx context.Context, id int64, created bool) {
	if id == 0 {
		return
	}
	trigger := spaces.TriggerEventChanged
	if created {
		trigger = spaces.TriggerEventCreated
	}
	a.scriptEvent(ctx, trigger, map[string]any{"event_id": id})
}

// scriptResourceStored reports an event or task a calendar sync brought in.
// A calendar's first sync is left out, since everything in it is new to the
// app rather than new on the server.
func (a *App) scriptResourceStored(ctx context.Context, cal storage.Calendar, row storage.CalendarEvent, created, wasDone bool) {
	if cal.LastSyncAt == "" || row.ID == 0 {
		return
	}
	switch {
	case row.Kind == storage.KindTask && created:
		a.scriptEventBudgeted(ctx, spaces.TriggerTaskCreated, map[string]any{"task_id": row.ID})
	case row.Kind == storage.KindTask && !wasDone && row.TaskCompleted != "":
		a.scriptEventBudgeted(ctx, spaces.TriggerTaskCompleted, map[string]any{"task_id": row.ID})
	case row.Kind != storage.KindTask && created:
		a.scriptEventBudgeted(ctx, spaces.TriggerEventCreated, map[string]any{"event_id": row.ID})
	case row.Kind != storage.KindTask:
		a.scriptEventBudgeted(ctx, spaces.TriggerEventChanged, map[string]any{"event_id": row.ID})
	}
}
