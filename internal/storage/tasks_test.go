package storage

import (
	"context"
	"testing"
	"time"
)

func taskList(t *testing.T, db *DB) Calendar {
	t.Helper()
	cals, err := db.ListCalendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cals {
		if c.HoldsTasks() {
			return c
		}
	}
	t.Fatal("no task list")
	return Calendar{}
}

// Tasks share the event table but never show up where events are read.
func TestTasksStayOutOfEventQueries(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	list := taskList(t, db)
	task := CalendarEvent{CalendarID: list.ID, UID: "t1", Data: "x", Kind: KindTask, Summary: "Pay rent", TaskDue: "2026-03-01"}
	if err := db.SaveCalendarEvent(ctx, &task); err != nil {
		t.Fatal(err)
	}
	events, err := db.QueryCalendarEvents(ctx, EventQuery{From: day(1), To: day(28)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("a task came back as an event: %+v", events)
	}
	if found, _ := db.FindCalendarEventsByUID(ctx, "t1"); len(found) != 0 {
		t.Errorf("a task was found as an invitation copy: %+v", found)
	}
	got, err := db.GetCalendarEvent(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsTask() || got.TaskDue != "2026-03-01" {
		t.Errorf("stored task = %+v", got)
	}
}

func TestQueryTasksByCompletion(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	list := taskList(t, db)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	rows := []CalendarEvent{
		{UID: "open"},
		{UID: "done-recently", TaskCompleted: now.Add(-24 * time.Hour).Format(time.RFC3339)},
		{UID: "done-long-ago", TaskCompleted: now.Add(-40 * 24 * time.Hour).Format(time.RFC3339)},
		{UID: "deleted", Pending: PendingDelete},
	}
	for i := range rows {
		rows[i].CalendarID, rows[i].Kind, rows[i].Data = list.ID, KindTask, "x"
		if err := db.SaveCalendarEvent(ctx, &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	uids := func(q TaskQuery) []string {
		t.Helper()
		got, err := db.QueryTasks(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range got {
			out = append(out, r.UID)
		}
		return out
	}
	if got := uids(TaskQuery{}); len(got) != 3 {
		t.Errorf("all = %v", got)
	}
	if got := uids(TaskQuery{OpenOnly: true}); len(got) != 1 || got[0] != "open" {
		t.Errorf("open = %v", got)
	}
	if got := uids(TaskQuery{CompletedSince: now.Add(-7 * 24 * time.Hour)}); len(got) != 2 || got[1] != "done-recently" {
		t.Errorf("recent = %v", got)
	}
}

func TestTaskMailIDsAndMessageLookup(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	list := taskList(t, db)
	rows := []CalendarEvent{
		{UID: "a", TaskMail: "one@example.com\ntwo@example.com"},
		{UID: "b", TaskMail: "two@example.com", TaskCompleted: "2026-03-01T00:00:00Z"},
		{UID: "c", TaskMail: "three@example.com", TaskCompleted: "2026-03-01T00:00:00Z"},
	}
	for i := range rows {
		rows[i].CalendarID, rows[i].Kind, rows[i].Data = list.ID, KindTask, "x"
		if err := db.SaveCalendarEvent(ctx, &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.TaskMailIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"one@example.com": false, "two@example.com": false, "three@example.com": true}
	for id, done := range want {
		if d, ok := got[id]; !ok || d != done {
			t.Errorf("%s = %v, %v; want %v", id, d, ok, done)
		}
	}
	found, err := db.MessageIDsByHeader(ctx, []string{"nobody@example.com"})
	if err != nil || len(found) != 0 {
		t.Errorf("lookup of an unknown id = %v, %v", found, err)
	}
}

func TestUpdateCalendarKeepsComponents(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	list := taskList(t, db)
	list.Name = "Errands"
	list.Components = ""
	if err := db.UpdateCalendar(ctx, list); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetCalendar(ctx, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Errands" || !got.HoldsTasks() {
		t.Errorf("calendar = %+v", got)
	}
}
