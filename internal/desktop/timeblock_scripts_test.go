package desktop

import (
	"context"
	"slices"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

type fakeBlockHost struct {
	blocked []int64
}

func (fakeBlockHost) Block(_ context.Context, id int64) (scripting.Object, error) {
	return blockObject(TimeBlockDTO{ID: id, Title: "Report", TaskID: 4}, "Report"), nil
}

func (f *fakeBlockHost) BlockTask(_ context.Context, taskID int64, minutes int) (scripting.Object, error) {
	f.blocked = append(f.blocked, taskID, int64(minutes))
	return blockObject(TimeBlockDTO{ID: 9, TaskID: taskID}, ""), nil
}

func TestBlockScriptEntriesAreLocalized(t *testing.T) {
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error {
		return r.Register(blockScriptEntries(&fakeBlockHost{}))
	})
	scriptingtest.Localized(t, r)
}

func TestBlockScriptFunction(t *testing.T) {
	host := &fakeBlockHost{}
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error {
		return r.Register(blockScriptEntries(host))
	})
	src := `local b = timeblocks.block_task(4, 45)
print(tostring(b.task_id))
timeblocks.block_task({id = 5})`
	run := scriptingtest.Run(t, r, scriptingtest.Script(1, src, permBlocksWrite), false)
	if run.Error != "" {
		t.Fatalf("run failed: %s", run.Error)
	}
	if got := scriptingtest.Logged(run); !slices.Equal(got, []string{"4"}) {
		t.Errorf("printed %v", got)
	}
	if !slices.Equal(host.blocked, []int64{4, 45, 5, 0}) {
		t.Errorf("blocked %v", host.blocked)
	}
	host.blocked = nil
	run = scriptingtest.Run(t, r, scriptingtest.Script(2, `timeblocks.block_task(4)`, permBlocksRead), false)
	if run.Error == "" || len(host.blocked) != 0 {
		t.Errorf("blocked without the permission: %v, %q", host.blocked, run.Error)
	}
}
