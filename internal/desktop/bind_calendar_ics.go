package desktop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/storage"
)

// ICSImportResultDTO counts what an .ics import did.
type ICSImportResultDTO struct {
	// Imported are new events, Updated ones that replaced an event with the
	// same UID, Skipped ones that could not be read.
	Imported int `json:"imported"`
	Updated  int `json:"updated"`
	Skipped  int `json:"skipped"`
}

// exportFormatJSCal asks ExportCalendar for JSCalendar instead of iCalendar.
const exportFormatJSCal = "jscal"

// ImportICSData imports calendar text into a calendar: iCalendar, or
// JSCalendar going by its content. It is what every import ends in, whether
// the file was picked, dropped or opened with Pelton.
func (a *App) ImportICSData(calendarID int64, data string) (ICSImportResultDTO, error) {
	if err := a.ready(); err != nil {
		return ICSImportResultDTO{}, err
	}
	return a.importICS(a.ctx, calendarID, []byte(data))
}

// ExportCalendar asks where to save and writes every event of a calendar there
// as one file, iCalendar for format "ics" and JSCalendar for "jscal". It
// returns the path, empty when the dialog was cancelled.
func (a *App) ExportCalendar(calendarID int64, format string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	cal, err := a.store.GetCalendar(a.ctx, calendarID)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(cal.Name)
	if name == "" {
		name = "calendar"
	}
	var data []byte
	filter := fileFilter{DisplayName: "iCalendar (*.ics)", Pattern: "*.ics"}
	ext := ".ics"
	if format == exportFormatJSCal {
		data, err = a.exportJSCal(a.ctx, *cal)
		filter = fileFilter{DisplayName: "JSCalendar (*.json)", Pattern: "*.json"}
		ext = ".json"
	} else {
		data, err = a.exportICS(a.ctx, *cal)
	}
	if err != nil {
		return "", err
	}
	path, err := a.saveFileDialog(saveDialogOptions{
		Title:           "Export calendar",
		DefaultFilename: name + ext,
		Filters:         []fileFilter{filter},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// parseCalendarData reads iCalendar or JSCalendar text into a VCALENDAR and
// says how many JSCalendar entries were left out.
func parseCalendarData(data []byte) (*ical.Component, int, error) {
	if ical.IsJSCalendar(data) {
		cal, report, err := ical.FromJSCalendar(data, time.Local)
		return cal, len(report.Skipped), err
	}
	cal, err := ical.ParseCalendar(bytes.NewReader(data))
	return cal, 0, err
}

func (a *App) importICS(ctx context.Context, calendarID int64, data []byte) (ICSImportResultDTO, error) {
	var result ICSImportResultDTO
	cal, err := a.store.GetCalendar(ctx, calendarID)
	if err != nil {
		return result, err
	}
	if cal.ReadOnly {
		return result, errCalendarReadOnly
	}
	parsed, skipped, err := parseCalendarData(data)
	if err != nil {
		return result, err
	}
	result.Skipped = skipped
	parsed.Remove("METHOD")
	objs, err := ical.Split(parsed, time.Local)
	if err != nil {
		return result, err
	}
	for _, obj := range objs {
		if obj.UID() == "" || obj.First() == nil || obj.First().Start.IsZero() {
			result.Skipped++
			continue
		}
		obj.EnsureTimezones()
		row, err := a.store.GetCalendarEventByUID(ctx, cal.ID, obj.UID())
		switch {
		case errors.Is(err, storage.ErrCalendarEventNotFound):
			row = &storage.CalendarEvent{CalendarID: cal.ID}
			result.Imported++
		case err != nil:
			return result, err
		default:
			result.Updated++
		}
		fillEventRow(row, obj)
		row.Pending, row.Conflict, row.SyncError = "", false, ""
		if !cal.Local() {
			row.Pending = storage.PendingUpdate
			if row.Path == "" {
				row.Pending = storage.PendingCreate
			}
		}
		if err := a.store.SaveCalendarEvent(ctx, row); err != nil {
			return result, err
		}
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	if !cal.Local() {
		a.syncCalendarsInBackground()
	}
	return result, nil
}

// calendarObjects reads the stored events of a calendar, skipping deleted and
// unreadable ones.
func (a *App) calendarObjects(ctx context.Context, cal storage.Calendar) ([]*ical.Object, error) {
	rows, err := a.store.ListCalendarEvents(ctx, cal.ID)
	if err != nil {
		return nil, err
	}
	var objs []*ical.Object
	for _, row := range rows {
		if row.Pending == storage.PendingDelete {
			continue
		}
		obj, err := parseStoredEvent(row)
		if err != nil {
			a.log.Warn("skip unreadable event on export", "event", row.ID, "err", err)
			continue
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

// exportICS renders a calendar as one VCALENDAR, each time zone definition
// once.
func (a *App) exportICS(ctx context.Context, cal storage.Calendar) ([]byte, error) {
	objs, err := a.calendarObjects(ctx, cal)
	if err != nil {
		return nil, err
	}
	out := &ical.Component{Name: "VCALENDAR"}
	out.Set("VERSION", "2.0")
	out.Set("PRODID", ical.ProdID)
	if cal.Name != "" {
		out.SetText("X-WR-CALNAME", cal.Name)
	}
	zones := map[string]bool{}
	var events []*ical.Component
	for _, obj := range objs {
		for _, tz := range obj.Calendar.ChildrenNamed("VTIMEZONE") {
			if id := tz.Text("TZID"); !zones[id] {
				zones[id] = true
				out.Children = append(out.Children, tz)
			}
		}
		for _, ev := range obj.Events() {
			events = append(events, ev.Component)
		}
	}
	out.Children = append(out.Children, events...)
	return []byte(out.String()), nil
}

// exportJSCal renders a calendar as one JSCalendar Group.
func (a *App) exportJSCal(ctx context.Context, cal storage.Calendar) ([]byte, error) {
	objs, err := a.calendarObjects(ctx, cal)
	if err != nil {
		return nil, err
	}
	return ical.ToJSCalendar(objs, cal.Name)
}
