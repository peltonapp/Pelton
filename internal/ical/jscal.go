package ical

// JSCalendar (RFC 8984) conversion. An import turns a JSCalendar Event, or a
// Group of them, into a VCALENDAR so it runs through the same code as an .ics
// file; an export turns calendar objects into a JSCalendar Group. The mapping
// covers what the event model holds; anything else is left out.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// JSCalMaxBytes caps the JSCalendar text an import reads.
const JSCalMaxBytes = 20 << 20

const (
	jsLocalLayout = "2006-01-02T15:04:05"
	jsUTCLayout   = "2006-01-02T15:04:05Z"
	jsMaxDepth    = 8
)

// JSCalSkip names one entry an import left out and why.
type JSCalSkip struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// JSCalReport says what a JSCalendar import read.
type JSCalReport struct {
	Events  int
	Skipped []JSCalSkip
}

// ErrNotJSCalendar means the data is not a JSCalendar object.
var ErrNotJSCalendar = errors.New("ical: not a JSCalendar object")

// IsJSCalendar reports whether data looks like a JSCalendar Event, Group or
// Task, or a list of them, going by the @type of the content rather than any
// file name.
func IsJSCalendar(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') || len(trimmed) > JSCalMaxBytes {
		return false
	}
	var probe any
	if json.Unmarshal(trimmed, &probe) != nil {
		return false
	}
	switch v := probe.(type) {
	case map[string]any:
		return jsTypeKnown(v)
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok && jsTypeKnown(m) {
				return true
			}
		}
	}
	return false
}

func jsTypeKnown(m map[string]any) bool {
	switch jsString(m, "@type") {
	case "Event", "Group", "Task":
		return true
	}
	return false
}

// FromJSCalendar reads JSCalendar data into a VCALENDAR. Entries it cannot
// use (tasks, events without a start) are skipped and listed in the report.
func FromJSCalendar(data []byte, local *time.Location) (*Component, JSCalReport, error) {
	var report JSCalReport
	if len(data) > JSCalMaxBytes {
		return nil, report, errors.New("ical: the JSCalendar data is too large")
	}
	var root any
	if err := json.Unmarshal(bytes.TrimSpace(data), &root); err != nil {
		return nil, report, fmt.Errorf("ical: JSCalendar: %w", err)
	}
	var entries []map[string]any
	if !collectJSEntries(root, &entries, &report, 0) && len(entries) == 0 && len(report.Skipped) == 0 {
		return nil, report, ErrNotJSCalendar
	}
	cal := &Component{Name: "VCALENDAR"}
	cal.Set("VERSION", "2.0")
	cal.Set("PRODID", ProdID)
	for i, entry := range entries {
		comps, err := jsEventComponents(entry, local)
		if err != nil {
			report.Skipped = append(report.Skipped, JSCalSkip{Index: i, Reason: err.Error()})
			continue
		}
		cal.Children = append(cal.Children, comps...)
		report.Events++
	}
	if report.Events == 0 {
		return cal, report, ErrNoEvent
	}
	return cal, report, nil
}

// collectJSEntries flattens Groups into their Events and notes the entries
// that are not events. It reports false when the root is not JSCalendar.
func collectJSEntries(v any, out *[]map[string]any, report *JSCalReport, depth int) bool {
	if depth > jsMaxDepth {
		return false
	}
	switch node := v.(type) {
	case []any:
		found := false
		for _, item := range node {
			if collectJSEntries(item, out, report, depth+1) {
				found = true
			}
		}
		return found
	case map[string]any:
		switch jsString(node, "@type") {
		case "Event":
			*out = append(*out, node)
			return true
		case "Group":
			if list, ok := node["entries"].([]any); ok {
				for _, item := range list {
					collectJSEntries(item, out, report, depth+1)
				}
			}
			return true
		case "Task":
			report.Skipped = append(report.Skipped, JSCalSkip{Index: len(*out) + len(report.Skipped), Reason: "a task, not an event"})
			return true
		}
	}
	return false
}

// jsEventComponents converts one JSCalendar Event into its VEVENT and the
// VEVENTs of any changed occurrences.
func jsEventComponents(m map[string]any, local *time.Location) ([]*Component, error) {
	ev := &Event{
		UID:         jsString(m, "uid"),
		Summary:     jsString(m, "title"),
		Description: jsString(m, "description"),
	}
	if ev.UID == "" {
		return nil, errors.New("no uid")
	}
	allDay := jsBool(m, "showWithoutTime")
	start, err := jsStart(jsString(m, "start"), jsString(m, "timeZone"), allDay)
	if err != nil {
		return nil, err
	}
	ev.Start = start
	if d := jsString(m, "duration"); d != "" {
		if dur, err := ParseDuration(d); err == nil && dur >= 0 {
			ev.End = jsEnd(start, dur)
		}
	}
	if ev.End.IsZero() && allDay {
		ev.End = jsEnd(start, 24*time.Hour)
	}
	ev.Location = jsLocation(m)
	ev.Status = strings.ToUpper(jsString(m, "status"))
	switch jsString(m, "freeBusyStatus") {
	case "free":
		ev.Transparency = "TRANSPARENT"
	case "busy":
		ev.Transparency = "OPAQUE"
	}
	if seq, ok := m["sequence"].(float64); ok && seq > 0 {
		ev.Sequence = int(seq)
	}
	if kw, ok := m["keywords"].(map[string]any); ok {
		for k, v := range kw {
			if b, _ := v.(bool); b {
				ev.Categories = append(ev.Categories, k)
			}
		}
		sort.Strings(ev.Categories)
	}
	if rules, ok := m["recurrenceRules"].([]any); ok && len(rules) > 0 {
		if rm, ok := rules[0].(map[string]any); ok {
			ev.RRule = jsRRule(rm, start)
		}
	}
	ev.Alarms = jsAlarms(m)
	ev.Organizer, ev.Attendees = jsParticipants(m)

	var overrides []*Event
	if ov, ok := m["recurrenceOverrides"].(map[string]any); ok && ev.RRule != nil {
		keys := make([]string, 0, len(ov))
		for k := range ov {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			patch, ok := ov[k].(map[string]any)
			if !ok {
				continue
			}
			rid, err := jsLocal(k, start)
			if err != nil {
				continue
			}
			if jsBool(patch, "excluded") {
				ev.ExDates = append(ev.ExDates, rid)
				continue
			}
			if o := jsOverride(ev, patch, rid); o != nil {
				overrides = append(overrides, o)
			}
		}
	}

	comp := ev.Apply()
	jsExtraProps(comp, m)
	out := []*Component{comp}
	for _, o := range overrides {
		out = append(out, o.Apply())
	}
	return out, nil
}

// jsStart reads start and timeZone. A missing zone is a floating time; a UTC
// zone is written with a Z.
func jsStart(s, zone string, allDay bool) (DateTime, error) {
	t, err := time.Parse(jsLocalLayout, s)
	if err != nil {
		return DateTime{}, errors.New("no readable start")
	}
	if t.Year() < 1900 || t.Year() > 2200 {
		return DateTime{}, errors.New("start out of range")
	}
	if allDay {
		return DateTime{Wall: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), AllDay: true}, nil
	}
	switch zone {
	case "":
		return DateTime{Wall: t}, nil
	case "Etc/UTC", "UTC", "Etc/GMT":
		return DateTime{Wall: t, UTC: true}, nil
	}
	if _, ok := lookupLocation(zone); !ok {
		return DateTime{Wall: t}, nil
	}
	return DateTime{Wall: t, TZID: zone}, nil
}

// jsLocal reads a recurrence id written as a local date-time, in the zone of
// the start.
func jsLocal(s string, like DateTime) (DateTime, error) {
	t, err := time.Parse(jsLocalLayout, s)
	if err != nil {
		return DateTime{}, err
	}
	d := like
	d.Wall = t
	if like.AllDay {
		d.Wall = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return d, nil
}

func jsEnd(start DateTime, dur time.Duration) DateTime {
	end := start
	if start.AllDay {
		days := int(dur / (24 * time.Hour))
		if days < 1 {
			days = 1
		}
		end.Wall = start.Wall.AddDate(0, 0, days)
		return end
	}
	end.Wall = start.Wall.Add(dur)
	return end
}

// jsLocation takes the name of the first location, in id order.
func jsLocation(m map[string]any) string {
	locs, ok := m["locations"].(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(locs))
	for k := range locs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if l, ok := locs[k].(map[string]any); ok {
			for _, key := range []string{"name", "title"} {
				if name := jsString(l, key); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

var jsDays = map[string]time.Weekday{"su": time.Sunday, "mo": time.Monday, "tu": time.Tuesday, "we": time.Wednesday, "th": time.Thursday, "fr": time.Friday, "sa": time.Saturday}

// jsRRule converts a RecurrenceRule. A rule in another calendar scale than
// gregorian is not taken.
func jsRRule(m map[string]any, start DateTime) *RRule {
	if scale := jsString(m, "rscale"); scale != "" && scale != "gregorian" {
		return nil
	}
	r := &RRule{Freq: Frequency(strings.ToUpper(jsString(m, "frequency"))), Interval: 1, WeekStart: time.Monday}
	switch r.Freq {
	case Secondly, Minutely, Hourly, Daily, Weekly, Monthly, Yearly:
	default:
		return nil
	}
	if n, ok := m["interval"].(float64); ok && n >= 1 {
		r.Interval = int(n)
	}
	if n, ok := m["count"].(float64); ok && n >= 1 {
		r.Count = int(n)
	}
	if u := jsString(m, "until"); u != "" {
		if t, err := time.Parse(jsLocalLayout, u); err == nil {
			if start.AllDay {
				r.Until = DateTime{Wall: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), AllDay: true}
			} else {
				r.Until = DateTime{Wall: t.UTC(), UTC: true}
				if loc, ok := lookupLocation(start.TZID); ok && start.TZID != "" {
					in := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc)
					r.Until = DateTime{Wall: in.UTC(), UTC: true}
				}
			}
			r.HasUntil = true
		}
	}
	if d, ok := jsDays[jsString(m, "firstDayOfWeek")]; ok {
		r.WeekStart = d
	}
	if list, ok := m["byDay"].([]any); ok {
		for _, item := range list {
			nd, ok := item.(map[string]any)
			if !ok {
				continue
			}
			day, ok := jsDays[jsString(nd, "day")]
			if !ok {
				continue
			}
			n, _ := nd["nthOfPeriod"].(float64)
			r.ByDay = append(r.ByDay, WeekdayNum{N: int(n), Day: day})
		}
	}
	r.ByMonthDay = jsInts(m["byMonthDay"])
	r.ByYearDay = jsInts(m["byYearDay"])
	r.ByWeekNo = jsInts(m["byWeekNo"])
	r.ByHour = jsInts(m["byHour"])
	r.ByMinute = jsInts(m["byMinute"])
	r.BySecond = jsInts(m["bySecond"])
	r.BySetPos = jsInts(m["bySetPosition"])
	if list, ok := m["byMonth"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				if n, err := strconv.Atoi(strings.TrimSuffix(s, "L")); err == nil && n >= 1 && n <= 12 {
					r.ByMonth = append(r.ByMonth, n)
				}
			}
		}
	}
	return r
}

func jsAlarms(m map[string]any) []Alarm {
	alerts, ok := m["alerts"].(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(alerts))
	for k := range alerts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Alarm
	for _, k := range keys {
		a, ok := alerts[k].(map[string]any)
		if !ok {
			continue
		}
		trig, ok := a["trigger"].(map[string]any)
		if !ok {
			continue
		}
		alarm := Alarm{Action: "DISPLAY"}
		if jsString(a, "action") == "email" {
			alarm.Action = "EMAIL"
		}
		switch jsString(trig, "@type") {
		case "OffsetTrigger", "":
			d, err := ParseDuration(jsString(trig, "offset"))
			if err != nil {
				continue
			}
			alarm.Trigger = d
			alarm.RelatedEnd = jsString(trig, "relativeTo") == "end"
		case "AbsoluteTrigger":
			t, err := time.Parse(time.RFC3339, jsString(trig, "when"))
			if err != nil {
				continue
			}
			alarm.Absolute = DateTime{Wall: t.UTC(), UTC: true}
		default:
			continue
		}
		out = append(out, alarm)
	}
	return out
}

var jsPartStats = map[string]string{
	"needs-action": PartStatNeedsAction, "accepted": PartStatAccepted, "declined": PartStatDeclined,
	"tentative": PartStatTentative, "delegated": "DELEGATED",
}

func jsParticipants(m map[string]any) (*Attendee, []Attendee) {
	parts, ok := m["participants"].(map[string]any)
	if !ok {
		return nil, nil
	}
	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var organizer *Attendee
	var attendees []Attendee
	for _, k := range keys {
		p, ok := parts[k].(map[string]any)
		if !ok {
			continue
		}
		email := jsString(p, "email")
		if send, ok := p["sendTo"].(map[string]any); ok && email == "" {
			email = MailtoAddress(jsString(send, "imip"))
		}
		if email == "" {
			continue
		}
		roles, _ := p["roles"].(map[string]any)
		a := Attendee{Email: email, Name: jsString(p, "name"), PartStat: jsPartStats[jsString(p, "participationStatus")], RSVP: jsBool(p, "expectReply")}
		switch {
		case jsBool(roles, "chair"):
			a.Role = "CHAIR"
		case jsBool(roles, "optional"):
			a.Role = "OPT-PARTICIPANT"
		case jsBool(roles, "informational"):
			a.Role = "NON-PARTICIPANT"
		default:
			a.Role = "REQ-PARTICIPANT"
		}
		if jsBool(roles, "owner") && organizer == nil {
			org := Attendee{Email: email, Name: a.Name}
			organizer = &org
			if !jsBool(roles, "attendee") {
				continue
			}
		}
		attendees = append(attendees, a)
	}
	return organizer, attendees
}

// jsOverride builds a changed occurrence from a patch: title, description,
// start and duration are taken; other patched properties are not.
func jsOverride(master *Event, patch map[string]any, rid DateTime) *Event {
	o := &Event{
		UID:          master.UID,
		Summary:      master.Summary,
		Description:  master.Description,
		Location:     master.Location,
		Status:       master.Status,
		Transparency: master.Transparency,
		Start:        rid,
		RecurrenceID: rid,
		Organizer:    master.Organizer,
		Attendees:    master.Attendees,
	}
	length := master.End.Wall.Sub(master.Start.Wall)
	changed := false
	for _, key := range []string{"title", "description", "start", "duration"} {
		if _, ok := patch[key].(string); ok {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if v, ok := patch["title"].(string); ok {
		o.Summary = v
	}
	if v, ok := patch["description"].(string); ok {
		o.Description = v
	}
	if v, ok := patch["start"].(string); ok {
		if d, err := jsLocal(v, master.Start); err == nil {
			o.Start = d
		}
	}
	if v, ok := patch["duration"].(string); ok {
		if d, err := ParseDuration(v); err == nil && d >= 0 {
			length = d
		}
	}
	if !master.End.IsZero() || length > 0 {
		o.End = jsEnd(o.Start, length)
	}
	return o
}

var jsPrivacy = map[string]string{"public": "PUBLIC", "private": "PRIVATE", "secret": "CONFIDENTIAL"}

// jsExtraProps writes the properties the Event struct has no field for.
func jsExtraProps(c *Component, m map[string]any) {
	if p, ok := jsPrivacy[jsString(m, "privacy")]; ok {
		c.Set("CLASS", p)
	}
	if t, err := time.Parse(time.RFC3339, jsString(m, "created")); err == nil {
		c.Set("CREATED", t.UTC().Format(layoutDateTime)+"Z")
	}
	if t, err := time.Parse(time.RFC3339, jsString(m, "updated")); err == nil {
		c.Set("LAST-MODIFIED", t.UTC().Format(layoutDateTime)+"Z")
	}
}

// ToJSCalendar renders calendar objects as one JSCalendar Group.
func ToJSCalendar(objs []*Object, name string) ([]byte, error) {
	entries := make([]map[string]any, 0, len(objs))
	for _, obj := range objs {
		if e := jsFromObject(obj); e != nil {
			entries = append(entries, e)
		}
	}
	group := map[string]any{"@type": "Group", "entries": entries}
	if name != "" {
		group["title"] = name
	}
	return json.MarshalIndent(group, "", "  ")
}

func jsFromObject(obj *Object) map[string]any {
	ev := obj.Master
	if ev == nil {
		ev = obj.First()
	}
	if ev == nil || ev.Start.IsZero() {
		return nil
	}
	out := jsFromEvent(ev, obj.Zones)
	out["@type"] = "Event"
	out["uid"] = ev.UID
	if ev.RRule != nil {
		out["recurrenceRules"] = []any{jsRuleOut(ev.RRule, ev.Start, obj.Zones)}
	}
	overrides := map[string]any{}
	for _, ex := range ev.ExDates {
		overrides[jsLocalOut(ex, ev.Start, obj.Zones)] = map[string]any{"excluded": true}
	}
	if obj.Master != nil {
		for _, o := range obj.Overrides {
			patch := map[string]any{}
			full := jsFromEvent(o, obj.Zones)
			for _, key := range []string{"title", "description", "start", "duration"} {
				if v, ok := full[key]; ok && fmt.Sprint(v) != fmt.Sprint(out[key]) {
					patch[key] = v
				}
			}
			overrides[jsLocalOut(o.RecurrenceID, ev.Start, obj.Zones)] = patch
		}
	}
	if len(overrides) > 0 {
		out["recurrenceOverrides"] = overrides
	}
	return out
}

func jsFromEvent(ev *Event, zones *Zones) map[string]any {
	out := map[string]any{}
	if ev.Summary != "" {
		out["title"] = ev.Summary
	}
	if ev.Description != "" {
		out["description"] = ev.Description
	}
	out["start"] = ev.Start.Wall.Format(jsLocalLayout)
	switch {
	case ev.Start.AllDay:
		out["showWithoutTime"] = true
	case ev.Start.UTC:
		out["timeZone"] = "Etc/UTC"
	case ev.Start.TZID != "":
		out["timeZone"] = ev.Start.TZID
		if zones != nil {
			if loc, ok := zones.Location(ev.Start.TZID); ok {
				out["timeZone"] = loc.String()
			}
		}
	}
	var length time.Duration
	switch {
	case !ev.End.IsZero() && zones != nil && (ev.End.TZID != ev.Start.TZID || ev.End.UTC != ev.Start.UTC):
		length = zones.Instant(ev.End).Sub(zones.Instant(ev.Start))
	case !ev.End.IsZero():
		length = ev.End.Wall.Sub(ev.Start.Wall)
	case ev.HasDuration:
		length = ev.Duration
	}
	if length > 0 {
		out["duration"] = FormatDuration(length)
	}
	if ev.Location != "" {
		out["locations"] = map[string]any{"1": map[string]any{"@type": "Location", "name": ev.Location}}
	}
	if ev.Status != "" {
		out["status"] = strings.ToLower(ev.Status)
	}
	switch ev.Transparency {
	case "TRANSPARENT":
		out["freeBusyStatus"] = "free"
	case "OPAQUE":
		out["freeBusyStatus"] = "busy"
	}
	if ev.Sequence > 0 {
		out["sequence"] = ev.Sequence
	}
	if len(ev.Categories) > 0 {
		kw := map[string]any{}
		for _, c := range ev.Categories {
			kw[c] = true
		}
		out["keywords"] = kw
	}
	if c := ev.Component; c != nil {
		for k, v := range jsPrivacy {
			if strings.EqualFold(c.Text("CLASS"), v) {
				out["privacy"] = k
			}
		}
		if t, err := ParseDateTime(c.Text("CREATED"), "", ""); err == nil && t.UTC {
			out["created"] = t.Wall.Format(jsUTCLayout)
		}
		if t, err := ParseDateTime(c.Text("LAST-MODIFIED"), "", ""); err == nil && t.UTC {
			out["updated"] = t.Wall.Format(jsUTCLayout)
		}
	}
	if len(ev.Alarms) > 0 {
		alerts := map[string]any{}
		for i, a := range ev.Alarms {
			alert := map[string]any{"@type": "Alert"}
			if a.Action == "EMAIL" {
				alert["action"] = "email"
			}
			if !a.Absolute.IsZero() {
				alert["trigger"] = map[string]any{"@type": "AbsoluteTrigger", "when": a.Absolute.Wall.Format(jsUTCLayout)}
			} else {
				trig := map[string]any{"@type": "OffsetTrigger", "offset": FormatDuration(a.Trigger)}
				if a.RelatedEnd {
					trig["relativeTo"] = "end"
				}
				alert["trigger"] = trig
			}
			alerts[strconv.Itoa(i+1)] = alert
		}
		out["alerts"] = alerts
	}
	if ev.Organizer != nil || len(ev.Attendees) > 0 {
		parts := map[string]any{}
		n := 0
		add := func(p map[string]any) {
			n++
			parts[strconv.Itoa(n)] = p
		}
		orgEmail := ""
		if ev.Organizer != nil && ev.Organizer.Email != "" {
			orgEmail = strings.ToLower(ev.Organizer.Email)
		}
		orgListed := false
		for _, a := range ev.Attendees {
			roles := map[string]any{"attendee": true}
			switch a.Role {
			case "CHAIR":
				roles["chair"] = true
			case "OPT-PARTICIPANT":
				roles["optional"] = true
			case "NON-PARTICIPANT":
				roles["informational"] = true
			}
			if orgEmail != "" && strings.EqualFold(a.Email, orgEmail) {
				roles["owner"] = true
				orgListed = true
			}
			p := jsParticipantOut(a.Email, a.Name, roles)
			for k, v := range jsPartStats {
				if v == a.PartStat {
					p["participationStatus"] = k
				}
			}
			if a.RSVP {
				p["expectReply"] = true
			}
			add(p)
		}
		if orgEmail != "" && !orgListed {
			add(jsParticipantOut(ev.Organizer.Email, ev.Organizer.Name, map[string]any{"owner": true}))
		}
		out["participants"] = parts
	}
	return out
}

func jsParticipantOut(email, name string, roles map[string]any) map[string]any {
	p := map[string]any{"@type": "Participant", "email": email, "sendTo": map[string]any{"imip": "mailto:" + email}, "roles": roles}
	if name != "" {
		p["name"] = name
	}
	return p
}

func jsRuleOut(r *RRule, start DateTime, zones *Zones) map[string]any {
	out := map[string]any{"@type": "RecurrenceRule", "frequency": strings.ToLower(string(r.Freq))}
	if r.Interval > 1 {
		out["interval"] = r.Interval
	}
	if r.Count > 0 {
		out["count"] = r.Count
	}
	if r.HasUntil {
		out["until"] = jsLocalOut(r.Until, start, zones)
	}
	if r.WeekStart != time.Monday {
		out["firstDayOfWeek"] = weekdayNamesLower(r.WeekStart)
	}
	if len(r.ByDay) > 0 {
		days := make([]any, len(r.ByDay))
		for i, d := range r.ByDay {
			nd := map[string]any{"@type": "NDay", "day": weekdayNamesLower(d.Day)}
			if d.N != 0 {
				nd["nthOfPeriod"] = d.N
			}
			days[i] = nd
		}
		out["byDay"] = days
	}
	setInts := func(key string, v []int) {
		if len(v) > 0 {
			out[key] = v
		}
	}
	setInts("byMonthDay", r.ByMonthDay)
	setInts("byYearDay", r.ByYearDay)
	setInts("byWeekNo", r.ByWeekNo)
	setInts("byHour", r.ByHour)
	setInts("byMinute", r.ByMinute)
	setInts("bySecond", r.BySecond)
	setInts("bySetPosition", r.BySetPos)
	if len(r.ByMonth) > 0 {
		months := make([]string, len(r.ByMonth))
		for i, m := range r.ByMonth {
			months[i] = strconv.Itoa(m)
		}
		out["byMonth"] = months
	}
	return out
}

// jsLocalOut writes a recurrence id as a local date-time in the zone of the
// start, which is how JSCalendar keys its overrides.
func jsLocalOut(d DateTime, start DateTime, zones *Zones) string {
	if zones != nil && d.UTC != start.UTC && !start.AllDay {
		return zones.WallIn(zones.Instant(d), start).Format(jsLocalLayout)
	}
	return d.Wall.Format(jsLocalLayout)
}

func weekdayNamesLower(d time.Weekday) string {
	return strings.ToLower(weekdayNames[d])
}

func jsString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func jsBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func jsInts(v any) []int {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []int
	for _, item := range list {
		if n, ok := item.(float64); ok {
			out = append(out, int(n))
		}
	}
	return out
}
