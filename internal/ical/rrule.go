package ical

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Frequency is an RRULE FREQ.
type Frequency string

// The recurrence frequencies RFC 5545 defines.
const (
	Secondly Frequency = "SECONDLY"
	Minutely Frequency = "MINUTELY"
	Hourly   Frequency = "HOURLY"
	Daily    Frequency = "DAILY"
	Weekly   Frequency = "WEEKLY"
	Monthly  Frequency = "MONTHLY"
	Yearly   Frequency = "YEARLY"
)

// WeekdayNum is one BYDAY entry: a weekday, with an ordinal like the 2 in 2MO
// or the -1 in -1FR. N is 0 for every such weekday.
type WeekdayNum struct {
	N   int
	Day time.Weekday
}

// RRule is a parsed recurrence rule.
type RRule struct {
	Freq     Frequency
	Interval int
	// Count is 0 when the rule has none.
	Count int
	// Until is set when HasUntil. It is either a UTC instant or, for a rule on
	// an all-day event, a date.
	Until    DateTime
	HasUntil bool

	BySecond   []int
	ByMinute   []int
	ByHour     []int
	ByDay      []WeekdayNum
	ByMonthDay []int
	ByYearDay  []int
	ByWeekNo   []int
	ByMonth    []int
	BySetPos   []int
	WeekStart  time.Weekday
	// extra keeps rule parts this package does not know, such as RSCALE, so a
	// rewrite does not drop them.
	extra []string
}

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

var weekdayNames = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// ParseRRule reads an RRULE value such as "FREQ=WEEKLY;BYDAY=MO,WE".
func ParseRRule(value string) (*RRule, error) {
	r := &RRule{Interval: 1, WeekStart: time.Monday}
	for _, part := range strings.Split(strings.TrimSpace(value), ";") {
		if part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("ical: rrule part %q has no value", part)
		}
		key = strings.ToUpper(key)
		var err error
		switch key {
		case "FREQ":
			r.Freq = Frequency(strings.ToUpper(val))
		case "INTERVAL":
			r.Interval, err = strconv.Atoi(val)
			if err == nil && r.Interval < 1 {
				err = fmt.Errorf("interval %d", r.Interval)
			}
		case "COUNT":
			r.Count, err = strconv.Atoi(val)
		case "UNTIL":
			r.Until, err = ParseDateTime(val, "", "")
			r.HasUntil = err == nil
		case "BYSECOND":
			r.BySecond, err = intList(val, 0, 60)
		case "BYMINUTE":
			r.ByMinute, err = intList(val, 0, 59)
		case "BYHOUR":
			r.ByHour, err = intList(val, 0, 23)
		case "BYMONTHDAY":
			r.ByMonthDay, err = signedList(val, 31)
		case "BYYEARDAY":
			r.ByYearDay, err = signedList(val, 366)
		case "BYWEEKNO":
			r.ByWeekNo, err = signedList(val, 53)
		case "BYMONTH":
			r.ByMonth, err = intList(val, 1, 12)
		case "BYSETPOS":
			r.BySetPos, err = signedList(val, 366)
		case "WKST":
			day, ok := weekdayCodes[strings.ToUpper(val)]
			if !ok {
				err = fmt.Errorf("weekday %q", val)
			}
			r.WeekStart = day
		case "BYDAY":
			for _, item := range strings.Split(val, ",") {
				item = strings.ToUpper(strings.TrimSpace(item))
				if len(item) < 2 {
					err = fmt.Errorf("weekday %q", item)
					break
				}
				day, ok := weekdayCodes[item[len(item)-2:]]
				if !ok {
					err = fmt.Errorf("weekday %q", item)
					break
				}
				n := 0
				if prefix := item[:len(item)-2]; prefix != "" {
					n, err = strconv.Atoi(prefix)
					if err != nil || n == 0 || n > 53 || n < -53 {
						err = fmt.Errorf("weekday ordinal %q", item)
						break
					}
				}
				r.ByDay = append(r.ByDay, WeekdayNum{N: n, Day: day})
			}
		default:
			r.extra = append(r.extra, part)
		}
		if err != nil {
			return nil, fmt.Errorf("ical: rrule %s: %w", key, err)
		}
	}
	switch r.Freq {
	case Secondly, Minutely, Hourly, Daily, Weekly, Monthly, Yearly:
	default:
		return nil, fmt.Errorf("ical: rrule has no valid FREQ in %q", value)
	}
	return r, nil
}

func intList(val string, lo, hi int) ([]int, error) {
	var out []int
	for _, s := range strings.Split(val, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < lo || n > hi {
			return nil, fmt.Errorf("value %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}

func signedList(val string, max int) ([]int, error) {
	var out []int
	for _, s := range strings.Split(val, ",") {
		n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "+"))
		if err != nil || n == 0 || n > max || n < -max {
			return nil, fmt.Errorf("value %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}

// String renders the rule as an RRULE value.
func (r *RRule) String() string {
	parts := []string{"FREQ=" + string(r.Freq)}
	if r.HasUntil {
		parts = append(parts, "UNTIL="+r.Until.Value())
	}
	if r.Count > 0 {
		parts = append(parts, "COUNT="+strconv.Itoa(r.Count))
	}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	addInts := func(name string, v []int) {
		if len(v) == 0 {
			return
		}
		s := make([]string, len(v))
		for i, n := range v {
			s[i] = strconv.Itoa(n)
		}
		parts = append(parts, name+"="+strings.Join(s, ","))
	}
	addInts("BYSECOND", r.BySecond)
	addInts("BYMINUTE", r.ByMinute)
	addInts("BYHOUR", r.ByHour)
	if len(r.ByDay) > 0 {
		s := make([]string, len(r.ByDay))
		for i, d := range r.ByDay {
			s[i] = weekdayNames[d.Day]
			if d.N != 0 {
				s[i] = strconv.Itoa(d.N) + s[i]
			}
		}
		parts = append(parts, "BYDAY="+strings.Join(s, ","))
	}
	addInts("BYMONTHDAY", r.ByMonthDay)
	addInts("BYYEARDAY", r.ByYearDay)
	addInts("BYWEEKNO", r.ByWeekNo)
	addInts("BYMONTH", r.ByMonth)
	addInts("BYSETPOS", r.BySetPos)
	if r.WeekStart != time.Monday {
		parts = append(parts, "WKST="+weekdayNames[r.WeekStart])
	}
	parts = append(parts, r.extra...)
	return strings.Join(parts, ";")
}

// maxEmptyPeriods stops a rule that can never match, such as the 30th of
// February every year, from looping forever.
const maxEmptyPeriods = 1000

// maxInstances caps one expansion. A minutely rule over years would otherwise
// eat the machine; nothing a calendar shows needs more.
const maxInstances = 50000

// instances returns the rule's wall-clock instances from dtstart up to, but not
// including, limit, honouring COUNT and the wall-clock until (zero for none).
// dtstart is always the first instance, as RFC 5545 requires, even when it
// does not match the rule itself.
func (r *RRule) instances(dtstart, until, limit time.Time) []time.Time {
	var out []time.Time
	if !dtstart.Before(limit) || (!until.IsZero() && dtstart.After(until)) {
		return nil
	}
	out = append(out, dtstart)
	emitted := 1
	if r.Count > 0 && emitted >= r.Count {
		return out
	}
	empty := 0
	for k := 0; ; k++ {
		periodStart, candidates := r.period(dtstart, k)
		if !periodStart.Before(limit) && periodStart.After(dtstart) {
			return out
		}
		if !until.IsZero() && periodStart.After(until) {
			return out
		}
		if len(candidates) == 0 {
			empty++
			if empty > maxEmptyPeriods {
				return out
			}
			continue
		}
		empty = 0
		for _, c := range candidates {
			if !c.After(dtstart) {
				continue
			}
			if !until.IsZero() && c.After(until) {
				return out
			}
			if !c.Before(limit) {
				return out
			}
			out = append(out, c)
			emitted++
			if (r.Count > 0 && emitted >= r.Count) || emitted >= maxInstances {
				return out
			}
		}
	}
}

// period returns the start of the k-th period after dtstart's and the sorted
// candidate instances in it, BYSETPOS applied.
func (r *RRule) period(dtstart time.Time, k int) (time.Time, []time.Time) {
	step := k * r.Interval
	var start time.Time
	var days []time.Time
	y, m, d := dtstart.Date()
	switch r.Freq {
	case Yearly:
		start = date(y+step, 1, 1)
		days = r.yearDays(y+step, dtstart)
	case Monthly:
		start = date(y, m+time.Month(step), 1)
		days = r.monthDays(start.Year(), start.Month(), dtstart)
	case Weekly:
		offset := (int(dtstart.Weekday()) - int(r.WeekStart) + 7) % 7
		start = date(y, m, d-offset+7*step)
		days = r.weekDays(start, dtstart)
	case Daily:
		start = date(y, m, d+step)
		if r.dayMatches(start) {
			days = []time.Time{start}
		}
	default:
		var unit time.Duration
		switch r.Freq {
		case Hourly:
			unit = time.Hour
		case Minutely:
			unit = time.Minute
		default:
			unit = time.Second
		}
		start = dtstart.Add(time.Duration(step) * unit)
		if r.dayMatches(date(start.Date())) && r.timeMatches(start) {
			return start, []time.Time{start}
		}
		return start, nil
	}
	var out []time.Time
	for _, day := range days {
		for _, tod := range r.times(dtstart) {
			out = append(out, day.Add(tod))
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return start, r.setPos(out)
}

// times returns the times of day the rule produces, as offsets from midnight.
func (r *RRule) times(dtstart time.Time) []time.Duration {
	hours := r.ByHour
	if len(hours) == 0 {
		hours = []int{dtstart.Hour()}
	}
	minutes := r.ByMinute
	if len(minutes) == 0 {
		minutes = []int{dtstart.Minute()}
	}
	seconds := r.BySecond
	if len(seconds) == 0 {
		seconds = []int{dtstart.Second()}
	}
	var out []time.Duration
	for _, h := range hours {
		for _, mi := range minutes {
			for _, s := range seconds {
				out = append(out, time.Duration(h)*time.Hour+time.Duration(mi)*time.Minute+time.Duration(s)*time.Second)
			}
		}
	}
	return out
}

func (r *RRule) timeMatches(t time.Time) bool {
	return (len(r.ByHour) == 0 || slices.Contains(r.ByHour, t.Hour())) &&
		(len(r.ByMinute) == 0 || slices.Contains(r.ByMinute, t.Minute())) &&
		(len(r.BySecond) == 0 || slices.Contains(r.BySecond, t.Second()))
}

func (r *RRule) setPos(sorted []time.Time) []time.Time {
	if len(r.BySetPos) == 0 || len(sorted) == 0 {
		return sorted
	}
	var out []time.Time
	for _, pos := range r.BySetPos {
		i := pos - 1
		if pos < 0 {
			i = len(sorted) + pos
		}
		if i >= 0 && i < len(sorted) && !slices.Contains(out, sorted[i]) {
			out = append(out, sorted[i])
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

func (r *RRule) yearDays(year int, dtstart time.Time) []time.Time {
	noDayRule := len(r.ByYearDay) == 0 && len(r.ByMonthDay) == 0 && len(r.ByDay) == 0 && len(r.ByWeekNo) == 0
	if noDayRule {
		months := r.ByMonth
		if len(months) == 0 {
			months = []int{int(dtstart.Month())}
		}
		var out []time.Time
		for _, mo := range months {
			if day, ok := validDate(year, time.Month(mo), dtstart.Day()); ok {
				out = append(out, day)
			}
		}
		return out
	}
	var out []time.Time
	for day := date(year, 1, 1); day.Year() == year; day = day.AddDate(0, 0, 1) {
		if len(r.ByMonth) > 0 && !slices.Contains(r.ByMonth, int(day.Month())) {
			continue
		}
		if len(r.ByWeekNo) > 0 && !r.weekNoMatches(day) {
			continue
		}
		if len(r.ByYearDay) > 0 && !yearDayMatches(r.ByYearDay, day) {
			continue
		}
		if len(r.ByMonthDay) > 0 && !monthDayMatches(r.ByMonthDay, day) {
			continue
		}
		if len(r.ByDay) > 0 {
			scope := "year"
			if len(r.ByMonth) > 0 {
				scope = "month"
			}
			if len(r.ByWeekNo) > 0 {
				scope = "week"
			}
			if !byDayMatches(r.ByDay, day, scope) {
				continue
			}
		}
		out = append(out, day)
	}
	return out
}

func (r *RRule) monthDays(year int, month time.Month, dtstart time.Time) []time.Time {
	if len(r.ByMonth) > 0 && !slices.Contains(r.ByMonth, int(month)) {
		return nil
	}
	if len(r.ByMonthDay) == 0 && len(r.ByDay) == 0 && len(r.ByYearDay) == 0 {
		if day, ok := validDate(year, month, dtstart.Day()); ok {
			return []time.Time{day}
		}
		return nil
	}
	var out []time.Time
	for day := date(year, month, 1); day.Month() == month; day = day.AddDate(0, 0, 1) {
		if len(r.ByMonthDay) > 0 && !monthDayMatches(r.ByMonthDay, day) {
			continue
		}
		if len(r.ByYearDay) > 0 && !yearDayMatches(r.ByYearDay, day) {
			continue
		}
		if len(r.ByDay) > 0 && !byDayMatches(r.ByDay, day, "month") {
			continue
		}
		out = append(out, day)
	}
	return out
}

func (r *RRule) weekDays(weekStart, dtstart time.Time) []time.Time {
	var out []time.Time
	for i := 0; i < 7; i++ {
		day := weekStart.AddDate(0, 0, i)
		if len(r.ByMonth) > 0 && !slices.Contains(r.ByMonth, int(day.Month())) {
			continue
		}
		if len(r.ByDay) == 0 {
			if day.Weekday() == dtstart.Weekday() {
				out = append(out, day)
			}
			continue
		}
		for _, wd := range r.ByDay {
			if wd.Day == day.Weekday() {
				out = append(out, day)
				break
			}
		}
	}
	return out
}

// dayMatches applies the day-level BY parts as limits, for DAILY and finer.
func (r *RRule) dayMatches(day time.Time) bool {
	if len(r.ByMonth) > 0 && !slices.Contains(r.ByMonth, int(day.Month())) {
		return false
	}
	if len(r.ByMonthDay) > 0 && !monthDayMatches(r.ByMonthDay, day) {
		return false
	}
	if len(r.ByYearDay) > 0 && !yearDayMatches(r.ByYearDay, day) {
		return false
	}
	if len(r.ByDay) > 0 {
		found := false
		for _, wd := range r.ByDay {
			if wd.Day == day.Weekday() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (r *RRule) weekNoMatches(day time.Time) bool {
	week, weeks := weekNumber(day, r.WeekStart)
	for _, n := range r.ByWeekNo {
		if n == week || (n < 0 && weeks+n+1 == week) {
			return true
		}
	}
	return false
}

// weekNumber returns the week of the year a day falls in under the given week
// start, where week 1 is the first with at least four days in the year, and
// how many weeks that year has.
func weekNumber(day time.Time, wkst time.Weekday) (int, int) {
	year := day.Year()
	first := week1Start(year, wkst)
	if day.Before(first) {
		year--
		first = week1Start(year, wkst)
	}
	next := week1Start(year+1, wkst)
	if !day.Before(next) {
		return 1, int(week1Start(year+2, wkst).Sub(next).Hours() / 24 / 7)
	}
	week := int(day.Sub(first).Hours()/24)/7 + 1
	return week, int(next.Sub(first).Hours() / 24 / 7)
}

func week1Start(year int, wkst time.Weekday) time.Time {
	jan1 := date(year, 1, 1)
	offset := (int(jan1.Weekday()) - int(wkst) + 7) % 7
	start := jan1.AddDate(0, 0, -offset)
	if offset >= 4 {
		start = start.AddDate(0, 0, 7)
	}
	return start
}

func monthDayMatches(list []int, day time.Time) bool {
	last := daysIn(day.Year(), day.Month())
	for _, n := range list {
		if n == day.Day() || (n < 0 && last+n+1 == day.Day()) {
			return true
		}
	}
	return false
}

func yearDayMatches(list []int, day time.Time) bool {
	total := 365
	if daysIn(day.Year(), time.February) == 29 {
		total = 366
	}
	yd := day.YearDay()
	for _, n := range list {
		if n == yd || (n < 0 && total+n+1 == yd) {
			return true
		}
	}
	return false
}

// byDayMatches checks a day against BYDAY, reading ordinals within the month
// or the year. Inside a week (with BYWEEKNO) ordinals carry no meaning.
func byDayMatches(list []WeekdayNum, day time.Time, scope string) bool {
	for _, wd := range list {
		if wd.Day != day.Weekday() {
			continue
		}
		if wd.N == 0 || scope == "week" {
			return true
		}
		var index, total int
		if scope == "month" {
			index = day.Day()
			total = daysIn(day.Year(), day.Month())
		} else {
			index = day.YearDay()
			total = date(day.Year(), 12, 31).YearDay()
		}
		nth := (index-1)/7 + 1
		fromEnd := -((total-index)/7 + 1)
		if wd.N == nth || wd.N == fromEnd {
			return true
		}
	}
	return false
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func validDate(y int, m time.Month, d int) (time.Time, bool) {
	t := date(y, m, d)
	return t, t.Month() == m && t.Day() == d
}

func daysIn(y int, m time.Month) int {
	return date(y, m+1, 0).Day()
}
