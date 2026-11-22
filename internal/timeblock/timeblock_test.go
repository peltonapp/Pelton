package timeblock

import (
	"fmt"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func weekdays() [7]bool {
	return [7]bool{false, true, true, true, true, true, false}
}

func rules(loc *time.Location) Rules {
	return Rules{Loc: loc, WorkStart: 9 * 60, WorkEnd: 17 * 60, WorkDays: weekdays(), MinSlot: 15 * time.Minute, Step: 15 * time.Minute}
}

func wall(loc *time.Location, y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

// spans renders spans as wall-clock text in loc, "Mon 09:00-10:00".
func spans(loc *time.Location, list []Span) string {
	var out []string
	for _, s := range list {
		a, b := s.Start.In(loc), s.End.In(loc)
		out = append(out, fmt.Sprintf("%s %s-%s", a.Format("Mon 02"), a.Format("15:04"), b.Format("15:04")))
	}
	return strings.Join(out, ", ")
}

func TestFreeTime(t *testing.T) {
	berlin := zone(t, "Europe/Berlin")
	// 2026-03-09 is a Monday.
	mon := func(h, m int) time.Time { return wall(berlin, 2026, 3, 9, h, m) }
	tests := []struct {
		name     string
		rules    func(Rules) Rules
		from, to time.Time
		busy     []Span
		want     string
	}{
		{
			name: "whole working day",
			from: mon(0, 0), to: mon(23, 59),
			want: "Mon 09 09:00-17:00",
		},
		{
			name: "events cut the day",
			from: mon(0, 0), to: mon(23, 59),
			busy: []Span{{mon(10, 0), mon(11, 0)}, {mon(13, 0), mon(14, 30)}},
			want: "Mon 09 09:00-10:00, Mon 09 11:00-13:00, Mon 09 14:30-17:00",
		},
		{
			name: "overlapping events merge",
			from: mon(0, 0), to: mon(23, 59),
			busy: []Span{{mon(10, 0), mon(12, 0)}, {mon(11, 0), mon(11, 30)}, {mon(11, 30), mon(13, 0)}},
			want: "Mon 09 09:00-10:00, Mon 09 13:00-17:00",
		},
		{
			name:  "buffer around busy time",
			rules: func(r Rules) Rules { r.Buffer = 10 * time.Minute; return r },
			from:  mon(0, 0), to: mon(23, 59),
			busy: []Span{{mon(10, 0), mon(11, 0)}},
			want: "Mon 09 09:00-09:50, Mon 09 11:15-17:00",
		},
		{
			name:  "gaps shorter than the minimum are dropped",
			rules: func(r Rules) Rules { r.MinSlot = 30 * time.Minute; return r },
			from:  mon(0, 0), to: mon(23, 59),
			busy: []Span{{mon(9, 20), mon(12, 0)}, {mon(12, 20), mon(17, 0)}},
			want: "",
		},
		{
			name: "starts from now, aligned to the step",
			from: mon(10, 7), to: mon(23, 59),
			want: "Mon 09 10:15-17:00",
		},
		{
			name: "zero free time",
			from: mon(0, 0), to: mon(23, 59),
			busy: []Span{{mon(8, 0), mon(18, 0)}},
			want: "",
		},
		{
			name: "weekend skipped",
			from: wall(berlin, 2026, 3, 7, 0, 0), to: wall(berlin, 2026, 3, 10, 0, 0),
			want: "Mon 09 09:00-17:00",
		},
		{
			name: "event spanning days",
			from: mon(0, 0), to: wall(berlin, 2026, 3, 11, 0, 0),
			busy: []Span{{mon(15, 0), wall(berlin, 2026, 3, 10, 11, 0)}},
			want: "Mon 09 09:00-15:00, Tue 10 11:00-17:00",
		},
		{
			name: "an all-day busy day blocks the whole day",
			from: mon(0, 0), to: wall(berlin, 2026, 3, 11, 0, 0),
			busy: []Span{{mon(0, 0), wall(berlin, 2026, 3, 10, 0, 0)}},
			want: "Tue 10 09:00-17:00",
		},
		{
			name:  "working hours to midnight",
			rules: func(r Rules) Rules { r.WorkStart, r.WorkEnd = 20*60, 24*60; return r },
			from:  mon(0, 0), to: wall(berlin, 2026, 3, 11, 0, 0),
			busy: []Span{{mon(23, 0), wall(berlin, 2026, 3, 10, 1, 0)}},
			want: "Mon 09 20:00-23:00, Tue 10 20:00-00:00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rules(berlin)
			if tt.rules != nil {
				r = tt.rules(r)
			}
			if got := spans(berlin, FreeTime(r, tt.from, tt.to, tt.busy)); got != tt.want {
				t.Errorf("free = %q, want %q", got, tt.want)
			}
		})
	}
}

// On the days the clocks change, working hours keep their wall-clock times
// although the day is 23 or 25 hours long.
func TestFreeTimeDaylightSaving(t *testing.T) {
	berlin := zone(t, "Europe/Berlin")
	r := rules(berlin)
	r.WorkDays = [7]bool{true, true, true, true, true, true, true}
	r.WorkStart, r.WorkEnd = 1*60, 4*60
	// spring forward: 02:00 becomes 03:00 on Sunday 2026-03-29.
	spring := FreeTime(r, wall(berlin, 2026, 3, 29, 0, 0), wall(berlin, 2026, 3, 30, 0, 0), nil)
	if got := spans(berlin, spring); got != "Sun 29 01:00-04:00" {
		t.Errorf("spring = %q", got)
	}
	if spring[0].Length() != 2*time.Hour {
		t.Errorf("spring length = %v, want 2h", spring[0].Length())
	}
	// fall back: 03:00 becomes 02:00 on Sunday 2026-10-25.
	fall := FreeTime(r, wall(berlin, 2026, 10, 25, 0, 0), wall(berlin, 2026, 10, 26, 0, 0), nil)
	if got := spans(berlin, fall); got != "Sun 25 01:00-04:00" {
		t.Errorf("fall = %q", got)
	}
	if fall[0].Length() != 4*time.Hour {
		t.Errorf("fall length = %v, want 4h", fall[0].Length())
	}
	// a normal working day across the change keeps 09:00 to 17:00.
	r.WorkStart, r.WorkEnd = 9*60, 17*60
	next := FreeTime(r, wall(berlin, 2026, 3, 28, 0, 0), wall(berlin, 2026, 3, 31, 0, 0), nil)
	if got := spans(berlin, next); got != "Sat 28 09:00-17:00, Sun 29 09:00-17:00, Mon 30 09:00-17:00" {
		t.Errorf("across the change = %q", got)
	}
}

// The same instants give different free time in different zones, since the
// working hours are the user's wall clock.
func TestFreeTimeZones(t *testing.T) {
	berlin := zone(t, "Europe/Berlin")
	newYork := zone(t, "America/New_York")
	from := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	busy := []Span{{time.Date(2026, 3, 9, 14, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC)}}
	if got := spans(berlin, FreeTime(rules(berlin), from, to, busy)); got != "Mon 09 09:00-15:00, Mon 09 16:00-17:00" {
		t.Errorf("berlin = %q", got)
	}
	if got := spans(newYork, FreeTime(rules(newYork), from, to, busy)); got != "Mon 09 09:00-10:00, Mon 09 11:00-17:00" {
		t.Errorf("new york = %q", got)
	}
	// India is five and a half hours ahead: the step still lands on the wall
	// clock's quarter hours.
	kolkata := zone(t, "Asia/Kolkata")
	got := FreeTime(rules(kolkata), time.Date(2026, 3, 9, 4, 7, 0, 0, time.UTC), to, nil)
	if s := spans(kolkata, got); s != "Mon 09 09:45-17:00" {
		t.Errorf("kolkata = %q", s)
	}
}

func TestFindSlots(t *testing.T) {
	berlin := zone(t, "Europe/Berlin")
	mon := func(h, m int) time.Time { return wall(berlin, 2026, 3, 9, h, m) }
	busy := []Span{{mon(9, 0), mon(10, 0)}, {mon(10, 30), mon(12, 0)}, {mon(13, 0), mon(16, 0)}}
	q := SlotQuery{Length: time.Hour, From: mon(8, 0), Days: 2, Limit: 5, PerDay: 2}
	got := FindSlots(rules(berlin), q, busy)
	var list []Span
	for _, s := range got {
		list = append(list, s.Span)
	}
	if s := spans(berlin, list); s != "Mon 09 12:00-13:00, Mon 09 16:00-17:00, Tue 10 09:00-10:00" {
		t.Errorf("slots = %q", s)
	}
	for _, s := range got {
		if s.AfterDue {
			t.Errorf("slot after due without a due date: %+v", s)
		}
	}

	q.Due = mon(17, 0)
	got = FindSlots(rules(berlin), q, busy)
	if got[0].AfterDue || got[1].AfterDue || !got[2].AfterDue {
		t.Errorf("after due flags = %v %v %v", got[0].AfterDue, got[1].AfterDue, got[2].AfterDue)
	}

	q.Length = 9 * time.Hour
	if got := FindSlots(rules(berlin), q, busy); len(got) != 0 {
		t.Errorf("a block longer than the working day found %v", got)
	}

	q.Length, q.PerDay, q.Limit = 30*time.Minute, 0, 2
	if got := FindSlots(rules(berlin), q, busy); len(got) != 2 {
		t.Errorf("limit not kept: %d", len(got))
	}
}

// Asking twice gives the same answer.
func TestFindSlotsDeterministic(t *testing.T) {
	berlin := zone(t, "Europe/Berlin")
	mon := func(h, m int) time.Time { return wall(berlin, 2026, 3, 9, h, m) }
	busy := []Span{{mon(13, 0), mon(14, 0)}, {mon(9, 0), mon(10, 0)}, {mon(11, 0), mon(11, 30)}}
	q := SlotQuery{Length: 45 * time.Minute, From: mon(8, 0), Days: 5, Limit: 6, PerDay: 2}
	first := FindSlots(rules(berlin), q, busy)
	for range 5 {
		again := FindSlots(rules(berlin), q, append([]Span(nil), busy...))
		if fmt.Sprint(again) != fmt.Sprint(first) {
			t.Fatalf("slots changed: %v then %v", first, again)
		}
	}
}

func pieces(loc *time.Location, p Plan) string {
	var out []string
	for _, x := range p.Pieces {
		a, b := x.Start.In(loc), x.End.In(loc)
		out = append(out, fmt.Sprintf("%s %s-%s %d/%d", x.TaskID, a.Format("15:04"), b.Format("15:04"), x.Part, x.Parts))
	}
	return strings.Join(out, ", ")
}

func TestPlanDay(t *testing.T) {
	berlin := zone(t, "Europe/Berlin")
	mon := func(h, m int) time.Time { return wall(berlin, 2026, 3, 9, h, m) }
	endOfDay := wall(berlin, 2026, 3, 10, 0, 0)

	t.Run("due date then priority", func(t *testing.T) {
		tasks := []PlanTask{
			{ID: "late-low", Due: endOfDay, Priority: 9, Need: 30 * time.Minute},
			{ID: "none", Need: 30 * time.Minute},
			{ID: "late-high", Due: endOfDay, Priority: 1, Need: 30 * time.Minute},
			{ID: "noon", Due: mon(12, 0), Need: 30 * time.Minute},
			{ID: "late-noprio", Due: endOfDay, Need: 30 * time.Minute},
		}
		p := PlanDay(rules(berlin), mon(0, 0), mon(8, 0), tasks, nil)
		want := "noon 09:00-09:30 1/1, late-high 09:30-10:00 1/1, late-low 10:00-10:30 1/1, late-noprio 10:30-11:00 1/1, none 11:00-11:30 1/1"
		if got := pieces(berlin, p); got != want {
			t.Errorf("plan = %q\nwant  %q", got, want)
		}
	})

	t.Run("a task that fits whole is not split", func(t *testing.T) {
		busy := []Span{{mon(9, 30), mon(10, 0)}, {mon(11, 0), mon(17, 0)}}
		p := PlanDay(rules(berlin), mon(0, 0), mon(8, 0), []PlanTask{{ID: "a", Need: time.Hour}}, busy)
		if got := pieces(berlin, p); got != "a 10:00-11:00 1/1" {
			t.Errorf("plan = %q", got)
		}
	})

	t.Run("a long task is split only when it fits nowhere whole", func(t *testing.T) {
		busy := []Span{{mon(10, 0), mon(11, 0)}, {mon(12, 0), mon(16, 0)}}
		p := PlanDay(rules(berlin), mon(0, 0), mon(8, 0), []PlanTask{{ID: "big", Need: 150 * time.Minute}}, busy)
		if got := pieces(berlin, p); got != "big 09:00-10:00 1/3, big 11:00-12:00 2/3, big 16:00-16:30 3/3" {
			t.Errorf("plan = %q", got)
		}
		if len(p.Short) != 0 || len(p.Unplaced) != 0 {
			t.Errorf("short %v unplaced %v", p.Short, p.Unplaced)
		}
	})

	t.Run("buffer after each piece", func(t *testing.T) {
		r := rules(berlin)
		r.Buffer = 10 * time.Minute
		p := PlanDay(r, mon(0, 0), mon(8, 0), []PlanTask{{ID: "a", Need: 30 * time.Minute}, {ID: "b", Need: 30 * time.Minute}}, nil)
		if got := pieces(berlin, p); got != "a 09:00-09:30 1/1, b 09:45-10:15 1/1" {
			t.Errorf("plan = %q", got)
		}
	})

	t.Run("not enough time", func(t *testing.T) {
		busy := []Span{{mon(9, 0), mon(16, 0)}}
		p := PlanDay(rules(berlin), mon(0, 0), mon(8, 0), []PlanTask{
			{ID: "first", Due: mon(17, 0), Need: 90 * time.Minute},
			{ID: "second", Due: endOfDay, Need: 30 * time.Minute},
		}, busy)
		if got := pieces(berlin, p); got != "first 16:00-17:00 1/1" {
			t.Errorf("plan = %q", got)
		}
		if fmt.Sprint(p.Short) != "[first]" || fmt.Sprint(p.Unplaced) != "[second]" {
			t.Errorf("short %v unplaced %v", p.Short, p.Unplaced)
		}
	})

	t.Run("zero free time", func(t *testing.T) {
		p := PlanDay(rules(berlin), mon(0, 0), mon(17, 30), []PlanTask{{ID: "a", Need: time.Hour}}, nil)
		if len(p.Pieces) != 0 || fmt.Sprint(p.Unplaced) != "[a]" {
			t.Errorf("plan after hours = %+v", p)
		}
	})

	t.Run("from now on", func(t *testing.T) {
		p := PlanDay(rules(berlin), mon(0, 0), mon(14, 5), []PlanTask{{ID: "a", Need: time.Hour}}, nil)
		if got := pieces(berlin, p); got != "a 14:15-15:15 1/1" {
			t.Errorf("plan = %q", got)
		}
	})

	t.Run("a day off has no time", func(t *testing.T) {
		sat := wall(berlin, 2026, 3, 14, 0, 0)
		p := PlanDay(rules(berlin), sat, sat, []PlanTask{{ID: "a", Need: time.Hour}}, nil)
		if len(p.Pieces) != 0 || len(p.Unplaced) != 1 {
			t.Errorf("plan on a saturday = %+v", p)
		}
	})

	t.Run("nothing needed is left out", func(t *testing.T) {
		p := PlanDay(rules(berlin), mon(0, 0), mon(8, 0), []PlanTask{{ID: "planned", Need: 0}}, nil)
		if len(p.Pieces) != 0 || len(p.Unplaced) != 0 {
			t.Errorf("plan = %+v", p)
		}
	})
}
