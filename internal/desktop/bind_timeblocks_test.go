package desktop

import (
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/caldav/caldavtest"
	"github.com/peltonapp/Pelton/internal/storage"
)

func blockTimes(day string, from, to string) (string, string) {
	start, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+from, time.Local)
	end, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+to, time.Local)
	return start.Format(time.RFC3339), end.Format(time.RFC3339)
}

func listBlocks(t *testing.T, a *App, day string) []TimeBlockDTO {
	t.Helper()
	from, to := blockTimes(day, "00:00", "23:59")
	got, err := a.ListTimeBlocks(from, to)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTimeBlockLifecycleFollowsTask(t *testing.T) {
	a := newCalendarTestApp(t)
	task, err := a.CreateTask(TaskRequest{Title: "Write the report", Due: "2030-01-07", Estimate: 90})
	if err != nil {
		t.Fatal(err)
	}
	if task.Task.Estimate != 90 {
		t.Fatalf("estimate = %d", task.Task.Estimate)
	}
	s1, e1 := blockTimes("2030-01-07", "09:00", "10:00")
	s2, e2 := blockTimes("2030-01-08", "09:00", "09:30")
	made, err := a.CreateTimeBlocks([]TimeBlockRequest{{TaskUID: task.Task.UID, Start: s1, End: e1}, {TaskUID: task.Task.UID, Start: s2, End: e2}})
	if err != nil || len(made) != 2 {
		t.Fatalf("made = %+v, %v", made, err)
	}
	if made[0].Title != "Write the report" || made[0].TaskID != task.Task.ID || made[0].CalendarID != 0 {
		t.Errorf("block = %+v", made[0])
	}
	plans, err := a.ListTaskPlans()
	if err != nil || len(plans) != 1 || plans[0].PlannedMinutes != 90 || plans[0].Future != 2 {
		t.Fatalf("plans = %+v, %v", plans, err)
	}

	if _, err := a.SetTaskDone(task.Task.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, b := range append(listBlocks(t, a, "2030-01-07"), listBlocks(t, a, "2030-01-08")...) {
		if !b.Done {
			t.Errorf("block of a completed task is open: %+v", b)
		}
	}
	if _, err := a.SetTaskDone(task.Task.ID, false); err != nil {
		t.Fatal(err)
	}
	if b := listBlocks(t, a, "2030-01-08"); len(b) != 1 || b[0].Done {
		t.Errorf("block ahead after reopening = %+v", b)
	}

	// a renamed block keeps its own title; sending the task's title back
	// keeps following the task.
	s3, e3 := blockTimes("2030-01-07", "11:00", "12:00")
	moved, err := a.UpdateTimeBlock(TimeBlockRequest{ID: made[0].ID, Title: "Write the report", Start: s3, End: e3})
	if err != nil || moved.Start != s3 {
		t.Fatalf("moved = %+v, %v", moved, err)
	}
	if row, _ := a.store.GetTimeBlock(a.ctx, made[0].ID); row.Title != "" {
		t.Errorf("the task's title was stored: %q", row.Title)
	}
	if _, err := a.UpdateTimeBlock(TimeBlockRequest{ID: made[0].ID, Title: "", Start: e3, End: s3}); err != errBlockTimes {
		t.Errorf("end before start = %v", err)
	}

	if err := a.ReleaseTaskTimeBlocks(task.Task.UID, true); err != nil {
		t.Fatal(err)
	}
	if b := listBlocks(t, a, "2030-01-07"); len(b) != 0 {
		t.Errorf("future block kept after asking to delete: %+v", b)
	}
}

func TestReleaseTaskTimeBlocksKeeps(t *testing.T) {
	a := newCalendarTestApp(t)
	task, err := a.CreateTask(TaskRequest{Title: "Call the bank"})
	if err != nil {
		t.Fatal(err)
	}
	s, e := blockTimes("2030-01-07", "09:00", "10:00")
	if _, err := a.createBlock(TimeBlockRequest{TaskUID: task.Task.UID, Start: s, End: e}); err != nil {
		t.Fatal(err)
	}
	if err := a.ReleaseTaskTimeBlocks(task.Task.UID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteTask(task.Task.ID, false); err != nil {
		t.Fatal(err)
	}
	got := listBlocks(t, a, "2030-01-07")
	if len(got) != 1 || got[0].Title != "Call the bank" || got[0].TaskUID != "" {
		t.Errorf("kept block = %+v", got)
	}
}

// A block written to a calendar stays linked: its moves and deletion reach
// the server, a move on the server is followed, and an event deleted there
// leaves the block marked rather than gone.
func TestTimeBlockCalendarLink(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	a, cal := remoteSetup(t, server)
	task, err := a.CreateTask(TaskRequest{Title: "Secret project plan"})
	if err != nil {
		t.Fatal(err)
	}
	s, e := blockTimes("2030-01-07", "09:00", "10:00")
	b, err := a.createBlock(TimeBlockRequest{TaskUID: task.Task.UID, Start: s, End: e, CalendarID: cal.ID})
	if err != nil {
		t.Fatal(err)
	}
	if b.CalendarID != cal.ID || b.LinkState != "" || b.Pending {
		t.Fatalf("written block = %+v", b)
	}
	row, _ := a.store.GetTimeBlock(a.ctx, b.ID)
	path := ""
	for _, p := range server.Paths() {
		if strings.Contains(p, row.EventUID) {
			path = p
		}
	}
	data, ok := server.Data(path)
	if !ok {
		t.Fatalf("no event on the server: %v", server.Paths())
	}
	if !strings.Contains(data, "SUMMARY:Focus time") || strings.Contains(data, "Secret project") ||
		!strings.Contains(data, "TRANSP:OPAQUE") || !strings.Contains(data, "CLASS:PRIVATE") {
		t.Errorf("event on the server says too much or too little:\n%s", data)
	}

	// moved here: the event moves.
	s2, e2 := blockTimes("2030-01-07", "14:00", "15:30")
	if _, err := a.UpdateTimeBlock(TimeBlockRequest{ID: b.ID, Start: s2, End: e2, CalendarID: cal.ID}); err != nil {
		t.Fatal(err)
	}
	start2, _ := time.Parse(time.RFC3339, s2)
	data, _ = server.Data(path)
	if !strings.Contains(data, "DTSTART:"+start2.UTC().Format("20060102T150405Z")) {
		t.Errorf("event not moved:\n%s", data)
	}

	// the user turns on including the title.
	settings := a.blockSettings()
	settings.IncludeTitle = true
	if err := a.SetTimeBlockSettings(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTimeBlock(TimeBlockRequest{ID: b.ID, Start: s2, End: e2, CalendarID: cal.ID}); err != nil {
		t.Fatal(err)
	}
	if data, _ = server.Data(path); !strings.Contains(data, "SUMMARY:Secret project plan") {
		t.Errorf("title not included:\n%s", data)
	}

	// moved on the server: the block follows after a sync.
	moved := strings.Replace(data, "DTSTART:"+start2.UTC().Format("20060102T150405Z"), "DTSTART:20300107T070000Z", 1)
	moved = strings.Replace(moved, "DTEND:"+start2.Add(90*time.Minute).UTC().Format("20060102T150405Z"), "DTEND:20300107T080000Z", 1)
	server.Put(path, moved)
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	got := listBlocks(t, a, "2030-01-07")
	if len(got) != 1 || got[0].LinkState != "" {
		t.Fatalf("after the server moved it: %+v", got)
	}
	if st, _ := time.Parse(time.RFC3339, got[0].Start); !st.Equal(time.Date(2030, 1, 7, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("block did not follow: %s", got[0].Start)
	}

	// made repeating on the server: out of step, not followed.
	server.Put(path, strings.Replace(moved, "END:VEVENT", "RRULE:FREQ=DAILY;COUNT=3\r\nEND:VEVENT", 1))
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	if got := listBlocks(t, a, "2030-01-07"); len(got) != 1 || got[0].LinkState != storage.BlockLinkOutOfStep {
		t.Fatalf("after the server made it repeat: %+v", got)
	}
	if _, err := a.SettleTimeBlockLink(b.ID, BlockLinkFollow); err != errBlockEventGone {
		t.Errorf("following a repeating event = %v", err)
	}

	// deleted on the server: the block stays, marked.
	server.Remove(path)
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	got = listBlocks(t, a, "2030-01-07")
	if len(got) != 1 || got[0].LinkState != storage.BlockLinkEventGone {
		t.Fatalf("after the server deleted it: %+v", got)
	}

	// written again: a new event.
	again, err := a.SettleTimeBlockLink(b.ID, BlockLinkRewrite)
	if err != nil || again.LinkState != "" {
		t.Fatalf("rewrite = %+v, %v", again, err)
	}
	if len(server.Paths()) != 1 {
		t.Errorf("server holds %v", server.Paths())
	}

	// deleted here: the event goes too.
	if err := a.DeleteTimeBlocks([]int64{b.ID}); err != nil {
		t.Fatal(err)
	}
	if len(server.Paths()) != 0 {
		t.Errorf("event kept after the block was deleted: %v", server.Paths())
	}
}

// With the server out of reach the block is made and its event waits in the
// queue; the next sync writes it.
func TestTimeBlockCalendarOffline(t *testing.T) {
	server := caldavtest.NewServer("me", "secret")
	a, cal := remoteSetup(t, server)
	server.Offline = true
	s, e := blockTimes("2030-01-07", "09:00", "10:00")
	b, err := a.createBlock(TimeBlockRequest{Title: "Deep work", Start: s, End: e, CalendarID: cal.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Pending || b.LinkState != "" {
		t.Fatalf("offline block = %+v", b)
	}
	server.Offline = false
	if err := a.syncAllCalendars(a.ctx); err != nil {
		t.Fatal(err)
	}
	if got := listBlocks(t, a, "2030-01-07"); len(got) != 1 || got[0].Pending || len(server.Paths()) != 1 {
		t.Errorf("after the sync: %+v on %v", got, server.Paths())
	}
}

// A block for a vault mail never shows the mail's subject or sender while
// the vault is locked, and never puts it in a calendar event unless the block
// itself asks for its title.
func TestTimeBlockVaultMail(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatalf("unlock: %+v", res)
	}
	settings := a.blockSettings()
	settings.IncludeTitle = true
	if err := a.SetTimeBlockSettings(settings); err != nil {
		t.Fatal(err)
	}
	cal := localCalendar(t, a)
	s, e := blockTimes("2030-01-07", "09:00", "09:30")
	made, err := a.BlockTimeForMessages([]int64{f.sealedID, f.clearID}, s)
	if err != nil || len(made) != 2 {
		t.Fatalf("made = %+v, %v", made, err)
	}
	if made[0].Title != vaultMarkSubject || made[0].MessageID != f.sealedID || made[1].Title != "Lunch" {
		t.Errorf("unlocked blocks = %+v", made)
	}
	for _, id := range []int64{made[0].ID, made[1].ID} {
		row, _ := a.store.GetTimeBlock(a.ctx, id)
		if strings.Contains(row.Title, vaultMarkSubject) {
			t.Errorf("the vault mail's subject was stored: %+v", row)
		}
	}
	if _, err := a.UpdateTimeBlock(TimeBlockRequest{ID: made[0].ID, Title: vaultMarkSubject, Start: s, End: e, CalendarID: cal}); err != nil {
		t.Fatal(err)
	}
	if row, _ := a.store.GetTimeBlock(a.ctx, made[0].ID); row.Title != "" {
		t.Errorf("sending the shown subject back stored it: %q", row.Title)
	}
	summary := func() string {
		t.Helper()
		occs := occurrences(t, a, "2030-01-06T00:00:00Z", "2030-01-09T00:00:00Z")
		if len(occs) != 1 {
			t.Fatalf("occurrences = %+v", occs)
		}
		return occs[0].Summary
	}
	if got := summary(); got != "Focus time" {
		t.Errorf("the include-title setting put a vault mail's subject in the event: %q", got)
	}

	a.LockAllMailVaults()
	got := listBlocks(t, a, "2030-01-07")
	if len(got) != 2 {
		t.Fatalf("blocks while locked = %+v", got)
	}
	locked := got[0]
	if !locked.Masked || locked.Title != "" || locked.MessageID != 0 || locked.FolderID != 0 || !locked.HasMail {
		t.Errorf("locked vault block says too much: %+v", locked)
	}
	if got[1].Masked || got[1].Title != "Lunch" {
		t.Errorf("block of a mail outside the vault = %+v", got[1])
	}
	// asking for its own title while locked still writes the generic one.
	if _, err := a.UpdateTimeBlock(TimeBlockRequest{ID: locked.ID, Start: s, End: e, CalendarID: cal, EventTitle: blockEventTitleOwn}); err != nil {
		t.Fatal(err)
	}
	if got := summary(); got != "Focus time" {
		t.Errorf("event while locked = %q", got)
	}
	// unlocked and asked for: the user said so.
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatalf("unlock: %+v", res)
	}
	if _, err := a.UpdateTimeBlock(TimeBlockRequest{ID: locked.ID, Start: s, End: e, CalendarID: cal, EventTitle: blockEventTitleOwn}); err != nil {
		t.Fatal(err)
	}
	if got := summary(); got != vaultMarkSubject {
		t.Errorf("event when asked for its title = %q", got)
	}
	a.LockAllMailVaults()
}

// Finding time and planning a day read the calendar, the blocks, the
// working hours and the estimates.
func TestFindTimeAndPlanDay(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	evStart, evEnd := blockTimes("2030-01-07", "09:00", "10:30")
	if _, err := a.CreateEvent(EventRequest{CalendarID: cal, Summary: "Standup", Start: evStart, End: evEnd}); err != nil {
		t.Fatal(err)
	}
	free, freeEnd := blockTimes("2030-01-07", "11:30", "12:30")
	if _, err := a.CreateEvent(EventRequest{CalendarID: cal, Summary: "Free time", Start: free, End: freeEnd, Transparency: "TRANSPARENT"}); err != nil {
		t.Fatal(err)
	}
	settings := a.blockSettings()
	settings.BufferMinutes = 0
	if err := a.SetTimeBlockSettings(settings); err != nil {
		t.Fatal(err)
	}
	report, err := a.CreateTask(TaskRequest{Title: "Report", Due: "2030-01-07", Estimate: 60, Priority: 5})
	if err != nil {
		t.Fatal(err)
	}
	urgent, err := a.CreateTask(TaskRequest{Title: "Urgent", Due: "2030-01-07", Estimate: 30, Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(TaskRequest{Title: "Later", Due: "2030-01-09", Estimate: 30}); err != nil {
		t.Fatal(err)
	}

	from, _ := blockTimes("2030-01-07", "00:00", "00:00")
	slots, err := a.FindFreeSlots(SlotRequest{TaskID: report.Task.ID, From: from})
	if err != nil || len(slots) == 0 {
		t.Fatalf("slots = %+v, %v", slots, err)
	}
	wantStart, wantEnd := blockTimes("2030-01-07", "10:30", "11:30")
	if slots[0].Start != wantStart || slots[0].End != wantEnd || slots[0].AfterDue {
		t.Errorf("first slot = %+v, want %s to %s", slots[0], wantStart, wantEnd)
	}
	for _, s := range slots {
		st, _ := time.Parse(time.RFC3339, s.Start)
		if st.In(time.Local).Day() > 7 && !s.AfterDue {
			t.Errorf("slot after the due day not marked: %+v", s)
		}
	}

	plan, err := a.PlanDay("2030-01-07")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tasks != 2 || len(plan.Proposals) != 2 || len(plan.Unplaced) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	u0, u1 := blockTimes("2030-01-07", "10:30", "11:00")
	r0, r1 := blockTimes("2030-01-07", "11:00", "12:00")
	got := plan.Proposals
	if got[0].TaskID != urgent.Task.ID || got[0].Start != u0 || got[0].End != u1 || got[1].TaskID != report.Task.ID || got[1].Start != r0 || got[1].End != r1 {
		t.Errorf("proposals = %+v", got)
	}

	// what is planned already is not planned again.
	if _, err := a.createBlock(TimeBlockRequest{TaskUID: urgent.Task.UID, Start: u0, End: u1}); err != nil {
		t.Fatal(err)
	}
	plan, err = a.PlanDay("2030-01-07")
	if err != nil || len(plan.Proposals) != 1 || plan.Proposals[0].TaskID != report.Task.ID || plan.Proposals[0].Start != r0 {
		t.Errorf("plan with a block = %+v, %v", plan, err)
	}
}
