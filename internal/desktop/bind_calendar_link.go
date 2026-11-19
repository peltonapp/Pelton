package desktop

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// An event link carries its events in the URL fragment: the letter z, then
// base64url without padding of a raw DEFLATE stream of a JSON array with short
// keys. The link is never fetched; everything needed is in the fragment.
const (
	eventLinkMaxInput   = 64 << 10
	eventLinkMaxDecoded = 1 << 20
	eventLinkMaxEvents  = 200
)

var (
	errEventLinkShape = errors.New("not an event link")
	errEventLinkEmpty = errors.New("the event link holds no readable events")
)

// EventLinkResultDTO is what an event link holds: the events it could read,
// as drafts with no calendar chosen, and how many entries it had to skip.
type EventLinkResultDTO struct {
	Events  []EventRequest `json:"events"`
	Skipped int            `json:"skipped"`
}

// DecodeEventLink reads the events out of a pasted event link. Nothing is
// fetched: the data is entirely in the fragment.
func (a *App) DecodeEventLink(link string) (EventLinkResultDTO, error) {
	return decodeEventLink(link, time.Local)
}

// eventLinkPayload returns the compressed payload of a link, or an error when
// the text does not have the shape of one.
func eventLinkPayload(link string) (string, error) {
	link = strings.TrimSpace(link)
	if link == "" || len(link) > eventLinkMaxInput {
		return "", errEventLinkShape
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errEventLinkShape
	}
	frag := u.EscapedFragment()
	if len(frag) < 2 || frag[0] != 'z' {
		return "", errEventLinkShape
	}
	data := frag[1:]
	for _, r := range data {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", errEventLinkShape
		}
	}
	return data, nil
}

func decodeEventLink(link string, loc *time.Location) (EventLinkResultDTO, error) {
	var out EventLinkResultDTO
	payload, err := eventLinkPayload(link)
	if err != nil {
		return out, err
	}
	compressed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return out, fmt.Errorf("event link: %w", err)
	}
	r := flate.NewReader(bytes.NewReader(compressed))
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, eventLinkMaxDecoded+1))
	if err != nil {
		return out, fmt.Errorf("event link: %w", err)
	}
	if len(raw) > eventLinkMaxDecoded {
		return out, errors.New("event link: the data is too large")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		var single json.RawMessage
		if json.Unmarshal(raw, &single) != nil || len(single) == 0 || single[0] != '{' {
			return out, fmt.Errorf("event link: %w", err)
		}
		entries = []json.RawMessage{single}
	}
	out.Events = []EventRequest{}
	for _, entry := range entries {
		if len(out.Events) >= eventLinkMaxEvents {
			out.Skipped++
			continue
		}
		var fields map[string]any
		if json.Unmarshal(entry, &fields) != nil {
			out.Skipped++
			continue
		}
		req, ok := linkEvent(fields, loc)
		if !ok {
			out.Skipped++
			continue
		}
		out.Events = append(out.Events, req)
	}
	if len(out.Events) == 0 {
		return out, errEventLinkEmpty
	}
	return out, nil
}

// linkEvent turns one entry of a link into an event draft, or reports false
// when it has no usable start.
func linkEvent(f map[string]any, loc *time.Location) (EventRequest, bool) {
	req := EventRequest{
		Summary:      linkString(f, "s"),
		Location:     linkString(f, "l"),
		Description:  linkString(f, "d"),
		Status:       linkEnum(f, "st", "TENTATIVE", "CONFIRMED", "CANCELLED"),
		Transparency: linkEnum(f, "tr", "OPAQUE", "TRANSPARENT"),
		Categories:   []string{},
		Reminders:    []ReminderDTO{},
		Attendees:    []AttendeeDTO{},
	}
	if u := linkString(f, "u"); strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		req.URL = u
	}
	utc := linkFlag(f, "U")
	req.AllDay = linkFlag(f, "A")

	start, startHasTime, ok := linkTime(linkString(f, "b"), utc, loc)
	if !ok {
		return req, false
	}
	end, _, endOK := linkTime(linkString(f, "e"), utc, loc)
	if !startHasTime {
		req.AllDay = true
	}
	if req.AllDay {
		first := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		last := first
		if endOK {
			last = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
		}
		if last.Before(first) {
			last = first
		}
		req.Start = first.Format(dateLayout)
		req.End = last.AddDate(0, 0, 1).Format(dateLayout)
	} else {
		if !endOK || !end.After(start) {
			end = start.Add(time.Hour)
		}
		req.Start = start.Format(time.RFC3339)
		req.End = end.Format(time.RFC3339)
	}
	req.RRule = linkRule(f)
	req.Reminders = linkAlarms(f["al"])
	return req, true
}

var linkLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"}

// linkTime reads a begin or end value: a date-time in local time (or UTC when
// the entry says so), or a bare date. hasTime is false for a bare date.
func linkTime(s string, utc bool, loc *time.Location) (t time.Time, hasTime bool, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 40 {
		return time.Time{}, false, false
	}
	zone := loc
	if utc {
		zone = time.UTC
	}
	if d, err := time.ParseInLocation(dateLayout, s, zone); err == nil {
		return d, false, plausibleYear(d)
	}
	if parsed, err := time.Parse(time.RFC3339, s); err == nil {
		return parsed.In(loc), true, plausibleYear(parsed)
	}
	for _, layout := range linkLayouts {
		parsed, err := time.ParseInLocation(layout, s, zone)
		if err != nil {
			continue
		}
		return parsed.In(loc), true, plausibleYear(parsed)
	}
	return time.Time{}, false, false
}

func plausibleYear(t time.Time) bool {
	return t.Year() >= 1900 && t.Year() <= 2200
}

var linkFreqs = map[string]bool{"DAILY": true, "WEEKLY": true, "MONTHLY": true, "YEARLY": true}

// linkRule builds the RRULE of an entry, or "" when it does not repeat.
func linkRule(f map[string]any) string {
	freq := strings.ToUpper(linkString(f, "f"))
	if !linkFreqs[freq] {
		return ""
	}
	parts := []string{"FREQ=" + freq}
	if iv, ok := linkNumber(f, "iv"); ok && iv > 1 && iv < 1000 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(int(iv)))
	}
	switch strings.ToLower(linkString(f, "re")) {
	case "count", "after":
		if ct, ok := linkNumber(f, "ct"); ok && ct >= 1 && ct < 10000 {
			parts = append(parts, "COUNT="+strconv.Itoa(int(ct)))
		}
	case "until", "on", "date":
		for _, key := range []string{"ud", "un", "until"} {
			if d, err := time.Parse(dateLayout, firstN(linkString(f, key), len(dateLayout))); err == nil && plausibleYear(d) {
				parts = append(parts, "UNTIL="+d.Format("20060102")+"T235959Z")
				break
			}
		}
	}
	return strings.Join(parts, ";")
}

var linkUnits = map[string]int{"M": 1, "H": 60, "D": 24 * 60, "W": 7 * 24 * 60}

// linkAlarms reads the reminders of an entry, skipping any it cannot place.
func linkAlarms(v any) []ReminderDTO {
	out := []ReminderDTO{}
	list, ok := v.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok || len(out) >= 10 {
			continue
		}
		amount, ok := linkNumber(m, "amount")
		unit := linkUnits[strings.ToUpper(linkString(m, "unit"))]
		if !ok || unit == 0 || amount < 0 || amount*float64(unit) > 366*24*60 {
			continue
		}
		minutes := int(amount) * unit
		rel := strings.ToLower(linkString(m, "rel"))
		r := ReminderDTO{MinutesBefore: minutes}
		if strings.HasPrefix(rel, "after") {
			r.MinutesBefore = -minutes
		}
		r.RelatedEnd = strings.HasSuffix(rel, "end")
		out = append(out, r)
	}
	return out
}

func linkString(f map[string]any, key string) string {
	switch v := f[key].(type) {
	case string:
		return strings.TrimSpace(firstN(v, 4096))
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func linkNumber(f map[string]any, key string) (float64, bool) {
	switch v := f[key].(type) {
	case float64:
		return math.Trunc(v), !math.IsNaN(v) && !math.IsInf(v, 0)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return float64(n), err == nil
	}
	return 0, false
}

func linkFlag(f map[string]any, key string) bool {
	switch v := f[key].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	}
	return false
}

func linkEnum(f map[string]any, key string, allowed ...string) string {
	v := strings.ToUpper(linkString(f, key))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
