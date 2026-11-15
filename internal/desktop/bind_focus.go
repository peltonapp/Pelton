package desktop

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Focus mode: a stretch of time in which new-mail notifications are held back,
// except from VIP senders, and the unread badge on the dock or tray icon is
// hidden. Mail keeps syncing; only the signals wait. The decision is made here,
// where notifications are raised, so it holds with the window closed to the
// tray. The state lives in the settings table and survives a restart. When
// focus ends, one summary notification and an in-app toast say what arrived.

const (
	// settingFocusState holds the running focus as focusState json.
	settingFocusState = "focus_state"
	// settingFocusSchedule holds the days and hours focus starts on its own, as
	// FocusScheduleDTO json.
	settingFocusSchedule = "focus_schedule"
)

// focusState is the stored state of focus mode.
type focusState struct {
	Active bool `json:"active"`
	// Until is when focus ends, in RFC 3339. Empty means it runs until the
	// user turns it off.
	Until string `json:"until"`
	// Scheduled is set when the schedule started this focus.
	Scheduled bool `json:"scheduled"`
	// Held counts the new inbox messages that arrived during focus, and HeldVIP
	// those of them from VIP senders.
	Held    int `json:"held"`
	HeldVIP int `json:"heldVip"`
	// SkipUntil is the end of a scheduled window the user ended early, so the
	// schedule does not start focus again before that window is over.
	SkipUntil string `json:"skipUntil"`
}

// FocusDTO is focus mode as the frontend sees it.
type FocusDTO struct {
	Active bool `json:"active"`
	// Until is when focus ends, in RFC 3339, or empty for "until I turn it off".
	Until string `json:"until"`
	// Scheduled is true when the schedule started this focus.
	Scheduled bool `json:"scheduled"`
	// Held is how many new messages arrived so far during this focus.
	Held int `json:"held"`
}

// FocusScheduleDTO is when focus starts on its own: on the given weekdays
// (0 is Sunday) from Start to End, both "HH:MM" in local time. An End at or
// before Start runs past midnight into the next day.
type FocusScheduleDTO struct {
	Enabled bool   `json:"enabled"`
	Days    []int  `json:"days"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// FocusSummaryDTO is the payload of EventFocusEnded: how many new messages
// arrived during the focus that just ended, and how many were from VIPs.
type FocusSummaryDTO struct {
	Total int `json:"total"`
	VIP   int `json:"vip"`
}

// defaultFocusSchedule is a working day, Monday to Friday, off until enabled.
func defaultFocusSchedule() FocusScheduleDTO {
	return FocusScheduleDTO{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "12:00"}
}

// notifyAllowed is the hold-back decision for one new message: a VIP sender
// always notifies, anyone else only with general notifications on and focus off.
func notifyAllowed(general, vip, focused bool) bool {
	if vip {
		return true
	}
	return general && !focused
}

// until parses the end time, reporting false for "until I turn it off".
func (s focusState) until() (time.Time, bool) {
	if s.Until == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s.Until)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// activeAt reports whether focus is on at now, counting a passed end time as off.
func (s focusState) activeAt(now time.Time) bool {
	if !s.Active {
		return false
	}
	end, ok := s.until()
	return !ok || now.Before(end)
}

// parseClock reads "HH:MM" into minutes past midnight.
func parseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// validSchedule reports whether a schedule can be stored.
func validSchedule(s FocusScheduleDTO) bool {
	if _, ok := parseClock(s.Start); !ok {
		return false
	}
	if _, ok := parseClock(s.End); !ok {
		return false
	}
	for _, d := range s.Days {
		if d < 0 || d > 6 {
			return false
		}
	}
	return true
}

// scheduleWindow reports whether now falls inside one of the schedule's
// windows, and when that window ends. A window belongs to the day it starts
// on, so one running past midnight is found from the day before as well.
func scheduleWindow(s FocusScheduleDTO, now time.Time) (time.Time, bool) {
	if !s.Enabled || len(s.Days) == 0 {
		return time.Time{}, false
	}
	start, ok1 := parseClock(s.Start)
	end, ok2 := parseClock(s.End)
	if !ok1 || !ok2 || start == end {
		return time.Time{}, false
	}
	length := end - start
	if length <= 0 {
		length += 24 * 60
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for back := 0; back <= 1; back++ {
		day := today.AddDate(0, 0, -back)
		if !slices.Contains(s.Days, int(day.Weekday())) {
			continue
		}
		from := day.Add(time.Duration(start) * time.Minute)
		to := from.Add(time.Duration(length) * time.Minute)
		if !now.Before(from) && now.Before(to) {
			return to, true
		}
	}
	return time.Time{}, false
}

// focusStep moves the stored state on to now: an elapsed focus ends, and the
// schedule starts one when a window is open. ended is true when this step
// ended a focus, with the summary of what arrived during it.
func focusStep(st focusState, sched FocusScheduleDTO, now time.Time) (next focusState, ended bool, summary FocusSummaryDTO) {
	next = st
	if st.Active && !st.activeAt(now) {
		summary = FocusSummaryDTO{Total: st.Held, VIP: st.HeldVIP}
		next = focusState{SkipUntil: st.SkipUntil}
		ended = true
	}
	if skip, err := time.Parse(time.RFC3339, next.SkipUntil); next.SkipUntil != "" && (err != nil || !now.Before(skip)) {
		next.SkipUntil = ""
	}
	if !next.Active {
		if end, ok := scheduleWindow(sched, now); ok && next.SkipUntil == "" {
			next = focusState{Active: true, Until: end.Format(time.RFC3339), Scheduled: true}
		}
	}
	return next, ended, summary
}

// focusSummaryText words the summary notification's body.
func focusSummaryText(s notifyStrings, sum FocusSummaryDTO) string {
	switch {
	case sum.Total == 1 && sum.VIP == 1:
		return s.focusOneVIP
	case sum.Total == 1:
		return s.focusOne
	case sum.VIP == 1:
		return fmt.Sprintf(s.focusManyOneVIP, sum.Total)
	case sum.VIP > 1:
		return fmt.Sprintf(s.focusManyVIP, sum.Total, sum.VIP)
	default:
		return fmt.Sprintf(s.summary, sum.Total)
	}
}

func (a *App) loadFocusState() focusState {
	var st focusState
	_ = a.store.GetJSON(a.ctx, settingFocusState, &st)
	return st
}

func (a *App) loadFocusSchedule() FocusScheduleDTO {
	sched := defaultFocusSchedule()
	_ = a.store.GetJSON(a.ctx, settingFocusSchedule, &sched)
	return sched
}

func (s focusState) dto() FocusDTO {
	return FocusDTO{Active: s.Active, Until: s.Until, Scheduled: s.Scheduled, Held: s.Held}
}

// advanceFocus applies focusStep to the stored state and announces any change.
// It runs on the snooze poller's ticker and before every hold-back decision.
func (a *App) advanceFocus(now time.Time) focusState {
	a.focusMu.Lock()
	st := a.loadFocusState()
	next, ended, summary := focusStep(st, a.loadFocusSchedule(), now)
	changed := next != st
	if changed {
		if err := a.store.SetJSON(a.ctx, settingFocusState, next); err != nil {
			a.log.Warn("focus: save state", "err", err)
		}
	}
	a.focusMu.Unlock()
	if ended {
		a.focusEnded(summary)
	}
	if changed {
		a.focusChanged(next)
	}
	return next
}

// focusChanged tells the frontend and re-applies the unread badge.
func (a *App) focusChanged(st focusState) {
	a.emit(EventFocusChanged, st.dto())
	a.applyUnreadBadge()
}

// focusEnded raises the summary: a toast in the app, always, and one OS
// notification when something arrived and new-mail notifications are on.
func (a *App) focusEnded(sum FocusSummaryDTO) {
	a.emit(EventFocusEnded, sum)
	if sum.Total == 0 || !a.boolSetting(settingNotifyNewMail, false) {
		return
	}
	s := notifyStringsFor(a.stringSetting(settingLanguage, "en"))
	a.sendNotification(notification{title: s.focusEnded, body: focusSummaryText(s, sum)})
}

// focusQuiet reports whether focus is on right now, for the unread badge.
func (a *App) focusQuiet() bool {
	if a.store == nil {
		return false
	}
	return a.loadFocusState().activeAt(time.Now())
}

// recordHeld adds new messages that arrived during focus to its summary.
func (a *App) recordHeld(total, vip int) {
	a.focusMu.Lock()
	defer a.focusMu.Unlock()
	st := a.loadFocusState()
	if !st.Active {
		return
	}
	st.Held += total
	st.HeldVIP += vip
	if err := a.store.SetJSON(a.ctx, settingFocusState, st); err != nil {
		a.log.Warn("focus: save held count", "err", err)
	}
}

// GetFocus returns focus mode as it stands now.
func (a *App) GetFocus() (FocusDTO, error) {
	if err := a.ready(); err != nil {
		return FocusDTO{}, err
	}
	return a.advanceFocus(time.Now()).dto(), nil
}

// StartFocus turns focus on until the given RFC 3339 time, or until it is
// turned off when untilRFC3339 is empty. Starting while focus is on changes
// its end and keeps the count of what arrived so far.
func (a *App) StartFocus(untilRFC3339 string) (FocusDTO, error) {
	if err := a.ready(); err != nil {
		return FocusDTO{}, err
	}
	now := time.Now()
	until := ""
	if untilRFC3339 != "" {
		t, err := time.Parse(time.RFC3339, untilRFC3339)
		if err != nil {
			return FocusDTO{}, fmt.Errorf("pelton: invalid focus end %q: %w", untilRFC3339, err)
		}
		if !t.After(now) {
			return FocusDTO{}, fmt.Errorf("pelton: focus end %q is not in the future", untilRFC3339)
		}
		until = t.UTC().Format(time.RFC3339)
	}
	a.focusMu.Lock()
	st := a.loadFocusState()
	next := focusState{Active: true, Until: until, SkipUntil: st.SkipUntil}
	if st.activeAt(now) {
		next.Held, next.HeldVIP = st.Held, st.HeldVIP
	}
	err := a.store.SetJSON(a.ctx, settingFocusState, next)
	a.focusMu.Unlock()
	if err != nil {
		return FocusDTO{}, err
	}
	a.focusChanged(next)
	return next.dto(), nil
}

// EndFocus turns focus off now and raises the summary. Ending a scheduled
// focus keeps the schedule from starting it again until its window is over.
func (a *App) EndFocus() error {
	if err := a.ready(); err != nil {
		return err
	}
	a.focusMu.Lock()
	st := a.loadFocusState()
	if !st.Active {
		a.focusMu.Unlock()
		return nil
	}
	next := focusState{SkipUntil: st.SkipUntil}
	if st.Scheduled {
		next.SkipUntil = st.Until
	}
	err := a.store.SetJSON(a.ctx, settingFocusState, next)
	a.focusMu.Unlock()
	if err != nil {
		return err
	}
	a.focusEnded(FocusSummaryDTO{Total: st.Held, VIP: st.HeldVIP})
	a.focusChanged(next)
	return nil
}

// GetFocusSchedule returns the days and hours focus starts on its own.
func (a *App) GetFocusSchedule() (FocusScheduleDTO, error) {
	if err := a.ready(); err != nil {
		return FocusScheduleDTO{}, err
	}
	sched := a.loadFocusSchedule()
	if sched.Days == nil {
		sched.Days = []int{}
	}
	return sched, nil
}

// SetFocusSchedule stores the schedule and applies it straight away, so a
// window that is already open starts focus without waiting for the ticker.
func (a *App) SetFocusSchedule(sched FocusScheduleDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !validSchedule(sched) {
		return fmt.Errorf("pelton: invalid focus schedule")
	}
	slices.Sort(sched.Days)
	sched.Days = slices.Compact(sched.Days)
	if err := a.store.SetJSON(a.ctx, settingFocusSchedule, sched); err != nil {
		return err
	}
	a.advanceFocus(time.Now())
	return nil
}
