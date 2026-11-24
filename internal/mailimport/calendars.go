package mailimport

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// readCalendar returns a calendar as iCalendar text.
func readCalendar(cal CalendarSource) ([]byte, error) {
	switch cal.Format {
	case FormatICS:
		f, err := os.Open(cal.Location)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, maxTextFile))
	case FormatTBCalendar:
		return thunderbirdCalendar(cal.Location, cal.ID)
	}
	return nil, fmt.Errorf("mailimport: unknown calendar format %q", cal.Format)
}

func countEvents(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	return countLinePrefix(f, "BEGIN:VEVENT", "BEGIN:VTODO")
}

func countThunderbirdItems(path, calID string) int {
	db, err := openReadOnly(path)
	if err != nil {
		return 0
	}
	defer db.Close()
	var events, todos int
	_ = db.QueryRow(`SELECT count(*) FROM cal_events WHERE cal_id = ? AND recurrence_id IS NULL`, calID).Scan(&events)
	_ = db.QueryRow(`SELECT count(*) FROM cal_todos WHERE cal_id = ? AND recurrence_id IS NULL`, calID).Scan(&todos)
	return events + todos
}

// Thunderbird's item flags that matter here.
const (
	tbAllDay = 8
)

// tbItem is one row of Thunderbird's cal_events or cal_todos.
type tbItem struct {
	id, title, privacy, status string
	priority, flags            int64
	start, end                 sql.NullInt64
	startTZ, endTZ             string
	recurrenceID               sql.NullInt64
	recurrenceTZ               string
	completed                  sql.NullInt64
	percent                    sql.NullInt64
	stamp                      int64
}

// thunderbirdCalendar rebuilds one local Thunderbird calendar as iCalendar
// text from its rows in calendar-data/local.sqlite: events and tasks, their
// recurrence rules, attendees and the properties stored beside them.
func thunderbirdCalendar(path, calID string) ([]byte, error) {
	db, err := openReadOnly(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Pelton//Thunderbird import//EN\r\n")
	queries := []struct{ kind, query string }{
		{"VEVENT", `SELECT id, coalesce(title,''), coalesce(privacy,''), coalesce(ical_status,''), coalesce(priority,0), coalesce(flags,0),
       event_start, coalesce(event_start_tz,''), event_end, coalesce(event_end_tz,''), recurrence_id, coalesce(recurrence_id_tz,''),
       NULL, NULL, coalesce(event_stamp,0)
FROM cal_events WHERE cal_id = ?`},
		{"VTODO", `SELECT id, coalesce(title,''), coalesce(privacy,''), coalesce(ical_status,''), coalesce(priority,0), coalesce(flags,0),
       todo_entry, coalesce(todo_entry_tz,''), todo_due, coalesce(todo_due_tz,''), recurrence_id, coalesce(recurrence_id_tz,''),
       todo_completed, todo_complete, 0
FROM cal_todos WHERE cal_id = ?`},
	}
	for _, q := range queries {
		items, err := tbItems(db, q.query, calID)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if err := writeTBItem(db, &b, q.kind, calID, it); err != nil {
				return nil, err
			}
		}
	}
	b.WriteString("END:VCALENDAR\r\n")
	return []byte(b.String()), nil
}

func tbItems(db *sql.DB, query, calID string) ([]tbItem, error) {
	rows, err := db.Query(query, calID)
	if err != nil {
		return nil, fmt.Errorf("mailimport: read calendar: %w", err)
	}
	defer rows.Close()
	var out []tbItem
	for rows.Next() {
		var it tbItem
		if err := rows.Scan(&it.id, &it.title, &it.privacy, &it.status, &it.priority, &it.flags,
			&it.start, &it.startTZ, &it.end, &it.endTZ, &it.recurrenceID, &it.recurrenceTZ,
			&it.completed, &it.percent, &it.stamp); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func writeTBItem(db *sql.DB, b *strings.Builder, kind, calID string, it tbItem) error {
	allDay := it.flags&tbAllDay != 0
	b.WriteString("BEGIN:" + kind + "\r\n")
	b.WriteString("UID:" + icsText(it.id) + "\r\n")
	stamp := time.Now().UTC()
	if it.stamp > 0 {
		stamp = prTime(it.stamp).UTC()
	}
	b.WriteString("DTSTAMP:" + stamp.Format("20060102T150405Z") + "\r\n")
	if it.title != "" {
		b.WriteString("SUMMARY:" + icsText(it.title) + "\r\n")
	}
	startName, endName := "DTSTART", "DTEND"
	if kind == "VTODO" {
		endName = "DUE"
	}
	if it.start.Valid {
		b.WriteString(icsTime(startName, it.start.Int64, it.startTZ, allDay))
	}
	if it.end.Valid {
		b.WriteString(icsTime(endName, it.end.Int64, it.endTZ, allDay))
	}
	if it.recurrenceID.Valid {
		b.WriteString(icsTime("RECURRENCE-ID", it.recurrenceID.Int64, it.recurrenceTZ, allDay))
	}
	if it.completed.Valid {
		b.WriteString(icsTime("COMPLETED", it.completed.Int64, "UTC", false))
	}
	if it.percent.Valid && it.percent.Int64 > 0 {
		fmt.Fprintf(b, "PERCENT-COMPLETE:%d\r\n", it.percent.Int64)
	}
	if it.status != "" {
		b.WriteString("STATUS:" + icsText(it.status) + "\r\n")
	}
	if it.privacy != "" {
		b.WriteString("CLASS:" + icsText(it.privacy) + "\r\n")
	}
	if it.priority > 0 {
		fmt.Fprintf(b, "PRIORITY:%d\r\n", it.priority)
	}
	if !it.recurrenceID.Valid {
		lines, err := tbLines(db, `SELECT icalString FROM cal_recurrence WHERE item_id = ? AND cal_id = ?`, it.id, calID)
		if err != nil {
			return err
		}
		for _, l := range lines {
			b.WriteString(strings.TrimRight(l, "\r\n") + "\r\n")
		}
	}
	attendees, err := tbLines(db, `SELECT icalString FROM cal_attendees WHERE item_id = ? AND cal_id = ? AND recurrence_id IS ?`,
		it.id, calID, it.recurrenceID)
	if err != nil {
		return err
	}
	for _, l := range attendees {
		b.WriteString(strings.TrimRight(l, "\r\n") + "\r\n")
	}
	rows, err := db.Query(`SELECT key, value FROM cal_properties WHERE item_id = ? AND cal_id = ? AND recurrence_id IS ?`,
		it.id, calID, it.recurrenceID)
	if err != nil {
		return fmt.Errorf("mailimport: read calendar properties: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		if icsCopiedProperty[key] {
			b.WriteString(key + ":" + icsText(value) + "\r\n")
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	b.WriteString("END:" + kind + "\r\n")
	return nil
}

// icsCopiedProperty are the stored properties carried over as text.
var icsCopiedProperty = map[string]bool{
	"DESCRIPTION": true, "LOCATION": true, "CATEGORIES": true, "URL": true, "TRANSP": true,
}

func tbLines(db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s.Valid && s.String != "" {
			out = append(out, s.String)
		}
	}
	return out, rows.Err()
}

// prTime reads Thunderbird's times, microseconds since 1970.
func prTime(us int64) time.Time {
	return time.UnixMicro(us)
}

func icsTime(name string, us int64, tz string, allDay bool) string {
	t := prTime(us)
	switch {
	case allDay:
		return name + ";VALUE=DATE:" + t.UTC().Format("20060102") + "\r\n"
	case tz == "" || tz == "UTC":
		return name + ":" + t.UTC().Format("20060102T150405Z") + "\r\n"
	case tz == "floating":
		return name + ":" + t.UTC().Format("20060102T150405") + "\r\n"
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return name + ";TZID=" + tz + ":" + t.In(loc).Format("20060102T150405") + "\r\n"
	}
	return name + ":" + t.UTC().Format("20060102T150405Z") + "\r\n"
}

func icsText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`)
	return r.Replace(s)
}
