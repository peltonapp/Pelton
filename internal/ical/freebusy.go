package ical

import (
	"bytes"
	"slices"
	"strings"
	"time"
)

// The FBTYPE values a FREEBUSY period carries (RFC 5545 3.2.9).
const (
	FBTypeFree            = "FREE"
	FBTypeBusy            = "BUSY"
	FBTypeBusyTentative   = "BUSY-TENTATIVE"
	FBTypeBusyUnavailable = "BUSY-UNAVAILABLE"
)

// BusyPeriod is one period of a VFREEBUSY reply, as absolute instants.
type BusyPeriod struct {
	Start time.Time
	End   time.Time
	// Type is BUSY, BUSY-TENTATIVE or BUSY-UNAVAILABLE.
	Type string
}

// FreeBusyRequest builds the iTIP REQUEST (RFC 5546 3.3.2) that asks for the
// busy time of the attendees between start and end. organizer is the address
// asking, which a CalDAV server checks against the logged-in user's own.
func FreeBusyRequest(uid, organizer string, attendees []string, start, end, now time.Time) []byte {
	cal := &Component{Name: "VCALENDAR"}
	cal.Set("VERSION", "2.0")
	cal.Set("PRODID", ProdID)
	cal.Set("METHOD", MethodRequest)
	fb := &Component{Name: "VFREEBUSY"}
	fb.Set("UID", uid)
	fb.Set("DTSTAMP", utcStamp(now))
	fb.Set("DTSTART", utcStamp(start))
	fb.Set("DTEND", utcStamp(end))
	fb.Add(&Prop{Name: "ORGANIZER", Value: "mailto:" + organizer})
	for _, a := range attendees {
		fb.Add(&Prop{Name: "ATTENDEE", Value: "mailto:" + a})
	}
	cal.Children = append(cal.Children, fb)
	var b bytes.Buffer
	_ = Encode(&b, cal)
	return b.Bytes()
}

func utcStamp(t time.Time) string {
	return t.UTC().Format(layoutDateTime) + "Z"
}

// ParseFreeBusy reads the busy periods of every VFREEBUSY in a reply, sorted
// by start. FREE periods are dropped, and a period without an FBTYPE is BUSY.
// A period may end in a date-time or carry a duration.
func ParseFreeBusy(data []byte, local *time.Location) ([]BusyPeriod, error) {
	cal, err := ParseCalendar(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	zones := NewZones(cal, local)
	out := []BusyPeriod{}
	for _, fb := range cal.ChildrenNamed("VFREEBUSY") {
		for _, p := range fb.PropsNamed("FREEBUSY") {
			kind := strings.ToUpper(strings.TrimSpace(p.Param("FBTYPE")))
			switch kind {
			case "":
				kind = FBTypeBusy
			case FBTypeFree:
				continue
			case FBTypeBusy, FBTypeBusyTentative, FBTypeBusyUnavailable:
			default:
				// an x-name or iana token the reader does not know is busy, as
				// RFC 5545 asks.
				kind = FBTypeBusy
			}
			for _, period := range strings.Split(p.Value, ",") {
				from, to, ok := strings.Cut(strings.TrimSpace(period), "/")
				if !ok {
					continue
				}
				startDT, err := ParseDateTime(from, p.Param("TZID"), "")
				if err != nil {
					continue
				}
				start := zones.Instant(startDT)
				var end time.Time
				if strings.HasPrefix(to, "P") || strings.HasPrefix(to, "+P") || strings.HasPrefix(to, "-P") {
					d, err := ParseDuration(strings.TrimPrefix(to, "+"))
					if err != nil {
						continue
					}
					end = start.Add(d)
				} else {
					endDT, err := ParseDateTime(to, p.Param("TZID"), "")
					if err != nil {
						continue
					}
					end = zones.Instant(endDT)
				}
				if end.After(start) {
					out = append(out, BusyPeriod{Start: start, End: end, Type: kind})
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b BusyPeriod) int { return a.Start.Compare(b.Start) })
	return out, nil
}
