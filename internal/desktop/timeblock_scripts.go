package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// What scripts see of time blocks: a block starting or ending, conditions on
// it, and blocking time for a task.

const (
	triggerBlockStarts = "timeblocks.starts"
	triggerBlockEnds   = "timeblocks.ends"
	permBlocksRead     = "timeblocks.read"
	permBlocksWrite    = "timeblocks.write"
)

// blockScriptHost is what the scripts reach time blocks through.
type blockScriptHost interface {
	Block(ctx context.Context, id int64) (scripting.Object, error)
	BlockTask(ctx context.Context, taskID int64, minutes int) (scripting.Object, error)
}

// blockObject is a block as scripts see it. title is empty for a block whose
// mail the script may not see.
func blockObject(b TimeBlockDTO, title string) scripting.Object {
	unix := func(s string) int64 {
		if t := parseEventTime(s); !t.IsZero() {
			return t.Unix()
		}
		return 0
	}
	return scripting.Object{Type: "block", Fields: map[string]any{
		"id": b.ID, "title": title, "start": unix(b.Start), "end": unix(b.End), "task_id": b.TaskID,
		"done": b.Done, "for_mail": b.HasMail, "on_calendar": b.CalendarID != 0,
	}}
}

func sampleBlock(now time.Time) map[string]any {
	start := now.Add(time.Hour).Truncate(time.Hour)
	return blockObject(TimeBlockDTO{ID: 1, Title: "Write the report", Start: start.Format(time.RFC3339),
		End: start.Add(time.Hour).Format(time.RFC3339), TaskID: 2}, "Write the report").Fields
}

func blockScriptEntries(h blockScriptHost) scripting.Entries {
	meta := scripting.EntryMeta
	load := func(c *scripting.PrepareContext, data map[string]any) (map[string]any, error) {
		out := map[string]any{}
		for k, v := range data {
			out[k] = v
		}
		id, ok := out["block_id"].(int64)
		if !ok {
			return out, nil
		}
		b, err := h.Block(c.Ctx, id)
		if err != nil {
			return nil, fmt.Errorf("timeblocks: load %d: %w", id, err)
		}
		out["block"] = b
		delete(out, "block_id")
		return out, nil
	}
	trigger := func(id string) scripting.Trigger {
		return scripting.Trigger{
			Meta: meta("trigger", id, "timeblocks"), Params: []scripting.Param{},
			Event:      []scripting.Field{scripting.NewField("block", "block")},
			Permission: permBlocksRead, Prepare: load,
		}
	}
	blockTask := scripting.NewFunction("timeblocks", "block_task", func(c *scripting.Call) ([]any, error) {
		var taskID int64
		switch v := c.Arg(0).(type) {
		case float64:
			taskID = int64(v)
		case map[string]any:
			if id, ok := v["id"].(float64); ok {
				taskID = int64(id)
			}
		}
		if taskID == 0 {
			return nil, fmt.Errorf("%s: argument 1 must be a task or its id", c.Function)
		}
		minutes := 0
		if c.Arg(1) != nil {
			n, err := c.Int(1)
			if err != nil || n < 0 || n > maxBlockMinutes {
				return nil, fmt.Errorf("%s: minutes are 0 to %d", c.Function, maxBlockMinutes)
			}
			minutes = int(n)
		}
		b, err := h.BlockTask(c.Ctx, taskID, minutes)
		if err != nil {
			return []any{nil, err.Error()}, nil
		}
		return []any{b}, nil
	}, scripting.NewParam("task", scripting.TypeRef), scripting.NewParam("minutes", scripting.TypeNumber))
	blockTask.Permission, blockTask.Effect = permBlocksWrite, true
	blockTask.Returns = []scripting.Field{scripting.NewField("block", "block")}
	blockTask.Dry = func(*scripting.Call) ([]any, error) { return []any{map[string]any{"id": 0}}, nil }

	ref := scripting.RefParam("block", "block", "event.block")
	taskRef := scripting.RefParam("task", "task", "event.task")
	minutes := scripting.NewParam("minutes", scripting.TypeNumber)
	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", permBlocksRead, "timeblocks"), Risk: "low"},
			{Meta: meta("perm", permBlocksWrite, "timeblocks"), Risk: "medium"},
		},
		ValueTypes: []scripting.ValueType{{Meta: meta("type", "block", "timeblocks"), Sample: sampleBlock, Fields: []scripting.Field{
			scripting.NewField("id", "number"), scripting.NewField("title", "string"), scripting.NewField("start", "number"),
			scripting.NewField("end", "number"), scripting.NewField("task_id", "number"), scripting.NewField("done", "bool"),
			scripting.NewField("for_mail", "bool"), scripting.NewField("on_calendar", "bool"),
		}}},
		Triggers: []scripting.Trigger{trigger(triggerBlockStarts), trigger(triggerBlockEnds)},
		Modules:  []scripting.Module{{Meta: meta("module", "timeblocks", "timeblocks"), Functions: []scripting.Function{blockTask}}},
		Conditions: []scripting.Condition{
			scripting.TextCondition(meta("cond", "timeblocks.title", "timeblocks"), "{{block}}.title", ref),
			{Meta: meta("cond", "timeblocks.for_task", "timeblocks"), Params: []scripting.Param{ref}, Lua: "({{block}}.task_id or 0) > 0"},
			{Meta: meta("cond", "timeblocks.for_mail", "timeblocks"), Params: []scripting.Param{ref}, Lua: "{{block}}.for_mail == true"},
		},
		Actions: []scripting.Action{{
			Meta: meta("action", "timeblocks.block_task", "timeblocks"), Params: []scripting.Param{taskRef, minutes},
			Outputs: []scripting.Field{scripting.NewField("id", "number")}, Permission: permBlocksWrite,
			Lua: "timeblocks.block_task({{task}}, {{minutes}})",
		}},
	}
}

// registerScriptTimeBlocks adds the time block entries to the scripts'
// registry.
func (a *App) registerScriptTimeBlocks(r *scripting.Registry) error {
	return r.Register(blockScriptEntries(scriptBlocks{app: a}))
}

type scriptBlocks struct {
	app *App
}

// scriptBlock is a block as a script may see it: the title of a block for a
// mail in a vault is left out unless the vault is open to scripts.
func (s scriptBlocks) scriptBlock(b TimeBlockDTO) scripting.Object {
	title := b.Title
	if b.HasMail {
		row, err := s.app.store.GetTimeBlock(s.app.ctx, b.ID)
		switch {
		case err != nil:
			title = ""
		case row.MessageID != 0 && s.app.inMailVaultMessage(row.MessageID):
			if _, err := s.app.store.GetMessage(scriptsCtx(s.app.ctx), row.MessageID); err != nil {
				title = ""
			}
		}
	}
	return blockObject(b, title)
}

func (s scriptBlocks) Block(_ context.Context, id int64) (scripting.Object, error) {
	row, err := s.app.store.GetTimeBlock(s.app.ctx, id)
	if err != nil {
		return scripting.Object{}, err
	}
	bc, err := s.app.newBlockContext()
	if err != nil {
		return scripting.Object{}, err
	}
	return s.scriptBlock(s.app.blockDTO(*row, bc)), nil
}

func (s scriptBlocks) BlockTask(_ context.Context, taskID int64, minutes int) (scripting.Object, error) {
	b, err := s.app.BlockTimeForTask(taskID, minutes)
	if err != nil {
		return scripting.Object{}, err
	}
	return s.scriptBlock(b), nil
}

// scriptBlocksAt reports the blocks that started or ended between the last
// tick and now. A done block starting fires nothing.
func (a *App) scriptBlocksAt(last, now time.Time) {
	if a.scripts == nil || (!a.scripts.Wants(triggerBlockStarts) && !a.scripts.Wants(triggerBlockEnds)) {
		return
	}
	rows, err := a.store.ListTimeBlocks(a.ctx, last.Add(-maxBlockMinutes*time.Minute), now.Add(time.Minute))
	if err != nil {
		a.log.Warn("scripts: list time blocks", "err", err)
		return
	}
	ctx := withScriptBudget(a.ctx)
	for _, b := range rows {
		if !b.Done && b.Start.After(last) && !b.Start.After(now) {
			a.scriptEventBudgeted(ctx, triggerBlockStarts, map[string]any{"block_id": b.ID})
		}
		if b.End.After(last) && !b.End.After(now) {
			a.scriptEventBudgeted(ctx, triggerBlockEnds, map[string]any{"block_id": b.ID})
		}
	}
}
