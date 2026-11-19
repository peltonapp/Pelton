package desktop

import (
	"bytes"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/ical"
)

// icsMaxBytes caps an .ics file read for import.
const icsMaxBytes = 20 << 20

var icsExtensions = map[string]bool{".ics": true, ".ical": true, ".icalendar": true, ".ifb": true}

var errNotICS = errors.New("not an iCalendar file")

// ICSFileDTO is an .ics file read for import: its text and what it holds, so
// the import can say how many events it is about to add before it does.
type ICSFileDTO struct {
	Name string `json:"name"`
	Data string `json:"data"`
	// Events counts the events that can be imported; Summaries names the first
	// few of them.
	Events    int      `json:"events"`
	Summaries []string `json:"summaries"`
	// Error is set instead of the rest when the file could not be read.
	Error string `json:"error"`
}

// OpenICSFile asks for an .ics file and reads it for import. A cancelled
// dialog returns an empty name.
func (a *App) OpenICSFile() (ICSFileDTO, error) {
	path, err := a.openFileDialog(openDialogOptions{
		Title:   "Import calendar",
		Filters: []fileFilter{
			{DisplayName: "Calendar files (*.ics, *.json)", Pattern: "*.ics;*.ical;*.ifb;*.icalendar;*.json;*.jscal"},
		},
	})
	if err != nil || path == "" {
		return ICSFileDTO{}, err
	}
	return readICSFile(path), nil
}

// ReadICSFiles reads dropped or opened .ics files for import. Files of any
// other kind come back with their error set.
func (a *App) ReadICSFiles(paths []string) []ICSFileDTO {
	out := make([]ICSFileDTO, 0, len(paths))
	for _, p := range paths {
		out = append(out, readICSFile(p))
	}
	return out
}

func readICSFile(path string) ICSFileDTO {
	file := ICSFileDTO{Name: filepath.Base(path), Summaries: []string{}}
	f, err := os.Open(path)
	if err != nil {
		file.Error = err.Error()
		return file
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, icsMaxBytes+1))
	if err != nil {
		file.Error = err.Error()
		return file
	}
	if len(data) > icsMaxBytes {
		file.Error = "the file is too large"
		return file
	}
	if !isICSPath(path) && !ical.IsJSCalendar(data) && !bytes.HasPrefix(bytes.TrimSpace(data), []byte("BEGIN:VCALENDAR")) {
		file.Error = errNotICS.Error()
		return file
	}
	count, summaries, err := inspectICS(data)
	if err != nil {
		file.Error = err.Error()
		return file
	}
	file.Data, file.Events, file.Summaries = string(data), count, summaries
	return file
}

func isICSPath(path string) bool {
	return icsExtensions[strings.ToLower(filepath.Ext(path))]
}

// inspectICS counts the events of an iCalendar text that an import would take,
// and names the first five.
func inspectICS(data []byte) (int, []string, error) {
	parsed, _, err := parseCalendarData(data)
	if err != nil {
		return 0, nil, err
	}
	objs, err := ical.Split(parsed, time.Local)
	if err != nil {
		return 0, nil, err
	}
	count := 0
	summaries := []string{}
	for _, obj := range objs {
		first := obj.First()
		if obj.UID() == "" || first == nil || first.Start.IsZero() {
			continue
		}
		count++
		if len(summaries) < 5 {
			summaries = append(summaries, first.Summary)
		}
	}
	return count, summaries, nil
}

// DecodeJSCalendar reads pasted JSCalendar text into event drafts with no
// calendar chosen, the way DecodeEventLink reads a link.
func (a *App) DecodeJSCalendar(text string) (EventLinkResultDTO, error) {
	out := EventLinkResultDTO{Events: []EventRequest{}}
	data := []byte(text)
	if len(data) > ical.JSCalMaxBytes || !ical.IsJSCalendar(data) {
		return out, ical.ErrNotJSCalendar
	}
	cal, report, err := ical.FromJSCalendar(data, time.Local)
	out.Skipped = len(report.Skipped)
	if err != nil {
		return out, err
	}
	objs, err := ical.Split(cal, time.Local)
	if err != nil {
		return out, err
	}
	for _, obj := range objs {
		if ev := obj.First(); ev != nil && !ev.Start.IsZero() {
			out.Events = append(out.Events, eventRequestOf(obj, ev))
		}
	}
	return out, nil
}

// eventRequestOf turns a parsed event into an editor draft with no calendar.
func eventRequestOf(obj *ical.Object, ev *ical.Event) EventRequest {
	start := obj.Zones.Instant(ev.Start)
	end := start.Add(ev.Duration)
	switch {
	case !ev.End.IsZero():
		end = obj.Zones.Instant(ev.End)
	case !ev.HasDuration && ev.Start.AllDay:
		end = start.AddDate(0, 0, 1)
	case !ev.HasDuration:
		end = start.Add(time.Hour)
	}
	req := EventRequest{
		Summary:      ev.Summary,
		Description:  ev.Description,
		Location:     ev.Location,
		URL:          ev.URL,
		Status:       ev.Status,
		Transparency: ev.Transparency,
		Categories:   append([]string{}, ev.Categories...),
		AllDay:       ev.Start.AllDay,
		Start:        formatEventTime(start, ev.Start.AllDay),
		End:          formatEventTime(end, ev.Start.AllDay),
		Reminders:    []ReminderDTO{},
		Attendees:    []AttendeeDTO{},
	}
	if ev.Start.AllDay {
		req.Start = ev.Start.Wall.Format(dateLayout)
		req.End = ev.Start.Wall.Add(end.Sub(start)).Format(dateLayout)
	}
	if ev.RRule != nil {
		req.RRule = ev.RRule.String()
	}
	for _, al := range ev.Alarms {
		if al.Absolute.IsZero() {
			req.Reminders = append(req.Reminders, ReminderDTO{MinutesBefore: int(-al.Trigger / time.Minute), RelatedEnd: al.RelatedEnd})
		}
	}
	if ev.Organizer != nil {
		req.Organizer = toAttendeeDTO(*ev.Organizer)
	}
	for _, at := range ev.Attendees {
		req.Attendees = append(req.Attendees, toAttendeeDTO(at))
	}
	return req
}

// firstICSArgs returns the .ics paths in a launch argument list. It is how
// opening an .ics file reaches the app on Linux and Windows.
func firstICSArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if u, err := url.Parse(a); err == nil && u.Scheme == "file" {
			a = u.Path
		}
		if !strings.HasPrefix(a, "-") && isICSPath(a) {
			out = append(out, a)
		}
	}
	return out
}

// icsOpenState holds .ics files the app was asked to open before the page was
// up, until the frontend picks them up with ConsumePendingICS.
type icsOpenState struct {
	mu      sync.Mutex
	pending []string
}

// deliverICS stashes .ics files the system asked the app to open and, once
// the app runs, tells the page to collect them. The page always collects
// through ConsumePendingICS, so a file that arrives while the page is still
// loading is neither lost nor imported twice.
func (a *App) deliverICS(paths []string) {
	if len(paths) == 0 {
		return
	}
	a.icsOpen.mu.Lock()
	a.icsOpen.pending = append(a.icsOpen.pending, paths...)
	a.icsOpen.mu.Unlock()
	if a.ctx != nil {
		a.emit(EventICSOpen, nil)
	}
}

// ConsumePendingICS returns the .ics files the app was launched to open, if
// any, and forgets them so a reload does not import them again.
func (a *App) ConsumePendingICS() []string {
	a.icsOpen.mu.Lock()
	defer a.icsOpen.mu.Unlock()
	out := a.icsOpen.pending
	a.icsOpen.pending = nil
	if out == nil {
		return []string{}
	}
	return out
}
