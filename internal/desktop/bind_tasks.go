// bind_tasks.go is the task list: VTODOs kept in local task lists and in the
// CalDAV collections that take them. A task is stored, queued, synced and
// put through conflicts exactly like an event (it is a row of the same
// table, see calendar_sync.go); this file reads and writes the task fields.
package desktop

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/peltonapp/Pelton/internal/caldav"
	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/storage"
)

const (
	settingTaskDefaultList   = "tasks_default_list_id"
	settingTaskDueTime       = "tasks_default_due_minutes"
	settingTaskBadge         = "tasks_badge"
	settingTaskInCalendar    = "tasks_in_calendar"
	settingTaskCompletedDays = "tasks_completed_days"
	settingTaskSort          = "tasks_sort"
)

// The rail badge counts and the sort orders the task settings accept.
var (
	taskBadges = []string{"overdue", "today", "off"}
	taskSorts  = []string{"manual", "due", "priority", "title", "created"}
)

var (
	errTaskNeedsList     = errors.New("pelton: choose a list for this task")
	errTaskListNotTasks  = errors.New("pelton: this calendar does not take tasks")
	errTaskNotATask      = errors.New("pelton: this is not a task")
	errTaskSettings      = errors.New("pelton: these task settings are not valid")
	errTaskUIDTaken      = errors.New("pelton: a task with this id is already in the list")
	errTaskDataUnusable  = errors.New("pelton: the task data could not be read")
	errTaskParentInvalid = errors.New("pelton: a task cannot be its own subtask")
)

// TaskListDTO is one task list for the sidebar and pickers: a local list or a
// CalDAV collection that takes VTODOs.
type TaskListDTO struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"accountId"`
	AccountName string `json:"accountName"`
	// Name is empty for the local list the app starts with, which the ui
	// labels itself.
	Name     string `json:"name"`
	Color    string `json:"color"`
	Local    bool   `json:"local"`
	ReadOnly bool   `json:"readOnly"`
	// Events is true for a collection that holds events too.
	Events    bool   `json:"events"`
	LastError string `json:"lastError"`
	// Default marks the list new tasks go to unless another is chosen.
	Default bool `json:"default"`
}

// TaskContactDTO is a person a task is for or about.
type TaskContactDTO struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// TaskDTO is one task. Due and Start are a date (2006-01-02), rfc3339 in the
// user's zone for a task with a time, or empty.
type TaskDTO struct {
	ID        int64  `json:"id"`
	ListID    int64  `json:"listId"`
	ListName  string `json:"listName"`
	ListColor string `json:"listColor"`
	UID       string `json:"uid"`
	Title     string `json:"title"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`
	Due       string `json:"due"`
	Start     string `json:"start"`
	// TimeZone is the IANA zone a timed due date was written in, empty for
	// UTC, floating and date-only tasks.
	TimeZone string `json:"timeZone"`
	Done     bool   `json:"done"`
	// Completed is rfc3339, empty while open.
	Completed string `json:"completed"`
	// Status is NEEDS-ACTION, IN-PROCESS, COMPLETED or CANCELLED, or empty.
	Status string `json:"status"`
	// Percent is 0 to 100. Priority is 0 for none, 1 highest through 9.
	Percent    int           `json:"percent"`
	Priority   int           `json:"priority"`
	RRule      string        `json:"rrule"`
	Reminders  []ReminderDTO `json:"reminders"`
	Parent     string        `json:"parent"`
	Categories []string      `json:"categories"`
	// MailLinks are mid: links to mails, EventUIDs the events it points at.
	MailLinks []string         `json:"mailLinks"`
	Contacts  []TaskContactDTO `json:"contacts"`
	EventUIDs []string         `json:"eventUids"`
	// SortOrder is the manual position, shared with other task apps.
	SortOrder int `json:"sortOrder"`
	// Created is rfc3339 from the task's CREATED, empty when it has none.
	Created   string `json:"created"`
	ReadOnly  bool   `json:"readOnly"`
	Pending   bool   `json:"pending"`
	Conflict  bool   `json:"conflict"`
	SyncError string `json:"syncError"`
}

// TaskRequest is the editor's task. An ID of 0 creates one in ListID; UID is
// only read then, to bring a task back under the id it had.
type TaskRequest struct {
	ID         int64            `json:"id"`
	ListID     int64            `json:"listId"`
	UID        string           `json:"uid"`
	Title      string           `json:"title"`
	Notes      string           `json:"notes"`
	URL        string           `json:"url"`
	Due        string           `json:"due"`
	Start      string           `json:"start"`
	TimeZone   string           `json:"timeZone"`
	Status     string           `json:"status"`
	Percent    int              `json:"percent"`
	Priority   int              `json:"priority"`
	RRule      string           `json:"rrule"`
	Reminders  []ReminderDTO    `json:"reminders"`
	Parent     string           `json:"parent"`
	Categories []string         `json:"categories"`
	MailLinks  []string         `json:"mailLinks"`
	Contacts   []TaskContactDTO `json:"contacts"`
	EventUIDs  []string         `json:"eventUids"`
	SortOrder  int              `json:"sortOrder"`
	// Force writes over the server's copy, for "keep mine" on a conflict.
	Force bool `json:"force"`
}

// TaskSaveResultDTO is the outcome of a save, like EventSaveResultDTO.
type TaskSaveResultDTO struct {
	Task     TaskDTO `json:"task"`
	Queued   bool    `json:"queued"`
	Conflict bool    `json:"conflict"`
	Server   TaskDTO `json:"server"`
	// Advanced is true when completing a repeating task moved it on to its
	// next occurrence instead of closing it.
	Advanced bool `json:"advanced"`
}

// RemovedTaskDTO is a deleted task as it was, which RestoreTasks takes back.
type RemovedTaskDTO struct {
	ListID int64  `json:"listId"`
	Data   string `json:"data"`
}

// TaskDeleteResultDTO is the outcome of a deletion: the tasks that went, the
// subtasks included, for undo.
type TaskDeleteResultDTO struct {
	Removed []RemovedTaskDTO `json:"removed"`
	Queued  bool             `json:"queued"`
}

// TaskMailMarkDTO says a stored message has a task: Done is true when every
// task linking to it is completed.
type TaskMailMarkDTO struct {
	MessageID int64 `json:"messageId"`
	Done      bool  `json:"done"`
}

// TaskSettingsDTO holds the task preferences.
type TaskSettingsDTO struct {
	// DefaultListID is where new tasks go, 0 for the first writable list.
	DefaultListID int64 `json:"defaultListId"`
	// DefaultDueMinutes is the time of day a due date picked without one
	// gets, as minutes after midnight, or -1 to keep it a date.
	DefaultDueMinutes int `json:"defaultDueMinutes"`
	// Badge is what the rail counts: overdue, today (due today or overdue)
	// or off.
	Badge string `json:"badge"`
	// ShowInCalendar puts tasks with a due date in the calendar views.
	ShowInCalendar bool `json:"showInCalendar"`
	// CompletedDays is how long a completed task stays listed, -1 for good.
	CompletedDays int `json:"completedDays"`
	// Sort is the order of a list: manual, due, priority, title or created.
	Sort string `json:"sort"`
}

// ListTaskLists returns every calendar that takes tasks, local lists first.
func (a *App) ListTaskLists() ([]TaskListDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	cals, err := a.store.ListCalendars(a.ctx)
	if err != nil {
		return nil, err
	}
	names, err := a.calendarAccountNames()
	if err != nil {
		return nil, err
	}
	def := a.defaultTaskListID(cals)
	out := []TaskListDTO{}
	for _, c := range cals {
		if c.HoldsTasks() {
			out = append(out, toTaskListDTO(c, names[c.AccountID], c.ID == def))
		}
	}
	return out, nil
}

// CreateTaskList adds a task list that lives only in this database.
func (a *App) CreateTaskList(name, color string) (TaskListDTO, error) {
	if err := a.ready(); err != nil {
		return TaskListDTO{}, err
	}
	cal := storage.Calendar{Name: strings.TrimSpace(name), Color: caldav.NormalizeColor(color), Visible: true, Components: storage.KindTask}
	cal.ColorSet = cal.Color != ""
	if _, err := a.store.CreateCalendar(a.ctx, &cal); err != nil {
		return TaskListDTO{}, err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	return toTaskListDTO(cal, "", false), nil
}

// DeleteTaskList removes a local task list and its tasks. The last local list
// stays, so there is always somewhere to put a task.
func (a *App) DeleteTaskList(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	cal, err := a.store.GetCalendar(a.ctx, id)
	if err != nil {
		return err
	}
	if !cal.HoldsTasks() {
		return errTaskListNotTasks
	}
	return a.DeleteLocalCalendar(id)
}

// ListTasks returns every task, leaving out completed ones older than the
// completed-days setting.
func (a *App) ListTasks() ([]TaskDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	q := storage.TaskQuery{}
	if days := a.intSetting(settingTaskCompletedDays, 30); days >= 0 {
		q.CompletedSince = time.Now().AddDate(0, 0, -days)
	}
	rows, err := a.store.QueryTasks(a.ctx, q)
	if err != nil {
		return nil, err
	}
	cals, err := a.calendarIndex(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TaskDTO, 0, len(rows))
	for _, row := range rows {
		obj, err := ical.ParseTodoObject([]byte(row.Data), time.Local)
		if err != nil {
			a.log.Warn("unreadable task", "task", row.ID, "err", err)
			continue
		}
		out = append(out, taskDTO(row, cals[row.CalendarID], obj))
	}
	return out, nil
}

// GetTask returns one task.
func (a *App) GetTask(id int64) (TaskDTO, error) {
	if err := a.ready(); err != nil {
		return TaskDTO{}, err
	}
	row, cal, obj, err := a.loadTask(id)
	if err != nil {
		return TaskDTO{}, err
	}
	return taskDTO(*row, *cal, obj), nil
}

// CreateTask stores a new task and writes it to its list's server.
func (a *App) CreateTask(req TaskRequest) (TaskSaveResultDTO, error) {
	if err := a.ready(); err != nil {
		return TaskSaveResultDTO{}, err
	}
	cal, err := a.writableTaskList(req.ListID)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	uid := strings.TrimSpace(req.UID)
	if uid == "" {
		uid = uuid.NewString()
	} else if _, err := a.store.GetCalendarEventByUID(a.ctx, cal.ID, uid); err == nil {
		return TaskSaveResultDTO{}, errTaskUIDTaken
	}
	todo := &ical.Todo{UID: uid}
	if err := applyTaskRequest(todo, req); err != nil {
		return TaskSaveResultDTO{}, err
	}
	if req.Status == ical.TodoCompleted {
		todo.SetDone(true, time.Now())
	}
	obj := ical.NewTodoObject(todo, time.Local)
	obj.Todo.Component.Set("CREATED", time.Now().UTC().Format("20060102T150405Z"))
	row := storage.CalendarEvent{CalendarID: cal.ID, UID: uid, Kind: storage.KindTask}
	return a.commitTask(a.ctx, *cal, row, obj, req.Force)
}

// UpdateTask saves an edit. A different ListID moves the task, with its
// subtasks, to that list. Completing a repeating task moves it on to its
// next occurrence.
func (a *App) UpdateTask(req TaskRequest) (TaskSaveResultDTO, error) {
	if err := a.ready(); err != nil {
		return TaskSaveResultDTO{}, err
	}
	row, cal, obj, err := a.loadTask(req.ID)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	if cal.ReadOnly {
		return TaskSaveResultDTO{}, errCalendarReadOnly
	}
	if strings.TrimSpace(req.Parent) == obj.Todo.UID {
		return TaskSaveResultDTO{}, errTaskParentInvalid
	}
	wasDone := obj.Todo.Done()
	if err := applyTaskRequest(obj.Todo, req); err != nil {
		return TaskSaveResultDTO{}, err
	}
	advanced := false
	if done := strings.EqualFold(req.Status, ical.TodoCompleted); done != wasDone {
		advanced = completeTodo(obj, done)
	}
	obj.Todo.Sequence++
	obj.EnsureTimezones()
	if req.ListID != 0 && req.ListID != row.CalendarID {
		return a.moveTask(*row, *cal, req.ListID, obj)
	}
	result, err := a.commitTask(a.ctx, *cal, *row, obj, req.Force)
	result.Advanced = advanced
	return result, err
}

// SetTaskDone completes a task or opens it again. A repeating task that is
// completed moves on to its next occurrence and stays open, which the result
// says with Advanced.
func (a *App) SetTaskDone(id int64, done bool) (TaskSaveResultDTO, error) {
	if err := a.ready(); err != nil {
		return TaskSaveResultDTO{}, err
	}
	row, cal, obj, err := a.loadTask(id)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	if cal.ReadOnly {
		return TaskSaveResultDTO{}, errCalendarReadOnly
	}
	advanced := completeTodo(obj, done)
	obj.Todo.Sequence++
	result, err := a.commitTask(a.ctx, *cal, *row, obj, false)
	result.Advanced = advanced
	return result, err
}

// completeTodo completes or reopens a task, moving a repeating one on to its
// next occurrence instead of completing it. It reports whether it moved on.
func completeTodo(obj *ical.TodoObject, done bool) bool {
	if done && obj.Advance() {
		return true
	}
	obj.Todo.SetDone(done, time.Now())
	return false
}

// SetTaskDue moves a task's due date, keeping its time of day when it has
// one and moving the start along by as much. due is a date (2006-01-02) or
// rfc3339; empty clears it.
func (a *App) SetTaskDue(id int64, due string) (TaskSaveResultDTO, error) {
	if err := a.ready(); err != nil {
		return TaskSaveResultDTO{}, err
	}
	row, cal, obj, err := a.loadTask(id)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	if cal.ReadOnly {
		return TaskSaveResultDTO{}, errCalendarReadOnly
	}
	t := obj.Todo
	tz := ""
	if t.Due.TZID != "" {
		if loc, ok := obj.Zones.Location(t.Due.TZID); ok {
			tz = loc.String()
		}
	}
	next, err := taskTime(due, tz)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	if !t.Due.IsZero() && !t.Start.IsZero() && !next.IsZero() && next.AllDay == t.Due.AllDay {
		t.Start.Wall = t.Start.Wall.Add(next.Wall.Sub(t.Due.Wall))
	}
	t.Due = next
	t.Sequence++
	obj.EnsureTimezones()
	return a.commitTask(a.ctx, *cal, *row, obj, false)
}

// ReorderTasks gives the tasks the manual order they are listed in, writing
// only the ones whose position changed.
func (a *App) ReorderTasks(ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	for i, id := range ids {
		row, cal, obj, err := a.loadTask(id)
		if err != nil {
			return err
		}
		position := (i + 1) * 10
		if cal.ReadOnly || obj.Todo.SortOrder == position {
			continue
		}
		obj.Todo.SortOrder = position
		if _, err := a.commitTask(a.ctx, *cal, *row, obj, false); err != nil {
			return err
		}
	}
	return nil
}

// DeleteTask deletes a task with its subtasks, here and on the server. The
// result carries what was deleted, for RestoreTasks.
func (a *App) DeleteTask(id int64, force bool) (TaskDeleteResultDTO, error) {
	if err := a.ready(); err != nil {
		return TaskDeleteResultDTO{}, err
	}
	row, cal, obj, err := a.loadTask(id)
	if err != nil {
		return TaskDeleteResultDTO{}, err
	}
	if cal.ReadOnly {
		return TaskDeleteResultDTO{}, errCalendarReadOnly
	}
	rows, err := a.taskFamily(*row, obj.Todo.UID)
	if err != nil {
		return TaskDeleteResultDTO{}, err
	}
	out := TaskDeleteResultDTO{Removed: []RemovedTaskDTO{}}
	for _, r := range rows {
		out.Removed = append(out.Removed, RemovedTaskDTO{ListID: r.CalendarID, Data: r.Data})
		queued, err := a.deleteTaskRow(a.ctx, *cal, r, force)
		if err != nil {
			return out, err
		}
		out.Queued = out.Queued || queued
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	return out, nil
}

// RestoreTasks brings deleted tasks back as they were, under their own UIDs,
// which is what undo of a deletion does.
func (a *App) RestoreTasks(items []RemovedTaskDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	for _, item := range items {
		cal, err := a.writableTaskList(item.ListID)
		if err != nil {
			return err
		}
		obj, err := ical.ParseTodoObject([]byte(item.Data), time.Local)
		if err != nil {
			return errTaskDataUnusable
		}
		row := storage.CalendarEvent{CalendarID: cal.ID, UID: obj.Todo.UID, Kind: storage.KindTask}
		if existing, err := a.store.GetCalendarEventByUID(a.ctx, cal.ID, obj.Todo.UID); err == nil {
			if existing.Pending != storage.PendingDelete {
				continue
			}
			row = *existing
			row.Pending = storage.PendingUpdate
		}
		if _, err := a.commitTask(a.ctx, *cal, row, obj, false); err != nil {
			return err
		}
	}
	return nil
}

// ResolveTaskConflict settles a task's conflict: keepMine writes the local
// version over the server's, otherwise the server's replaces the local one.
func (a *App) ResolveTaskConflict(id int64, keepMine bool) (TaskDTO, error) {
	if err := a.ready(); err != nil {
		return TaskDTO{}, err
	}
	cal, fresh, err := a.resolveRowConflict(id, keepMine)
	if err != nil || fresh == nil {
		return TaskDTO{}, err
	}
	obj, err := ical.ParseTodoObject([]byte(fresh.Data), time.Local)
	if err != nil {
		return TaskDTO{}, err
	}
	return taskDTO(*fresh, *cal, obj), nil
}

// ListTaskMailMarks returns the stored messages that tasks link to, so the
// message list and the reader can mark them.
func (a *App) ListTaskMailMarks() ([]TaskMailMarkDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	links, err := a.store.TaskMailIDs(a.ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(links))
	for id := range links {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	found, err := a.store.MessageIDsByHeader(a.ctx, ids)
	if err != nil {
		return nil, err
	}
	out := []TaskMailMarkDTO{}
	for header, messages := range found {
		for _, m := range messages {
			out = append(out, TaskMailMarkDTO{MessageID: m, Done: links[header]})
		}
	}
	slices.SortFunc(out, func(x, y TaskMailMarkDTO) int { return int(x.MessageID - y.MessageID) })
	return out, nil
}

// EventRefDTO says where an event a task points at is: its id and when it
// starts, for showing it in the calendar. Found is false when no calendar has
// an event with that UID.
type EventRefDTO struct {
	Found   bool   `json:"found"`
	EventID int64  `json:"eventId"`
	Start   string `json:"start"`
	AllDay  bool   `json:"allDay"`
	Summary string `json:"summary"`
}

// FindEventByUID finds the event a task links to by its UID.
func (a *App) FindEventByUID(uid string) (EventRefDTO, error) {
	if err := a.ready(); err != nil {
		return EventRefDTO{}, err
	}
	rows, err := a.store.FindCalendarEventsByUID(a.ctx, strings.TrimSpace(uid))
	if err != nil {
		return EventRefDTO{}, err
	}
	for _, row := range rows {
		if row.Pending == storage.PendingDelete {
			continue
		}
		obj, err := parseStoredEvent(row)
		if err != nil || obj.First() == nil {
			continue
		}
		ev := obj.First()
		return EventRefDTO{
			Found:   true,
			EventID: row.ID,
			Start:   formatEventTime(obj.Zones.Instant(ev.Start), ev.Start.AllDay),
			AllDay:  ev.Start.AllDay,
			Summary: ev.Summary,
		}, nil
	}
	return EventRefDTO{}, nil
}

// GetTaskSettings returns the task preferences.
func (a *App) GetTaskSettings() (TaskSettingsDTO, error) {
	if err := a.ready(); err != nil {
		return TaskSettingsDTO{}, err
	}
	return TaskSettingsDTO{
		DefaultListID:     int64(a.intSetting(settingTaskDefaultList, 0)),
		DefaultDueMinutes: a.intSetting(settingTaskDueTime, -1),
		Badge:             oneOf(a.stringSetting(settingTaskBadge, "today"), taskBadges, "today"),
		ShowInCalendar:    a.boolSetting(settingTaskInCalendar, true),
		CompletedDays:     a.intSetting(settingTaskCompletedDays, 30),
		Sort:              oneOf(a.stringSetting(settingTaskSort, "manual"), taskSorts, "manual"),
	}, nil
}

// SetTaskSettings saves the task preferences. An unknown badge or sort, a due
// time outside the day and a negative keep other than -1 are refused.
func (a *App) SetTaskSettings(s TaskSettingsDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !slices.Contains(taskBadges, s.Badge) || !slices.Contains(taskSorts, s.Sort) ||
		s.DefaultDueMinutes < -1 || s.DefaultDueMinutes >= minutesPerDay || s.CompletedDays < -1 {
		return errTaskSettings
	}
	if err := a.store.SetInt(a.ctx, settingTaskDefaultList, int(s.DefaultListID)); err != nil {
		return err
	}
	if err := a.store.SetInt(a.ctx, settingTaskDueTime, s.DefaultDueMinutes); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingTaskBadge, s.Badge); err != nil {
		return err
	}
	if err := a.store.SetBool(a.ctx, settingTaskInCalendar, s.ShowInCalendar); err != nil {
		return err
	}
	if err := a.store.SetInt(a.ctx, settingTaskCompletedDays, s.CompletedDays); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingTaskSort, s.Sort); err != nil {
		return err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{})
	return nil
}

// --- helpers ---------------------------------------------------------------

// loadTask reads a task row, its list and its parsed object.
func (a *App) loadTask(id int64) (*storage.CalendarEvent, *storage.Calendar, *ical.TodoObject, error) {
	row, err := a.store.GetCalendarEvent(a.ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	if !row.IsTask() {
		return nil, nil, nil, errTaskNotATask
	}
	cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
	if err != nil {
		return nil, nil, nil, err
	}
	obj, err := ical.ParseTodoObject([]byte(row.Data), time.Local)
	if err != nil {
		return nil, nil, nil, err
	}
	return row, cal, obj, nil
}

// writableTaskList returns the list a new task goes to: the one named, or the
// default when id is 0.
func (a *App) writableTaskList(id int64) (*storage.Calendar, error) {
	if id == 0 {
		cals, err := a.store.ListCalendars(a.ctx)
		if err != nil {
			return nil, err
		}
		if id = a.defaultTaskListID(cals); id == 0 {
			return nil, errTaskNeedsList
		}
	}
	cal, err := a.store.GetCalendar(a.ctx, id)
	if err != nil {
		return nil, err
	}
	if !cal.HoldsTasks() {
		return nil, errTaskListNotTasks
	}
	if cal.ReadOnly {
		return nil, errCalendarReadOnly
	}
	return cal, nil
}

// defaultTaskListID is where a new task goes: the chosen default while it is
// still a writable list, else the first writable one.
func (a *App) defaultTaskListID(cals []storage.Calendar) int64 {
	chosen := int64(a.intSetting(settingTaskDefaultList, 0))
	var first int64
	for _, c := range cals {
		if c.ReadOnly || !c.HoldsTasks() {
			continue
		}
		if c.ID == chosen {
			return c.ID
		}
		if first == 0 {
			first = c.ID
		}
	}
	return first
}

// taskFamily returns a task and every subtask below it in the same list,
// the task itself first.
func (a *App) taskFamily(root storage.CalendarEvent, uid string) ([]storage.CalendarEvent, error) {
	rows, err := a.store.QueryTasks(a.ctx, storage.TaskQuery{CalendarIDs: []int64{root.CalendarID}})
	if err != nil {
		return nil, err
	}
	children := map[string][]storage.CalendarEvent{}
	for _, r := range rows {
		obj, err := ical.ParseTodoObject([]byte(r.Data), time.Local)
		if err != nil || obj.Todo.Parent == "" {
			continue
		}
		children[obj.Todo.Parent] = append(children[obj.Todo.Parent], r)
	}
	out := []storage.CalendarEvent{root}
	seen := map[string]bool{uid: true}
	queue := []string{uid}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range children[parent] {
			if seen[child.UID] {
				continue
			}
			seen[child.UID] = true
			out = append(out, child)
			queue = append(queue, child.UID)
		}
	}
	return out, nil
}

// commitTask stores an edited task and, for a synced list, writes it to the
// server right away, the way commitEvent does for an event.
func (a *App) commitTask(ctx context.Context, cal storage.Calendar, row storage.CalendarEvent, obj *ical.TodoObject, force bool) (TaskSaveResultDTO, error) {
	fillTaskRow(&row, obj)
	row.SyncError = ""
	if cal.Local() {
		row.Pending, row.Conflict = "", false
	} else if row.Pending != storage.PendingCreate {
		row.Pending = storage.PendingUpdate
		if row.Path == "" {
			row.Pending = storage.PendingCreate
		}
	}
	if err := a.store.SaveCalendarEvent(ctx, &row); err != nil {
		return TaskSaveResultDTO{}, err
	}
	var result TaskSaveResultDTO
	if !cal.Local() {
		out, err := a.pushRowNow(ctx, cal, &row, force)
		if err != nil {
			return TaskSaveResultDTO{}, err
		}
		result.Queued, result.Conflict = out.queued, out.conflict
		if out.server != nil {
			if server, err := ical.ParseTodoObject(out.server.Data, time.Local); err == nil {
				shown := row
				shown.ETag = out.server.ETag
				result.Server = taskDTO(shown, cal, server)
			}
		}
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	saved, err := a.store.GetCalendarEventByUID(ctx, cal.ID, row.UID)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	savedObj, err := ical.ParseTodoObject([]byte(saved.Data), time.Local)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	result.Task = taskDTO(*saved, cal, savedObj)
	return result, nil
}

// deleteTaskRow deletes one task row here and on the server. It reports
// whether the deletion waits in the queue.
func (a *App) deleteTaskRow(ctx context.Context, cal storage.Calendar, row storage.CalendarEvent, force bool) (bool, error) {
	if cal.Local() || row.Path == "" {
		return false, a.store.DeleteCalendarEvent(ctx, row.ID)
	}
	row.Pending = storage.PendingDelete
	if err := a.store.SaveCalendarEvent(ctx, &row); err != nil {
		return false, err
	}
	out, err := a.pushRowNow(ctx, cal, &row, force)
	return out.queued, err
}

// moveTask puts a task and its subtasks in another list: created there, then
// deleted from where they were.
func (a *App) moveTask(row storage.CalendarEvent, from storage.Calendar, toID int64, obj *ical.TodoObject) (TaskSaveResultDTO, error) {
	to, err := a.writableTaskList(toID)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	family, err := a.taskFamily(row, obj.Todo.UID)
	if err != nil {
		return TaskSaveResultDTO{}, err
	}
	result, err := a.commitTask(a.ctx, *to, storage.CalendarEvent{CalendarID: to.ID, UID: obj.Todo.UID, Kind: storage.KindTask}, obj, false)
	if err != nil {
		return result, err
	}
	for _, r := range family[1:] {
		child, err := ical.ParseTodoObject([]byte(r.Data), time.Local)
		if err != nil {
			continue
		}
		if _, err := a.commitTask(a.ctx, *to, storage.CalendarEvent{CalendarID: to.ID, UID: r.UID, Kind: storage.KindTask}, child, false); err != nil {
			return result, err
		}
	}
	for _, r := range family {
		if _, err := a.deleteTaskRow(a.ctx, from, r, false); err != nil {
			return result, err
		}
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{from.ID, to.ID}})
	return result, nil
}

// applyTaskRequest writes the editor's fields into a task. Completion is
// left to the caller, which knows whether the task was done before.
func applyTaskRequest(t *ical.Todo, req TaskRequest) error {
	t.Summary = strings.TrimSpace(req.Title)
	t.Description = req.Notes
	t.URL = strings.TrimSpace(req.URL)
	due, err := taskTime(req.Due, req.TimeZone)
	if err != nil {
		return err
	}
	start, err := taskTime(req.Start, req.TimeZone)
	if err != nil {
		return err
	}
	t.Due, t.Start = due, start
	if status := strings.ToUpper(strings.TrimSpace(req.Status)); status != ical.TodoCompleted {
		t.Status = status
	}
	if !t.Done() {
		t.Percent = max(0, min(100, req.Percent))
	}
	t.Priority = max(0, min(9, req.Priority))
	t.RRule = nil
	if rule := strings.TrimSpace(strings.TrimPrefix(req.RRule, "RRULE:")); rule != "" {
		parsed, err := ical.ParseRRule(rule)
		if err != nil {
			return err
		}
		t.RRule = parsed
	}
	t.Alarms = remindersToAlarms(t.Alarms, req.Reminders)
	t.Parent = strings.TrimSpace(req.Parent)
	t.Categories = cleanList(req.Categories)
	t.Related = cleanList(req.EventUIDs)
	var attachments []string
	for _, link := range t.Attachments {
		if !strings.HasPrefix(strings.ToLower(link), "mid:") {
			attachments = append(attachments, link)
		}
	}
	t.Attachments = append(attachments, cleanList(req.MailLinks)...)
	t.Contacts = nil
	for _, c := range req.Contacts {
		name, email := strings.TrimSpace(c.Name), strings.TrimSpace(c.Email)
		if name != "" || email != "" {
			t.Contacts = append(t.Contacts, ical.Contact{Name: name, Email: email})
		}
	}
	t.SortOrder = req.SortOrder
	return nil
}

func cleanList(list []string) []string {
	var out []string
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// taskTime reads a due or start date: a date (2006-01-02), rfc3339 written in
// tz (or UTC when tz is empty), or nothing.
func taskTime(s, tz string) (ical.DateTime, error) {
	s = strings.TrimSpace(s)
	if len(s) == len(dateLayout) {
		return requestTime(s, true, "")
	}
	return requestTime(s, false, tz)
}

// fillTaskRow writes the task and its index columns into a row.
func fillTaskRow(row *storage.CalendarEvent, obj *ical.TodoObject) {
	t := obj.Todo
	row.Kind = storage.KindTask
	row.UID = t.UID
	row.Data = string(obj.Encode())
	row.Summary, row.Location = t.Summary, ""
	row.RangeStart, row.RangeEnd = time.Time{}, time.Time{}
	row.Recurring = t.RRule != nil
	row.HasAlarms = len(t.Alarms) > 0
	row.TaskDue = ""
	switch {
	case t.Due.AllDay:
		row.TaskDue = t.Due.Wall.Format(dateLayout)
	case !t.Due.IsZero():
		row.TaskDue = obj.Zones.Instant(t.Due).UTC().Format(time.RFC3339)
	}
	row.TaskCompleted = ""
	if t.Done() {
		at := time.Now()
		if !t.Completed.IsZero() {
			at = obj.Zones.Instant(t.Completed)
		}
		row.TaskCompleted = at.UTC().Format(time.RFC3339)
	}
	var mail []string
	for _, link := range t.Attachments {
		if id := midHeader(link); id != "" {
			mail = append(mail, id)
		}
	}
	row.TaskMail = strings.Join(mail, "\n")
}

// midHeader returns the Message-ID a mid: link names, or "".
func midHeader(link string) string {
	link = strings.TrimSpace(link)
	if len(link) < 5 || !strings.EqualFold(link[:4], "mid:") {
		return ""
	}
	raw, _, _ := strings.Cut(link[4:], "/")
	id, err := url.PathUnescape(raw)
	if err != nil {
		return ""
	}
	return strings.Trim(id, "<>")
}

// taskTimeText renders a due or start date for the ui.
func taskTimeText(obj *ical.TodoObject, d ical.DateTime) string {
	switch {
	case d.IsZero():
		return ""
	case d.AllDay:
		return d.Wall.Format(dateLayout)
	}
	return formatEventTime(obj.Zones.Instant(d), false)
}

func taskDTO(row storage.CalendarEvent, cal storage.Calendar, obj *ical.TodoObject) TaskDTO {
	t := obj.Todo
	dto := TaskDTO{
		ID:         row.ID,
		ListID:     cal.ID,
		ListName:   cal.Name,
		ListColor:  cal.Color,
		UID:        t.UID,
		Title:      t.Summary,
		Notes:      t.Description,
		URL:        t.URL,
		Due:        taskTimeText(obj, t.Due),
		Start:      taskTimeText(obj, t.Start),
		Done:       t.Done(),
		Status:     t.Status,
		Percent:    t.Percent,
		Priority:   t.Priority,
		Reminders:  []ReminderDTO{},
		Parent:     t.Parent,
		Categories: append([]string{}, t.Categories...),
		MailLinks:  []string{},
		Contacts:   []TaskContactDTO{},
		EventUIDs:  append([]string{}, t.Related...),
		SortOrder:  t.SortOrder,
		ReadOnly:   cal.ReadOnly,
		Pending:    row.Pending != "",
		Conflict:   row.Conflict,
		SyncError:  row.SyncError,
	}
	if row.TaskCompleted != "" {
		if at, err := time.Parse(time.RFC3339, row.TaskCompleted); err == nil {
			dto.Completed = at.In(time.Local).Format(time.RFC3339)
		}
	}
	if !t.Due.IsZero() && !t.Due.AllDay && !t.Due.UTC {
		if loc, ok := obj.Zones.Location(t.Due.TZID); ok {
			dto.TimeZone = loc.String()
		}
	}
	if t.RRule != nil {
		dto.RRule = t.RRule.String()
	}
	for _, link := range t.Attachments {
		if strings.HasPrefix(strings.ToLower(link), "mid:") {
			dto.MailLinks = append(dto.MailLinks, link)
		}
	}
	for _, c := range t.Contacts {
		dto.Contacts = append(dto.Contacts, TaskContactDTO{Name: c.Name, Email: c.Email})
	}
	for _, al := range t.Alarms {
		r := ReminderDTO{MinutesBefore: int(-al.Trigger / time.Minute), RelatedEnd: al.RelatedEnd, Action: al.Action}
		if !al.Absolute.IsZero() {
			r.Absolute = obj.Zones.Instant(al.Absolute).Format(time.RFC3339)
		}
		dto.Reminders = append(dto.Reminders, r)
	}
	if created, err := ical.ParseDateTime(t.Component.Text("CREATED"), "", ""); err == nil && created.UTC {
		dto.Created = created.Wall.In(time.Local).Format(time.RFC3339)
	}
	if dto.Categories == nil {
		dto.Categories = []string{}
	}
	return dto
}

func toTaskListDTO(c storage.Calendar, accountName string, isDefault bool) TaskListDTO {
	return TaskListDTO{
		ID:          c.ID,
		AccountID:   c.AccountID,
		AccountName: accountName,
		Name:        c.Name,
		Color:       c.Color,
		Local:       c.Local(),
		ReadOnly:    c.ReadOnly,
		Events:      c.HoldsEvents(),
		LastError:   c.LastError,
		Default:     isDefault,
	}
}
