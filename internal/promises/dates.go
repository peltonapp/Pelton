package promises

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Due is when a promise is due: a day, or an instant when the text named a
// time of day. Day is midnight in the zone the date was resolved in.
type Due struct {
	Day   time.Time
	Timed bool
}

// IsZero reports whether there is no due date.
func (d Due) IsZero() bool { return d.Day.IsZero() }

// String is the due date as a task stores it: 2006-01-02 for a day, rfc3339
// for a time, "" for none.
func (d Due) String() string {
	switch {
	case d.Day.IsZero():
		return ""
	case d.Timed:
		return d.Day.Format(time.RFC3339)
	}
	return d.Day.Format("2006-01-02")
}

// Clock is what relative dates are read against: when the mail was written,
// in the user's zone, and the first day of the user's week.
type Clock struct {
	Ref       time.Time
	Location  *time.Location
	WeekStart time.Weekday
}

func (c Clock) ref() time.Time {
	loc := c.Location
	if loc == nil {
		loc = time.Local
	}
	return c.Ref.In(loc)
}

var (
	isoDate    = regexp.MustCompile(`\b(\d{4})-(\d{1,2})-(\d{1,2})\b`)
	dottedDate = regexp.MustCompile(`\b(\d{1,2})\.(\d{1,2})\.(\d{4}\b|\d{2}\b)?`)
	slashDate  = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})(?:/(\d{4}|\d{2}))?\b`)
)

type dateHit struct {
	start, end int
	resolve    func() (time.Time, bool)
	timed      bool
}

// findDue returns the first date the folded sentence names, resolved against
// the clock, with the time of day it names when it names one.
func findDue(folded string, l *language, c Clock) Due {
	ref := c.ref()
	today := midnight(ref)
	text := folded
	if l.exclusions != nil {
		text = l.exclusions.ReplaceAllStringFunc(text, func(m string) string { return strings.Repeat(" ", len(m)) })
	}
	var hits []dateHit
	for _, p := range l.phrases {
		for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
			group := func(name string) string {
				i := p.re.SubexpIndex(name)
				if i < 0 || m[2*i] < 0 {
					return ""
				}
				return strings.ToLower(text[m[2*i]:m[2*i+1]])
			}
			p := p
			hit := dateHit{start: m[0], end: m[1]}
			hit.resolve = func() (time.Time, bool) { return resolvePhrase(p, group, l, today, c.WeekStart) }
			if p.rel == "in" && l.units[group("unit")] == "hour" {
				hit.timed = true
				hit.resolve = func() (time.Time, bool) {
					n, ok := number(group("number"), l)
					return ref.Add(time.Duration(n) * time.Hour), ok
				}
			}
			hits = append(hits, hit)
		}
	}
	for _, m := range isoDate.FindAllStringSubmatchIndex(text, -1) {
		parts := subs(text, m)
		hits = append(hits, dateHit{start: m[0], end: m[1], resolve: func() (time.Time, bool) {
			return calendarDate(atoi(parts[1]), atoi(parts[2]), atoi(parts[3]), today)
		}})
	}
	for _, m := range dottedDate.FindAllStringSubmatchIndex(text, -1) {
		parts := subs(text, m)
		hits = append(hits, dateHit{start: m[0], end: m[1], resolve: func() (time.Time, bool) {
			return calendarDate(year(parts[3]), atoi(parts[2]), atoi(parts[1]), today)
		}})
	}
	for _, m := range slashDate.FindAllStringSubmatchIndex(text, -1) {
		parts := subs(text, m)
		hits = append(hits, dateHit{start: m[0], end: m[1], resolve: func() (time.Time, bool) {
			a, b := atoi(parts[1]), atoi(parts[2])
			day, month := a, b
			if (l.dateOrder == "mdy" && a <= 12) || b > 12 {
				day, month = b, a
			}
			return calendarDate(year(parts[3]), month, day, today)
		}})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].start != hits[j].start {
			return hits[i].start < hits[j].start
		}
		return hits[i].end-hits[i].start > hits[j].end-hits[j].start
	})
	hour, minute, hasTime := findTime(text, l)
	for _, h := range hits {
		day, ok := h.resolve()
		if !ok {
			continue
		}
		if h.timed {
			return Due{Day: day, Timed: true}
		}
		if hasTime {
			return Due{Day: time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location()), Timed: true}
		}
		return Due{Day: day}
	}
	if hasTime {
		at := time.Date(today.Year(), today.Month(), today.Day(), hour, minute, 0, 0, today.Location())
		if !at.After(ref) {
			at = at.AddDate(0, 0, 1)
		}
		return Due{Day: at, Timed: true}
	}
	return Due{}
}

func subs(text string, m []int) []string {
	out := make([]string, len(m)/2)
	for i := range out {
		if m[2*i] >= 0 {
			out[i] = text[m[2*i]:m[2*i+1]]
		}
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// year reads a written year, two digits meaning this century, or -1 for none.
func year(s string) int {
	switch len(s) {
	case 4:
		return atoi(s)
	case 2:
		return 2000 + atoi(s)
	}
	return -1
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// calendarDate builds a date, refusing one that does not exist. Without a
// year (y < 0) it is the next such date from today on.
func calendarDate(y, month, day int, today time.Time) (time.Time, bool) {
	guess := y
	if y < 0 {
		guess = today.Year()
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}
	d := time.Date(guess, time.Month(month), day, 0, 0, 0, 0, today.Location())
	if d.Month() != time.Month(month) {
		return time.Time{}, false
	}
	if y < 0 && d.Before(today) {
		d = d.AddDate(1, 0, 0)
	}
	return d, true
}

// number reads a count written as digits or as a word of the language.
func number(s string, l *language) (int, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		return n, n > 0 && n < 1000
	}
	n, ok := l.numbers[strings.Join(strings.Fields(s), " ")]
	return n, ok
}

// startOfWeek is the first day of the week d is in.
func startOfWeek(d time.Time, first time.Weekday) time.Time {
	back := (int(d.Weekday()) - int(first) + 7) % 7
	return d.AddDate(0, 0, -back)
}

// inWeek is the day of the week wd in the week that starts on start.
func inWeek(start time.Time, first, wd time.Weekday) time.Time {
	return start.AddDate(0, 0, (int(wd)-int(first)+7)%7)
}

func addWorkdays(d time.Time, n int) time.Time {
	for n > 0 {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			n--
		}
	}
	return d
}

func resolvePhrase(p phrase, group func(string) string, l *language, today time.Time, first time.Weekday) (time.Time, bool) {
	switch p.rel {
	case "today":
		return today, true
	case "day":
		return today.AddDate(0, 0, p.n), true
	case "weekend":
		friday := inWeek(startOfWeek(today, first).AddDate(0, 0, 7*p.n), first, time.Friday)
		if friday.Before(today) {
			friday = friday.AddDate(0, 0, 7)
		}
		return friday, true
	case "weekstart":
		d := startOfWeek(today, first).AddDate(0, 0, 7*p.n)
		for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			d = d.AddDate(0, 0, 1)
		}
		return d, true
	case "monthend":
		return time.Date(today.Year(), today.Month()+time.Month(p.n)+1, 0, 0, 0, 0, 0, today.Location()), true
	case "weekday":
		wd, ok := l.weekdays[group("weekday")]
		if !ok {
			return time.Time{}, false
		}
		ahead := (wd - int(today.Weekday()) + 7) % 7
		if ahead == 0 {
			ahead = 7
		}
		return today.AddDate(0, 0, ahead), true
	case "nextweekday":
		wd, ok := l.weekdays[group("weekday")]
		if !ok {
			return time.Time{}, false
		}
		return inWeek(startOfWeek(today, first).AddDate(0, 0, 7), first, time.Weekday(wd)), true
	case "in":
		n, ok := number(group("number"), l)
		if !ok {
			return time.Time{}, false
		}
		switch l.units[strings.Join(strings.Fields(group("unit")), " ")] {
		case "day":
			return today.AddDate(0, 0, n), true
		case "bday":
			return addWorkdays(today, n), true
		case "week":
			return today.AddDate(0, 0, 7*n), true
		case "month":
			return today.AddDate(0, n, 0), true
		}
	case "daymonth", "monthday":
		month, ok := l.months[group("month")]
		if !ok {
			return time.Time{}, false
		}
		y := -1
		if s := group("year"); s != "" {
			y = atoi(s)
		}
		return calendarDate(y, month, atoi(group("day")), today)
	}
	return time.Time{}, false
}

// findTime returns the first time of day the sentence names.
func findTime(text string, l *language) (hour, minute int, ok bool) {
	for _, t := range l.times {
		m := t.re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		if t.fixed != "" {
			h, mm, _ := strings.Cut(t.fixed, ":")
			return atoi(h), atoi(mm), true
		}
		hour = atoi(m[1])
		if len(m) > 2 && m[2] != "" {
			minute = atoi(m[2])
		}
		if t.ampm && len(m) > 3 {
			pm := strings.HasPrefix(strings.ToLower(m[3]), "p")
			switch {
			case pm && hour < 12:
				hour += 12
			case !pm && hour == 12:
				hour = 0
			}
		}
		if hour > 23 || minute > 59 {
			continue
		}
		return hour, minute, true
	}
	return 0, 0, false
}
