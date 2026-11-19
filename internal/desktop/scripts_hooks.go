package desktop

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/peltonapp/Pelton/internal/chat"
	"github.com/peltonapp/Pelton/internal/rfc822"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/spaces"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// scriptBatchCap is how many events one batch of changes, such as a feed
// refresh, a calendar sync or a mail sync, hands the scripts at most. The
// rest are dropped, so a sync of thousands of changes cannot flood the
// dispatcher.
const scriptBatchCap = 50

// scriptClockEvery is how often the clock checks for events about to start
// and tasks that fell due.
const scriptClockEvery = time.Minute

// maxStartLead is the earliest a "starts in N minutes" script can run: a
// week ahead.
const maxStartLead = 7 * 24 * 60

// scriptEvent hands an event caused from ctx to the scripts that listen for
// it, with the loop depth and the source script of the run ctx belongs to.
func (a *App) scriptEvent(ctx context.Context, trigger string, data map[string]any) {
	a.emitScript(scripting.EventFrom(ctx, trigger, data))
}

type scriptBudgetKey struct{}

// withScriptBudget gives a batch of changes its share of script events.
func withScriptBudget(ctx context.Context) context.Context {
	left := &atomic.Int32{}
	left.Store(scriptBatchCap)
	return context.WithValue(ctx, scriptBudgetKey{}, left)
}

// scriptEventBudgeted is scriptEvent for a change inside a batch: it hands
// the event on while the batch has budget left, and drops it without one.
func (a *App) scriptEventBudgeted(ctx context.Context, trigger string, data map[string]any) {
	left, ok := ctx.Value(scriptBudgetKey{}).(*atomic.Int32)
	if !ok || a.scripts == nil || !a.scripts.Wants(trigger) {
		return
	}
	switch n := left.Add(-1); {
	case n >= 0:
		a.scriptEvent(ctx, trigger, data)
	case n == -1:
		a.log.Warn("scripts: too many changes at once, later ones start no scripts", "trigger", trigger, "cap", scriptBatchCap)
	}
}

// scriptFeedItems reports the new items of a feed refresh.
func (a *App) scriptFeedItems(ids []int64) {
	ctx := withScriptBudget(a.ctx)
	for _, id := range ids {
		a.scriptEventBudgeted(ctx, spaces.TriggerFeedItem, map[string]any{"item_id": id})
	}
}

// scriptChatMessages reports chat messages others sent.
func (a *App) scriptChatMessages(list []chat.Notification) {
	ctx := withScriptBudget(a.ctx)
	for _, n := range list {
		a.scriptEventBudgeted(ctx, spaces.TriggerChatMessage, map[string]any{"chat": spaces.ChatMessageObject(spaces.ChatMessage{
			RoomID: n.RoomID, Room: n.RoomName, EventID: n.EventID, Sender: n.Sender, Body: n.Body, Direct: n.Direct, Mention: n.Mention,
		})})
	}
}

// scriptInvitations reports new mail that carries a calendar invitation as
// an .ics attachment. Mail from a first sync is left out, and an invitation
// only inline in the message text is not found here, since finding it
// would mean fetching every new message's source.
func (a *App) scriptInvitations(ids []int64, firstSync bool) {
	if firstSync || a.scripts == nil || !a.scripts.Wants(spaces.TriggerInvitation) {
		return
	}
	ctx := withScriptBudget(a.ctx)
	for _, id := range ids {
		atts, err := a.store.ListAttachments(scriptsCtx(a.ctx), id)
		if err != nil || !slices.ContainsFunc(atts, func(att storage.Attachment) bool { return rfc822.IsCalendar(att.ContentType, att.Filename) }) {
			continue
		}
		inv, err := a.ParseInvite(id)
		if err != nil || !inv.Found {
			continue
		}
		m, err := a.store.GetMessage(scriptsCtx(a.ctx), id)
		if err != nil {
			continue
		}
		msg, err := a.scriptMessage(*m)
		if err != nil {
			continue
		}
		a.scriptEventBudgeted(ctx, spaces.TriggerInvitation, map[string]any{
			"message": scripting.MessageObject(msg), "invitation": spaces.InvitationObject(scriptInvitationOf(inv)),
		})
	}
}

// scriptServerFlags reports the read and flag changes a sync took from the
// server.
func (a *App) scriptServerFlags(changes []psync.FlagChange) {
	ctx := withScriptBudget(a.ctx)
	for _, ch := range changes {
		if !a.scriptSeesMessage(ch.ID) {
			continue
		}
		if (ch.Before^ch.After)&storage.FlagSeen != 0 {
			a.scriptEventBudgeted(ctx, scripting.TriggerMailReadMark, map[string]any{"message_id": ch.ID, "read": ch.After&storage.FlagSeen != 0})
		}
		if (ch.Before^ch.After)&storage.FlagFlagged != 0 {
			a.scriptEventBudgeted(ctx, scripting.TriggerMailFlagged, map[string]any{"message_id": ch.ID, "flagged": ch.After&storage.FlagFlagged != 0})
		}
	}
}

// runScriptClock fires the triggers that depend on time passing rather than
// on a change: events about to start and tasks falling due. Each tick
// covers the time since the last, so nothing fires twice or is skipped while
// the app runs.
func (a *App) runScriptClock() {
	last := time.Now()
	t := time.NewTicker(scriptClockEvery)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-t.C:
			a.scriptEventsStarting(last, now)
			a.scriptTasksDue(last, now)
			last = now
		}
	}
}

// scriptEventsStarting reports the events that start in N minutes, for every
// N a script listens for, between the last tick and now.
func (a *App) scriptEventsStarting(last, now time.Time) {
	if a.scripts == nil || !a.scripts.Wants(spaces.TriggerEventStarts) {
		return
	}
	var leads []int
	for _, p := range a.scripts.Listening(spaces.TriggerEventStarts) {
		n, ok := p["minutes"].(float64)
		if ok && n >= 0 && n <= maxStartLead && !slices.Contains(leads, int(n)) {
			leads = append(leads, int(n))
		}
	}
	cal := &scriptCalendar{app: a}
	ctx := withScriptBudget(a.ctx)
	for _, lead := range leads {
		d := time.Duration(lead) * time.Minute
		from, to := last.Add(d), now.Add(d)
		events, err := cal.occurrences(from.Add(-time.Minute), to.Add(time.Minute))
		if err != nil {
			a.log.Warn("scripts: list upcoming events", "err", err)
			return
		}
		for _, e := range events {
			if e.Start.After(from) && !e.Start.After(to) {
				a.scriptEventBudgeted(ctx, spaces.TriggerEventStarts, map[string]any{"event": spaces.EventObject(e), "minutes": lead})
			}
		}
	}
}

// scriptTasksDue reports the open tasks that fell due between the last tick
// and now.
func (a *App) scriptTasksDue(last, now time.Time) {
	if a.scripts == nil || !a.scripts.Wants(spaces.TriggerTaskOverdue) {
		return
	}
	tasks, err := a.ListTasks()
	if err != nil {
		a.log.Warn("scripts: list tasks", "err", err)
		return
	}
	ctx := withScriptBudget(a.ctx)
	for _, t := range tasks {
		due := taskDueTime(t.Due)
		if !t.Done && due.After(last) && !due.After(now) {
			a.scriptEventBudgeted(ctx, spaces.TriggerTaskOverdue, map[string]any{"task_id": t.ID})
		}
	}
}
