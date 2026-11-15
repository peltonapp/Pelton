package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func day(d int) time.Time {
	return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC)
}

// The migrations leave one local calendar and one local task list, so an
// install with no server has somewhere to put an event and a task.
func TestMigrationSeedsLocalCalendar(t *testing.T) {
	db := newTestDB(t)
	cals, err := db.ListCalendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 2 {
		t.Fatalf("calendars = %+v", cals)
	}
	events, tasks := cals[0], cals[1]
	if !events.Local() || !events.Visible || !events.HoldsEvents() || events.HoldsTasks() {
		t.Errorf("event calendar = %+v", events)
	}
	if !tasks.Local() || !tasks.HoldsTasks() || tasks.HoldsEvents() {
		t.Errorf("task list = %+v", tasks)
	}
}

func TestCalendarAccountCascade(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	acct := CalendarAccount{Name: "Nextcloud", URL: "https://cloud.example.com", Username: "me"}
	if _, err := db.CreateCalendarAccount(ctx, &acct); err != nil {
		t.Fatal(err)
	}
	cal := Calendar{AccountID: acct.ID, Path: "/cal/work/", Name: "Work", Visible: true}
	if _, err := db.CreateCalendar(ctx, &cal); err != nil {
		t.Fatal(err)
	}
	ev := CalendarEvent{CalendarID: cal.ID, UID: "u1", Path: "/cal/work/u1.ics", Data: "x", RangeStart: day(1), RangeEnd: day(2)}
	if err := db.SaveCalendarEvent(ctx, &ev); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteCalendarAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCalendar(ctx, cal.ID); !errors.Is(err, ErrCalendarNotFound) {
		t.Errorf("calendar survived its account: %v", err)
	}
	if _, err := db.GetCalendarEvent(ctx, ev.ID); !errors.Is(err, ErrCalendarEventNotFound) {
		t.Errorf("event survived its calendar: %v", err)
	}
}

func TestSaveCalendarEventUpsertsByUID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	cals, _ := db.ListCalendars(ctx)
	local := cals[0].ID
	ev := CalendarEvent{CalendarID: local, UID: "same", Data: "v1", Summary: "One", RangeStart: day(1), RangeEnd: day(1)}
	if err := db.SaveCalendarEvent(ctx, &ev); err != nil {
		t.Fatal(err)
	}
	first := ev.ID
	ev.Summary, ev.Data = "Two", "v2"
	if err := db.SaveCalendarEvent(ctx, &ev); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetCalendarEventByUID(ctx, local, "same")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first || got.Summary != "Two" || !got.RangeStart.Equal(day(1)) {
		t.Errorf("event = %+v", got)
	}
}

func TestQueryCalendarEventsWindow(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	cals, _ := db.ListCalendars(ctx)
	local := cals[0]
	hidden := Calendar{Name: "Hidden", Visible: false}
	if _, err := db.CreateCalendar(ctx, &hidden); err != nil {
		t.Fatal(err)
	}
	save := func(calID int64, uid string, start, end time.Time, pending string, alarms bool) {
		t.Helper()
		ev := CalendarEvent{CalendarID: calID, UID: uid, Data: "x", RangeStart: start, RangeEnd: end, Pending: pending, HasAlarms: alarms}
		if err := db.SaveCalendarEvent(ctx, &ev); err != nil {
			t.Fatal(err)
		}
	}
	save(local.ID, "before", day(1), day(2), "", false)
	save(local.ID, "inside", day(10), day(11), "", true)
	save(local.ID, "endless", day(1), time.Time{}, "", false)
	save(local.ID, "instant", day(5), day(5), "", false)
	save(local.ID, "deleted", day(10), day(11), PendingDelete, false)
	save(hidden.ID, "hidden", day(10), day(11), "", false)

	uids := func(q EventQuery) map[string]bool {
		t.Helper()
		evs, err := db.QueryCalendarEvents(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range evs {
			out[e.UID] = true
		}
		return out
	}
	got := uids(EventQuery{From: day(5), To: day(20), VisibleOnly: true})
	want := map[string]bool{"inside": true, "endless": true, "instant": true}
	if len(got) != len(want) {
		t.Errorf("visible window = %v, want %v", got, want)
	}
	for uid := range want {
		if !got[uid] {
			t.Errorf("missing %q in %v", uid, got)
		}
	}
	if all := uids(EventQuery{From: day(5), To: day(20)}); !all["hidden"] {
		t.Error("a hidden calendar was left out without VisibleOnly")
	}
	if alarms := uids(EventQuery{From: day(1), To: day(30), AlarmsOnly: true}); len(alarms) != 1 || !alarms["inside"] {
		t.Errorf("alarms = %v", alarms)
	}
	if only := uids(EventQuery{From: day(1), To: day(30), CalendarIDs: []int64{hidden.ID}}); len(only) != 1 {
		t.Errorf("calendar filter = %v", only)
	}
}

// A deletion reported by the server must not throw away an edit made here
// that has not reached it yet.
func TestDeleteByPathKeepsPendingEdits(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	cals, _ := db.ListCalendars(ctx)
	ev := CalendarEvent{CalendarID: cals[0].ID, UID: "p", Path: "/c/p.ics", Data: "x", Pending: PendingUpdate, RangeStart: day(1)}
	if err := db.SaveCalendarEvent(ctx, &ev); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteCalendarEventByPath(ctx, cals[0].ID, "/c/p.ics"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCalendarEvent(ctx, ev.ID); err != nil {
		t.Errorf("pending edit was dropped: %v", err)
	}
	pending, err := db.ListPendingCalendarEvents(ctx, cals[0].ID)
	if err != nil || len(pending) != 1 {
		t.Errorf("pending = %v, %v", pending, err)
	}
}

func TestRecordCalendarSync(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	cals, _ := db.ListCalendars(ctx)
	id := cals[0].ID
	if err := db.RecordCalendarSync(ctx, id, "c1", "t1", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordCalendarSync(ctx, id, "", "", "server down"); err != nil {
		t.Fatal(err)
	}
	cal, _ := db.GetCalendar(ctx, id)
	if cal.CTag != "c1" || cal.SyncToken != "t1" || cal.LastError != "server down" || cal.LastSyncAt == "" {
		t.Errorf("calendar = %+v", cal)
	}
}
