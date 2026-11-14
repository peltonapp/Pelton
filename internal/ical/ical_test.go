package ical

import (
	"strings"
	"testing"
	"time"
)

const sampleEvent = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//Example//Other Client//EN\r\n" +
	"X-WR-CALNAME:Work\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:abc-123@example.com\r\n" +
	"DTSTAMP:20260101T120000Z\r\n" +
	"DTSTART;TZID=Europe/Berlin:20260310T090000\r\n" +
	"DTEND;TZID=Europe/Berlin:20260310T100000\r\n" +
	"SUMMARY:Planning\\, round 2\r\n" +
	"DESCRIPTION:Line one\\nLine two\\; with a semicolon\r\n" +
	"LOCATION:Room 4\r\n" +
	"CATEGORIES:Work,Planning\r\n" +
	"STATUS:CONFIRMED\r\n" +
	"TRANSP:OPAQUE\r\n" +
	"URL:https://example.com/meeting\r\n" +
	"ORGANIZER;CN=\"Boss, The\":mailto:boss@example.com\r\n" +
	"ATTENDEE;CN=Arne;PARTSTAT=NEEDS-ACTION;RSVP=TRUE;X-CUSTOM=keep:mailto:arne@example.com\r\n" +
	"X-GOOGLE-CONFERENCE:https://meet.example.com/xyz\r\n" +
	"ATTACH;FMTTYPE=application/pdf:https://example.com/agenda.pdf\r\n" +
	"BEGIN:VALARM\r\n" +
	"ACTION:DISPLAY\r\n" +
	"TRIGGER:-PT15M\r\n" +
	"DESCRIPTION:Planning soon\r\n" +
	"X-WR-ALARMUID:alarm-1\r\n" +
	"END:VALARM\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseEventFields(t *testing.T) {
	obj, err := ParseObject([]byte(sampleEvent), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	ev := obj.Master
	if ev.Summary != "Planning, round 2" {
		t.Errorf("summary = %q", ev.Summary)
	}
	if ev.Description != "Line one\nLine two; with a semicolon" {
		t.Errorf("description = %q", ev.Description)
	}
	if ev.Start.TZID != "Europe/Berlin" || ev.Start.Wall.Hour() != 9 {
		t.Errorf("start = %+v", ev.Start)
	}
	if got := obj.Zones.Instant(ev.Start); !got.Equal(time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("start instant = %v", got)
	}
	if len(ev.Categories) != 2 || ev.Categories[1] != "Planning" {
		t.Errorf("categories = %v", ev.Categories)
	}
	if ev.Organizer == nil || ev.Organizer.Name != "Boss, The" || ev.Organizer.Email != "boss@example.com" {
		t.Errorf("organizer = %+v", ev.Organizer)
	}
	if len(ev.Attendees) != 1 || ev.Attendees[0].PartStat != PartStatNeedsAction || !ev.Attendees[0].RSVP {
		t.Errorf("attendees = %+v", ev.Attendees)
	}
	if len(ev.Alarms) != 1 || ev.Alarms[0].Trigger != -15*time.Minute {
		t.Errorf("alarms = %+v", ev.Alarms)
	}
	if ev.URL != "https://example.com/meeting" || ev.Status != StatusConfirmed || ev.Transparency != "OPAQUE" {
		t.Errorf("url/status/transp = %q %q %q", ev.URL, ev.Status, ev.Transparency)
	}
}

// An edit here must not cost another client's data: everything Pelton has no
// field for survives a change to a field it does have.
func TestRoundTripKeepsUnknownProperties(t *testing.T) {
	obj, err := ParseObject([]byte(sampleEvent), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	obj.Master.Summary = "Planning, round 3"
	obj.Master.Attendees[0].PartStat = PartStatAccepted
	obj.Master.Alarms[0].Trigger = -30 * time.Minute
	obj.Master.Apply()
	out := string(obj.Encode())

	for _, want := range []string{
		"X-WR-CALNAME:Work",
		"X-GOOGLE-CONFERENCE:https://meet.example.com/xyz",
		"ATTACH;FMTTYPE=application/pdf:https://example.com/agenda.pdf",
		"X-CUSTOM=keep",
		"X-WR-ALARMUID:alarm-1",
		"PARTSTAT=ACCEPTED",
		"TRIGGER:-PT30M",
		`SUMMARY:Planning\, round 3`,
		`ORGANIZER;CN="Boss, The":mailto:boss@example.com`,
	} {
		if !strings.Contains(unfoldString(out), want) {
			t.Errorf("output lost %q:\n%s", want, out)
		}
	}

	again, err := ParseObject([]byte(out), time.UTC)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if again.Master.Summary != "Planning, round 3" || again.Master.Description != obj.Master.Description {
		t.Errorf("reparsed = %q / %q", again.Master.Summary, again.Master.Description)
	}
}

func unfoldString(s string) string {
	return strings.ReplaceAll(s, "\r\n ", "")
}

func TestFoldingKeepsUTF8Intact(t *testing.T) {
	c := &Component{Name: "VEVENT"}
	long := strings.Repeat("Grüße aus Köln ", 20)
	c.SetText("SUMMARY", long)
	out := c.String()
	for _, line := range strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("line of %d octets: %q", len(line), line)
		}
		if !strings.HasPrefix(line, " ") && strings.ContainsRune(line, '\uFFFD') {
			t.Errorf("broken rune in %q", line)
		}
	}
	roots, err := Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if got := roots[0].Text("SUMMARY"); got != long {
		t.Errorf("unfolded summary differs:\n%q\n%q", got, long)
	}
}

func TestParseToleratesBareLineFeeds(t *testing.T) {
	data := strings.ReplaceAll(sampleEvent, "\r\n", "\n")
	if _, err := ParseObject([]byte(data), time.UTC); err != nil {
		t.Fatal(err)
	}
}

func TestParseRejectsUnbalanced(t *testing.T) {
	for _, data := range []string{
		"BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nEND:VCALENDAR\r\n",
		"BEGIN:VCALENDAR\r\n",
		"",
	} {
		if _, err := Parse(strings.NewReader(data)); err == nil {
			t.Errorf("Parse(%q) accepted", data)
		}
	}
}

func TestDurations(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"-PT15M", -15 * time.Minute},
		{"P1D", 24 * time.Hour},
		{"P1W", 7 * 24 * time.Hour},
		{"PT1H30M", 90 * time.Minute},
		{"P1DT2H", 26 * time.Hour},
		{"PT0S", 0},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseDuration(%q) = %v, %v", tt.in, got, err)
		}
		back, _ := ParseDuration(FormatDuration(got))
		if back != got {
			t.Errorf("FormatDuration(%v) = %q does not read back", got, FormatDuration(got))
		}
	}
	if _, err := ParseDuration("15M"); err == nil {
		t.Error("a duration without P was accepted")
	}
}

func TestParamCaretEscapes(t *testing.T) {
	p := &Prop{Name: "ATTENDEE", Value: "mailto:a@example.com"}
	p.SetParam("CN", `Ann "The Boss"`)
	c := &Component{Name: "VEVENT", Props: []*Prop{p}}
	roots, err := Parse(strings.NewReader("BEGIN:VCALENDAR\r\n" + c.String() + "END:VCALENDAR\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := roots[0].Children[0].Prop("ATTENDEE").Param("CN"); got != `Ann "The Boss"` {
		t.Errorf("CN = %q", got)
	}
}
