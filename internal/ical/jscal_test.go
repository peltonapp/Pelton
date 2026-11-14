package ical

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// RFC 8984 section 6 examples, trimmed to the parts the tests look at.
const (
	rfcSimpleEvent = `{"@type":"Event","uid":"a8df6573-0474-496d-8496-033ad45d7fea","updated":"2020-01-02T18:23:04Z","title":"Some event","start":"2020-01-15T13:00:00","timeZone":"America/New_York","duration":"PT1H"}`
	rfcGroup       = `{"@type":"Group","uid":"bf0ac22b-4989-4caf-9ebd-54301b4ee51a","updated":"2020-01-15T18:00:00Z","name":"A simple group","entries":[{"@type":"Event","uid":"a8df6573-0474-496d-8496-033ad45d7fea","updated":"2020-01-02T18:23:04Z","title":"Some event","start":"2020-01-15T13:00:00","timeZone":"America/New_York","duration":"PT1H"},{"@type":"Task","uid":"2a358cee-6489-4f14-a57f-c104db4dc2f2","updated":"2020-01-09T14:32:01Z","title":"Do something"}]}`
	rfcAllDay      = `{"@type":"Event","uid":"a8df6573-0474-496d-8496-033ad45d7fea","title":"April Fool's Day","showWithoutTime":true,"start":"1900-04-01T00:00:00","duration":"P1D","recurrenceRules":[{"@type":"RecurrenceRule","frequency":"yearly"}]}`
	rfcFloating    = `{"@type":"Event","uid":"a8df6573-0474-496d-8496-033ad45d7fea","title":"Yoga","start":"2018-01-01T07:00:00","duration":"PT1H","recurrenceRules":[{"@type":"RecurrenceRule","frequency":"daily"}]}`
	rfcOverrides   = `{"@type":"Event","uid":"22e8ba4b-3bd1-4b9e-8dce-e2a1d7c0a8f2","title":"Calculus I","start":"2020-01-08T09:00:00","timeZone":"Europe/London","duration":"PT1H30M","locations":{"mlab":{"@type":"Location","title":"Math lab room 1","description":"Math Lab I, Department of Mathematics"}},"recurrenceRules":[{"@type":"RecurrenceRule","frequency":"weekly","until":"2020-06-24T09:00:00"}],"recurrenceOverrides":{"2020-01-07T14:00:00":{"title":"Introduction to Calculus I (optional)"},"2020-04-01T09:00:00":{"excluded":true},"2020-06-25T09:00:00":{"title":"Calculus I Exam","start":"2020-06-25T10:00:00","duration":"PT2H"}}}`
	rfcParticipants = `{"@type":"Event","uid":"29a6e8e4-3e1f-4e1a-8d4a-6a7c1b6f2d77","title":"FooBar team meeting","start":"2020-01-08T09:00:00","timeZone":"Africa/Johannesburg","duration":"PT1H","recurrenceRules":[{"@type":"RecurrenceRule","frequency":"weekly"}],"participants":{"dG9t":{"@type":"Participant","name":"Tom Tool","email":"tom@foobar.example.com","sendTo":{"imip":"mailto:tom@calendar.example.com"},"participationStatus":"accepted","roles":{"attendee":true}},"em9l":{"@type":"Participant","name":"Zoe Zelda","email":"zoe@foobar.example.com","sendTo":{"imip":"mailto:zoe@foobar.example.com"},"participationStatus":"accepted","roles":{"owner":true,"attendee":true,"chair":true}}},"recurrenceOverrides":{"2020-03-04T09:00:00":{"participants/dG9t/participationStatus":"declined"}}}`
)

func importJS(t *testing.T, data string) ([]*Object, JSCalReport) {
	t.Helper()
	cal, report, err := FromJSCalendar([]byte(data), time.UTC)
	if err != nil {
		t.Fatalf("FromJSCalendar: %v", err)
	}
	objs, err := Split(cal, time.UTC)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	return objs, report
}

func TestFromJSCalendarExamples(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		events  int
		skipped int
		check   func(t *testing.T, ev *Event, obj *Object)
	}{
		{"simple event", rfcSimpleEvent, 1, 0, func(t *testing.T, ev *Event, obj *Object) {
			if ev.Summary != "Some event" || ev.Start.TZID != "America/New_York" || ev.Start.Value() != "20200115T130000" {
				t.Errorf("start %+v summary %q", ev.Start, ev.Summary)
			}
			if ev.End.Wall.Sub(ev.Start.Wall) != time.Hour {
				t.Errorf("end %v", ev.End)
			}
			if got := ev.Component.Text("LAST-MODIFIED"); got != "20200102T182304Z" {
				t.Errorf("updated %q", got)
			}
		}},
		{"group with a task", rfcGroup, 1, 1, nil},
		{"all day", rfcAllDay, 1, 0, func(t *testing.T, ev *Event, obj *Object) {
			if !ev.Start.AllDay || ev.Start.Value() != "19000401" || ev.End.Value() != "19000402" {
				t.Errorf("all day %+v to %+v", ev.Start, ev.End)
			}
			if ev.RRule == nil || ev.RRule.Freq != Yearly {
				t.Errorf("rule %+v", ev.RRule)
			}
		}},
		{"floating", rfcFloating, 1, 0, func(t *testing.T, ev *Event, obj *Object) {
			if ev.Start.TZID != "" || ev.Start.UTC || ev.Start.AllDay {
				t.Errorf("not floating: %+v", ev.Start)
			}
		}},
		{"overrides", rfcOverrides, 1, 0, func(t *testing.T, ev *Event, obj *Object) {
			if ev.Location != "Math lab room 1" {
				t.Errorf("location %q", ev.Location)
			}
			if ev.RRule == nil || !ev.RRule.HasUntil || ev.RRule.Until.Value() != "20200624T080000Z" {
				t.Errorf("until %+v", ev.RRule)
			}
			if len(ev.ExDates) != 1 || ev.ExDates[0].Value() != "20200401T090000" {
				t.Errorf("exdates %+v", ev.ExDates)
			}
			if len(obj.Overrides) != 2 {
				t.Fatalf("overrides %d", len(obj.Overrides))
			}
			exam := obj.Overrides[1]
			if exam.Summary != "Calculus I Exam" || exam.Start.Value() != "20200625T100000" || exam.End.Wall.Sub(exam.Start.Wall) != 2*time.Hour {
				t.Errorf("exam %+v", exam)
			}
		}},
		{"participants", rfcParticipants, 1, 0, func(t *testing.T, ev *Event, obj *Object) {
			if ev.Organizer == nil || ev.Organizer.Email != "zoe@foobar.example.com" {
				t.Errorf("organizer %+v", ev.Organizer)
			}
			if len(ev.Attendees) != 2 || ev.Attendees[0].PartStat != PartStatAccepted || ev.Attendees[1].Role != "CHAIR" {
				t.Errorf("attendees %+v", ev.Attendees)
			}
			if len(obj.Overrides) != 0 {
				t.Errorf("a patch Pelton cannot hold made an override")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !IsJSCalendar([]byte(c.data)) {
				t.Fatal("not detected as JSCalendar")
			}
			objs, report := importJS(t, c.data)
			if report.Events != c.events || len(report.Skipped) != c.skipped || len(objs) != c.events {
				t.Fatalf("events %d skipped %v objects %d", report.Events, report.Skipped, len(objs))
			}
			if c.check != nil {
				c.check(t, objs[0].Master, objs[0])
			}
		})
	}
}

func TestFromJSCalendarLenient(t *testing.T) {
	data := `[{"@type":"Event","uid":"1","title":"no start"},{"@type":"Event","uid":"2","title":"ok","start":"2026-05-01T10:00:00","timeZone":"Etc/UTC","duration":"PT30M","futureProperty":{"x":1},"status":"tentative","freeBusyStatus":"free","privacy":"private","sequence":3,"alerts":{"a":{"@type":"Alert","trigger":{"@type":"OffsetTrigger","offset":"-PT15M"}},"b":{"@type":"Alert","trigger":{"@type":"UnknownTrigger"}}}},{"@type":"Task","uid":"3"},{"@type":"Note"}]`
	objs, report := importJS(t, data)
	if report.Events != 1 || len(report.Skipped) != 2 {
		t.Fatalf("events %d skipped %+v", report.Events, report.Skipped)
	}
	ev := objs[0].Master
	if !ev.Start.UTC || ev.Status != "TENTATIVE" || ev.Transparency != "TRANSPARENT" || ev.Sequence != 3 {
		t.Errorf("fields %+v", ev)
	}
	if ev.Component.Text("CLASS") != "PRIVATE" {
		t.Errorf("class %q", ev.Component.Text("CLASS"))
	}
	if len(ev.Alarms) != 1 || ev.Alarms[0].Trigger != -15*time.Minute {
		t.Errorf("alarms %+v", ev.Alarms)
	}
}

func TestIsJSCalendar(t *testing.T) {
	for in, want := range map[string]bool{
		rfcSimpleEvent:                   true,
		`[{"@type":"Group","entries":[]}]`: true,
		`{"@type":"Contact"}`:             false,
		`{"title":"x"}`:                   false,
		"BEGIN:VCALENDAR":                 false,
		"":                                false,
	} {
		if got := IsJSCalendar([]byte(in)); got != want {
			t.Errorf("%q: %v", in, got)
		}
	}
	if _, _, err := FromJSCalendar([]byte(`{"@type":"Contact"}`), time.UTC); err == nil {
		t.Error("a non-calendar object imported")
	}
	if _, _, err := FromJSCalendar([]byte(strings.Repeat(" ", JSCalMaxBytes+1)), time.UTC); err == nil {
		t.Error("oversized data imported")
	}
}

func TestJSCalendarRoundTrip(t *testing.T) {
	for _, data := range []string{rfcSimpleEvent, rfcAllDay, rfcOverrides, rfcParticipants} {
		objs, _ := importJS(t, data)
		out, err := ToJSCalendar(objs, "Home")
		if err != nil {
			t.Fatal(err)
		}
		var group struct {
			Type    string           `json:"@type"`
			Title   string           `json:"title"`
			Entries []map[string]any `json:"entries"`
		}
		if err := json.Unmarshal(out, &group); err != nil || group.Type != "Group" || group.Title != "Home" || len(group.Entries) != 1 {
			t.Fatalf("export: %v %s", err, out)
		}
		again, _ := importJS(t, string(out))
		a, b := objs[0].Master, again[0].Master
		if a.UID != b.UID || a.Summary != b.Summary || a.Start != b.Start || a.End != b.End || a.Location != b.Location {
			t.Errorf("round trip changed the event:\n%+v\n%+v", a, b)
		}
		if (a.RRule == nil) != (b.RRule == nil) || (a.RRule != nil && a.RRule.String() != b.RRule.String()) {
			t.Errorf("rule %v vs %v", a.RRule, b.RRule)
		}
		if len(a.ExDates) != len(b.ExDates) || len(objs[0].Overrides) != len(again[0].Overrides) || len(a.Attendees) != len(b.Attendees) {
			t.Errorf("exdates, overrides or attendees changed")
		}
	}
}
