package desktop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/caldav/caldavtest"
	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

func newCalendarTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler), queue: outbox.NewQueue(store)}
}

func localCalendar(t *testing.T, a *App) int64 {
	t.Helper()
	cals, err := a.ListCalendars()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cals {
		if c.Local {
			return c.ID
		}
	}
	t.Fatal("no local calendar")
	return 0
}

func occurrences(t *testing.T, a *App, from, to string, ids ...int64) []OccurrenceDTO {
	t.Helper()
	occs, err := a.ListOccurrences(from, to, ids)
	if err != nil {
		t.Fatal(err)
	}
	return occs
}

func summaries(occs []OccurrenceDTO) []string {
	var out []string
	for _, o := range occs {
		out = append(out, o.Summary)
	}
	return out
}

func TestLocalEventLifecycle(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	created, err := a.CreateEvent(EventRequest{
		CalendarID: cal, Summary: "Dentist", Location: "Main St",
		Start: "2026-04-02T10:00:00Z", End: "2026-04-02T11:00:00Z",
		Reminders: []ReminderDTO{{MinutesBefore: 30}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Queued || created.Conflict || created.Event.ID == 0 {
		t.Fatalf("create = %+v", created)
	}
	occs := occurrences(t, a, "2026-04-01T00:00:00Z", "2026-04-03T00:00:00Z")
	if len(occs) != 1 || occs[0].Summary != "Dentist" || !occs[0].HasReminders || occs[0].Recurring {
		t.Fatalf("occurrences = %+v", occs)
	}

	got, err := a.GetEvent(created.Event.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reminders) != 1 || got.Reminders[0].MinutesBefore != 30 {
		t.Errorf("reminders = %+v", got.Reminders)
	}
	req := EventRequest{ID: got.ID, Summary: "Dentist, moved", Start: "2026-04-02T14:00:00Z", End: "2026-04-02T15:00:00Z"}
	if _, err := a.UpdateEvent(req); err != nil {
		t.Fatal(err)
	}
	occs = occurrences(t, a, "2026-04-02T13:00:00Z", "2026-04-02T16:00:00Z")
	if len(occs) != 1 || occs[0].Summary != "Dentist, moved" {
		t.Fatalf("after update = %+v", occs)
	}

	if _, err := a.DeleteEvent(got.ID, "", "", false); err != nil {
		t.Fatal(err)
	}
	if occs := occurrences(t, a, "2026-04-01T00:00:00Z", "2026-04-03T00:00:00Z"); len(occs) != 0 {
		t.Errorf("deleted event still listed: %+v", occs)
	}
}

func TestEventRejectsEndBeforeStart(t *testing.T) {
	a := newCalendarTestApp(t)
	_, err := a.CreateEvent(EventRequest{CalendarID: localCalendar(t, a), Start: "2026-04-02T10:00:00Z", End: "2026-04-02T09:00:00Z"})
	if err != errEventEndBeforeStart {
		t.Errorf("err = %v", err)
	}
}

// Five Monday meetings in Berlin, edited one occurrence at a time, then from
// one on, then deleted one by one.
func TestRecurringEditScopes(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	created, err := a.CreateEvent(EventRequest{
		CalendarID: cal, Summary: "Sync", TimeZone: "Europe/Berlin",
		Start: "2026-03-16T09:00:00+01:00", End: "2026-03-16T09:30:00+01:00",
		RRule: "FREQ=WEEKLY;COUNT=5",
	})
	if err != nil {
		t.Fatal(err)
	}
	const from, to = "2026-03-01T00:00:00Z", "2026-05-01T00:00:00Z"
	occs := occurrences(t, a, from, to)
	if len(occs) != 5 {
		t.Fatalf("got %d occurrences", len(occs))
	}
	// across the DST change the meeting stays at nine in Berlin.
	if occs[2].RecurrenceID != "2026-03-30T07:00:00Z" {
		t.Errorf("third occurrence = %s", occs[2].RecurrenceID)
	}

	second := occs[1]
	if _, err := a.UpdateEvent(EventRequest{
		ID: created.Event.ID, Summary: "Sync, later", TimeZone: "Europe/Berlin",
		Start: "2026-03-23T11:00:00+01:00", End: "2026-03-23T11:30:00+01:00",
		Scope: ScopeThis, RecurrenceID: second.RecurrenceID,
	}); err != nil {
		t.Fatal(err)
	}
	occs = occurrences(t, a, from, to)
	if len(occs) != 5 || occs[1].Summary != "Sync, later" || !occs[1].Override || occs[0].Summary != "Sync" {
		t.Fatalf("after this-only edit: %v", summaries(occs))
	}
	single, err := a.GetEvent(created.Event.ID, second.RecurrenceID)
	if err != nil || single.Summary != "Sync, later" || single.RRule == "" {
		t.Errorf("occurrence = %+v, %v", single, err)
	}

	fourth := occs[3]
	if _, err := a.UpdateEvent(EventRequest{
		ID: created.Event.ID, Summary: "Sync v2", TimeZone: "Europe/Berlin",
		Start: "2026-04-06T10:00:00+02:00", End: "2026-04-06T10:30:00+02:00",
		RRule: "FREQ=WEEKLY;COUNT=5", Scope: ScopeFuture, RecurrenceID: fourth.RecurrenceID,
	}); err != nil {
		t.Fatal(err)
	}
	occs = occurrences(t, a, from, to)
	got := summaries(occs)
	want := []string{"Sync", "Sync, later", "Sync", "Sync v2", "Sync v2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after split = %v, want %v", got, want)
	}
	if occs[0].EventID == occs[3].EventID {
		t.Error("the split did not create a second event")
	}

	if _, err := a.DeleteEvent(occs[2].EventID, ScopeThis, occs[2].RecurrenceID, false); err != nil {
		t.Fatal(err)
	}
	if n := len(occurrences(t, a, from, to)); n != 4 {
		t.Errorf("after deleting one occurrence: %d", n)
	}
	if _, err := a.DeleteEvent(occs[3].EventID, ScopeFuture, occs[3].RecurrenceID, false); err != nil {
		t.Fatal(err)
	}
	if n := len(occurrences(t, a, from, to)); n != 2 {
		t.Errorf("after deleting the second series from its start: %d", n)
	}
}

// Moving one occurrence of a series with ScopeAll moves them all, and keeps
// the overrides paired with their occurrences.
func TestRecurringEditAllShiftsSeries(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	created, err := a.CreateEvent(EventRequest{
		CalendarID: cal, Summary: "Standup", Start: "2026-05-04T08:00:00Z", End: "2026-05-04T08:15:00Z",
		RRule: "FREQ=DAILY;COUNT=3",
	})
	if err != nil {
		t.Fatal(err)
	}
	occs := occurrences(t, a, "2026-05-01T00:00:00Z", "2026-05-10T00:00:00Z")
	if _, err := a.UpdateEvent(EventRequest{ID: created.Event.ID, Summary: "Override", Start: "2026-05-06T12:00:00Z", End: "2026-05-06T12:15:00Z", Scope: ScopeThis, RecurrenceID: occs[2].RecurrenceID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateEvent(EventRequest{
		ID: created.Event.ID, Summary: "Standup", Start: "2026-05-05T09:00:00Z", End: "2026-05-05T09:15:00Z",
		RRule: "FREQ=DAILY;COUNT=3", Scope: ScopeAll, RecurrenceID: occs[1].RecurrenceID,
	}); err != nil {
		t.Fatal(err)
	}
	occs = occurrences(t, a, "2026-05-01T00:00:00Z", "2026-05-10T00:00:00Z")
	if len(occs) != 3 {
		t.Fatalf("occurrences = %+v", occs)
	}
	if occs[0].RecurrenceID != "2026-05-04T09:00:00Z" {
		t.Errorf("series start = %s, want moved by an hour", occs[0].RecurrenceID)
	}
	if occs[2].Summary != "Override" || occs[2].RecurrenceID != "2026-05-06T09:00:00Z" {
		t.Errorf("override = %+v", occs[2])
	}
}

// remoteSetup is an app with one CalDAV account on a fake server, synced once.
func remoteSetup(t *testing.T, server *caldavtest.Server) (*App, storage.Calendar) {
	t.Helper()
	a := newCalendarTestApp(t)
	srv := httptest.NewServer(server)
	t.Cleanup(srv.Close)
	acct := storage.CalendarAccount{Name: "Fake", URL: srv.URL + "/dav/", Username: "me"}
	if _, err := a.store.CreateCalendarAccount(a.ctx, &acct); err != nil {
		t.Fatal(err)
	}
	if err := credentials.StoreCalendarPassword(acct.ID, "secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentials.DeleteCalendarPassword(acct.ID) })
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	cals, err := a.store.ListAccountCalendars(a.ctx, acct.ID)
	if err != nil || len(cals) != 1 {
		t.Fatalf("calendars = %+v, %v", cals, err)
	}
	return a, cals[0]
}

func serverEvent(uid, summary, start string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:other\r\nBEGIN:VEVENT\r\nUID:" + uid +
		"\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:" + start + "\r\nDURATION:PT1H\r\nSUMMARY:" + summary +
		"\r\nX-OTHER-CLIENT:keep me\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func TestRemoteSyncWriteAndConflict(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	server.Put(caldavtest.WorkPath+"existing.ics", serverEvent("existing", "From the phone", "20260601T100000Z"))
	a, cal := remoteSetup(t, server)

	if cal.Name != "Work" || cal.Color != "#ff8800" {
		t.Errorf("calendar = %+v", cal)
	}
	occs := occurrences(t, a, "2026-06-01T00:00:00Z", "2026-06-02T00:00:00Z")
	if len(occs) != 1 || occs[0].Summary != "From the phone" {
		t.Fatalf("synced occurrences = %+v", occs)
	}

	created, err := a.CreateEvent(EventRequest{CalendarID: cal.ID, Summary: "Made here", Start: "2026-06-01T12:00:00Z", End: "2026-06-01T13:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Queued || created.Event.Pending {
		t.Fatalf("create was not written: %+v", created)
	}
	if len(server.Paths()) != 2 {
		t.Errorf("server holds %v", server.Paths())
	}

	// an edit here keeps what the other client wrote.
	existing := occs[0]
	if _, err := a.UpdateEvent(EventRequest{ID: existing.EventID, Summary: "Edited here", Start: "2026-06-01T10:00:00Z", End: "2026-06-01T11:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	data, _ := server.Data(caldavtest.WorkPath + "existing.ics")
	if !strings.Contains(data, "SUMMARY:Edited here") || !strings.Contains(data, "X-OTHER-CLIENT:keep me") {
		t.Errorf("server copy = %s", data)
	}

	// the phone changes it again; an edit from the stale copy is a conflict.
	server.Put(caldavtest.WorkPath+"existing.ics", serverEvent("existing", "Phone again", "20260601T100000Z"))
	res, err := a.UpdateEvent(EventRequest{ID: existing.EventID, Summary: "Mine", Start: "2026-06-01T10:00:00Z", End: "2026-06-01T11:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Conflict || res.Server.Summary != "Phone again" || !res.Event.Conflict {
		t.Fatalf("conflict = %+v", res)
	}
	// a sync leaves a conflicted event alone.
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetEvent(existing.EventID, ""); got.Summary != "Mine" {
		t.Errorf("sync overwrote the conflicted edit: %q", got.Summary)
	}
	if _, err := a.ResolveEventConflict(existing.EventID, true); err != nil {
		t.Fatal(err)
	}
	data, _ = server.Data(caldavtest.WorkPath + "existing.ics")
	if !strings.Contains(data, "SUMMARY:Mine") {
		t.Errorf("keep mine did not write: %s", data)
	}
}

func TestResolveConflictTakingTheServers(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	server.Put(caldavtest.WorkPath+"e.ics", serverEvent("e", "Original", "20260601T100000Z"))
	a, _ := remoteSetup(t, server)
	occs := occurrences(t, a, "2026-06-01T00:00:00Z", "2026-06-02T00:00:00Z")
	server.Put(caldavtest.WorkPath+"e.ics", serverEvent("e", "Theirs", "20260601T100000Z"))
	res, err := a.UpdateEvent(EventRequest{ID: occs[0].EventID, Summary: "Mine", Start: "2026-06-01T10:00:00Z", End: "2026-06-01T11:00:00Z"})
	if err != nil || !res.Conflict {
		t.Fatalf("update = %+v, %v", res, err)
	}
	got, err := a.ResolveEventConflict(occs[0].EventID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "Theirs" || got.Conflict || got.Pending {
		t.Errorf("resolved = %+v", got)
	}
}

// An edit made while the server is unreachable waits in the queue and goes
// out with the next sync.
func TestOfflineEditQueuesAndRetries(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	a, cal := remoteSetup(t, server)
	server.Offline = true
	res, err := a.CreateEvent(EventRequest{CalendarID: cal.ID, Summary: "Offline", Start: "2026-07-01T10:00:00Z", End: "2026-07-01T11:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Queued || !res.Event.Pending {
		t.Fatalf("result = %+v", res)
	}
	if len(server.Paths()) != 0 {
		t.Fatal("written while offline")
	}
	server.Offline = false
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	if len(server.Paths()) != 1 {
		t.Errorf("queued event not written: %v", server.Paths())
	}
	got, _ := a.GetEvent(res.Event.ID, "")
	if got.Pending {
		t.Error("still pending after the sync")
	}
}

// A server without sync-collection is read by etag, and a deletion there
// reaches the local copy.
func TestSyncByETagDropsDeleted(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	server.NoSync = true
	server.Put(caldavtest.WorkPath+"a.ics", serverEvent("a", "A", "20260801T100000Z"))
	server.Put(caldavtest.WorkPath+"b.ics", serverEvent("b", "B", "20260801T120000Z"))
	a, _ := remoteSetup(t, server)
	if n := len(occurrences(t, a, "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z")); n != 2 {
		t.Fatalf("got %d", n)
	}
	server.Remove(caldavtest.WorkPath + "a.ics")
	server.Put(caldavtest.WorkPath+"b.ics", serverEvent("b", "B changed", "20260801T120000Z"))
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	occs := occurrences(t, a, "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z")
	if len(occs) != 1 || occs[0].Summary != "B changed" {
		t.Errorf("after sync = %v", summaries(occs))
	}
}

func TestRemoteDeleteAndHiddenCalendar(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	server.Put(caldavtest.WorkPath+"a.ics", serverEvent("a", "A", "20260801T100000Z"))
	a, cal := remoteSetup(t, server)
	if err := a.SetCalendarVisible(cal.ID, false); err != nil {
		t.Fatal(err)
	}
	if n := len(occurrences(t, a, "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z")); n != 0 {
		t.Errorf("hidden calendar listed %d", n)
	}
	occs := occurrences(t, a, "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z", cal.ID)
	if len(occs) != 1 {
		t.Fatalf("explicit calendar listed %d", len(occs))
	}
	if _, err := a.DeleteEvent(occs[0].EventID, "", "", false); err != nil {
		t.Fatal(err)
	}
	if len(server.Paths()) != 0 {
		t.Errorf("server still holds %v", server.Paths())
	}
}

const testInvite = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:other\r\nMETHOD:REQUEST\r\n" +
	"BEGIN:VEVENT\r\nUID:invite-1@example.com\r\nSEQUENCE:0\r\nDTSTAMP:20260101T000000Z\r\n" +
	"DTSTART:20260910T090000Z\r\nDTEND:20260910T100000Z\r\nSUMMARY:Kickoff\r\n" +
	"ORGANIZER;CN=Org:mailto:org@example.com\r\n" +
	"ATTENDEE;CN=Me;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:me@example.com\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

func seedInviteMessage(t *testing.T, a *App, accountID, folderID int64, uid uint32, ics string) int64 {
	t.Helper()
	id, err := a.store.InsertMessageWithAttachments(a.ctx, &storage.Message{
		AccountID: accountID, FolderID: folderID, UID: uid, Subject: "Invitation: Kickoff",
		MessageID: "<invite-" + string(rune('a'+uid)) + "@example.com>", FromAddress: "org@example.com", Date: time.Now().UTC(),
	}, []storage.IncomingAttachment{{Filename: "invite.ics", ContentType: "text/calendar", Content: bytes.NewReader([]byte(ics))}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAnswerInviteStoresEventAndQueuesReply(t *testing.T) {
	a := newCalendarTestApp(t)
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.com", DisplayName: "Me", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if folder.ID, err = a.store.CreateFolder(a.ctx, &folder); err != nil {
		t.Fatal(err)
	}
	msgID := seedInviteMessage(t, a, accountID, folder.ID, 1, testInvite)

	inv, err := a.ParseInvite(msgID)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Found || inv.Method != ical.MethodRequest || inv.MyPartStat != ical.PartStatNeedsAction || inv.Organizer.Email != "org@example.com" {
		t.Fatalf("invite = %+v", inv)
	}

	res, err := a.AnswerInvite(InviteAnswerRequest{MessageID: msgID, Response: "accepted", SendReply: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.EventID == 0 || res.OutboxID == 0 {
		t.Fatalf("answer = %+v", res)
	}
	ev, err := a.GetEvent(res.EventID, "")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Summary != "Kickoff" || len(ev.Attendees) != 1 || ev.Attendees[0].PartStat != ical.PartStatAccepted {
		t.Errorf("stored = %+v", ev)
	}
	queued, err := a.queue.List(a.ctx)
	if err != nil || len(queued) != 1 {
		t.Fatalf("outbox = %v, %v", queued, err)
	}
	raw := string(queued[0].Raw)
	if queued[0].Recipients[0] != "org@example.com" || !strings.Contains(raw, "method=REPLY") || !strings.Contains(raw, "Accepted: Kickoff") {
		t.Errorf("reply = %s", raw)
	}

	again, _ := a.ParseInvite(msgID)
	if again.ExistingEventID != res.EventID || again.MyPartStat != ical.PartStatAccepted {
		t.Errorf("second read = %+v", again)
	}

	cancel := strings.Replace(strings.Replace(testInvite, "METHOD:REQUEST", "METHOD:CANCEL", 1), "SEQUENCE:0", "SEQUENCE:1", 1)
	cancelID := seedInviteMessage(t, a, accountID, folder.ID, 2, cancel)
	if _, err := a.ApplyInvite(cancelID); err != nil {
		t.Fatal(err)
	}
	if ev, _ := a.GetEvent(res.EventID, ""); ev.Status != ical.StatusCancelled {
		t.Errorf("status after cancel = %q", ev.Status)
	}

	if _, err := a.AnswerInvite(InviteAnswerRequest{MessageID: msgID, Response: "declined"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetEvent(res.EventID, ""); err == nil {
		t.Error("declining kept the event")
	}
}

func TestParseInviteWithoutCalendarData(t *testing.T) {
	a := newCalendarTestApp(t)
	accountID, _ := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.com", Local: true})
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	folder.ID, _ = a.store.CreateFolder(a.ctx, &folder)
	id, _ := a.store.InsertMessage(a.ctx, &storage.Message{AccountID: accountID, FolderID: folder.ID, UID: 1, Subject: "hi"})
	inv, err := a.ParseInvite(id)
	if err != nil || inv.Found {
		t.Errorf("invite = %+v, %v", inv, err)
	}
}

func TestImportExportICS(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:x\r\nMETHOD:PUBLISH\r\n" +
		"BEGIN:VEVENT\r\nUID:one\r\nDTSTART;TZID=Europe/Paris:20260301T090000\r\nDTEND;TZID=Europe/Paris:20260301T100000\r\nSUMMARY:One\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:two\r\nDTSTART;VALUE=DATE:20260302\r\nSUMMARY:Two\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:broken\r\nSUMMARY:No start\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	res, err := a.ImportICSData(cal, data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 || res.Skipped != 1 {
		t.Errorf("import = %+v", res)
	}
	again, _ := a.ImportICSData(cal, data)
	if again.Updated != 2 || again.Imported != 0 {
		t.Errorf("reimport = %+v", again)
	}
	stored, _ := a.store.GetCalendar(a.ctx, cal)
	out, err := a.exportICS(context.Background(), *stored)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if strings.Count(text, "BEGIN:VEVENT") != 2 || strings.Count(text, "BEGIN:VTIMEZONE") != 1 || strings.Contains(text, "METHOD:") {
		t.Errorf("export = %s", text)
	}
	if _, err := ical.ParseCalendar(strings.NewReader(text)); err != nil {
		t.Errorf("export does not parse: %v", err)
	}
}

func TestDueReminders(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	start := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	if _, err := a.CreateEvent(EventRequest{
		CalendarID: cal, Summary: "Call", Start: start.Format(time.RFC3339), End: start.Add(time.Hour).Format(time.RFC3339),
		Reminders: []ReminderDTO{{MinutesBefore: 15}, {MinutesBefore: 5}},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	due := a.dueReminders(now.Add(-6*time.Minute), now)
	if len(due) != 1 || due[0].occ.Event.Summary != "Call" {
		t.Fatalf("due = %+v", due)
	}
	if later := a.dueReminders(now, now.Add(6*time.Minute)); len(later) != 1 {
		t.Errorf("the five-minute reminder was not found: %d", len(later))
	}
	body := reminderBody(due[0].occ, calendarTextFor("en"), "24")
	if !strings.Contains(body, start.In(time.Local).Format("15:04")) {
		t.Errorf("body = %q", body)
	}
}

func TestCreateEventDraftFromMessage(t *testing.T) {
	a := newCalendarTestApp(t)
	accountID, _ := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.com", Local: true})
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	folder.ID, _ = a.store.CreateFolder(a.ctx, &folder)
	id, err := a.store.InsertMessage(a.ctx, &storage.Message{
		AccountID: accountID, FolderID: folder.ID, UID: 1,
		Subject: "Re: AW: Planning", FromName: "Ann", FromAddress: "ann@example.com",
		ToAddresses: "Me <me@example.com>, Bob <bob@example.com>", CcAddresses: "ann@example.com",
		BodyPlain: "Shall we meet on Tuesday?",
	})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := a.CreateEventDraftFromMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Summary != "Planning" || !strings.Contains(draft.Description, "Tuesday") || draft.CalendarID == 0 {
		t.Errorf("draft = %+v", draft)
	}
	var emails []string
	for _, at := range draft.Attendees {
		emails = append(emails, at.Email)
	}
	if strings.Join(emails, ",") != "ann@example.com,bob@example.com" {
		t.Errorf("attendees = %v", emails)
	}
}

func TestCalendarSettingsRoundTrip(t *testing.T) {
	a := newCalendarTestApp(t)
	got, err := a.GetCalendarSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := CalendarSettingsDTO{
		SyncIntervalMinutes: 15, RemindersEnabled: true, DefaultReminderMinutes: 15,
		DefaultView: "week", WeekStart: -1, WorkDayStart: 9 * 60, WorkDayEnd: 17 * 60, TimeZoneDisplay: "off",
	}
	if got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}

	want = CalendarSettingsDTO{
		SyncIntervalMinutes: 30, RemindersEnabled: false, DefaultReminderMinutes: -1, DefaultView: "agenda",
		WeekStart: 1, WorkDayStart: 8*60 + 30, WorkDayEnd: 18 * 60, TimeZoneDisplay: "dual", SecondaryTimeZone: "Asia/Tokyo",
	}
	if err := a.SetCalendarSettings(want); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetCalendarSettings(); got != want {
		t.Errorf("saved = %+v, want %+v", got, want)
	}
}

func TestCalendarSettingsRefusesInvalid(t *testing.T) {
	a := newCalendarTestApp(t)
	valid, _ := a.GetCalendarSettings()
	tests := []struct {
		name   string
		change func(*CalendarSettingsDTO)
	}{
		{"unknown view", func(s *CalendarSettingsDTO) { s.DefaultView = "year" }},
		{"week start", func(s *CalendarSettingsDTO) { s.WeekStart = 7 }},
		{"end before start", func(s *CalendarSettingsDTO) { s.WorkDayStart, s.WorkDayEnd = 600, 540 }},
		{"past midnight", func(s *CalendarSettingsDTO) { s.WorkDayEnd = 25 * 60 }},
		{"zone display", func(s *CalendarSettingsDTO) { s.TimeZoneDisplay = "both" }},
		{"unknown zone", func(s *CalendarSettingsDTO) { s.SecondaryTimeZone = "Mars/Olympus" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := valid
			tt.change(&s)
			if err := a.SetCalendarSettings(s); !errors.Is(err, errCalendarSettings) {
				t.Errorf("err = %v", err)
			}
		})
	}
	if got, _ := a.GetCalendarSettings(); got != valid {
		t.Errorf("a refused save changed the settings: %+v", got)
	}
}
