package syncsched

import (
	"slices"
	"sync"
)

// Group is the running work of the schedulers of one account. A scheduler
// replaced while a task of it still runs (a soft Stop that gave up waiting)
// leaves that task running to its end; a new scheduler in the same Group
// does not start a task beside it that one scheduler's own rules would keep
// apart: the same Key, a list beside a list or bodies of the same folder, or
// anything beside a barrier, and no barrier beside anything. Tasks of other
// folders go ahead. When such a task ends, the other schedulers are told,
// except for an end that changed nothing they could act on (see end).
//
// Set it in Options.Group; schedulers of different accounts never share one.
// The zero value is ready to use.
type Group struct {
	mu      sync.Mutex
	running map[any][]shape                        // per scheduler, its tasks running now
	wakes   map[any]func(folder int64, freed bool) // per scheduler not stopped, how to tell it
}

// shape is what the exclusion rules read of a task.
type shape struct {
	kind     Kind
	folderID int64
	key      string
	barrier  bool
}

func shapeOf[S any](t Task[S]) shape {
	return shape{kind: t.Kind, folderID: t.FolderID, key: t.Key, barrier: t.Barrier}
}

// join adds a scheduler, which wake tells after another one's task of
// folder (0: of none) ended; freed is false for an end that released no
// ids (ErrSaturated, or a task that never ran).
func (g *Group) join(owner any, wake func(folder int64, freed bool)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.wakes == nil {
		g.wakes = make(map[any]func(folder int64, freed bool))
	}
	g.wakes[owner] = wake
}

// leave stops telling a stopped scheduler. Its tasks still running keep
// their place until they end.
func (g *Group) leave(owner any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.wakes, owner)
}

// reserve records a task of owner as running unless a task of another
// scheduler keeps it from starting; it reports whether it did. The check and
// the record are one step under g.mu, so two schedulers never both admit
// tasks that conflict. Callers hold their scheduler's mu: the lock order is
// Scheduler.mu, then Group.mu, never the reverse (the wakes end returns are
// called on no lock).
func (g *Group) reserve(owner any, s shape) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocksLocked(owner, s) {
		return false
	}
	if g.running == nil {
		g.running = make(map[any][]shape)
	}
	g.running[owner] = append(g.running[owner], s)
	return true
}

// end records the end of a task of owner and returns how to tell the other
// schedulers; call those on no lock. freed reports whether the task may have
// released ids a saturated unit waits for.
//
// An end that freed nothing while owner still runs a task of the same shape
// (another copy of a width entry) tells no one: what the other schedulers
// may start (blocksLocked) and what holds their held entries (runsFolder)
// are the same as before it. Telling them anyway is what let two saturated
// units of one folder in two schedulers wake each other forever while a
// third unit held the ids. Any other end that freed nothing is told, as not
// freed: it may have been its folder's last running task, or have lifted an
// exclusion, and a held entry with nothing of its folder running must open.
func (g *Group) end(owner any, s shape, freed bool) []func() {
	g.mu.Lock()
	defer g.mu.Unlock()
	list := g.running[owner]
	for i, r := range list {
		if r == s {
			list = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(g.running, owner)
	} else {
		g.running[owner] = list
	}
	if !freed && slices.Contains(list, s) {
		return nil
	}
	var wake []func()
	for o, fn := range g.wakes {
		if o != owner {
			wake = append(wake, func() { fn(s.folderID, freed) })
		}
	}
	return wake
}

// runsFolder reports whether a task of folder runs in a scheduler other
// than owner.
func (g *Group) runsFolder(owner any, folder int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for o, list := range g.running {
		if o == owner {
			continue
		}
		for _, r := range list {
			if r.folderID == folder {
				return true
			}
		}
	}
	return false
}

// blocks reports whether a task of another scheduler than owner keeps s
// from starting now. It is advisory: by the time the caller acts another
// scheduler may have started one; reserve decides.
func (g *Group) blocks(owner any, s shape) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.blocksLocked(owner, s)
}

func (g *Group) blocksLocked(owner any, s shape) bool {
	for o, list := range g.running {
		if o == owner {
			continue
		}
		for _, r := range list {
			if s.barrier || conflicts(r, s) {
				return true
			}
		}
	}
	return false
}

// conflicts reports whether next may not start while running runs: the
// rules one scheduler applies to its own running tasks, but for barriers
// (a running barrier holds everything).
func conflicts(running, next shape) bool {
	if running.barrier {
		return true
	}
	if next.key != "" && running.key == next.key {
		return true
	}
	if next.folderID == 0 || running.folderID != next.folderID {
		return false
	}
	switch {
	case next.kind == KindBodies && running.kind.lists():
		return true
	case next.kind.lists() && running.kind.lists():
		return true
	case next.kind == KindFullCheck && running.kind == KindBodies:
		return true
	}
	return false
}
