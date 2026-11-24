package ical

// JSCalendar Tasks (RFC 8984 section 5.2) in and out, mapped onto the fields
// Todo carries.

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

var jsProgress = map[string]string{
	"needs-action": TodoNeedsAction,
	"in-process":   TodoInProcess,
	"completed":    TodoCompleted,
	"cancelled":    TodoCancelled,
	"failed":       TodoCancelled,
}

// jsTaskComponent converts one JSCalendar Task into a VTODO.
func jsTaskComponent(m map[string]any) (*Component, error) {
	t := &Todo{
		UID:         jsString(m, "uid"),
		Summary:     jsString(m, "title"),
		Description: jsString(m, "description"),
		Status:      jsProgress[jsString(m, "progress")],
	}
	if t.UID == "" {
		return nil, errors.New("no uid")
	}
	allDay := jsBool(m, "showWithoutTime")
	zone := jsString(m, "timeZone")
	if s := jsString(m, "due"); s != "" {
		due, err := jsStart(s, zone, allDay)
		if err != nil {
			return nil, err
		}
		t.Due = due
	}
	if s := jsString(m, "start"); s != "" {
		if start, err := jsStart(s, zone, allDay); err == nil {
			t.Start = start
		}
	}
	if p, ok := m["priority"].(float64); ok {
		t.Priority = clampInt(int(p), 0, 9)
	}
	if p, ok := m["percentComplete"].(float64); ok {
		t.Percent = clampInt(int(p), 0, 100)
	}
	if t.Status == TodoCompleted {
		at := time.Now()
		if u, err := time.Parse(time.RFC3339, jsString(m, "progressUpdated")); err == nil {
			at = u
		}
		t.Completed = DateTime{Wall: at.UTC().Truncate(time.Second), UTC: true}
	}
	if kw, ok := m["keywords"].(map[string]any); ok {
		for k, v := range kw {
			if b, _ := v.(bool); b {
				t.Categories = append(t.Categories, k)
			}
		}
		sort.Strings(t.Categories)
	}
	if rules, ok := m["recurrenceRules"].([]any); ok && len(rules) > 0 {
		anchor := t.Start
		if anchor.IsZero() {
			anchor = t.Due
		}
		if rm, ok := rules[0].(map[string]any); ok && !anchor.IsZero() {
			t.RRule = jsRRule(rm, anchor)
		}
	}
	if rel, ok := m["relatedTo"].(map[string]any); ok {
		keys := make([]string, 0, len(rel))
		for k := range rel {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, uid := range keys {
			r, _ := rel[uid].(map[string]any)
			kinds, _ := r["relation"].(map[string]any)
			switch {
			case jsBool(kinds, "parent") && t.Parent == "":
				t.Parent = uid
			case len(kinds) == 0 || jsBool(kinds, "sibling"):
				t.Related = append(t.Related, uid)
			}
		}
	}
	if links, ok := m["links"].(map[string]any); ok {
		keys := make([]string, 0, len(links))
		for k := range links {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if l, ok := links[k].(map[string]any); ok {
				if href := jsString(l, "href"); href != "" {
					t.Attachments = append(t.Attachments, href)
				}
			}
		}
	}
	t.Alarms = jsAlarms(m)
	comp := t.Apply()
	jsExtraProps(comp, m)
	return comp, nil
}

// jsFromTodo renders a task as a JSCalendar Task.
func jsFromTodo(o *TodoObject) map[string]any {
	t := o.Todo
	out := map[string]any{"@type": "Task", "uid": t.UID}
	if t.Summary != "" {
		out["title"] = t.Summary
	}
	if t.Description != "" {
		out["description"] = t.Description
	}
	anchor := t.anchor()
	if !t.Due.IsZero() {
		out["due"] = t.Due.Wall.Format(jsLocalLayout)
	}
	if !t.Start.IsZero() {
		out["start"] = t.Start.Wall.Format(jsLocalLayout)
	}
	switch {
	case anchor.IsZero():
	case anchor.AllDay:
		out["showWithoutTime"] = true
	case anchor.UTC:
		out["timeZone"] = "Etc/UTC"
	case anchor.TZID != "":
		out["timeZone"] = anchor.TZID
		if loc, ok := o.Zones.Location(anchor.TZID); ok {
			out["timeZone"] = loc.String()
		}
	}
	if t.Priority > 0 {
		out["priority"] = t.Priority
	}
	if t.Percent > 0 {
		out["percentComplete"] = t.Percent
	}
	for k, v := range jsProgress {
		if v == t.Status && k != "failed" {
			out["progress"] = k
		}
	}
	if !t.Completed.IsZero() {
		out["progressUpdated"] = t.Completed.Wall.Format(jsUTCLayout)
	}
	if len(t.Categories) > 0 {
		kw := map[string]any{}
		for _, c := range t.Categories {
			kw[c] = true
		}
		out["keywords"] = kw
	}
	if t.RRule != nil && !anchor.IsZero() {
		out["recurrenceRules"] = []any{jsRuleOut(t.RRule, anchor, o.Zones)}
	}
	if t.Parent != "" || len(t.Related) > 0 {
		rel := map[string]any{}
		if t.Parent != "" {
			rel[t.Parent] = map[string]any{"@type": "Relation", "relation": map[string]any{"parent": true}}
		}
		for _, uid := range t.Related {
			rel[uid] = map[string]any{"@type": "Relation", "relation": map[string]any{"sibling": true}}
		}
		out["relatedTo"] = rel
	}
	if len(t.Attachments) > 0 {
		links := map[string]any{}
		for i, href := range t.Attachments {
			link := map[string]any{"@type": "Link", "href": href}
			if strings.HasPrefix(strings.ToLower(href), "mid:") {
				link["contentType"] = "message/rfc822"
			}
			links[strconv.Itoa(i+1)] = link
		}
		out["links"] = links
	}
	if len(t.Alarms) > 0 {
		ev := jsFromEvent(&Event{Start: DateTime{Wall: time.Unix(0, 0).UTC()}, Alarms: t.Alarms}, o.Zones)
		out["alerts"] = ev["alerts"]
	}
	return out
}
