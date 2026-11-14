package desktop

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const sampleEventLinkJSON = `[{"s":"Test Event","l":"Test Location, 53111Bonn","u":"https://pelton.app","b":"2026-11-15T00:00","e":"2026-11-15T01:00","U":1,"st":"CONFIRMED","tr":"TRANSPARENT","cl":"CONFIDENTIAL","pr":"1","f":"WEEKLY","iv":1,"re":"never","ct":10,"al":[{"amount":15,"unit":"M","rel":"before-start"}],"fn":"events.ics"},{"s":"Another test","b":"2026-11-15","e":"2026-11-15","A":1,"iv":1,"re":"never","ct":10}]`

func eventLink(t *testing.T, host, data string) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return "https://" + host + "/e/#z" + base64.RawURLEncoding.EncodeToString(buf.Bytes())
}

func TestDecodeEventLinkSample(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	got, err := decodeEventLink(eventLink(t, "arne.sh", sampleEventLinkJSON), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 || got.Skipped != 0 {
		t.Fatalf("events %d skipped %d", len(got.Events), got.Skipped)
	}
	first := got.Events[0]
	if first.Summary != "Test Event" || first.Location != "Test Location, 53111Bonn" || first.URL != "https://pelton.app" {
		t.Errorf("first fields: %+v", first)
	}
	// midnight UTC is one in the morning in Berlin in November.
	if first.Start != "2026-11-15T01:00:00+01:00" || first.End != "2026-11-15T02:00:00+01:00" || first.AllDay {
		t.Errorf("first times: %s to %s all day %v", first.Start, first.End, first.AllDay)
	}
	if first.RRule != "FREQ=WEEKLY" {
		t.Errorf("rule %q", first.RRule)
	}
	if first.Status != "CONFIRMED" || first.Transparency != "TRANSPARENT" {
		t.Errorf("status %q transparency %q", first.Status, first.Transparency)
	}
	if len(first.Reminders) != 1 || first.Reminders[0].MinutesBefore != 15 || first.Reminders[0].RelatedEnd {
		t.Errorf("reminders %+v", first.Reminders)
	}
	second := got.Events[1]
	if !second.AllDay || second.Start != "2026-11-15" || second.End != "2026-11-16" || second.RRule != "" {
		t.Errorf("second: %+v", second)
	}
}

func TestDecodeEventLinkLenient(t *testing.T) {
	data := `[{"s":"bad","b":"not a date"},{"s":"ok","b":"2026-03-01T10:00","e":"2026-03-01T09:00","f":"DAILY","iv":"2","re":"count","ct":5,"al":[{"amount":1,"unit":"H","rel":"before-start"},{"amount":"x","unit":"M"}],"zz":true},{"s":"far","b":"9999-01-01"}]`
	got, err := decodeEventLink(eventLink(t, "example.org", data), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || got.Skipped != 2 {
		t.Fatalf("events %d skipped %d", len(got.Events), got.Skipped)
	}
	ev := got.Events[0]
	if ev.Start != "2026-03-01T10:00:00Z" || ev.End != "2026-03-01T11:00:00Z" {
		t.Errorf("an end before the start becomes an hour: %s to %s", ev.Start, ev.End)
	}
	if ev.RRule != "FREQ=DAILY;INTERVAL=2;COUNT=5" {
		t.Errorf("rule %q", ev.RRule)
	}
	if len(ev.Reminders) != 1 || ev.Reminders[0].MinutesBefore != 60 {
		t.Errorf("reminders %+v", ev.Reminders)
	}
}

func TestDecodeEventLinkRejects(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"no fragment": "https://arne.sh/e/",
		"wrong mark":  "https://arne.sh/e/#y" + strings.Repeat("A", 8),
		"bad chars":   "https://arne.sh/e/#zab+c/",
		"not http":    "file:///e/#zAAAA",
		"too long":    "https://arne.sh/e/#z" + strings.Repeat("A", eventLinkMaxInput),
		"not deflate": "https://arne.sh/e/#z" + base64.RawURLEncoding.EncodeToString([]byte("plain text")),
	}
	for name, link := range cases {
		if _, err := decodeEventLink(link, time.UTC); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := decodeEventLink(eventLink(t, "arne.sh", `[]`), time.UTC); err == nil {
		t.Error("an empty list: no error")
	}
	if _, err := decodeEventLink(eventLink(t, "arne.sh", `[`+strings.Repeat(" ", eventLinkMaxDecoded)+`]`), time.UTC); err == nil {
		t.Error("oversized data: no error")
	}
}
