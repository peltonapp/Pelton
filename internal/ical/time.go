package ical

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	// Windows ships no zoneinfo database, and without one every TZID would
	// fall back to a fixed offset there.
	_ "time/tzdata"
)

// DateTime is a DATE or DATE-TIME value as written: the wall-clock fields
// plus how to read them. Wall carries the fields in time.UTC only as a label;
// Zones.Instant turns it into a real instant.
type DateTime struct {
	Wall time.Time
	// AllDay is a DATE value.
	AllDay bool
	// UTC is a DATE-TIME written with a trailing Z.
	UTC bool
	// TZID names the zone the wall clock is in. Empty with UTC false means a
	// floating time, which is read in the user's own zone.
	TZID string
}

// IsZero reports whether the value is unset.
func (d DateTime) IsZero() bool {
	return d.Wall.IsZero()
}

const (
	layoutDate     = "20060102"
	layoutDateTime = "20060102T150405"
)

// ParseDateTime reads a DATE or DATE-TIME. valueType is the VALUE parameter,
// which may be empty: a bare eight-digit value is a date either way.
func ParseDateTime(value, tzid, valueType string) (DateTime, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(valueType, "DATE") || len(value) == len(layoutDate) {
		t, err := time.ParseInLocation(layoutDate, value, time.UTC)
		if err != nil {
			return DateTime{}, fmt.Errorf("ical: date %q: %w", value, err)
		}
		return DateTime{Wall: t, AllDay: true}, nil
	}
	utc := strings.HasSuffix(value, "Z") || strings.HasSuffix(value, "z")
	if utc {
		value = value[:len(value)-1]
		tzid = ""
	}
	t, err := time.ParseInLocation(layoutDateTime, value, time.UTC)
	if err != nil {
		return DateTime{}, fmt.Errorf("ical: date-time %q: %w", value, err)
	}
	return DateTime{Wall: t, UTC: utc, TZID: tzid}, nil
}

// Value renders the value without its parameters.
func (d DateTime) Value() string {
	if d.AllDay {
		return d.Wall.Format(layoutDate)
	}
	if d.UTC {
		return d.Wall.Format(layoutDateTime) + "Z"
	}
	return d.Wall.Format(layoutDateTime)
}

// Prop builds a property carrying the value with its TZID or VALUE=DATE.
func (d DateTime) Prop(name string) *Prop {
	p := &Prop{Name: name, Value: d.Value()}
	switch {
	case d.AllDay:
		p.Params = []Param{{Name: "VALUE", Values: []string{"DATE"}}}
	case d.TZID != "" && !d.UTC:
		p.Params = []Param{{Name: "TZID", Values: []string{d.TZID}}}
	}
	return p
}

// propDateTime reads a date property, or the zero value when it is absent.
func propDateTime(p *Prop) (DateTime, error) {
	if p == nil {
		return DateTime{}, nil
	}
	return ParseDateTime(p.Value, p.Param("TZID"), p.Param("VALUE"))
}

// propDateTimes reads a list property such as EXDATE or RDATE, which can carry
// several comma separated values on one line and appear on several lines.
// RDATE periods (start/end) keep only their start.
func propDateTimes(props []*Prop) []DateTime {
	var out []DateTime
	for _, p := range props {
		for _, v := range strings.Split(p.Value, ",") {
			if start, _, ok := strings.Cut(v, "/"); ok {
				v = start
			}
			d, err := ParseDateTime(v, p.Param("TZID"), p.Param("VALUE"))
			if err == nil {
				out = append(out, d)
			}
		}
	}
	return out
}

// ParseDuration reads an RFC 5545 duration such as -PT15M or P1DT2H. Days and
// weeks are nominal: applied to a wall clock, a day is always 24 hours there.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") || len(s) < 2 {
		return 0, fmt.Errorf("ical: duration %q", s)
	}
	s = s[1:]
	var total time.Duration
	inTime := false
	num := ""
	for _, ch := range s {
		switch {
		case ch >= '0' && ch <= '9':
			num += string(ch)
		case ch == 'T':
			inTime = true
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, fmt.Errorf("ical: duration %q", s)
			}
			num = ""
			switch {
			case ch == 'W' && !inTime:
				total += time.Duration(n) * 7 * 24 * time.Hour
			case ch == 'D' && !inTime:
				total += time.Duration(n) * 24 * time.Hour
			case ch == 'H' && inTime:
				total += time.Duration(n) * time.Hour
			case ch == 'M' && inTime:
				total += time.Duration(n) * time.Minute
			case ch == 'S' && inTime:
				total += time.Duration(n) * time.Second
			default:
				return 0, fmt.Errorf("ical: duration %q", s)
			}
		}
	}
	if num != "" {
		return 0, fmt.Errorf("ical: duration %q", s)
	}
	if neg {
		total = -total
	}
	return total, nil
}

// FormatDuration renders a duration, using whole days or weeks when it is
// one, which is what other clients show as "1 day before".
func FormatDuration(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	if d == 0 {
		return "PT0S"
	}
	day := 24 * time.Hour
	if d%(7*day) == 0 {
		return fmt.Sprintf("%sP%dW", sign, d/(7*day))
	}
	var b strings.Builder
	b.WriteString(sign + "P")
	if days := d / day; days > 0 {
		fmt.Fprintf(&b, "%dD", days)
		d -= days * day
	}
	if d > 0 {
		b.WriteString("T")
		if h := d / time.Hour; h > 0 {
			fmt.Fprintf(&b, "%dH", h)
			d -= h * time.Hour
		}
		if m := d / time.Minute; m > 0 {
			fmt.Fprintf(&b, "%dM", m)
			d -= m * time.Minute
		}
		if d > 0 {
			fmt.Fprintf(&b, "%dS", d/time.Second)
		}
	}
	return b.String()
}

// Zones resolves the TZIDs one calendar object uses: an IANA name where the
// TZID is or contains one, a known Windows zone name, or else the VTIMEZONE
// definition the object carries.
type Zones struct {
	// Local is the user's zone, for floating times and all-day dates.
	Local *time.Location
	defs  map[string]*Component
	cache map[string]zone
}

// NewZones collects the VTIMEZONE definitions in cal. local is the zone
// floating times are read in; nil means time.Local.
func NewZones(cal *Component, local *time.Location) *Zones {
	if local == nil {
		local = time.Local
	}
	z := &Zones{Local: local, defs: map[string]*Component{}, cache: map[string]zone{}}
	if cal != nil {
		for _, tz := range cal.ChildrenNamed("VTIMEZONE") {
			if id := tz.Text("TZID"); id != "" {
				z.defs[id] = tz
			}
		}
	}
	return z
}

// Instant returns the moment a value stands for. A date is midnight in the
// user's zone.
func (z *Zones) Instant(d DateTime) time.Time {
	if d.IsZero() {
		return time.Time{}
	}
	return z.zoneOf(d).toUTC(d.Wall)
}

// WallIn returns the wall-clock fields of t in the zone like reads its value
// in, so a rule can be expanded on the clock the event was written in.
func (z *Zones) WallIn(t time.Time, like DateTime) time.Time {
	return z.zoneOf(like).toWall(t)
}

// Location returns the IANA location a TZID names, when it names one.
func (z *Zones) Location(tzid string) (*time.Location, bool) {
	return lookupLocation(tzid)
}

func (z *Zones) zoneOf(d DateTime) zone {
	switch {
	case d.UTC:
		return locZone{time.UTC}
	case d.AllDay || d.TZID == "":
		return locZone{z.Local}
	}
	if cached, ok := z.cache[d.TZID]; ok {
		return cached
	}
	var found zone = locZone{z.Local}
	if loc, ok := lookupLocation(d.TZID); ok {
		found = locZone{loc}
	} else if def, ok := z.defs[d.TZID]; ok {
		if vz := newVTimezone(def); vz != nil {
			found = vz
		}
	}
	z.cache[d.TZID] = found
	return found
}

type zone interface {
	toUTC(wall time.Time) time.Time
	toWall(t time.Time) time.Time
}

type locZone struct{ loc *time.Location }

func (l locZone) toUTC(w time.Time) time.Time {
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, l.loc).UTC()
}

func (l locZone) toWall(t time.Time) time.Time {
	in := t.In(l.loc)
	return time.Date(in.Year(), in.Month(), in.Day(), in.Hour(), in.Minute(), in.Second(), 0, time.UTC)
}

// lookupLocation resolves a TZID to an IANA zone. Some producers prefix the
// name with a vendor path ("/mozilla.org/20050126_1/Europe/Berlin"), and
// Outlook writes Windows names.
func lookupLocation(tzid string) (*time.Location, bool) {
	tzid = strings.Trim(strings.TrimSpace(tzid), `"`)
	if tzid == "" {
		return nil, false
	}
	if name, ok := windowsZones[tzid]; ok {
		tzid = name
	}
	if loc, err := time.LoadLocation(tzid); err == nil && tzid != "Local" {
		return loc, true
	}
	parts := strings.Split(strings.Trim(tzid, "/"), "/")
	for i := 1; i < len(parts); i++ {
		if loc, err := time.LoadLocation(strings.Join(parts[i:], "/")); err == nil {
			return loc, true
		}
	}
	return nil, false
}

// windowsZones maps the Windows zone names Outlook and Exchange write as a
// TZID to IANA names, for the zones most invitations arrive in.
var windowsZones = map[string]string{
	"Dateline Standard Time":          "Etc/GMT+12",
	"Hawaiian Standard Time":          "Pacific/Honolulu",
	"Alaskan Standard Time":           "America/Anchorage",
	"Pacific Standard Time":           "America/Los_Angeles",
	"Mountain Standard Time":          "America/Denver",
	"US Mountain Standard Time":       "America/Phoenix",
	"Central Standard Time":           "America/Chicago",
	"Eastern Standard Time":           "America/New_York",
	"Atlantic Standard Time":          "America/Halifax",
	"Newfoundland Standard Time":      "America/St_Johns",
	"E. South America Standard Time":  "America/Sao_Paulo",
	"Argentina Standard Time":         "America/Buenos_Aires",
	"UTC":                             "UTC",
	"GMT Standard Time":               "Europe/London",
	"Greenwich Standard Time":         "Atlantic/Reykjavik",
	"W. Europe Standard Time":         "Europe/Berlin",
	"Central Europe Standard Time":    "Europe/Budapest",
	"Central European Standard Time":  "Europe/Warsaw",
	"Romance Standard Time":           "Europe/Paris",
	"E. Europe Standard Time":         "Europe/Chisinau",
	"FLE Standard Time":               "Europe/Kiev",
	"GTB Standard Time":               "Europe/Bucharest",
	"Turkey Standard Time":            "Europe/Istanbul",
	"Israel Standard Time":            "Asia/Jerusalem",
	"Russian Standard Time":           "Europe/Moscow",
	"Arabian Standard Time":           "Asia/Dubai",
	"Arab Standard Time":              "Asia/Riyadh",
	"Egypt Standard Time":             "Africa/Cairo",
	"South Africa Standard Time":      "Africa/Johannesburg",
	"India Standard Time":             "Asia/Kolkata",
	"China Standard Time":             "Asia/Shanghai",
	"Singapore Standard Time":         "Asia/Singapore",
	"Tokyo Standard Time":             "Asia/Tokyo",
	"Korea Standard Time":             "Asia/Seoul",
	"AUS Eastern Standard Time":       "Australia/Sydney",
	"E. Australia Standard Time":      "Australia/Brisbane",
	"Cen. Australia Standard Time":    "Australia/Adelaide",
	"W. Australia Standard Time":      "Australia/Perth",
	"New Zealand Standard Time":       "Pacific/Auckland",
	"SE Asia Standard Time":           "Asia/Bangkok",
	"Pakistan Standard Time":          "Asia/Karachi",
	"Iran Standard Time":              "Asia/Tehran",
	"W. Central Africa Standard Time": "Africa/Lagos",
	"Mexico Standard Time":            "America/Mexico_City",
	"Central Standard Time (Mexico)":  "America/Mexico_City",
	"SA Pacific Standard Time":        "America/Bogota",
}

// vtimezone reads offsets from a VTIMEZONE's STANDARD and DAYLIGHT rules, for
// a TZID no IANA name stands behind.
type vtimezone struct {
	observances []observance
}

type observance struct {
	start      time.Time
	rule       *RRule
	rdates     []time.Time
	offsetFrom time.Duration
	offsetTo   time.Duration
}

func newVTimezone(def *Component) *vtimezone {
	vz := &vtimezone{}
	for _, child := range def.Children {
		if child.Name != "STANDARD" && child.Name != "DAYLIGHT" {
			continue
		}
		start, err := propDateTime(child.Prop("DTSTART"))
		if err != nil || start.IsZero() {
			continue
		}
		from, errFrom := parseOffset(child.Text("TZOFFSETFROM"))
		to, errTo := parseOffset(child.Text("TZOFFSETTO"))
		if errFrom != nil || errTo != nil {
			continue
		}
		obs := observance{start: start.Wall, offsetFrom: from, offsetTo: to}
		if p := child.Prop("RRULE"); p != nil {
			if rule, err := ParseRRule(p.Value); err == nil {
				obs.rule = rule
			}
		}
		for _, d := range propDateTimes(child.PropsNamed("RDATE")) {
			obs.rdates = append(obs.rdates, d.Wall)
		}
		vz.observances = append(vz.observances, obs)
	}
	if len(vz.observances) == 0 {
		return nil
	}
	return vz
}

// offsetAt returns the UTC offset in force at instant t: that of the latest
// observance onset at or before t.
// Before the first onset, the earliest observance's TZOFFSETFROM applies.
func (vz *vtimezone) offsetAt(t time.Time) time.Duration {
	var best time.Time
	first := vz.observances[0]
	for _, obs := range vz.observances[1:] {
		if obs.start.Before(first.start) {
			first = obs
		}
	}
	offset := first.offsetFrom
	for _, obs := range vz.observances {
		for _, onset := range obs.onsets(t) {
			utc := onset.Add(-obs.offsetFrom)
			if utc.After(t) {
				continue
			}
			if best.IsZero() || utc.After(best) {
				best = utc
				offset = obs.offsetTo
			}
		}
	}
	return offset
}

// onsets returns the observance's wall-clock onsets up to the end of t's
// year, which is all offsetAt needs to find the latest one before t.
func (obs observance) onsets(t time.Time) []time.Time {
	limit := date(t.Year()+1, 1, 1)
	var out []time.Time
	if obs.rule != nil {
		var until time.Time
		if obs.rule.HasUntil {
			until = obs.rule.Until.Wall.Add(obs.offsetFrom)
		}
		out = obs.rule.instances(obs.start, until, limit)
	} else if obs.start.Before(limit) {
		out = append(out, obs.start)
	}
	for _, r := range obs.rdates {
		if r.Before(limit) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

func (vz *vtimezone) toUTC(w time.Time) time.Time {
	guess := w.Add(-vz.offsetAt(w))
	return w.Add(-vz.offsetAt(guess))
}

func (vz *vtimezone) toWall(t time.Time) time.Time {
	return t.UTC().Add(vz.offsetAt(t.UTC()))
}

// parseOffset reads a UTC offset like +0200 or -053000.
func parseOffset(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) != 5 && len(s) != 7 {
		return 0, fmt.Errorf("ical: utc offset %q", s)
	}
	sign := time.Duration(1)
	switch s[0] {
	case '-':
		sign = -1
	case '+':
	default:
		return 0, fmt.Errorf("ical: utc offset %q", s)
	}
	h, err1 := strconv.Atoi(s[1:3])
	m, err2 := strconv.Atoi(s[3:5])
	sec := 0
	var err3 error
	if len(s) == 7 {
		sec, err3 = strconv.Atoi(s[5:7])
	}
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, fmt.Errorf("ical: utc offset %q", s)
	}
	return sign * (time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second), nil
}
