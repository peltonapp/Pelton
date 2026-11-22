package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func at(h, m int) time.Time {
	return time.Date(2026, 3, 10, h, m, 0, 0, time.UTC)
}

func TestTimeBlockRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	cals, err := db.ListCalendars(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b := TimeBlock{Title: "Write report", TitleOwn: true, Start: at(9, 0), End: at(10, 30), TaskUID: "task-1",
		MessageID: 7, MessageHeader: "abc@example.org", CalendarID: cals[0].ID, EventUID: "ev-1", EventTitle: "generic"}
	if _, err := db.CreateTimeBlock(ctx, &b); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetTimeBlock(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != b.Title || !got.Start.Equal(b.Start) || !got.End.Equal(b.End) || got.TaskUID != "task-1" ||
		got.MessageID != 7 || got.MessageHeader != "abc@example.org" || got.CalendarID != cals[0].ID ||
		got.EventUID != "ev-1" || got.EventTitle != "generic" || got.Done || got.LinkState != BlockLinkOK {
		t.Fatalf("stored block = %+v", got)
	}
	got.Done, got.LinkState, got.End = true, BlockLinkOutOfStep, at(11, 0)
	if err := db.UpdateTimeBlock(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := db.GetTimeBlock(ctx, b.ID)
	if !again.Done || again.LinkState != BlockLinkOutOfStep || !again.End.Equal(at(11, 0)) {
		t.Errorf("updated block = %+v", again)
	}
	if err := db.DeleteTimeBlock(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetTimeBlock(ctx, b.ID); !errors.Is(err, ErrTimeBlockNotFound) {
		t.Errorf("get after delete = %v", err)
	}
	if err := db.DeleteTimeBlock(ctx, b.ID); !errors.Is(err, ErrTimeBlockNotFound) {
		t.Errorf("second delete = %v", err)
	}
}

// Only a title the user typed is stored; one copied from a mail or a task is
// dropped on the way in, so the row never holds it.
func TestTimeBlockStoresOnlyOwnTitles(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	copied := TimeBlock{Title: "Invoice 42 from Ada", Start: at(9, 0), End: at(10, 0), MessageID: 3, MessageHeader: "m@x"}
	if _, err := db.CreateTimeBlock(ctx, &copied); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetTimeBlock(ctx, copied.ID)
	if got.Title != "" || got.TitleOwn {
		t.Fatalf("copied title stored: %+v", got)
	}
	got.Title = "Pay it"
	got.TitleOwn = true
	if err := db.UpdateTimeBlock(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.GetTimeBlock(ctx, copied.ID); again.Title != "Pay it" || !again.TitleOwn {
		t.Errorf("own title = %+v", again)
	}
	got.Title, got.TitleOwn = "Invoice 42 from Ada", false
	if err := db.UpdateTimeBlock(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.GetTimeBlock(ctx, copied.ID); again.Title != "" {
		t.Errorf("a title not the user's own was kept: %q", again.Title)
	}
}

// 0119 clears the titles copied from mail into blocks made before it and
// keeps the ones on blocks with nothing to copy from.
func TestOwnTitleMigrationClearsCopiedTitles(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "partial.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ensureMigrationsTable(ctx); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version <= 118 {
			if err := db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, q := range []string{
		`INSERT INTO time_blocks (id, title, start_at, end_at, message_id, message_header) VALUES (1, 'Invoice from Ada', 'a', 'b', 7, 'x@y')`,
		`INSERT INTO time_blocks (id, title, start_at, end_at, task_uid) VALUES (2, 'Renamed', 'a', 'b', 't1')`,
		`INSERT INTO time_blocks (id, title, start_at, end_at) VALUES (3, 'Deep work', 'a', 'b')`,
	} {
		if _, err := db.sql.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[int64]struct {
		title string
		own   int
	}{1: {"", 0}, 2: {"", 0}, 3: {"Deep work", 1}}
	for id, w := range want {
		var title string
		var own int
		if err := db.sql.QueryRowContext(ctx, `SELECT title, title_own FROM time_blocks WHERE id = ?`, id).Scan(&title, &own); err != nil {
			t.Fatal(err)
		}
		if title != w.title || own != w.own {
			t.Errorf("block %d: title %q own %d, want %q %d", id, title, own, w.title, w.own)
		}
	}
}

func TestListTimeBlocksByOverlap(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for _, b := range []TimeBlock{
		{Title: "before", TitleOwn: true, Start: at(7, 0), End: at(8, 0)},
		{Title: "touching start", TitleOwn: true, Start: at(8, 0), End: at(9, 0)},
		{Title: "inside", TitleOwn: true, Start: at(9, 30), End: at(10, 0)},
		{Title: "across end", TitleOwn: true, Start: at(11, 30), End: at(12, 30)},
		{Title: "after", TitleOwn: true, Start: at(12, 0), End: at(13, 0)},
	} {
		if _, err := db.CreateTimeBlock(ctx, &b); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListTimeBlocks(ctx, at(9, 0), at(12, 0))
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, b := range got {
		titles = append(titles, b.Title)
	}
	if len(titles) != 2 || titles[0] != "inside" || titles[1] != "across end" {
		t.Errorf("blocks in window = %v", titles)
	}
}

func TestTaskTimeBlocksDoneAndUnlink(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	past := TimeBlock{Start: at(8, 0), End: at(9, 0), TaskUID: "t"}
	future := TimeBlock{Start: at(14, 0), End: at(15, 0), TaskUID: "t", Title: "Own title", TitleOwn: true}
	other := TimeBlock{Start: at(14, 0), End: at(15, 0), TaskUID: "u"}
	for _, b := range []*TimeBlock{&past, &future, &other} {
		if _, err := db.CreateTimeBlock(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := db.SetTaskTimeBlocksDone(ctx, "t", true, time.Time{}); err != nil || n != 2 {
		t.Fatalf("done = %d, %v", n, err)
	}
	if n, err := db.SetTaskTimeBlocksDone(ctx, "t", false, at(12, 0)); err != nil || n != 1 {
		t.Fatalf("reopened = %d, %v", n, err)
	}
	p, _ := db.GetTimeBlock(ctx, past.ID)
	f, _ := db.GetTimeBlock(ctx, future.ID)
	o, _ := db.GetTimeBlock(ctx, other.ID)
	if !p.Done || f.Done || o.Done {
		t.Errorf("done after reopen: past %v future %v other %v", p.Done, f.Done, o.Done)
	}
	list, err := db.TaskTimeBlocks(ctx, []string{"t"})
	if err != nil || len(list) != 2 {
		t.Fatalf("task blocks = %v, %v", list, err)
	}
	all, err := db.AllTaskTimeBlocks(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("all task blocks = %d, %v", len(all), err)
	}
	if err := db.UnlinkTaskTimeBlocks(ctx, "t", "Task title"); err != nil {
		t.Fatal(err)
	}
	p, _ = db.GetTimeBlock(ctx, past.ID)
	f, _ = db.GetTimeBlock(ctx, future.ID)
	if p.TaskUID != "" || p.Title != "Task title" || f.Title != "Own title" {
		t.Errorf("unlinked: past %+v future %+v", p, f)
	}
}

// A block written to a calendar keeps its event's UID when the calendar
// goes, so it can say its event is gone instead of quietly losing the link.
func TestLinkedTimeBlockOutlivesCalendar(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	cal := Calendar{Name: "Work", Visible: true}
	if _, err := db.CreateCalendar(ctx, &cal); err != nil {
		t.Fatal(err)
	}
	b := TimeBlock{Start: at(9, 0), End: at(10, 0), CalendarID: cal.ID, EventUID: "ev"}
	if _, err := db.CreateTimeBlock(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteCalendar(ctx, cal.ID); err != nil {
		t.Fatal(err)
	}
	linked, err := db.LinkedTimeBlocks(ctx)
	if err != nil || len(linked) != 1 {
		t.Fatalf("linked = %v, %v", linked, err)
	}
	if linked[0].CalendarID != 0 || linked[0].EventUID != "ev" {
		t.Errorf("after the calendar went: %+v", linked[0])
	}
}
