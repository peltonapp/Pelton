package ical

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const otherClientTodo = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Other//EN\r\n" +
	"BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\nBEGIN:STANDARD\r\nDTSTART:19701025T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nRRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU\r\nEND:STANDARD\r\nBEGIN:DAYLIGHT\r\nDTSTART:19700329T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU\r\nEND:DAYLIGHT\r\nEND:VTIMEZONE\r\n" +
	"BEGIN:VTODO\r\nUID:todo-1\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:Pay rent\\, flat 2\r\nDESCRIPTION:Line one\\nLine two\r\n" +
	"DTSTART;TZID=Europe/Berlin:20260301T090000\r\nDUE;TZID=Europe/Berlin:20260301T180000\r\n" +
	"STATUS:IN-PROCESS\r\nPERCENT-COMPLETE:40\r\nPRIORITY:1\r\nRRULE:FREQ=MONTHLY;BYMONTHDAY=1;COUNT=3\r\n" +
	"RELATED-TO;RELTYPE=PARENT:parent-uid\r\nRELATED-TO;RELTYPE=SIBLING:event-uid\r\nRELATED-TO;RELTYPE=CHILD:child-uid\r\n" +
	"URL:https://example.com/rent\r\nCATEGORIES:home,money\r\nATTACH;FMTTYPE=message/rfc822:mid:abc@example.com\r\n" +
	"CONTACT;ALTREP=\"mailto:ada@example.com\":Ada Lovelace\r\nX-APPLE-SORT-ORDER:7\r\nX-OTHER-CLIENT:keep me\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:-PT15M\r\nDESCRIPTION:Rent\r\nX-ALARM-EXTRA:keep\r\nEND:VALARM\r\n" +
	"END:VTODO\r\nEND:VCALENDAR\r\n"

func TestParseTodoReadsEveryField(t *testing.T) {
	obj, err := ParseTodoObject([]byte(otherClientTodo), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	td := obj.Todo
	switch {
	case td.UID != "todo-1", td.Summary != "Pay rent, flat 2", td.Description != "Line one\nLine two":
		t.Errorf("text fields %+v", td)
	case td.Status != TodoInProcess, td.Percent != 40, td.Priority != 1, td.SortOrder != 7:
		t.Errorf("status fields %+v", td)
	case td.Start.TZID != "Europe/Berlin", td.Due.Value() != "20260301T180000":
		t.Errorf("dates %+v %+v", td.Start, td.Due)
	case td.RRule == nil || td.RRule.Count != 3:
		t.Errorf("rule %+v", td.RRule)
	case td.Parent != "parent-uid", len(td.Related) != 1 || td.Related[0] != "event-uid":
		t.Errorf("relations %q %v", td.Parent, td.Related)
	case len(td.Attachments) != 1 || td.Attachments[0] != "mid:abc@example.com":
		t.Errorf("attachments %v", td.Attachments)
	case len(td.Contacts) != 1 || td.Contacts[0] != (Contact{Name: "Ada Lovelace", Email: "ada@example.com"}):
		t.Errorf("contacts %+v", td.Contacts)
	case len(td.Categories) != 2, len(td.Alarms) != 1 || !td.Alarms[0].RelatedEnd:
		t.Errorf("categories %v alarms %+v", td.Categories, td.Alarms)
	}
	if td.Done() {
		t.Error("an open task reads as done")
	}
}

// An edit here touches only what Pelton owns: the other client's property,
// the alarm's extra line, the CHILD relation and the zone all survive.
func TestTodoEditKeepsUnknownProperties(t *testing.T) {
	obj, err := ParseTodoObject([]byte(otherClientTodo), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	obj.Todo.Summary = "Pay rent"
	obj.Todo.Contacts = append(obj.Todo.Contacts, Contact{Name: "Bob", Email: "bob@example.com"})
	obj.Todo.SetDone(true, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	out := string(obj.Encode())
	for _, want := range []string{
		"X-OTHER-CLIENT:keep me", "X-ALARM-EXTRA:keep", "RELATED-TO;RELTYPE=CHILD:child-uid",
		"RELATED-TO;RELTYPE=PARENT:parent-uid", "RELATED-TO;RELTYPE=SIBLING:event-uid",
		"BEGIN:VTIMEZONE", "STATUS:COMPLETED", "PERCENT-COMPLETE:100", "COMPLETED:20260301T120000Z",
		"CONTACT;ALTREP=\"mailto:bob@example.com\":Bob", "ATTACH;FMTTYPE=message/rfc822:mid:abc@example.com",
		"SUMMARY:Pay rent\r\n", "X-APPLE-SORT-ORDER:7", "URL:https://example.com/rent", "CATEGORIES:home,money",
		"RRULE:FREQ=MONTHLY;COUNT=3;BYMONTHDAY=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	again, err := ParseTodoObject([]byte(out), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Todo.Done() || len(again.Todo.Contacts) != 2 {
		t.Errorf("reparsed %+v", again.Todo)
	}
}

func TestTodoEstimate(t *testing.T) {
	src := strings.Replace(otherClientTodo, "X-APPLE-SORT-ORDER:7\r\n", "X-APPLE-SORT-ORDER:7\r\nESTIMATED-DURATION:PT1H30M\r\n", 1)
	obj, err := ParseTodoObject([]byte(src), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Todo.Estimate != 90*time.Minute {
		t.Fatalf("estimate = %v", obj.Todo.Estimate)
	}
	obj.Todo.Estimate = 45 * time.Minute
	if out := string(obj.Encode()); !strings.Contains(out, "ESTIMATED-DURATION:PT45M\r\n") {
		t.Errorf("changed estimate not written:\n%s", out)
	}
	obj.Todo.Estimate = 0
	if out := string(obj.Encode()); strings.Contains(out, "ESTIMATED-DURATION") {
		t.Errorf("cleared estimate kept:\n%s", out)
	}
	bad, err := ParseTodoObject([]byte(strings.Replace(src, "PT1H30M", "soon", 1)), time.UTC)
	if err != nil || bad.Todo.Estimate != 0 {
		t.Errorf("unreadable estimate = %v, %v", bad.Todo.Estimate, err)
	}
}

func TestTodoAdvance(t *testing.T) {
	obj, err := ParseTodoObject([]byte(otherClientTodo), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	obj.Todo.SetDone(true, time.Now())
	if !obj.Advance() {
		t.Fatal("first advance refused")
	}
	td := obj.Todo
	if td.Done() || td.Start.Value() != "20260401T090000" || td.Due.Value() != "20260401T180000" || td.RRule.Count != 2 {
		t.Errorf("after one: %+v %+v %v", td.Start, td.Due, td.RRule)
	}
	if !obj.Advance() || obj.Advance() {
		t.Error("COUNT=3 should allow exactly two advances")
	}
	if td.Due.Value() != "20260501T180000" {
		t.Errorf("last due %s", td.Due.Value())
	}
}

func TestTodoAdvanceDateOnlyDue(t *testing.T) {
	td := &Todo{UID: "x", Due: DateTime{Wall: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), AllDay: true}}
	td.RRule, _ = ParseRRule("FREQ=WEEKLY")
	obj := NewTodoObject(td, time.UTC)
	if !obj.Advance() || td.Due.Value() != "20260207" || !td.Due.AllDay {
		t.Errorf("due %+v", td.Due)
	}
	plain := NewTodoObject(&Todo{UID: "y"}, time.UTC)
	if plain.Advance() {
		t.Error("a task that does not repeat advanced")
	}
}

func TestSplitTodosAndHasTodo(t *testing.T) {
	if !HasTodo([]byte(otherClientTodo)) {
		t.Error("todo data not recognised")
	}
	if HasTodo([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nEND:VEVENT\r\nBEGIN:VTODO\r\nEND:VTODO\r\nEND:VCALENDAR")) {
		t.Error("data with an event read as a task")
	}
	cal, err := ParseCalendar(strings.NewReader("BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:a\r\nEND:VTODO\r\nBEGIN:VEVENT\r\nUID:e\r\nDTSTART:20260101T000000Z\r\nEND:VEVENT\r\nBEGIN:VTODO\r\nUID:b\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	todos := SplitTodos(cal, time.UTC)
	if len(todos) != 2 || todos[0].Todo.UID != "a" || todos[1].Todo.UID != "b" {
		t.Fatalf("todos %+v", todos)
	}
	if _, err := ParseTodoObject([]byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"), time.UTC); err != ErrNoTodo {
		t.Errorf("err = %v", err)
	}
}

func TestJSCalendarTaskRoundTrip(t *testing.T) {
	data := `{"@type":"Task","uid":"t1","title":"File taxes","due":"2026-04-30T17:00:00","timeZone":"Europe/Berlin","priority":1,"percentComplete":20,"progress":"in-process","keywords":{"money":true},"relatedTo":{"p1":{"@type":"Relation","relation":{"parent":true}}},"links":{"1":{"@type":"Link","href":"mid:x@example.com"}},"alerts":{"1":{"@type":"Alert","trigger":{"@type":"OffsetTrigger","offset":"-PT1H","relativeTo":"end"}}}}`
	cal, report, err := FromJSCalendar([]byte(data), time.UTC)
	if err != nil || report.Tasks != 1 {
		t.Fatalf("import %+v %v", report, err)
	}
	todos := SplitTodos(cal, time.UTC)
	if len(todos) != 1 {
		t.Fatal("no task")
	}
	td := todos[0].Todo
	if td.Summary != "File taxes" || td.Due.TZID != "Europe/Berlin" || td.Priority != 1 || td.Percent != 20 ||
		td.Status != TodoInProcess || td.Parent != "p1" || len(td.Attachments) != 1 || len(td.Alarms) != 1 || !td.Alarms[0].RelatedEnd {
		t.Errorf("task %+v", td)
	}
	out, err := ToJSCalendar(nil, todos, "Errands")
	if err != nil {
		t.Fatal(err)
	}
	var group struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(out, &group); err != nil || len(group.Entries) != 1 {
		t.Fatalf("export %s %v", out, err)
	}
	e := group.Entries[0]
	if e["@type"] != "Task" || e["due"] != "2026-04-30T17:00:00" || e["progress"] != "in-process" || e["timeZone"] != "Europe/Berlin" {
		t.Errorf("exported %v", e)
	}
}
