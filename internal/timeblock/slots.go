// Package timeblock finds free time for blocks of work and lays tasks out in
// it. It is pure: the busy times, the working hours and the clock come in as
// arguments, nothing is read from disk or the network, and the same input
// always gives the same answer.
//
// Days and working hours are read as wall-clock time in Rules.Loc, so a day
// that gains or loses an hour to daylight saving still runs from the same
// clock time to the same clock time.
package timeblock

import (
	"slices"
	"time"
)

// Span is a stretch of time, End exclusive.
type Span struct {
	Start time.Time
	End   time.Time
}

// Length is how long the span lasts, zero for an empty or inverted one.
func (s Span) Length() time.Duration {
	if !s.End.After(s.Start) {
		return 0
	}
	return s.End.Sub(s.Start)
}

// Rules says where free time can be found.
type Rules struct {
	// Loc is the zone days and working hours are read in.
	Loc *time.Location
	// WorkStart and WorkEnd are the working hours as minutes after midnight;
	// a WorkEnd of 1440 runs to midnight.
	WorkStart int
	WorkEnd   int
	// WorkDays marks the days of the week that have working hours, indexed
	// by time.Weekday.
	WorkDays [7]bool
	// MinSlot is the shortest free time worth using.
	MinSlot time.Duration
	// Buffer is kept free before and after everything already planned.
	Buffer time.Duration
	// Step aligns the start of free time to whole multiples of it past the
	// hour, read on the wall clock. Zero leaves starts as they fall.
	Step time.Duration
}

// dayStart is midnight of t's day in loc.
func dayStart(t time.Time, loc *time.Location) time.Time {
	w := t.In(loc)
	return time.Date(w.Year(), w.Month(), w.Day(), 0, 0, 0, 0, loc)
}

// nextDay is midnight of the day after day, which is 23 or 25 hours on a
// daylight saving change.
func nextDay(day time.Time, loc *time.Location) time.Time {
	w := day.In(loc)
	return time.Date(w.Year(), w.Month(), w.Day()+1, 0, 0, 0, 0, loc)
}

// workHours is the working time of day's day, empty on a day off.
func (r Rules) workHours(day time.Time) Span {
	w := day.In(r.Loc)
	if !r.WorkDays[w.Weekday()] || r.WorkEnd <= r.WorkStart {
		return Span{}
	}
	at := func(minutes int) time.Time {
		return time.Date(w.Year(), w.Month(), w.Day(), minutes/60, minutes%60, 0, 0, r.Loc)
	}
	return Span{Start: at(r.WorkStart), End: at(r.WorkEnd)}
}

// alignUp moves t forward to the next Step boundary on the wall clock.
func (r Rules) alignUp(t time.Time) time.Time {
	if r.Step <= 0 {
		return t
	}
	w := t.In(r.Loc)
	wall := time.Duration(w.Hour())*time.Hour + time.Duration(w.Minute())*time.Minute +
		time.Duration(w.Second())*time.Second + time.Duration(w.Nanosecond())
	rest := wall % r.Step
	if rest == 0 {
		return t
	}
	return t.Add(r.Step - rest)
}

// mergeBusy widens each busy span by the buffer and joins the ones that touch
// or overlap, sorted by start.
func mergeBusy(busy []Span, buffer time.Duration) []Span {
	list := make([]Span, 0, len(busy))
	for _, b := range busy {
		if b.End.Before(b.Start) {
			continue
		}
		list = append(list, Span{Start: b.Start.Add(-buffer), End: b.End.Add(buffer)})
	}
	slices.SortFunc(list, func(a, b Span) int { return a.Start.Compare(b.Start) })
	out := list[:0]
	for _, b := range list {
		if n := len(out); n > 0 && !b.Start.After(out[n-1].End) {
			if b.End.After(out[n-1].End) {
				out[n-1].End = b.End
			}
			continue
		}
		out = append(out, b)
	}
	return out
}

// FreeTime returns the free time in [from, to): working hours on working days,
// minus every busy span widened by the buffer, each piece starting on a Step
// boundary and at least MinSlot long. The pieces are in order and never span
// two days.
func FreeTime(r Rules, from, to time.Time, busy []Span) []Span {
	if r.Loc == nil {
		r.Loc = time.Local
	}
	merged := mergeBusy(busy, r.Buffer)
	var out []Span
	for day := dayStart(from, r.Loc); day.Before(to); day = nextDay(day, r.Loc) {
		work := r.workHours(day)
		if work.Length() == 0 {
			continue
		}
		if work.Start.Before(from) {
			work.Start = from
		}
		if work.End.After(to) {
			work.End = to
		}
		if work.Length() > 0 {
			out = append(out, r.carve(work, merged)...)
		}
	}
	return out
}

// carve cuts the busy spans out of one stretch of working time.
func (r Rules) carve(work Span, busy []Span) []Span {
	var out []Span
	cursor := work.Start
	keep := func(s Span) {
		s.Start = r.alignUp(s.Start)
		if s.Length() > 0 && s.Length() >= r.MinSlot {
			out = append(out, s)
		}
	}
	for _, b := range busy {
		if !b.End.After(cursor) {
			continue
		}
		if !b.Start.Before(work.End) {
			break
		}
		if b.Start.After(cursor) {
			keep(Span{Start: cursor, End: b.Start})
		}
		cursor = b.End
	}
	if cursor.Before(work.End) {
		keep(Span{Start: cursor, End: work.End})
	}
	return out
}

// Slot is a proposed time for a block.
type Slot struct {
	Span
	// AfterDue is true when the slot ends after the due date.
	AfterDue bool
}

// SlotQuery describes the block free time is looked for.
type SlotQuery struct {
	// Length is how long the block lasts.
	Length time.Duration
	// From is the earliest start, usually now.
	From time.Time
	// Days is how many days from From's day are looked at.
	Days int
	// Due is when the work has to be done by, zero for no due date.
	Due time.Time
	// Limit caps the slots returned.
	Limit int
	// PerDay caps the slots offered on one day, so later days get a turn.
	PerDay int
}

// FindSlots proposes times for a block of q.Length: the start of each piece of
// free time long enough to hold it, earliest first, at most q.PerDay a day and
// q.Limit in all. Slots that end after the due date are still offered, marked
// AfterDue, so there is something to pick when the due date cannot be met.
func FindSlots(r Rules, q SlotQuery, busy []Span) []Slot {
	if r.Loc == nil {
		r.Loc = time.Local
	}
	if q.Length <= 0 || q.Days <= 0 || q.Limit <= 0 {
		return nil
	}
	to := dayStart(q.From, r.Loc)
	for range q.Days {
		to = nextDay(to, r.Loc)
	}
	perDay := map[time.Time]int{}
	var out []Slot
	for _, free := range FreeTime(r, q.From, to, busy) {
		if free.Length() < q.Length {
			continue
		}
		day := dayStart(free.Start, r.Loc)
		if q.PerDay > 0 && perDay[day] >= q.PerDay {
			continue
		}
		perDay[day]++
		s := Slot{Span: Span{Start: free.Start, End: free.Start.Add(q.Length)}}
		s.AfterDue = !q.Due.IsZero() && s.End.After(q.Due)
		out = append(out, s)
		if len(out) >= q.Limit {
			break
		}
	}
	return out
}
