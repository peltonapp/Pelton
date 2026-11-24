package ical

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ProdID is the PRODID Pelton writes on a calendar it creates.
const ProdID = "-//Pelton//Pelton Calendar//EN"

// Object is one calendar object resource: the VEVENTs sharing a UID (the
// master and the overrides of single occurrences) inside their VCALENDAR,
// with the VTIMEZONE definitions they need.
type Object struct {
	Calendar *Component
	// Master is nil for an object holding only overrides, which is what an
	// invitation to one occurrence of someone else's series looks like.
	Master    *Event
	Overrides []*Event
	Zones     *Zones
}

// ErrNoEvent means the calendar data holds no VEVENT.
var ErrNoEvent = errors.New("ical: no event in the calendar data")

// ParseObject reads one calendar object resource. local is the zone floating
// times are read in; nil means time.Local.
func ParseObject(data []byte, local *time.Location) (*Object, error) {
	cal, err := ParseCalendar(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	objs, err := Split(cal, local)
	if err != nil {
		return nil, err
	}
	return objs[0], nil
}

// Split groups the VEVENTs of a calendar by UID into objects, each carrying
// the calendar's own properties and its time zone definitions. It is what an
// import or an invitation goes through, since one file can hold many events.
func Split(cal *Component, local *time.Location) ([]*Object, error) {
	var order []string
	groups := map[string][]*Component{}
	for _, child := range cal.ChildrenNamed("VEVENT") {
		uid := child.Text("UID")
		if _, seen := groups[uid]; !seen {
			order = append(order, uid)
		}
		groups[uid] = append(groups[uid], child)
	}
	if len(order) == 0 {
		return nil, ErrNoEvent
	}
	var out []*Object
	for _, uid := range order {
		wrapper := &Component{Name: "VCALENDAR"}
		for _, p := range cal.Props {
			wrapper.Props = append(wrapper.Props, &Prop{Name: p.Name, Params: p.Params, Value: p.Value})
		}
		for _, tz := range cal.ChildrenNamed("VTIMEZONE") {
			wrapper.Children = append(wrapper.Children, tz.Clone())
		}
		obj := &Object{Calendar: wrapper, Zones: NewZones(wrapper, local)}
		for _, comp := range groups[uid] {
			ev, err := ParseEvent(comp.Clone())
			if err != nil {
				return nil, err
			}
			if ev.RecurrenceID.IsZero() && obj.Master == nil {
				obj.Master = ev
			} else {
				obj.Overrides = append(obj.Overrides, ev)
			}
		}
		obj.dropUnusedZones()
		out = append(out, obj)
	}
	return out, nil
}

// NewObject wraps a new event in a VCALENDAR of its own.
func NewObject(ev *Event, local *time.Location) *Object {
	cal := &Component{Name: "VCALENDAR"}
	cal.Set("VERSION", "2.0")
	cal.Set("PRODID", ProdID)
	ev.Apply()
	obj := &Object{Calendar: cal, Master: ev, Zones: NewZones(cal, local)}
	obj.EnsureTimezones()
	return obj
}

// UID is the UID every event in the object shares.
func (o *Object) UID() string {
	if o.Master != nil {
		return o.Master.UID
	}
	if len(o.Overrides) > 0 {
		return o.Overrides[0].UID
	}
	return ""
}

// First returns the master, or the first override for an object without one.
func (o *Object) First() *Event {
	if o.Master != nil {
		return o.Master
	}
	if len(o.Overrides) > 0 {
		return o.Overrides[0]
	}
	return nil
}

// Events returns the master first, then the overrides.
func (o *Object) Events() []*Event {
	var out []*Event
	if o.Master != nil {
		out = append(out, o.Master)
	}
	return append(out, o.Overrides...)
}

// Encode renders the object. The VEVENT children are rebuilt from Master and
// Overrides, so an override added or removed through the methods below lands
// in the output; every other child of the VCALENDAR stays.
func (o *Object) Encode() []byte {
	var kept []*Component
	for _, child := range o.Calendar.Children {
		if child.Name != "VEVENT" {
			kept = append(kept, child)
		}
	}
	for _, ev := range o.Events() {
		kept = append(kept, ev.Component)
	}
	o.Calendar.Children = kept
	var b bytes.Buffer
	_ = Encode(&b, o.Calendar)
	return b.Bytes()
}

// SetMethod sets or clears the iTIP METHOD. A stored object carries none; an
// invitation or a reply does.
func (o *Object) SetMethod(method string) {
	o.Calendar.Set("METHOD", strings.ToUpper(method))
}

// EnsureTimezones adds a VTIMEZONE for every TZID the events use that has no
// definition yet, built from the zone database. RFC 5545 requires one, and
// stricter servers refuse an object without it.
func (o *Object) EnsureTimezones() {
	have := map[string]bool{}
	for _, tz := range o.Calendar.ChildrenNamed("VTIMEZONE") {
		have[tz.Text("TZID")] = true
	}
	var added []*Component
	for _, id := range o.tzids() {
		if have[id] {
			continue
		}
		loc, ok := lookupLocation(id)
		if !ok {
			continue
		}
		year := time.Now().Year()
		if first := o.First(); first != nil && !first.Start.IsZero() {
			year = first.Start.Wall.Year()
		}
		added = append(added, VTimezone(id, loc, year))
		have[id] = true
	}
	o.Calendar.Children = append(added, o.Calendar.Children...)
	o.Zones = NewZones(o.Calendar, o.Zones.Local)
}

// tzids lists the TZIDs the events reference.
func (o *Object) tzids() []string {
	var out []string
	for _, ev := range o.Events() {
		for _, p := range ev.Component.Props {
			if id := p.Param("TZID"); id != "" && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

// dropUnusedZones removes VTIMEZONE definitions none of the events use, which
// is what splitting a file holding many events leaves behind.
func (o *Object) dropUnusedZones() {
	used := o.tzids()
	var kept []*Component
	for _, child := range o.Calendar.Children {
		if child.Name == "VTIMEZONE" && !slices.Contains(used, child.Text("TZID")) {
			continue
		}
		kept = append(kept, child)
	}
	o.Calendar.Children = kept
}

// Occurrence is one instance of an event in a time window.
type Occurrence struct {
	// Event supplies the fields: the master, or the override replacing this
	// occurrence.
	Event *Event
	Start time.Time
	End   time.Time
	// RecurrenceID is the original start of a recurring occurrence, zero for
	// an event that does not repeat. It is what names the occurrence when it
	// is edited or deleted on its own.
	RecurrenceID time.Time
	AllDay       bool
	Override     bool
}

// Expand returns the occurrences that overlap [from, to), sorted by start. An
// EXDATE removes an occurrence; an override replaces it, wherever the
// override has moved it to.
func (o *Object) Expand(from, to time.Time) []Occurrence {
	var out []Occurrence
	overridden := map[int64]bool{}
	excluded := o.excluded()
	for _, ov := range o.Overrides {
		rid := o.Zones.Instant(ov.RecurrenceID)
		if !ov.RecurrenceID.IsZero() {
			overridden[rid.Unix()] = true
			if excluded[rid.Unix()] {
				continue
			}
		}
		start := o.Zones.Instant(ov.Start)
		end := o.Zones.Instant(DateTime{Wall: ov.endWall(o.Zones, ov.Start.Wall), AllDay: ov.Start.AllDay, UTC: ov.Start.UTC, TZID: ov.Start.TZID})
		if overlaps(start, end, from, to) {
			out = append(out, Occurrence{Event: ov, Start: start, End: end, RecurrenceID: rid, AllDay: ov.Start.AllDay, Override: true})
		}
	}
	if m := o.Master; m != nil && !m.Start.IsZero() {
		limit := o.Zones.WallIn(to, m.Start).AddDate(0, 0, 2)
		for _, wall := range o.walls(limit) {
			instant := o.Zones.Instant(withWall(m.Start, wall))
			if excluded[instant.Unix()] || overridden[instant.Unix()] {
				continue
			}
			end := o.Zones.Instant(withWall(m.Start, m.endWall(o.Zones, wall)))
			if !overlaps(instant, end, from, to) {
				continue
			}
			occ := Occurrence{Event: m, Start: instant, End: end, AllDay: m.Start.AllDay}
			if m.IsRecurring() {
				occ.RecurrenceID = instant
			}
			out = append(out, occ)
		}
	}
	slices.SortStableFunc(out, func(a, b Occurrence) int { return a.Start.Compare(b.Start) })
	return out
}

// farFuture bounds the expansion that finds where a finite series ends.
var farFuture = time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

// Bounds returns the span the object's occurrences cover, for indexing. open
// is true for a series with no end, whose end is then meaningless.
func (o *Object) Bounds() (start, end time.Time, open bool) {
	consider := func(s, e time.Time) {
		if start.IsZero() || s.Before(start) {
			start = s
		}
		if e.After(end) {
			end = e
		}
	}
	for _, ov := range o.Overrides {
		s := o.Zones.Instant(ov.Start)
		consider(s, o.Zones.Instant(withWall(ov.Start, ov.endWall(o.Zones, ov.Start.Wall))))
	}
	m := o.Master
	if m == nil || m.Start.IsZero() {
		return start, end, false
	}
	s := o.Zones.Instant(m.Start)
	consider(s, o.Zones.Instant(withWall(m.Start, m.endWall(o.Zones, m.Start.Wall))))
	if m.RRule != nil && m.RRule.Count == 0 && !m.RRule.HasUntil {
		return start, end, true
	}
	walls := o.walls(o.Zones.WallIn(farFuture, m.Start))
	if len(walls) > 0 {
		last := walls[len(walls)-1]
		consider(o.Zones.Instant(withWall(m.Start, last)), o.Zones.Instant(withWall(m.Start, m.endWall(o.Zones, last))))
	}
	return start, end, false
}

// walls returns the master's instance start times on its own wall clock up to
// limit: the rule's instances and the RDATEs, before EXDATE is applied.
func (o *Object) walls(limit time.Time) []time.Time {
	m := o.Master
	if !m.IsRecurring() {
		return []time.Time{m.Start.Wall}
	}
	var out []time.Time
	if m.RRule != nil {
		out = m.RRule.instances(m.Start.Wall, o.untilWall(), limit)
	} else {
		out = []time.Time{m.Start.Wall}
	}
	for _, r := range m.RDates {
		w := r.Wall
		if r.UTC != m.Start.UTC || r.TZID != m.Start.TZID {
			w = o.Zones.WallIn(o.Zones.Instant(r), m.Start)
		}
		if r.AllDay && !m.Start.AllDay {
			w = w.Add(m.Start.Wall.Sub(date(m.Start.Wall.Date())))
		}
		if w.Before(limit) && !slices.ContainsFunc(out, w.Equal) {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

// untilWall converts the rule's UNTIL to the master's wall clock. A date
// UNTIL on a timed event includes that whole day.
func (o *Object) untilWall() time.Time {
	r := o.Master.RRule
	if r == nil || !r.HasUntil {
		return time.Time{}
	}
	switch {
	case r.Until.AllDay && !o.Master.Start.AllDay:
		return r.Until.Wall.Add(24*time.Hour - time.Second)
	case r.Until.AllDay:
		return r.Until.Wall
	case o.Master.Start.AllDay:
		return date(o.Zones.WallIn(o.Zones.Instant(r.Until), o.Master.Start).Date())
	}
	return o.Zones.WallIn(o.Zones.Instant(r.Until), o.Master.Start)
}

// excluded is the set of EXDATE instants, by unix second.
func (o *Object) excluded() map[int64]bool {
	out := map[int64]bool{}
	if o.Master == nil {
		return out
	}
	for _, ex := range o.Master.ExDates {
		if ex.AllDay && !o.Master.Start.AllDay {
			ex = withWall(o.Master.Start, ex.Wall.Add(o.Master.Start.Wall.Sub(date(o.Master.Start.Wall.Date()))))
		}
		out[o.Zones.Instant(ex).Unix()] = true
	}
	return out
}

func overlaps(start, end, from, to time.Time) bool {
	if end.Equal(start) {
		return !start.Before(from) && start.Before(to)
	}
	return start.Before(to) && end.After(from)
}

// withWall returns like with its wall clock replaced.
func withWall(like DateTime, wall time.Time) DateTime {
	like.Wall = wall
	return like
}

// DateLike returns the instant t written the way like is: as a date, in UTC,
// or on like's zone's wall clock. A RECURRENCE-ID or EXDATE has to match the
// DTSTART it refers to in form, or other clients will not pair them.
func (o *Object) DateLike(t time.Time, like DateTime) DateTime {
	switch {
	case like.AllDay:
		w := o.Zones.WallIn(t, like)
		return DateTime{Wall: date(w.Date()), AllDay: true}
	case like.UTC:
		return DateTime{Wall: t.UTC(), UTC: true}
	}
	return DateTime{Wall: o.Zones.WallIn(t, like), TZID: like.TZID}
}

// FindOverride returns the override for the occurrence originally starting at
// rid, or nil.
func (o *Object) FindOverride(rid time.Time) *Event {
	for _, ov := range o.Overrides {
		if o.Zones.Instant(ov.RecurrenceID).Equal(rid) {
			return ov
		}
	}
	return nil
}

// Override returns the override for the occurrence originally starting at
// rid, creating one from the master when there is none yet.
func (o *Object) Override(rid time.Time) *Event {
	if ov := o.FindOverride(rid); ov != nil {
		return ov
	}
	m := o.Master
	comp := m.Component.Clone()
	for _, name := range []string{"RRULE", "RDATE", "EXDATE", "EXRULE"} {
		comp.Remove(name)
	}
	ov, _ := ParseEvent(comp)
	ov.RecurrenceID = o.DateLike(rid, m.Start)
	ov.Start = ov.RecurrenceID
	if !m.End.IsZero() {
		end := o.Zones.Instant(withWall(m.Start, m.endWall(o.Zones, ov.Start.Wall)))
		ov.End = o.DateLike(end, m.End)
	}
	ov.Apply()
	o.Overrides = append(o.Overrides, ov)
	return ov
}

// ExcludeOccurrence removes one occurrence: an EXDATE on the master, and the
// override for it if there was one.
func (o *Object) ExcludeOccurrence(rid time.Time) {
	o.dropOverrides(func(t time.Time) bool { return t.Equal(rid) })
	if o.Master == nil {
		return
	}
	o.Master.ExDates = append(o.Master.ExDates, o.DateLike(rid, o.Master.Start))
	o.Master.Apply()
}

// TruncateBefore ends the series just before the occurrence at rid, dropping
// the overrides, RDATEs and EXDATEs from there on. It returns false when rid
// is the first occurrence, so nothing of the series is left.
func (o *Object) TruncateBefore(rid time.Time) bool {
	m := o.Master
	if m == nil || !rid.After(o.Zones.Instant(m.Start)) {
		return false
	}
	if m.RRule != nil {
		m.RRule.Count = 0
		m.RRule.HasUntil = true
		if m.Start.AllDay {
			day := o.DateLike(rid, m.Start)
			m.RRule.Until = DateTime{Wall: day.Wall.AddDate(0, 0, -1), AllDay: true}
		} else {
			m.RRule.Until = DateTime{Wall: rid.UTC().Add(-time.Second), UTC: true}
		}
	}
	keep := func(list []DateTime) []DateTime {
		var out []DateTime
		for _, d := range list {
			if o.Zones.Instant(d).Before(rid) {
				out = append(out, d)
			}
		}
		return out
	}
	m.RDates = keep(m.RDates)
	m.ExDates = keep(m.ExDates)
	o.dropOverrides(func(t time.Time) bool { return !t.Before(rid) })
	m.Apply()
	return true
}

// SplitAt cuts the series in two at the occurrence starting at rid. The
// returned object, under newUID, holds the occurrences from rid on with the
// same rule (its COUNT reduced by what stays behind) and the overrides that
// fall there; this object keeps the ones before. It returns nil when rid is
// the first occurrence, where there is nothing to split off.
func (o *Object) SplitAt(rid time.Time, newUID string) *Object {
	m := o.Master
	if m == nil || !rid.After(o.Zones.Instant(m.Start)) {
		return nil
	}
	future := &Object{Calendar: o.Calendar.Clone(), Zones: o.Zones}
	future.Calendar.RemoveChildren("VEVENT")

	head, _ := ParseEvent(m.Component.Clone())
	head.UID = newUID
	head.Start = o.DateLike(rid, m.Start)
	if !m.End.IsZero() {
		end := o.Zones.Instant(withWall(m.Start, m.endWall(o.Zones, head.Start.Wall)))
		head.End = o.DateLike(end, m.End)
	}
	if m.RRule != nil && m.RRule.Count > 0 {
		before := 0
		for _, w := range m.RRule.instances(m.Start.Wall, time.Time{}, o.Zones.WallIn(rid, m.Start)) {
			if o.Zones.Instant(withWall(m.Start, w)).Before(rid) {
				before++
			}
		}
		head.RRule.Count = max(m.RRule.Count-before, 1)
	}
	keepFrom := func(list []DateTime) []DateTime {
		var out []DateTime
		for _, d := range list {
			if !o.Zones.Instant(d).Before(rid) {
				out = append(out, d)
			}
		}
		return out
	}
	head.RDates = keepFrom(head.RDates)
	head.ExDates = keepFrom(head.ExDates)
	head.Apply()
	future.Master = head
	for _, ov := range o.Overrides {
		if o.Zones.Instant(ov.RecurrenceID).Before(rid) {
			continue
		}
		moved, _ := ParseEvent(ov.Component.Clone())
		moved.UID = newUID
		moved.Apply()
		future.Overrides = append(future.Overrides, moved)
	}
	o.TruncateBefore(rid)
	return future
}

func (o *Object) dropOverrides(drop func(rid time.Time) bool) {
	var kept []*Event
	for _, ov := range o.Overrides {
		if !drop(o.Zones.Instant(ov.RecurrenceID)) {
			kept = append(kept, ov)
		}
	}
	o.Overrides = kept
}

// VTimezone builds a VTIMEZONE for loc from the transitions it has in year:
// a STANDARD and, for a zone with daylight saving, a DAYLIGHT observance with
// yearly rules. A zone whose rules change later than that year is described
// as it was in that year, which is what the event needs.
func VTimezone(tzid string, loc *time.Location, year int) *Component {
	tz := &Component{Name: "VTIMEZONE"}
	tz.Set("TZID", tzid)
	transitions := yearTransitions(loc, year)
	if len(transitions) == 0 {
		_, offset := time.Date(year, 1, 1, 0, 0, 0, 0, loc).Zone()
		std := &Component{Name: "STANDARD"}
		std.Set("DTSTART", "19700101T000000")
		std.Set("TZOFFSETFROM", formatOffset(offset))
		std.Set("TZOFFSETTO", formatOffset(offset))
		tz.Children = append(tz.Children, std)
		return tz
	}
	for _, tr := range transitions {
		name := "STANDARD"
		if tr.isDST {
			name = "DAYLIGHT"
		}
		obs := &Component{Name: name}
		wall := tr.at.Add(time.Duration(tr.from) * time.Second).UTC()
		start := time.Date(1970, wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, time.UTC)
		obs.Set("DTSTART", start.Format(layoutDateTime))
		obs.Set("TZOFFSETFROM", formatOffset(tr.from))
		obs.Set("TZOFFSETTO", formatOffset(tr.to))
		nth := (wall.Day()-1)/7 + 1
		byday := weekdayNames[wall.Weekday()]
		if wall.Day()+7 > daysIn(wall.Year(), wall.Month()) {
			byday = "-1" + byday
		} else {
			byday = strconv.Itoa(nth) + byday
		}
		obs.Set("RRULE", "FREQ=YEARLY;BYMONTH="+strconv.Itoa(int(wall.Month()))+";BYDAY="+byday)
		tz.Children = append(tz.Children, obs)
	}
	return tz
}

type transition struct {
	at       time.Time
	from, to int
	isDST    bool
}

// yearTransitions finds the offset changes of loc in year by scanning day by
// day and narrowing each change down to the second.
func yearTransitions(loc *time.Location, year int) []transition {
	var out []transition
	prev := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	_, prevOffset := prev.Zone()
	for day := 1; day <= 366; day++ {
		cur := prev.Add(24 * time.Hour)
		if cur.Year() > year {
			break
		}
		_, offset := cur.Zone()
		if offset != prevOffset {
			lo, hi := prev, cur
			for hi.Sub(lo) > time.Second {
				mid := lo.Add(hi.Sub(lo) / 2)
				if _, o := mid.Zone(); o == prevOffset {
					lo = mid
				} else {
					hi = mid
				}
			}
			out = append(out, transition{at: hi.UTC(), from: prevOffset, to: offset, isDST: hi.IsDST()})
		}
		prev, prevOffset = cur, offset
	}
	return out
}

func formatOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return sign + pad2(seconds/3600) + pad2(seconds%3600/60)
}

func pad2(n int) string {
	return fmt.Sprintf("%02d", n)
}
