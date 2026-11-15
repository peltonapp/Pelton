package desktop

import (
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestNotifyAllowedHoldsBackAllButVIPsDuringFocus(t *testing.T) {
	tests := []struct {
		general, vip, focused, want bool
	}{
		{general: true, vip: false, focused: false, want: true},
		{general: false, vip: false, focused: false, want: false},
		{general: true, vip: false, focused: true, want: false},
		{general: false, vip: true, focused: true, want: true},
		{general: true, vip: true, focused: true, want: true},
		{general: false, vip: true, focused: false, want: true},
	}
	for _, tt := range tests {
		if got := notifyAllowed(tt.general, tt.vip, tt.focused); got != tt.want {
			t.Errorf("notifyAllowed(general=%v, vip=%v, focused=%v) = %v, want %v", tt.general, tt.vip, tt.focused, got, tt.want)
		}
	}
}

// focusAt builds a local time on a known week: 2026-11-16 is a Monday.
func focusAt(day, hour, minute int) time.Time {
	return time.Date(2026, time.November, day, hour, minute, 0, 0, time.Local)
}

func TestScheduleWindow(t *testing.T) {
	weekdays := FocusScheduleDTO{Enabled: true, Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "12:00"}
	overnight := FocusScheduleDTO{Enabled: true, Days: []int{5}, Start: "22:00", End: "06:30"}
	tests := []struct {
		name    string
		sched   FocusScheduleDTO
		now     time.Time
		wantIn  bool
		wantEnd time.Time
	}{
		{"monday inside", weekdays, focusAt(16, 10, 0), true, focusAt(16, 12, 0)},
		{"start is inside", weekdays, focusAt(16, 9, 0), true, focusAt(16, 12, 0)},
		{"end is outside", weekdays, focusAt(16, 12, 0), false, time.Time{}},
		{"before start", weekdays, focusAt(16, 8, 59), false, time.Time{}},
		{"sunday is not listed", weekdays, focusAt(15, 10, 0), false, time.Time{}},
		{"disabled", FocusScheduleDTO{Days: []int{1}, Start: "09:00", End: "12:00"}, focusAt(16, 10, 0), false, time.Time{}},
		{"overnight on the start day", overnight, focusAt(20, 23, 0), true, focusAt(21, 6, 30)},
		{"overnight past midnight", overnight, focusAt(21, 3, 0), true, focusAt(21, 6, 30)},
		{"overnight after its end", overnight, focusAt(21, 7, 0), false, time.Time{}},
		{"overnight from an unlisted day", overnight, focusAt(22, 3, 0), false, time.Time{}},
	}
	for _, tt := range tests {
		end, in := scheduleWindow(tt.sched, tt.now)
		if in != tt.wantIn || !end.Equal(tt.wantEnd) {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", tt.name, end, in, tt.wantEnd, tt.wantIn)
		}
	}
}

func TestValidSchedule(t *testing.T) {
	good := FocusScheduleDTO{Days: []int{0, 6}, Start: "00:00", End: "23:59"}
	if !validSchedule(good) {
		t.Errorf("%+v should be valid", good)
	}
	for _, bad := range []FocusScheduleDTO{
		{Days: []int{7}, Start: "09:00", End: "10:00"},
		{Days: []int{1}, Start: "9", End: "10:00"},
		{Days: []int{1}, Start: "24:00", End: "10:00"},
		{Days: []int{1}, Start: "09:00", End: "10:60"},
	} {
		if validSchedule(bad) {
			t.Errorf("%+v should be refused", bad)
		}
	}
}

func TestFocusStepEndsAnElapsedFocusWithItsSummary(t *testing.T) {
	now := focusAt(16, 10, 0)
	st := focusState{Active: true, Until: now.Add(-time.Minute).Format(time.RFC3339), Held: 7, HeldVIP: 2}
	next, ended, sum := focusStep(st, FocusScheduleDTO{}, now)
	if !ended || next.Active {
		t.Fatalf("an elapsed focus should end, got %+v ended=%v", next, ended)
	}
	if sum != (FocusSummaryDTO{Total: 7, VIP: 2}) {
		t.Errorf("summary = %+v", sum)
	}
}

func TestFocusStepKeepsARunningFocus(t *testing.T) {
	now := focusAt(16, 10, 0)
	for _, st := range []focusState{
		{Active: true, Until: now.Add(time.Minute).Format(time.RFC3339), Held: 3},
		{Active: true, Held: 3},
	} {
		next, ended, _ := focusStep(st, FocusScheduleDTO{}, now)
		if ended || next != st {
			t.Errorf("%+v should run on, got %+v ended=%v", st, next, ended)
		}
	}
}

func TestFocusStepStartsAScheduledFocus(t *testing.T) {
	sched := FocusScheduleDTO{Enabled: true, Days: []int{1}, Start: "09:00", End: "12:00"}
	next, ended, _ := focusStep(focusState{}, sched, focusAt(16, 9, 30))
	if ended || !next.Active || !next.Scheduled {
		t.Fatalf("the window should start focus, got %+v", next)
	}
	if end, _ := next.until(); !end.Equal(focusAt(16, 12, 0)) {
		t.Errorf("scheduled focus should end with its window, got %v", end)
	}
}

func TestFocusStepRespectsAWindowEndedByHand(t *testing.T) {
	sched := FocusScheduleDTO{Enabled: true, Days: []int{1}, Start: "09:00", End: "12:00"}
	skip := focusState{SkipUntil: focusAt(16, 12, 0).Format(time.RFC3339)}
	if next, _, _ := focusStep(skip, sched, focusAt(16, 10, 0)); next.Active {
		t.Errorf("a window ended by hand should stay off, got %+v", next)
	}
	// once that window is over the skip is dropped, ready for the next one.
	if next, _, _ := focusStep(skip, sched, focusAt(16, 12, 1)); next.SkipUntil != "" || next.Active {
		t.Errorf("the skip should lapse with its window, got %+v", next)
	}
}

func TestFocusSummaryText(t *testing.T) {
	s := notifyStringsFor("en")
	tests := []struct {
		sum  FocusSummaryDTO
		want string
	}{
		{FocusSummaryDTO{Total: 1}, "1 new message"},
		{FocusSummaryDTO{Total: 1, VIP: 1}, "1 new message, from a VIP"},
		{FocusSummaryDTO{Total: 7}, "7 new messages"},
		{FocusSummaryDTO{Total: 7, VIP: 1}, "7 new messages, 1 from a VIP"},
		{FocusSummaryDTO{Total: 7, VIP: 2}, "7 new messages, 2 from VIPs"},
	}
	for _, tt := range tests {
		if got := focusSummaryText(s, tt.sum); got != tt.want {
			t.Errorf("focusSummaryText(%+v) = %q, want %q", tt.sum, got, tt.want)
		}
	}
	for lang, l := range notifyLocales {
		if l.focusEnded == "" || l.focusOne == "" || l.focusOneVIP == "" || l.focusManyVIP == "" || l.focusManyOneVIP == "" {
			t.Errorf("%s is missing focus summary text", lang)
		}
	}
}

func TestFocusStateSurvivesAndCountsHeldMail(t *testing.T) {
	a := newVIPTestApp(t)

	if f, err := a.GetFocus(); err != nil || f.Active {
		t.Fatalf("focus should start off, got %+v err %v", f, err)
	}
	until := time.Now().Add(25 * time.Minute).UTC().Format(time.RFC3339)
	if _, err := a.StartFocus(until); err != nil {
		t.Fatalf("start: %v", err)
	}
	a.recordHeld(3, 1)
	a.recordHeld(2, 0)

	// a fresh read of the store is what a restart sees.
	f, err := a.GetFocus()
	if err != nil || !f.Active || f.Until != until || f.Held != 5 {
		t.Fatalf("focus after restart = %+v err %v", f, err)
	}
	if !a.focusQuiet() {
		t.Error("the badge should be quiet during focus")
	}
	if err := a.EndFocus(); err != nil {
		t.Fatalf("end: %v", err)
	}
	if f, _ := a.GetFocus(); f.Active || f.Held != 0 {
		t.Errorf("focus should be off and reset, got %+v", f)
	}
	if a.focusQuiet() {
		t.Error("the badge should come back after focus")
	}
}

// new inbox mail during focus is counted for the summary whether or not it
// notifies, and mail already read or outside the inbox is not.
func TestNewMailDuringFocusIsCountedForTheSummary(t *testing.T) {
	a := newTrashTestApp(t)
	accountID := seedAccount(t, a)
	inbox := seedFolder(t, a, accountID, "INBOX", "INBOX", nil, 0)
	other := seedFolder(t, a, accountID, "News", "News", nil, 0)
	if err := a.AddVIPSender("boss@example.test"); err != nil {
		t.Fatal(err)
	}
	insert := func(folderID int64, uid uint32, from string, flags storage.Flag) int64 {
		t.Helper()
		id, err := a.store.InsertMessage(a.ctx, &storage.Message{
			AccountID: accountID, FolderID: folderID, UID: uid, FromAddress: from,
			MessageID: "<" + from + ">", Date: time.Now().UTC(), Flags: flags,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ids := []int64{
		insert(inbox.ID, 1, "boss@example.test", 0),
		insert(inbox.ID, 2, "a@example.test", 0),
		insert(inbox.ID, 3, "b@example.test", 0),
		insert(inbox.ID, 4, "seen@example.test", storage.FlagSeen),
	}
	elsewhere := []int64{insert(other.ID, 5, "c@example.test", 0)}

	// outside focus nothing is counted.
	a.notifyNewMail(inbox, ids)
	if _, err := a.StartFocus(""); err != nil {
		t.Fatal(err)
	}
	a.notifyNewMail(inbox, ids)
	a.notifyNewMail(other, elsewhere)

	st := a.loadFocusState()
	if st.Held != 3 || st.HeldVIP != 1 {
		t.Errorf("held = %d (vip %d), want 3 (vip 1)", st.Held, st.HeldVIP)
	}
}

func TestStartFocusRefusesAPastEnd(t *testing.T) {
	a := newVIPTestApp(t)
	if _, err := a.StartFocus(time.Now().Add(-time.Minute).Format(time.RFC3339)); err == nil {
		t.Error("an end in the past should be refused")
	}
	if _, err := a.StartFocus("soon"); err == nil {
		t.Error("an unparsable end should be refused")
	}
	if f, err := a.StartFocus(""); err != nil || !f.Active || f.Until != "" {
		t.Errorf("an empty end runs until turned off, got %+v err %v", f, err)
	}
}

func TestEndingAScheduledFocusSkipsTheRestOfItsWindow(t *testing.T) {
	a := newVIPTestApp(t)
	now := time.Now()
	day := int(now.Weekday())
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	if start.Day() != now.Day() || end.Day() != now.Day() {
		t.Skip("the window would cross midnight")
	}
	sched := FocusScheduleDTO{Enabled: true, Days: []int{day}, Start: start.Format("15:04"), End: end.Format("15:04")}
	if err := a.SetFocusSchedule(sched); err != nil {
		t.Fatalf("set schedule: %v", err)
	}
	f, _ := a.GetFocus()
	if !f.Active || !f.Scheduled {
		t.Fatalf("an open window should start focus at once, got %+v", f)
	}
	if err := a.EndFocus(); err != nil {
		t.Fatalf("end: %v", err)
	}
	if f, _ := a.GetFocus(); f.Active {
		t.Errorf("the schedule should not restart focus in the window it was ended in, got %+v", f)
	}
	got, err := a.GetFocusSchedule()
	if err != nil || !got.Enabled || len(got.Days) != 1 || got.Start != sched.Start {
		t.Errorf("schedule round trip = %+v err %v", got, err)
	}
}
