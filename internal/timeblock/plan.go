package timeblock

import (
	"slices"
	"time"
)

// PlanTask is a task to fit into a day.
type PlanTask struct {
	// ID names the task in the result.
	ID string
	// Due is when the task is due, zero for none. A task due on a date is due
	// at the end of that day.
	Due time.Time
	// Priority is 0 for none, 1 the highest through 9 the lowest.
	Priority int
	// Need is how much time it still needs.
	Need time.Duration
}

// Piece is one block of a plan: a task, or part of one, at a time.
type Piece struct {
	TaskID string
	Span
	// Part counts the pieces of a task that had to be split, from 1, and
	// Parts says how many there are; both are 1 for a task in one piece.
	Part  int
	Parts int
}

// Plan is a day laid out.
type Plan struct {
	Pieces []Piece
	// Short lists the tasks that got some time but less than they need.
	Short []string
	// Unplaced lists the tasks that got no time at all.
	Unplaced []string
}

// rank orders tasks for planning: the earliest due date first, tasks without
// one last; then the highest priority, no priority last; then as given.
func rank(tasks []PlanTask) []PlanTask {
	out := slices.Clone(tasks)
	prio := func(p int) int {
		if p <= 0 {
			return 10
		}
		return p
	}
	slices.SortStableFunc(out, func(a, b PlanTask) int {
		switch {
		case a.Due.IsZero() != b.Due.IsZero():
			if a.Due.IsZero() {
				return 1
			}
			return -1
		case !a.Due.Equal(b.Due):
			return a.Due.Compare(b.Due)
		}
		return prio(a.Priority) - prio(b.Priority)
	})
	return out
}

// PlanDay lays tasks out in the free time of day's day from now on. Tasks go
// in rank order; each takes the earliest piece of free time that holds all
// it needs, and only a task that fits in no single piece is split across the
// earliest pieces left. What a task takes, plus the buffer after it, is no
// longer free for the next. Nothing is stored: the plan is a proposal.
func PlanDay(r Rules, day, now time.Time, tasks []PlanTask, busy []Span) Plan {
	if r.Loc == nil {
		r.Loc = time.Local
	}
	start := dayStart(day, r.Loc)
	from := start
	if now.After(from) {
		from = now
	}
	free := FreeTime(r, from, nextDay(start, r.Loc), busy)
	var plan Plan
	take := func(i int, length time.Duration) Span {
		got := Span{Start: free[i].Start, End: free[i].Start.Add(length)}
		rest := Span{Start: r.alignUp(got.End.Add(r.Buffer)), End: free[i].End}
		if rest.Length() > 0 && rest.Length() >= r.MinSlot {
			free[i] = rest
		} else {
			free = slices.Delete(free, i, i+1)
		}
		return got
	}
	for _, task := range rank(tasks) {
		if task.Need <= 0 {
			continue
		}
		whole := slices.IndexFunc(free, func(s Span) bool { return s.Length() >= task.Need })
		if whole >= 0 {
			plan.Pieces = append(plan.Pieces, Piece{TaskID: task.ID, Span: take(whole, task.Need), Part: 1, Parts: 1})
			continue
		}
		var parts []Span
		left := task.Need
		for left > 0 && len(free) > 0 {
			length := min(free[0].Length(), left)
			parts = append(parts, take(0, length))
			left -= length
		}
		switch {
		case len(parts) == 0:
			plan.Unplaced = append(plan.Unplaced, task.ID)
			continue
		case left > 0:
			plan.Short = append(plan.Short, task.ID)
		}
		for i, s := range parts {
			plan.Pieces = append(plan.Pieces, Piece{TaskID: task.ID, Span: s, Part: i + 1, Parts: len(parts)})
		}
	}
	slices.SortStableFunc(plan.Pieces, func(a, b Piece) int { return a.Start.Compare(b.Start) })
	return plan
}
