package desktop

import (
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/caldav/caldavtest"
	"github.com/peltonapp/Pelton/internal/storage"
)

func localTaskList(t *testing.T, a *App) int64 {
	t.Helper()
	lists, err := a.ListTaskLists()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lists {
		if l.Local {
			return l.ID
		}
	}
	t.Fatal("no local task list")
	return 0
}

func taskByTitle(t *testing.T, a *App, title string) TaskDTO {
	t.Helper()
	tasks, err := a.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Title == title {
			return task
		}
	}
	t.Fatalf("no task %q in %+v", title, tasks)
	return TaskDTO{}
}

// The task list and the event calendar stay apart: neither shows up in the
// other's listing.
func TestTaskListsAndCalendarsApart(t *testing.T) {
	a := newCalendarTestApp(t)
	cals, err := a.ListCalendars()
	if err != nil || len(cals) != 1 {
		t.Fatalf("calendars = %+v, %v", cals, err)
	}
	lists, err := a.ListTaskLists()
	if err != nil || len(lists) != 1 || !lists[0].Local || !lists[0].Default || lists[0].ID == cals[0].ID {
		t.Fatalf("task lists = %+v, %v", lists, err)
	}
	if err := a.DeleteTaskList(lists[0].ID); err != errCalendarLastLocal {
		t.Errorf("the last local list went: %v", err)
	}
	extra, err := a.CreateTaskList("Errands", "#00AA00")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteTaskList(extra.ID); err != nil {
		t.Errorf("delete a second list: %v", err)
	}
	if err := a.DeleteTaskList(cals[0].ID); err != errTaskListNotTasks {
		t.Errorf("an event calendar was deleted as a task list: %v", err)
	}
}

func TestLocalTaskLifecycle(t *testing.T) {
	a := newCalendarTestApp(t)
	list := localTaskList(t, a)
	created, err := a.CreateTask(TaskRequest{
		ListID: list, Title: "Call Ada", Notes: "about the engine", Due: "2026-05-04T15:00:00Z", TimeZone: "Europe/Berlin",
		Priority: 1, Categories: []string{"work", " ", "work"}, Reminders: []ReminderDTO{{MinutesBefore: 15, RelatedEnd: true}},
		MailLinks: []string{"mid:abc@example.com"}, Contacts: []TaskContactDTO{{Name: "Ada", Email: "ada@example.com"}},
		EventUIDs: []string{"event-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := created.Task
	if task.ID == 0 || task.Title != "Call Ada" || task.TimeZone != "Europe/Berlin" || task.Priority != 1 || task.Done || task.Created == "" {
		t.Fatalf("created = %+v", task)
	}
	due, _ := time.Parse(time.RFC3339, task.Due)
	if !due.Equal(time.Date(2026, 5, 4, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("due = %s", task.Due)
	}
	if len(task.Categories) != 1 || len(task.Reminders) != 1 || !task.Reminders[0].RelatedEnd ||
		len(task.MailLinks) != 1 || len(task.Contacts) != 1 || task.Contacts[0].Email != "ada@example.com" || len(task.EventUIDs) != 1 {
		t.Errorf("links and reminders = %+v", task)
	}

	sub, err := a.CreateTask(TaskRequest{ListID: list, Title: "Find the number", Parent: task.UID, Due: "2026-05-03"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Task.Parent != task.UID || sub.Task.Due != "2026-05-03" {
		t.Errorf("subtask = %+v", sub.Task)
	}

	done, err := a.SetTaskDone(task.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !done.Task.Done || done.Task.Completed == "" || done.Task.Percent != 100 || done.Advanced {
		t.Errorf("done = %+v", done)
	}
	again, err := a.SetTaskDone(task.ID, false)
	if err != nil || again.Task.Done || again.Task.Completed != "" {
		t.Errorf("reopened = %+v, %v", again.Task, err)
	}

	removed, err := a.DeleteTask(task.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Removed) != 2 {
		t.Fatalf("the subtask was not deleted with its parent: %+v", removed)
	}
	if tasks, _ := a.ListTasks(); len(tasks) != 0 {
		t.Fatalf("tasks after delete = %+v", tasks)
	}
	if err := a.RestoreTasks(removed.Removed); err != nil {
		t.Fatal(err)
	}
	back := taskByTitle(t, a, "Find the number")
	if back.Parent != task.UID || taskByTitle(t, a, "Call Ada").UID != task.UID {
		t.Errorf("restored tasks lost their ids: %+v", back)
	}
}

func TestRepeatingTaskMovesOn(t *testing.T) {
	a := newCalendarTestApp(t)
	created, err := a.CreateTask(TaskRequest{Title: "Pay rent", Due: "2026-06-01", RRule: "FREQ=MONTHLY;BYMONTHDAY=1"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.SetTaskDone(created.Task.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Advanced || res.Task.Done || res.Task.Due != "2026-07-01" {
		t.Errorf("after completing = %+v", res)
	}
}

func TestSetTaskDueKeepsTime(t *testing.T) {
	a := newCalendarTestApp(t)
	created, err := a.CreateTask(TaskRequest{Title: "Timed", Due: "2026-06-01T09:30:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := a.SetTaskDue(created.Task.ID, "2026-06-03T09:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	due, _ := time.Parse(time.RFC3339, moved.Task.Due)
	if !due.Equal(time.Date(2026, 6, 3, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("due = %s", moved.Task.Due)
	}
	cleared, err := a.SetTaskDue(created.Task.ID, "")
	if err != nil || cleared.Task.Due != "" {
		t.Errorf("cleared = %+v, %v", cleared.Task, err)
	}
}

func TestReorderAndMoveTasks(t *testing.T) {
	a := newCalendarTestApp(t)
	list := localTaskList(t, a)
	other, err := a.CreateTaskList("Other", "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, title := range []string{"one", "two", "three"} {
		res, err := a.CreateTask(TaskRequest{ListID: list, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.Task.ID)
	}
	if err := a.ReorderTasks([]int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	if got := taskByTitle(t, a, "three").SortOrder; got != 10 {
		t.Errorf("three sorts at %d", got)
	}
	two := taskByTitle(t, a, "two")
	req := TaskRequest{ID: two.ID, ListID: other.ID, Title: "two", SortOrder: two.SortOrder}
	if _, err := a.UpdateTask(req); err != nil {
		t.Fatal(err)
	}
	if moved := taskByTitle(t, a, "two"); moved.ListID != other.ID || moved.UID != two.UID {
		t.Errorf("moved = %+v", moved)
	}
}

// A task on the server from another client syncs in, keeps what that client
// wrote through an edit here, and a task made here goes to the server.
func TestRemoteTaskSync(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	server.Put(caldavtest.TasksPath+"phone.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:other\r\nBEGIN:VTODO\r\nUID:phone\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:From the phone\r\nDUE;VALUE=DATE:20260610\r\nX-OTHER-CLIENT:keep me\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	a, _ := remoteSetup(t, server)
	list := remoteTaskList(t, a)
	if !list.HoldsTasks() || list.HoldsEvents() {
		t.Fatalf("task list components = %q", list.Components)
	}
	phone := taskByTitle(t, a, "From the phone")
	if phone.Due != "2026-06-10" || phone.ListID != list.ID {
		t.Fatalf("synced = %+v", phone)
	}
	if occs := occurrences(t, a, "2026-06-01T00:00:00Z", "2026-06-30T00:00:00Z"); len(occs) != 0 {
		t.Errorf("a task came through as an event: %+v", occs)
	}

	if _, err := a.SetTaskDone(phone.ID, true); err != nil {
		t.Fatal(err)
	}
	data, _ := server.Data(caldavtest.TasksPath + "phone.ics")
	if !strings.Contains(data, "STATUS:COMPLETED") || !strings.Contains(data, "X-OTHER-CLIENT:keep me") {
		t.Errorf("server copy = %s", data)
	}

	made, err := a.CreateTask(TaskRequest{ListID: list.ID, Title: "Made here"})
	if err != nil || made.Queued || made.Task.Pending {
		t.Fatalf("create = %+v, %v", made, err)
	}
	if len(server.Paths()) != 2 {
		t.Errorf("server holds %v", server.Paths())
	}

	// another client changes the task; an edit from the stale copy conflicts.
	server.Put(caldavtest.TasksPath+"phone.ics", strings.Replace(data, "From the phone", "Phone again", 1))
	res, err := a.UpdateTask(TaskRequest{ID: phone.ID, Title: "Mine"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Conflict || res.Server.Title != "Phone again" {
		t.Fatalf("conflict = %+v", res)
	}
	resolved, err := a.ResolveTaskConflict(phone.ID, false)
	if err != nil || resolved.Title != "Phone again" || resolved.Conflict {
		t.Errorf("resolved = %+v, %v", resolved, err)
	}

	// a deletion on the server reaches the local copy.
	server.Remove(caldavtest.TasksPath + "phone.ics")
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	tasks, _ := a.ListTasks()
	if len(tasks) != 1 || tasks[0].Title != "Made here" {
		t.Errorf("after the server deleted one: %+v", tasks)
	}
}

func TestOfflineTaskQueues(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	a, _ := remoteSetup(t, server)
	list := remoteTaskList(t, a)
	server.Offline = true
	res, err := a.CreateTask(TaskRequest{ListID: list.ID, Title: "Offline"})
	if err != nil || !res.Queued || !res.Task.Pending {
		t.Fatalf("result = %+v, %v", res, err)
	}
	server.Offline = false
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	if len(server.Paths()) != 1 || taskByTitle(t, a, "Offline").Pending {
		t.Errorf("queued task not written: %v", server.Paths())
	}
}

func TestTaskMailMarks(t *testing.T) {
	a := newCalendarTestApp(t)
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.com", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if folder.ID, err = a.store.CreateFolder(a.ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := a.store.InsertMessageWithAttachments(a.ctx, &storage.Message{
		AccountID: accountID, FolderID: folder.ID, UID: 1, Subject: "Invoice", MessageID: "<inv/7@example.com>",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src, err := a.MailEventSource(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(TaskRequest{Title: "Pay invoice", MailLinks: []string{src.Link}}); err != nil {
		t.Fatal(err)
	}
	marks, err := a.ListTaskMailMarks()
	if err != nil || len(marks) != 1 || marks[0].MessageID != id || marks[0].Done {
		t.Fatalf("marks = %+v, %v", marks, err)
	}
	task := taskByTitle(t, a, "Pay invoice")
	if _, err := a.SetTaskDone(task.ID, true); err != nil {
		t.Fatal(err)
	}
	if marks, _ := a.ListTaskMailMarks(); len(marks) != 1 || !marks[0].Done {
		t.Errorf("marks after done = %+v", marks)
	}
}

func TestDueTaskReminders(t *testing.T) {
	a := newCalendarTestApp(t)
	due := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Minute)
	if _, err := a.CreateTask(TaskRequest{Title: "Soon", Due: due.Format(time.RFC3339), Reminders: []ReminderDTO{{MinutesBefore: 30, RelatedEnd: true}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(TaskRequest{Title: "No date", Reminders: []ReminderDTO{{MinutesBefore: 0, RelatedEnd: true}}}); err != nil {
		t.Fatal(err)
	}
	got := a.dueTaskReminders(time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
	if len(got) != 1 || got[0].obj.Todo.Summary != "Soon" {
		t.Fatalf("due reminders = %+v", got)
	}
	task := taskByTitle(t, a, "Soon")
	if _, err := a.SetTaskDone(task.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := a.dueTaskReminders(time.Now().Add(-time.Minute), time.Now().Add(time.Minute)); len(got) != 0 {
		t.Errorf("a completed task reminded: %+v", got)
	}
}

func TestExportTaskListICS(t *testing.T) {
	a := newCalendarTestApp(t)
	list := localTaskList(t, a)
	if _, err := a.CreateTask(TaskRequest{ListID: list, Title: "Exported", Due: "2026-01-02T10:00:00Z", TimeZone: "Europe/Berlin"}); err != nil {
		t.Fatal(err)
	}
	cal, err := a.store.GetCalendar(a.ctx, list)
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.exportICS(a.ctx, *cal)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "BEGIN:VTODO") || !strings.Contains(text, "SUMMARY:Exported") || strings.Count(text, "BEGIN:VTIMEZONE") != 1 {
		t.Errorf("export = %s", text)
	}
	js, err := a.exportJSCal(a.ctx, *cal)
	if err != nil || !strings.Contains(string(js), `"@type": "Task"`) {
		t.Errorf("jscal export = %s, %v", js, err)
	}
	res, err := a.ImportICSData(list, text)
	if err != nil || res.Tasks != 1 || res.Imported != 0 {
		t.Errorf("reimport = %+v, %v", res, err)
	}
}

func TestTaskSettingsRoundTrip(t *testing.T) {
	a := newCalendarTestApp(t)
	got, err := a.GetTaskSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Badge != "today" || got.Sort != "manual" || !got.ShowInCalendar || got.DefaultDueMinutes != -1 || got.CompletedDays != 30 {
		t.Errorf("defaults = %+v", got)
	}
	want := TaskSettingsDTO{DefaultListID: 5, DefaultDueMinutes: 17 * 60, Badge: "overdue", ShowInCalendar: false, CompletedDays: -1, Sort: "due"}
	if err := a.SetTaskSettings(want); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetTaskSettings(); got != want {
		t.Errorf("saved %+v, got %+v", want, got)
	}
	for _, bad := range []TaskSettingsDTO{
		{Badge: "all", Sort: "due"},
		{Badge: "off", Sort: "random"},
		{Badge: "off", Sort: "due", DefaultDueMinutes: minutesPerDay},
		{Badge: "off", Sort: "due", CompletedDays: -2},
	} {
		if err := a.SetTaskSettings(bad); err != errTaskSettings {
			t.Errorf("%+v accepted: %v", bad, err)
		}
	}
}
