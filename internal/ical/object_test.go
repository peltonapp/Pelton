package ical

import (
	"strings"
	"testing"
	"time"
)

func calendar(lines ...string) []byte {
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCALENDAR\r\n")
}

func mustObject(t *testing.T, local *time.Location, lines ...string) *Object {
	t.Helper()
	obj, err := ParseObject(calendar(lines...), local)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func utc(s string) time.Time {
	return wall(s)
}

func starts(occs []Occurrence) []string {
	var out []string
	for _, o := range occs {
		out = append(out, o.Start.UTC().Format(layoutDateTime)+"Z")
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A weekly meeting at nine in Berlin stays at nine on the wall clock when the
// clocks go forward, so its UTC time moves by an hour.
func TestExpandAcrossDST(t *testing.T) {
	obj := mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:dst", "DTSTART;TZID=Europe/Berlin:20260322T090000",
		"DTEND;TZID=Europe/Berlin:20260322T100000", "RRULE:FREQ=WEEKLY", "END:VEVENT")
	got := starts(obj.Expand(utc("20260320T000000"), utc("20260406T000000")))
	want := []string{"20260322T080000Z", "20260329T070000Z", "20260405T070000Z"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	occ := obj.Expand(utc("20260329T000000"), utc("20260330T000000"))[0]
	if occ.End.Sub(occ.Start) != time.Hour {
		t.Errorf("duration across DST = %v", occ.End.Sub(occ.Start))
	}
}

// The same zone described only by its VTIMEZONE, under a TZID no zone
// database knows, has to give the same answer.
func TestExpandWithCustomVTimezone(t *testing.T) {
	obj := mustObject(t, time.UTC,
		"BEGIN:VTIMEZONE", "TZID:My Office Time",
		"BEGIN:STANDARD", "DTSTART:19701025T030000", "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU",
		"TZOFFSETFROM:+0200", "TZOFFSETTO:+0100", "END:STANDARD",
		"BEGIN:DAYLIGHT", "DTSTART:19700329T020000", "RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU",
		"TZOFFSETFROM:+0100", "TZOFFSETTO:+0200", "END:DAYLIGHT",
		"END:VTIMEZONE",
		"BEGIN:VEVENT", "UID:custom", "DTSTART;TZID=My Office Time:20260322T090000",
		"DURATION:PT1H", "RRULE:FREQ=WEEKLY;COUNT=2", "END:VEVENT")
	got := starts(obj.Expand(utc("20260101T000000"), utc("20270101T000000")))
	want := []string{"20260322T080000Z", "20260329T070000Z"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A VTIMEZONE built from the zone database reads back to the same offsets.
func TestGeneratedVTimezoneMatchesZone(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	tz := VTimezone("Custom/NY", loc, 2026)
	vz := newVTimezone(tz)
	if vz == nil {
		t.Fatal("no observances")
	}
	for _, at := range []time.Time{
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2027, 7, 15, 12, 0, 0, 0, time.UTC),
	} {
		_, want := at.In(loc).Zone()
		if got := vz.offsetAt(at); got != time.Duration(want)*time.Second {
			t.Errorf("offset at %v = %v, want %ds", at, got, want)
		}
	}
}

func TestExpandAllDayInUsersZone(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	obj := mustObject(t, ny,
		"BEGIN:VEVENT", "UID:bday", "DTSTART;VALUE=DATE:20200315", "RRULE:FREQ=YEARLY", "END:VEVENT")
	occs := obj.Expand(time.Date(2026, 1, 1, 0, 0, 0, 0, ny), time.Date(2027, 1, 1, 0, 0, 0, 0, ny))
	if len(occs) != 1 {
		t.Fatalf("got %d occurrences", len(occs))
	}
	o := occs[0]
	if !o.AllDay || !o.Start.Equal(time.Date(2026, 3, 15, 0, 0, 0, 0, ny)) || !o.End.Equal(time.Date(2026, 3, 16, 0, 0, 0, 0, ny)) {
		t.Errorf("occurrence = %+v", o)
	}
}

func TestExpandExdateAndOverride(t *testing.T) {
	obj := mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:series", "DTSTART:20260105T090000Z", "DTEND:20260105T100000Z",
		"RRULE:FREQ=DAILY;COUNT=5", "EXDATE:20260106T090000Z", "SUMMARY:Standup", "END:VEVENT",
		"BEGIN:VEVENT", "UID:series", "RECURRENCE-ID:20260107T090000Z",
		"DTSTART:20260115T140000Z", "DTEND:20260115T150000Z", "SUMMARY:Standup moved", "END:VEVENT")

	got := starts(obj.Expand(utc("20260101T000000"), utc("20260110T000000")))
	want := []string{"20260105T090000Z", "20260108T090000Z", "20260109T090000Z"}
	if !equalStrings(got, want) {
		t.Errorf("first week = %v, want %v", got, want)
	}

	moved := obj.Expand(utc("20260115T000000"), utc("20260116T000000"))
	if len(moved) != 1 || !moved[0].Override || moved[0].Event.Summary != "Standup moved" {
		t.Fatalf("moved occurrence = %+v", moved)
	}
	if !moved[0].RecurrenceID.Equal(utc("20260107T090000")) {
		t.Errorf("recurrence id = %v", moved[0].RecurrenceID)
	}
}

func TestExpandWindowAfterCountedStart(t *testing.T) {
	obj := mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:c", "DTSTART:20260101T080000Z", "RRULE:FREQ=DAILY;COUNT=10", "END:VEVENT")
	got := obj.Expand(utc("20260105T000000"), utc("20260120T000000"))
	if len(got) != 6 {
		t.Errorf("got %v, want the 5th to the 10th", starts(got))
	}
}

func TestExpandUntilDateOnTimedEvent(t *testing.T) {
	obj := mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:u", "DTSTART:20260101T200000Z", "RRULE:FREQ=DAILY;UNTIL=20260103", "END:VEVENT")
	if got := obj.Expand(utc("20250101T000000"), utc("20270101T000000")); len(got) != 3 {
		t.Errorf("got %v, want three", starts(got))
	}
}

func TestBounds(t *testing.T) {
	finite := mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:f", "DTSTART:20260101T080000Z", "DTEND:20260101T090000Z", "RRULE:FREQ=WEEKLY;COUNT=3", "END:VEVENT")
	start, end, open := finite.Bounds()
	if open || !start.Equal(utc("20260101T080000")) || !end.Equal(utc("20260115T090000")) {
		t.Errorf("finite bounds = %v %v %v", start, end, open)
	}
	infinite := mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:i", "DTSTART:20260101T080000Z", "RRULE:FREQ=WEEKLY", "END:VEVENT")
	if _, _, open := infinite.Bounds(); !open {
		t.Error("an endless series was reported closed")
	}
}

func berlinSeries(t *testing.T) *Object {
	return mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:weekly", "DTSTART;TZID=Europe/Berlin:20260105T090000",
		"DTEND;TZID=Europe/Berlin:20260105T093000", "RRULE:FREQ=WEEKLY;COUNT=6", "SUMMARY:Sync", "END:VEVENT")
}

func TestExcludeOccurrenceWritesMatchingExdate(t *testing.T) {
	obj := berlinSeries(t)
	rid := utc("20260112T080000")
	obj.ExcludeOccurrence(rid)
	out := string(obj.Encode())
	if !strings.Contains(out, "EXDATE;TZID=Europe/Berlin:20260112T090000") {
		t.Errorf("exdate not written in the start's form:\n%s", out)
	}
	again, _ := ParseObject([]byte(out), time.UTC)
	if n := len(again.Expand(utc("20260101T000000"), utc("20270101T000000"))); n != 5 {
		t.Errorf("got %d occurrences after excluding one, want 5", n)
	}
}

func TestOverrideOneOccurrence(t *testing.T) {
	obj := berlinSeries(t)
	ov := obj.Override(utc("20260119T080000"))
	ov.Summary = "Sync, moved"
	ov.Start.Wall = ov.Start.Wall.Add(2 * time.Hour)
	ov.End.Wall = ov.End.Wall.Add(2 * time.Hour)
	ov.Apply()
	out := string(obj.Encode())
	if !strings.Contains(out, "RECURRENCE-ID;TZID=Europe/Berlin:20260119T090000") {
		t.Errorf("recurrence id not written in the start's form:\n%s", out)
	}
	again, _ := ParseObject([]byte(out), time.UTC)
	occs := again.Expand(utc("20260119T000000"), utc("20260120T000000"))
	if len(occs) != 1 || occs[0].Event.Summary != "Sync, moved" || !occs[0].Start.Equal(utc("20260119T100000")) {
		t.Errorf("override = %+v", occs)
	}
	if strings.Count(out, "RRULE") != 1 {
		t.Error("the override kept the rule")
	}
}

func TestSplitAtKeepsTheRemainingCount(t *testing.T) {
	obj := berlinSeries(t)
	obj.Override(utc("20260202T080000")).Summary = "late override"
	future := obj.SplitAt(utc("20260126T080000"), "weekly-2")
	if future == nil {
		t.Fatal("nothing split off")
	}
	past := obj.Expand(utc("20250101T000000"), utc("20280101T000000"))
	rest := future.Expand(utc("20250101T000000"), utc("20280101T000000"))
	if len(past) != 3 || len(rest) != 3 {
		t.Fatalf("split into %d and %d, want 3 and 3: %v / %v", len(past), len(rest), starts(past), starts(rest))
	}
	if future.UID() != "weekly-2" || len(future.Overrides) != 1 || len(obj.Overrides) != 0 {
		t.Errorf("overrides went to the wrong half: %d / %d", len(obj.Overrides), len(future.Overrides))
	}
	if !rest[0].Start.Equal(utc("20260126T080000")) {
		t.Errorf("future starts at %v", rest[0].Start)
	}
	if obj.SplitAt(utc("20260105T080000"), "x") != nil {
		t.Error("split at the first occurrence returned a second series")
	}
}

func TestTruncateAllDaySeries(t *testing.T) {
	obj := mustObject(t, time.UTC,
		"BEGIN:VEVENT", "UID:ad", "DTSTART;VALUE=DATE:20260101", "RRULE:FREQ=DAILY", "END:VEVENT")
	if !obj.TruncateBefore(utc("20260104T000000")) {
		t.Fatal("truncate reported nothing left")
	}
	if !strings.Contains(string(obj.Encode()), "UNTIL=20260103") {
		t.Errorf("until not a date:\n%s", obj.Encode())
	}
	if n := len(obj.Expand(utc("20250101T000000"), utc("20280101T000000"))); n != 3 {
		t.Errorf("got %d days, want 3", n)
	}
}

func TestNewObjectAddsTimezone(t *testing.T) {
	ev := &Event{
		UID:     "new-1",
		Summary: "Lunch",
		Start:   DateTime{Wall: wall("20260601T120000"), TZID: "Europe/Paris"},
		End:     DateTime{Wall: wall("20260601T130000"), TZID: "Europe/Paris"},
		Alarms:  []Alarm{{Trigger: -10 * time.Minute}},
	}
	obj := NewObject(ev, time.UTC)
	out := string(obj.Encode())
	for _, want := range []string{"BEGIN:VTIMEZONE", "TZID:Europe/Paris", "BEGIN:DAYLIGHT", "TRIGGER:-PT10M", "ACTION:DISPLAY", "PRODID:" + ProdID} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestSplitGroupsByUID(t *testing.T) {
	cal, err := ParseCalendar(strings.NewReader(string(calendar(
		"BEGIN:VEVENT", "UID:a", "DTSTART:20260101T080000Z", "END:VEVENT",
		"BEGIN:VEVENT", "UID:b", "DTSTART:20260102T080000Z", "END:VEVENT",
		"BEGIN:VEVENT", "UID:a", "RECURRENCE-ID:20260101T080000Z", "DTSTART:20260101T090000Z", "END:VEVENT",
		"BEGIN:VTODO", "UID:t", "END:VTODO",
	))))
	if err != nil {
		t.Fatal(err)
	}
	objs, err := Split(cal, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[0].UID() != "a" || len(objs[0].Overrides) != 1 || objs[1].UID() != "b" {
		t.Errorf("split = %d objects", len(objs))
	}
}
