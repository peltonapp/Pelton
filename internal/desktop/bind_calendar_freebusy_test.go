package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/caldav/caldavtest"
	"github.com/peltonapp/Pelton/internal/ical"
)

func vevent(lines ...string) *ical.Object {
	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\nBEGIN:VEVENT\r\nDTSTAMP:20260101T000000Z\r\n" +
		strings.Join(lines, "\r\n") + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	obj, err := ical.ParseObject([]byte(data), time.UTC)
	if err != nil {
		panic(err)
	}
	return obj
}

func spanStrings(spans []BusySpanDTO) []string {
	var out []string
	for _, s := range spans {
		start, _ := time.Parse(time.RFC3339, s.Start)
		end, _ := time.Parse(time.RFC3339, s.End)
		out = append(out, start.UTC().Format("02T15:04")+"-"+end.UTC().Format("02T15:04")+" "+s.Kind)
	}
	return out
}

func TestOwnBusy(t *testing.T) {
	me := []string{"me@example.com"}
	objs := []*ical.Object{
		// a weekly series that started two weeks before
		vevent("UID:weekly", "DTSTART:20261102T090000Z", "DTEND:20261102T100000Z", "RRULE:FREQ=WEEKLY"),
		// a daily series with the day in question removed
		vevent("UID:daily", "DTSTART:20261110T070000Z", "DURATION:PT30M", "RRULE:FREQ=DAILY", "EXDATE:20261116T070000Z"),
		// all-day events: busy only when they say so
		vevent("UID:birthday", "DTSTART;VALUE=DATE:20261116", "DTEND;VALUE=DATE:20261117"),
		vevent("UID:offsite", "DTSTART;VALUE=DATE:20261116", "DTEND;VALUE=DATE:20261117", "TRANSP:OPAQUE"),
		vevent("UID:free", "DTSTART:20261116T110000Z", "DTEND:20261116T120000Z", "TRANSP:TRANSPARENT"),
		vevent("UID:cancelled", "DTSTART:20261116T120000Z", "DTEND:20261116T130000Z", "STATUS:CANCELLED"),
		vevent("UID:declined", "DTSTART:20261116T130000Z", "DTEND:20261116T140000Z",
			"ORGANIZER:mailto:boss@example.com", "ATTENDEE;PARTSTAT=DECLINED:mailto:ME@example.com"),
		vevent("UID:maybe", "DTSTART:20261116T140000Z", "DTEND:20261116T150000Z",
			"ORGANIZER:mailto:boss@example.com", "ATTENDEE;PARTSTAT=TENTATIVE:mailto:me@example.com"),
		vevent("UID:unanswered", "DTSTART:20261116T150000Z", "DTEND:20261116T153000Z",
			"ORGANIZER:mailto:boss@example.com", "ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:me@example.com"),
		vevent("UID:accepted", "DTSTART:20261116T160000Z", "DTEND:20261116T170000Z",
			"ORGANIZER:mailto:boss@example.com", "ATTENDEE;PARTSTAT=ACCEPTED:mailto:me@example.com"),
		vevent("UID:tentative", "DTSTART:20261116T170000Z", "DTEND:20261116T180000Z", "STATUS:TENTATIVE"),
		// runs in from the evening before
		vevent("UID:overnight", "DTSTART:20261115T230000Z", "DTEND:20261116T010000Z"),
		// the day after, outside the window
		vevent("UID:later", "DTSTART:20261117T090000Z", "DTEND:20261117T100000Z"),
	}
	from := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	got := spanStrings(ownBusy(objs, me, from, from.Add(24*time.Hour)))
	want := []string{
		"16T00:00-16T01:00 busy",
		"16T00:00-17T00:00 busy",
		"16T09:00-16T10:00 busy",
		"16T14:00-16T15:00 tentative",
		"16T15:00-16T15:30 tentative",
		"16T16:00-16T17:00 busy",
		"16T17:00-16T18:00 tentative",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("busy =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOwnBusyAcrossZones(t *testing.T) {
	// 09:00 in New York is 14:00 UTC in November.
	obj := vevent("UID:ny", "DTSTART;TZID=America/New_York:20261116T090000", "DTEND;TZID=America/New_York:20261116T100000")
	from := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	got := spanStrings(ownBusy([]*ical.Object{obj}, nil, from, from.Add(24*time.Hour)))
	if len(got) != 1 || got[0] != "16T14:00-16T15:00 busy" {
		t.Errorf("busy = %v", got)
	}
}

func TestLookupFreeBusyLocalCalendar(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	for _, ev := range []EventRequest{
		{CalendarID: cal, Summary: "Dentist", Start: "2026-11-16T10:00:00Z", End: "2026-11-16T11:00:00Z"},
		{CalendarID: cal, Summary: "Edited", Start: "2026-11-16T12:00:00Z", End: "2026-11-16T13:00:00Z"},
	} {
		if _, err := a.CreateEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	occs := occurrences(t, a, "2026-11-16T00:00:00Z", "2026-11-17T00:00:00Z")
	var editing int64
	for _, o := range occs {
		if o.Summary == "Edited" {
			editing = o.EventID
		}
	}
	got, err := a.LookupFreeBusy(context.Background(), FreeBusyRequest{
		CalendarID: cal, From: "2026-11-16T00:00:00Z", To: "2026-11-17T00:00:00Z",
		Attendees: []string{"ada@example.com", "mailto:ADA@example.com", " "}, ExcludeEventID: editing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := spanStrings(got.Mine); len(s) != 1 || s[0] != "16T10:00-16T11:00 busy" {
		t.Errorf("mine = %v", s)
	}
	if len(got.Attendees) != 1 || got.Attendees[0].Known || got.Attendees[0].Reason != freeBusyLocal {
		t.Errorf("attendees = %+v", got.Attendees)
	}
}

func TestLookupFreeBusyRefusesBadWindow(t *testing.T) {
	a := newCalendarTestApp(t)
	for _, req := range []FreeBusyRequest{
		{From: "yesterday", To: "2026-11-17T00:00:00Z"},
		{From: "2026-11-17T00:00:00Z", To: "2026-11-16T00:00:00Z"},
		{From: "2026-01-01T00:00:00Z", To: "2026-12-31T00:00:00Z"},
	} {
		if _, err := a.LookupFreeBusy(context.Background(), req); !errors.Is(err, errFreeBusyWindow) {
			t.Errorf("%+v: err = %v", req, err)
		}
	}
}

func TestLookupFreeBusyServerWithoutScheduling(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	a, cal := remoteSetup(t, server)
	got, err := a.LookupFreeBusy(context.Background(), FreeBusyRequest{
		CalendarID: cal.ID, From: "2026-11-16T00:00:00Z", To: "2026-11-17T00:00:00Z",
		Attendees: []string{"ada@example.com", "grace@example.com"},
	})
	if err != nil {
		t.Fatalf("a server without scheduling failed the lookup: %v", err)
	}
	for _, at := range got.Attendees {
		if at.Known || at.Reason != freeBusyUnsupported {
			t.Errorf("attendee = %+v", at)
		}
	}
	if len(got.Attendees) != 2 {
		t.Errorf("attendees = %+v", got.Attendees)
	}
}

func TestLookupFreeBusyFromServer(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	server.ScheduleReply = `<?xml version="1.0" encoding="utf-8"?>
<C:schedule-response xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <C:response>
    <C:recipient><D:href>mailto:ada@example.com</D:href></C:recipient>
    <C:request-status>2.0;Success</C:request-status>
    <C:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
METHOD:REPLY
BEGIN:VFREEBUSY
DTSTART:20261116T000000Z
DTEND:20261117T000000Z
FREEBUSY;FBTYPE=BUSY:20261115T230000Z/20261116T100000Z
FREEBUSY;FBTYPE=BUSY-TENTATIVE:20261116T120000Z/PT1H
FREEBUSY;FBTYPE=BUSY-UNAVAILABLE:20261116T180000Z/20261117T000000Z
END:VFREEBUSY
END:VCALENDAR
</C:calendar-data>
  </C:response>
  <C:response>
    <C:recipient><D:href>mailto:outside@example.org</D:href></C:recipient>
    <C:request-status>3.7;Invalid calendar user</C:request-status>
  </C:response>
</C:schedule-response>`
	a, cal := remoteSetup(t, server)
	got, err := a.LookupFreeBusy(context.Background(), FreeBusyRequest{
		CalendarID: cal.ID, From: "2026-11-16T00:00:00Z", To: "2026-11-17T00:00:00Z",
		Organizer: "someone-else@example.com",
		Attendees: []string{"ada@example.com", "outside@example.org"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(server.Scheduled, "ORGANIZER:mailto:"+caldavtest.Address) {
		t.Errorf("request not made as the server's own address:\n%s", server.Scheduled)
	}
	ada, outside := got.Attendees[0], got.Attendees[1]
	if !ada.Known || ada.Reason != "" {
		t.Fatalf("ada = %+v", ada)
	}
	want := []string{"16T00:00-16T10:00 busy", "16T12:00-16T13:00 tentative", "16T18:00-17T00:00 unavailable"}
	if s := spanStrings(ada.Busy); strings.Join(s, ",") != strings.Join(want, ",") {
		t.Errorf("ada busy = %v, want %v", s, want)
	}
	if outside.Known || outside.Reason != freeBusyUnanswered || outside.Status != "3.7;Invalid calendar user" {
		t.Errorf("outside = %+v", outside)
	}
}

func TestLookupFreeBusyServerOffline(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	a, cal := remoteSetup(t, server)
	server.Offline = true
	_, err := a.LookupFreeBusy(context.Background(), FreeBusyRequest{
		CalendarID: cal.ID, From: "2026-11-16T00:00:00Z", To: "2026-11-17T00:00:00Z", Attendees: []string{"ada@example.com"},
	})
	if err == nil {
		t.Fatal("an unreachable server should fail the lookup")
	}
}
