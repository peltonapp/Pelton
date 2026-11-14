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

// ImportICS asks for an .ics file and imports its events into a calendar. A
// cancelled dialog imports nothing. In a synced calendar the events are
// written to the server by a background sync.
func (a *App) ImportICS(calendarID int64) (ICSImportResultDTO, error) {
	if err := a.ready(); err != nil {
		return ICSImportResultDTO{}, err
	}
	path, err := a.openFileDialog(openDialogOptions{
		Title:   "Import calendar",
		Filters: []fileFilter{{DisplayName: "iCalendar (*.ics)", Pattern: "*.ics;*.ical;*.ifb;*.icalendar"}},
	})
	if err != nil || path == "" {
		return ICSImportResultDTO{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ICSImportResultDTO{}, err
	}
	return a.importICS(a.ctx, calendarID, data)
}

// ImportICSData imports iCalendar text into a calendar, for a file dropped on
// the window or an .ics attachment.
func (a *App) ImportICSData(calendarID int64, data string) (ICSImportResultDTO, error) {
	if err := a.ready(); err != nil {
		return ICSImportResultDTO{}, err
	}
	return a.importICS(a.ctx, calendarID, []byte(data))
}

// ExportICS asks where to save and writes every event of a calendar there as
// one .ics file. It returns the path, empty when the dialog was cancelled.
func (a *App) ExportICS(calendarID int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	cal, err := a.store.GetCalendar(a.ctx, calendarID)
	if err != nil {
		return "", err
	}
	data, err := a.exportICS(a.ctx, *cal)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(cal.Name)
	if name == "" {
		name = "calendar"
	}
	path, err := a.saveFileDialog(saveDialogOptions{
		Title:           "Export calendar",
		DefaultFilename: name + ".ics",
		Filters:         []fileFilter{{DisplayName: "iCalendar (*.ics)", Pattern: "*.ics"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
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
	parsed, err := ical.ParseCalendar(bytes.NewReader(data))
	if err != nil {
		return result, err
	}
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

// exportICS renders a calendar as one VCALENDAR, each time zone definition
// once.
func (a *App) exportICS(ctx context.Context, cal storage.Calendar) ([]byte, error) {
	rows, err := a.store.ListCalendarEvents(ctx, cal.ID)
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
	for _, row := range rows {
		if row.Pending == storage.PendingDelete {
			continue
		}
		obj, err := parseStoredEvent(row)
		if err != nil {
			a.log.Warn("skip unreadable event on export", "event", row.ID, "err", err)
			continue
		}
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
