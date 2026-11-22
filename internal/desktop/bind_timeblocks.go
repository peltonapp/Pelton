// bind_timeblocks.go is time blocking: blocks of time set aside in the
// calendar for a task or a mail. A block lives in this database. When the
// user asks, it is also written to one of their calendars as a busy event
// and stays linked to it: moving or deleting the block reaches the event,
// and a change to the event on the server is followed or, when it cannot be,
// marked as out of step for the user to settle.
package desktop

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/timeblock"
)

const (
	settingBlockMinutes      = "timeblock_default_minutes"
	settingBlockMinSlot      = "timeblock_min_slot_minutes"
	settingBlockBuffer       = "timeblock_buffer_minutes"
	settingBlockWorkDays     = "timeblock_work_days"
	settingBlockWrite        = "timeblock_write_calendar"
	settingBlockCalendar     = "timeblock_calendar_id"
	settingBlockEventTitle   = "timeblock_event_title"
	settingBlockIncludeTitle = "timeblock_include_title"
	settingBlockSearchDays   = "timeblock_search_days"
)

// The defaults of the time blocking settings.
const (
	defaultBlockMinutes    = 30
	defaultBlockMinSlot    = 15
	defaultBlockBuffer     = 5
	defaultBlockSearchDays = 7
	defaultBlockEventTitle = "Focus time"
	defaultBlockWorkDays   = "1,2,3,4,5"
	blockStep              = 15 * time.Minute
	maxBlockSearchDays     = 31
	maxBlockMinutes        = 12 * 60
	slotsOffered           = 6
	slotsPerDay            = 2
)

// The ways a block's event is named.
const (
	blockEventTitleFollow  = ""
	blockEventTitleGeneric = "generic"
	blockEventTitleOwn     = "title"
)

// The ways an out-of-step link is settled.
const (
	BlockLinkFollow  = "follow"
	BlockLinkRewrite = "rewrite"
	BlockLinkForget  = "forget"
)

var (
	errBlockTimes      = errors.New("pelton: a time block needs a start before its end")
	errBlockTooLong    = errors.New("pelton: a time block can be at most twelve hours")
	errBlockSettings   = errors.New("pelton: these time blocking settings are not valid")
	errBlockNoFreeTime = errors.New("pelton: no free time was found in the days looked at")
	errBlockLinkAction = errors.New("pelton: settle a link with follow, rewrite or forget")
	errBlockNotLinked  = errors.New("pelton: this block is not written to a calendar")
	errBlockEventGone  = errors.New("pelton: the calendar event of this block is gone")
)

// TimeBlockDTO is one block for the calendar. Start and End are rfc3339 in
// the user's zone.
type TimeBlockDTO struct {
	ID int64 `json:"id"`
	// Title is what the block is called: its own title, its task's or its
	// mail's subject. It is empty while Masked.
	Title string `json:"title"`
	Start string `json:"start"`
	End   string `json:"end"`
	// TaskID and TaskUID name the task the block is for; TaskID is 0 when the
	// task is not found any more.
	TaskID  int64  `json:"taskId"`
	TaskUID string `json:"taskUid"`
	// MessageID, AccountID and FolderID say where the mail the block is for
	// lives, for showing it in Mail; all 0 when there is none or it cannot be
	// shown.
	MessageID int64 `json:"messageId"`
	AccountID int64 `json:"accountId"`
	FolderID  int64 `json:"folderId"`
	// HasMail is true for a block made for a mail, even one that cannot be
	// shown now.
	HasMail bool `json:"hasMail"`
	// Masked is true while the block's mail is in a locked vault: the block
	// shows as busy and says nothing about the mail.
	Masked bool `json:"masked"`
	Done   bool `json:"done"`
	// CalendarID is the calendar the block is written to, 0 for a block kept
	// on this device only.
	CalendarID int64 `json:"calendarId"`
	// EventTitle is how the event is named: "" follows the setting,
	// "generic" or "title".
	EventTitle string `json:"eventTitle"`
	// LinkState is "", "out_of_step" or "event_gone".
	LinkState string `json:"linkState"`
	// Pending is true while the event change waits to reach the server.
	Pending bool `json:"pending"`
}

// TimeBlockRequest makes or changes a block. An ID of 0 makes one; the task
// and the mail are only read then. CalendarID 0 keeps the block on this
// device only.
type TimeBlockRequest struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Start      string `json:"start"`
	End        string `json:"end"`
	TaskUID    string `json:"taskUid"`
	MessageID  int64  `json:"messageId"`
	Done       bool   `json:"done"`
	CalendarID int64  `json:"calendarId"`
	EventTitle string `json:"eventTitle"`
}

// TimeBlockSettingsDTO holds the time blocking preferences. The working
// hours are the calendar's.
type TimeBlockSettingsDTO struct {
	// DefaultMinutes is how long a block lasts when its task has no estimate.
	DefaultMinutes int `json:"defaultMinutes"`
	// MinSlotMinutes is the shortest free time worth a block.
	MinSlotMinutes int `json:"minSlotMinutes"`
	// BufferMinutes is kept free between blocks and around events.
	BufferMinutes int `json:"bufferMinutes"`
	// WorkDays are the days of the week with working hours, 0 for Sunday.
	WorkDays []int `json:"workDays"`
	// SearchDays is how many days "Find time" looks at.
	SearchDays int `json:"searchDays"`
	// WriteToCalendar writes new blocks to CalendarID as busy events.
	WriteToCalendar bool  `json:"writeToCalendar"`
	CalendarID      int64 `json:"calendarId"`
	// EventTitle is what a written event is called unless IncludeTitle is on.
	EventTitle   string `json:"eventTitle"`
	IncludeTitle bool   `json:"includeTitle"`
}

// SlotRequest asks for free time: for a task (its estimate, what is still
// unplanned and its due date count) or for Minutes when TaskID is 0. From is
// rfc3339, empty for now.
type SlotRequest struct {
	TaskID  int64  `json:"taskId"`
	Minutes int    `json:"minutes"`
	From    string `json:"from"`
}

// SlotDTO is a proposed time. AfterDue is true when it ends after the task's
// due date.
type SlotDTO struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	AfterDue bool   `json:"afterDue"`
}

// BlockProposalDTO is one block of a day plan, not stored. Part and Parts
// number the pieces of a task that had to be split.
type BlockProposalDTO struct {
	TaskID  int64  `json:"taskId"`
	TaskUID string `json:"taskUid"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Part    int    `json:"part"`
	Parts   int    `json:"parts"`
}

// DayPlanDTO is a day laid out: the proposals, and the titles of the tasks
// that got less time than they need or none at all.
type DayPlanDTO struct {
	Proposals []BlockProposalDTO `json:"proposals"`
	Short     []string           `json:"short"`
	Unplaced  []string           `json:"unplaced"`
	// Tasks counts the tasks that were due and still needed time.
	Tasks int `json:"tasks"`
}

// TaskPlanDTO is how much time a task has in blocks: planned in all, and
// still ahead. Future counts the blocks that have not started.
type TaskPlanDTO struct {
	TaskUID        string `json:"taskUid"`
	PlannedMinutes int    `json:"plannedMinutes"`
	AheadMinutes   int    `json:"aheadMinutes"`
	Future         int    `json:"future"`
}

// --- settings ---------------------------------------------------------------

// GetTimeBlockSettings returns the time blocking preferences.
func (a *App) GetTimeBlockSettings() (TimeBlockSettingsDTO, error) {
	if err := a.ready(); err != nil {
		return TimeBlockSettingsDTO{}, err
	}
	return a.blockSettings(), nil
}

func (a *App) blockSettings() TimeBlockSettingsDTO {
	return TimeBlockSettingsDTO{
		DefaultMinutes:  a.intSetting(settingBlockMinutes, defaultBlockMinutes),
		MinSlotMinutes:  a.intSetting(settingBlockMinSlot, defaultBlockMinSlot),
		BufferMinutes:   a.intSetting(settingBlockBuffer, defaultBlockBuffer),
		WorkDays:        parseWorkDays(a.stringSetting(settingBlockWorkDays, defaultBlockWorkDays)),
		SearchDays:      a.intSetting(settingBlockSearchDays, defaultBlockSearchDays),
		WriteToCalendar: a.boolSetting(settingBlockWrite, false),
		CalendarID:      int64(a.intSetting(settingBlockCalendar, 0)),
		EventTitle:      a.stringSetting(settingBlockEventTitle, defaultBlockEventTitle),
		IncludeTitle:    a.boolSetting(settingBlockIncludeTitle, false),
	}
}

// SetTimeBlockSettings saves the time blocking preferences. Lengths outside
// what a block can be, a negative buffer, unknown week days, a search of more
// than a month and an empty event title are refused.
func (a *App) SetTimeBlockSettings(s TimeBlockSettingsDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	s.EventTitle = strings.TrimSpace(s.EventTitle)
	if s.DefaultMinutes < 5 || s.DefaultMinutes > maxBlockMinutes || s.MinSlotMinutes < 5 || s.MinSlotMinutes > maxBlockMinutes ||
		s.BufferMinutes < 0 || s.BufferMinutes > 120 || s.SearchDays < 1 || s.SearchDays > maxBlockSearchDays ||
		s.EventTitle == "" || slices.ContainsFunc(s.WorkDays, func(d int) bool { return d < 0 || d > 6 }) {
		return errBlockSettings
	}
	days := slices.Clone(s.WorkDays)
	slices.Sort(days)
	days = slices.Compact(days)
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = strconv.Itoa(d)
	}
	for _, step := range []func() error{
		func() error { return a.store.SetInt(a.ctx, settingBlockMinutes, s.DefaultMinutes) },
		func() error { return a.store.SetInt(a.ctx, settingBlockMinSlot, s.MinSlotMinutes) },
		func() error { return a.store.SetInt(a.ctx, settingBlockBuffer, s.BufferMinutes) },
		func() error { return a.store.Set(a.ctx, settingBlockWorkDays, strings.Join(parts, ",")) },
		func() error { return a.store.SetInt(a.ctx, settingBlockSearchDays, s.SearchDays) },
		func() error { return a.store.SetBool(a.ctx, settingBlockWrite, s.WriteToCalendar) },
		func() error { return a.store.SetInt(a.ctx, settingBlockCalendar, int(s.CalendarID)) },
		func() error { return a.store.Set(a.ctx, settingBlockEventTitle, s.EventTitle) },
		func() error { return a.store.SetBool(a.ctx, settingBlockIncludeTitle, s.IncludeTitle) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// parseWorkDays reads "1,2,3" into week days, leaving out anything else. An
// empty setting means no working days at all.
func parseWorkDays(s string) []int {
	out := []int{}
	for _, part := range strings.Split(s, ",") {
		d, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && d >= 0 && d <= 6 && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out
}

// blockRules are the settings as the free time search takes them.
func (a *App) blockRules(s TimeBlockSettingsDTO) timeblock.Rules {
	r := timeblock.Rules{
		Loc:       time.Local,
		WorkStart: a.intSetting(settingCalendarWorkStart, defaultWorkStart),
		WorkEnd:   a.intSetting(settingCalendarWorkEnd, defaultWorkEnd),
		MinSlot:   time.Duration(s.MinSlotMinutes) * time.Minute,
		Buffer:    time.Duration(s.BufferMinutes) * time.Minute,
		Step:      blockStep,
	}
	for _, d := range s.WorkDays {
		r.WorkDays[d] = true
	}
	return r
}

// defaultBlockCalendar is where a new block is written when the user did not
// choose: the chosen calendar while it can be written to, else the default
// event calendar, or 0 when blocks stay on this device.
func (a *App) defaultBlockCalendar(s TimeBlockSettingsDTO) int64 {
	if !s.WriteToCalendar {
		return 0
	}
	cals, err := a.store.ListCalendars(a.ctx)
	if err != nil {
		return 0
	}
	for _, c := range cals {
		if c.ID == s.CalendarID && !c.ReadOnly && c.HoldsEvents() {
			return c.ID
		}
	}
	return a.defaultCalendarID(cals)
}

// --- reading ------------------------------------------------------------------

// blockContext is what showing blocks needs once per call: the tasks by UID
// and the settings.
type blockContext struct {
	tasks    map[string]TaskDTO
	settings TimeBlockSettingsDTO
}

func (a *App) newBlockContext() (*blockContext, error) {
	tasks, err := a.ListTasks()
	if err != nil {
		return nil, err
	}
	bc := &blockContext{tasks: map[string]TaskDTO{}, settings: a.blockSettings()}
	for _, t := range tasks {
		bc.tasks[t.UID] = t
	}
	return bc, nil
}

// blockMail is the mail a block links to as the user may see it now.
type blockMail struct {
	// inVault is true for a mail in a vault, locked or not.
	inVault bool
	// masked is true while that vault is locked, or the mail cannot be
	// placed: the block then says nothing about it.
	masked bool
	msg    *storage.Message
}

// mailOf resolves a block's mail. A mail that went is looked up again by its
// Message-ID, which finds it after a move; one in a vault the user cannot see
// now is masked, and so is any mail whose vault cannot be told.
func (a *App) mailOf(b *storage.TimeBlock) blockMail {
	if b.MessageID == 0 && b.MessageHeader == "" {
		return blockMail{}
	}
	if b.MessageID != 0 {
		if _, err := a.store.MessageVault(a.ctx, b.MessageID); errors.Is(err, storage.ErrMessageNotFound) && b.MessageHeader != "" {
			if ref, err := a.store.FindMessageByHeaderID(a.mailCtx(), b.MessageHeader); err == nil {
				b.MessageID = ref.ID
				if err := a.store.UpdateTimeBlock(a.ctx, b); err != nil {
					a.log.Warn("time block: relink mail", "block", b.ID, "err", err)
				}
			}
		}
	}
	vault, err := a.store.MessageVault(a.ctx, b.MessageID)
	switch {
	case errors.Is(err, storage.ErrMessageNotFound):
		return blockMail{}
	case err != nil:
		return blockMail{inVault: true, masked: true}
	}
	m, err := a.store.GetMessage(a.mailCtx(), b.MessageID)
	if err != nil {
		return blockMail{inVault: vault != 0, masked: vault != 0}
	}
	return blockMail{inVault: vault != 0, msg: m}
}

// shownTitle is the title a block shows: its own, its task's, or its mail's
// subject; empty while the mail is masked.
func shownTitle(b storage.TimeBlock, task TaskDTO, mail blockMail) string {
	switch {
	case mail.masked:
		return ""
	case b.Title != "":
		return b.Title
	case task.Title != "":
		return task.Title
	case mail.msg != nil:
		return stripSubjectPrefixes(mail.msg.Subject)
	}
	return ""
}

// eventSummary is what a block's calendar event is called: the generic title
// unless the block or the setting says to use the block's own. The title of a
// block for a vault mail goes into the event only when the block itself asks
// for it, and never while the vault is locked.
func eventSummary(b storage.TimeBlock, shown string, mail blockMail, s TimeBlockSettingsDTO) string {
	generic := strings.TrimSpace(s.EventTitle)
	if generic == "" {
		generic = defaultBlockEventTitle
	}
	if shown == "" || mail.masked {
		return generic
	}
	switch b.EventTitle {
	case blockEventTitleOwn:
		return shown
	case blockEventTitleFollow:
		if s.IncludeTitle && !mail.inVault {
			return shown
		}
	}
	return generic
}

func (a *App) blockDTO(b storage.TimeBlock, bc *blockContext) TimeBlockDTO {
	mail := a.mailOf(&b)
	task := bc.tasks[b.TaskUID]
	dto := TimeBlockDTO{
		ID:         b.ID,
		Title:      shownTitle(b, task, mail),
		Start:      formatEventTime(b.Start, false),
		End:        formatEventTime(b.End, false),
		TaskID:     task.ID,
		TaskUID:    b.TaskUID,
		HasMail:    b.MessageID != 0 || b.MessageHeader != "",
		Masked:     mail.masked,
		Done:       b.Done,
		CalendarID: b.CalendarID,
		EventTitle: b.EventTitle,
		LinkState:  b.LinkState,
	}
	if mail.msg != nil {
		dto.MessageID, dto.AccountID, dto.FolderID = mail.msg.ID, mail.msg.AccountID, mail.msg.FolderID
	}
	if b.CalendarID != 0 && b.EventUID != "" {
		if row, err := a.store.GetCalendarEventByUID(a.ctx, b.CalendarID, b.EventUID); err == nil {
			dto.Pending = row.Pending != ""
		}
	}
	return dto
}

// ListTimeBlocks returns the blocks overlapping [from, to), both rfc3339, by
// start. Blocks written to a calendar are first compared with their events,
// so one whose event moved on the server follows it, and one whose event went
// or changed past following is marked.
func (a *App) ListTimeBlocks(from, to string) ([]TimeBlockDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	start, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return nil, err
	}
	if err := a.reconcileBlocks(a.ctx); err != nil {
		a.log.Warn("time blocks: compare with calendar events", "err", err)
	}
	rows, err := a.store.ListTimeBlocks(a.ctx, start, end)
	if err != nil {
		return nil, err
	}
	bc, err := a.newBlockContext()
	if err != nil {
		return nil, err
	}
	out := make([]TimeBlockDTO, 0, len(rows))
	for _, b := range rows {
		out = append(out, a.blockDTO(b, bc))
	}
	return out, nil
}

// ListTaskPlans returns, for every task with blocks, how much time they hold
// and how much of it is still ahead.
func (a *App) ListTaskPlans() ([]TaskPlanDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	rows, err := a.store.AllTaskTimeBlocks(a.ctx)
	if err != nil {
		return nil, err
	}
	return taskPlans(rows, time.Now()), nil
}

func taskPlans(rows []storage.TimeBlock, now time.Time) []TaskPlanDTO {
	byUID := map[string]*TaskPlanDTO{}
	var order []string
	for _, b := range rows {
		p := byUID[b.TaskUID]
		if p == nil {
			p = &TaskPlanDTO{TaskUID: b.TaskUID}
			byUID[b.TaskUID] = p
			order = append(order, b.TaskUID)
		}
		minutes := int(b.End.Sub(b.Start) / time.Minute)
		p.PlannedMinutes += minutes
		if b.End.After(now) {
			p.AheadMinutes += int(b.End.Sub(maxTime(b.Start, now)) / time.Minute)
		}
		if b.Start.After(now) {
			p.Future++
		}
	}
	out := make([]TaskPlanDTO, 0, len(order))
	for _, uid := range order {
		out = append(out, *byUID[uid])
	}
	return out
}

// --- changing -----------------------------------------------------------------

func parseBlockTimes(start, end string) (time.Time, time.Time, error) {
	s, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return s, s, err
	}
	e, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return s, e, err
	}
	if !e.After(s) {
		return s, e, errBlockTimes
	}
	if e.Sub(s) > maxBlockMinutes*time.Minute {
		return s, e, errBlockTooLong
	}
	return s.UTC().Truncate(time.Second), e.UTC().Truncate(time.Second), nil
}

func validEventTitle(mode string) string {
	if mode == blockEventTitleGeneric || mode == blockEventTitleOwn {
		return mode
	}
	return blockEventTitleFollow
}

// CreateTimeBlock makes a block, and writes it to its calendar when it names
// one. A block for a mail in a vault keeps no title of the mail's: its title
// is read from the mail while the vault is open.
func (a *App) CreateTimeBlock(req TimeBlockRequest) (TimeBlockDTO, error) {
	if err := a.ready(); err != nil {
		return TimeBlockDTO{}, err
	}
	out, err := a.createBlocks([]TimeBlockRequest{req})
	if err != nil {
		return TimeBlockDTO{}, err
	}
	return out[0], nil
}

// CreateTimeBlocks makes several blocks at once, which is how a day plan is
// accepted. Nothing is made when one of them is not valid.
func (a *App) CreateTimeBlocks(reqs []TimeBlockRequest) ([]TimeBlockDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.createBlocks(reqs)
}

func (a *App) createBlocks(reqs []TimeBlockRequest) ([]TimeBlockDTO, error) {
	blocks := make([]storage.TimeBlock, 0, len(reqs))
	for _, req := range reqs {
		start, end, err := parseBlockTimes(req.Start, req.End)
		if err != nil {
			return nil, err
		}
		b := storage.TimeBlock{
			Title: strings.TrimSpace(req.Title), Start: start, End: end, TaskUID: strings.TrimSpace(req.TaskUID),
			Done: req.Done, CalendarID: req.CalendarID, EventTitle: validEventTitle(req.EventTitle),
		}
		if req.MessageID != 0 {
			m, err := a.store.GetMessage(a.mailCtx(), req.MessageID)
			if err != nil {
				return nil, err
			}
			b.MessageID, b.MessageHeader = m.ID, strings.Trim(strings.TrimSpace(m.MessageID), "<>")
			if a.inMailVaultMessage(m.ID) {
				b.Title = ""
			}
		}
		blocks = append(blocks, b)
	}
	bc, err := a.newBlockContext()
	if err != nil {
		return nil, err
	}
	out := make([]TimeBlockDTO, 0, len(blocks))
	for i := range blocks {
		b := &blocks[i]
		calendarID := b.CalendarID
		b.CalendarID = 0
		if _, err := a.store.CreateTimeBlock(a.ctx, b); err != nil {
			return out, err
		}
		if calendarID != 0 {
			b.CalendarID = calendarID
			a.writeBlockEvent(a.ctx, b, bc)
		}
		out = append(out, a.blockDTO(*b, bc))
	}
	a.emit(EventTimeBlocksChanged, nil)
	return out, nil
}

// UpdateTimeBlock changes a block's title, times, done state and calendar.
// A block written to a calendar takes the change to its event; one moved to
// another calendar leaves the old event and is written to the new one, and one
// taken off its calendar deletes its event there.
func (a *App) UpdateTimeBlock(req TimeBlockRequest) (TimeBlockDTO, error) {
	if err := a.ready(); err != nil {
		return TimeBlockDTO{}, err
	}
	b, err := a.store.GetTimeBlock(a.ctx, req.ID)
	if err != nil {
		return TimeBlockDTO{}, err
	}
	start, end, err := parseBlockTimes(req.Start, req.End)
	if err != nil {
		return TimeBlockDTO{}, err
	}
	bc, err := a.newBlockContext()
	if err != nil {
		return TimeBlockDTO{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == shownTitle(storage.TimeBlock{}, bc.tasks[b.TaskUID], a.mailOf(b)) && b.Title == "" {
		title = ""
	}
	b.Title, b.Start, b.End, b.Done, b.EventTitle = title, start, end, req.Done, validEventTitle(req.EventTitle)
	if req.CalendarID != b.CalendarID {
		if b.CalendarID != 0 && b.EventUID != "" && b.LinkState != storage.BlockLinkEventGone {
			if err := a.deleteBlockEvent(a.ctx, b); err != nil {
				return TimeBlockDTO{}, err
			}
		}
		b.CalendarID, b.EventUID, b.LinkState = req.CalendarID, "", storage.BlockLinkOK
	}
	if err := a.store.UpdateTimeBlock(a.ctx, b); err != nil {
		return TimeBlockDTO{}, err
	}
	if b.CalendarID != 0 && b.LinkState == storage.BlockLinkOK {
		a.writeBlockEvent(a.ctx, b, bc)
	}
	a.emit(EventTimeBlocksChanged, nil)
	return a.blockDTO(*b, bc), nil
}

// DeleteTimeBlock deletes a block and, when it is written to a calendar, its
// event there. A block whose event is already gone just goes.
func (a *App) DeleteTimeBlock(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.deleteBlock(a.ctx, id); err != nil {
		return err
	}
	a.emit(EventTimeBlocksChanged, nil)
	return nil
}

// DeleteTimeBlocks deletes several blocks, which is how an accepted plan is
// undone.
func (a *App) DeleteTimeBlocks(ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := a.deleteBlock(a.ctx, id); err != nil && !errors.Is(err, storage.ErrTimeBlockNotFound) {
			return err
		}
	}
	a.emit(EventTimeBlocksChanged, nil)
	return nil
}

func (a *App) deleteBlock(ctx context.Context, id int64) error {
	b, err := a.store.GetTimeBlock(ctx, id)
	if err != nil {
		return err
	}
	if b.CalendarID != 0 && b.EventUID != "" && b.LinkState != storage.BlockLinkEventGone {
		if err := a.deleteBlockEvent(ctx, b); err != nil {
			return err
		}
	}
	return a.store.DeleteTimeBlock(ctx, id)
}

// SettleTimeBlockLink settles a block that is out of step with its event:
// follow takes the event's time, rewrite writes the block's time to the
// calendar again (making a new event when the old one is gone), forget keeps
// the block on this device only and leaves the event as it is.
func (a *App) SettleTimeBlockLink(id int64, action string) (TimeBlockDTO, error) {
	if err := a.ready(); err != nil {
		return TimeBlockDTO{}, err
	}
	b, err := a.store.GetTimeBlock(a.ctx, id)
	if err != nil {
		return TimeBlockDTO{}, err
	}
	if b.EventUID == "" && b.CalendarID == 0 {
		return TimeBlockDTO{}, errBlockNotLinked
	}
	bc, err := a.newBlockContext()
	if err != nil {
		return TimeBlockDTO{}, err
	}
	switch action {
	case BlockLinkFollow:
		ev, err := a.blockEvent(a.ctx, b)
		if err != nil {
			return TimeBlockDTO{}, err
		}
		if ev == nil || ev.recurring || ev.allDay {
			return TimeBlockDTO{}, errBlockEventGone
		}
		b.Start, b.End, b.LinkState = ev.start, ev.end, storage.BlockLinkOK
	case BlockLinkRewrite:
		if b.CalendarID == 0 {
			return TimeBlockDTO{}, errBlockEventGone
		}
		if b.LinkState == storage.BlockLinkEventGone {
			b.EventUID = ""
		}
		b.LinkState = storage.BlockLinkOK
	case BlockLinkForget:
		b.CalendarID, b.EventUID, b.LinkState = 0, "", storage.BlockLinkOK
	default:
		return TimeBlockDTO{}, errBlockLinkAction
	}
	if err := a.store.UpdateTimeBlock(a.ctx, b); err != nil {
		return TimeBlockDTO{}, err
	}
	if action == BlockLinkRewrite {
		a.writeBlockEvent(a.ctx, b, bc)
	}
	a.emit(EventTimeBlocksChanged, nil)
	return a.blockDTO(*b, bc), nil
}

// --- the calendar link ------------------------------------------------------------

// linkedEvent is what a block compares with its event.
type linkedEvent struct {
	row       *storage.CalendarEvent
	obj       *ical.Object
	start     time.Time
	end       time.Time
	recurring bool
	allDay    bool
}

// blockEvent reads a block's event, nil when it is gone or waits to be
// deleted.
func (a *App) blockEvent(ctx context.Context, b *storage.TimeBlock) (*linkedEvent, error) {
	if b.CalendarID == 0 || b.EventUID == "" {
		return nil, nil
	}
	row, err := a.store.GetCalendarEventByUID(ctx, b.CalendarID, b.EventUID)
	if errors.Is(err, storage.ErrCalendarEventNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.Pending == storage.PendingDelete {
		return nil, nil
	}
	obj, err := parseStoredEvent(*row)
	if err != nil {
		return nil, err
	}
	first := obj.First()
	if first == nil {
		return nil, nil
	}
	ev := &linkedEvent{row: row, obj: obj, start: row.RangeStart, end: row.RangeEnd,
		recurring: row.Recurring || len(obj.Overrides) > 0, allDay: first.Start.AllDay}
	return ev, nil
}

// writeBlockEvent writes a block to its calendar as a busy event: a new one,
// or its existing event moved and renamed. A failure does not undo the block:
// the block is marked out of step, which the user settles.
func (a *App) writeBlockEvent(ctx context.Context, b *storage.TimeBlock, bc *blockContext) {
	if err := a.pushBlockEvent(ctx, b, bc); err != nil {
		a.log.Warn("time block: write calendar event", "block", b.ID, "err", err)
		b.LinkState = storage.BlockLinkOutOfStep
	}
	if err := a.store.UpdateTimeBlock(ctx, b); err != nil {
		a.log.Warn("time block: save link", "block", b.ID, "err", err)
	}
}

func (a *App) pushBlockEvent(ctx context.Context, b *storage.TimeBlock, bc *blockContext) error {
	cal, err := a.store.GetCalendar(ctx, b.CalendarID)
	if err != nil {
		return err
	}
	if cal.ReadOnly || !cal.HoldsEvents() {
		return errCalendarReadOnly
	}
	mail := a.mailOf(b)
	summary := eventSummary(*b, shownTitle(*b, bc.tasks[b.TaskUID], mail), mail, bc.settings)
	start := ical.DateTime{Wall: b.Start.UTC(), UTC: true}
	end := ical.DateTime{Wall: b.End.UTC(), UTC: true}

	existing, err := a.blockEvent(ctx, b)
	if err != nil {
		return err
	}
	if b.EventUID != "" && existing == nil {
		b.LinkState = storage.BlockLinkEventGone
		return nil
	}
	if existing != nil {
		if existing.recurring || existing.allDay {
			b.LinkState = storage.BlockLinkOutOfStep
			return nil
		}
		ev := existing.obj.First()
		ev.Start, ev.End, ev.HasDuration, ev.Summary = start, end, false, summary
		ev.Sequence++
		ev.Apply()
		existing.obj.EnsureTimezones()
		res, err := a.commitEvent(ctx, *cal, *existing.row, existing.obj, time.Time{}, false)
		if err != nil {
			return err
		}
		if res.Conflict {
			b.LinkState = storage.BlockLinkOutOfStep
		}
		return nil
	}

	ev := &ical.Event{UID: uuid.NewString(), Summary: summary, Start: start, End: end, Transparency: "OPAQUE"}
	obj := ical.NewObject(ev, time.Local)
	ev.Component.Set("CLASS", "PRIVATE")
	res, err := a.commitEvent(ctx, *cal, newEventRow(*cal, obj), obj, time.Time{}, false)
	if err != nil {
		return err
	}
	b.EventUID = ev.UID
	if res.Conflict {
		b.LinkState = storage.BlockLinkOutOfStep
	}
	return nil
}

// deleteBlockEvent deletes a block's event from its calendar, if it is still
// there.
func (a *App) deleteBlockEvent(ctx context.Context, b *storage.TimeBlock) error {
	ev, err := a.blockEvent(ctx, b)
	if err != nil || ev == nil {
		return err
	}
	cal, err := a.store.GetCalendar(ctx, b.CalendarID)
	if err != nil {
		return err
	}
	if cal.ReadOnly {
		return errCalendarReadOnly
	}
	_, err = a.deleteEventRow(ctx, *cal, *ev.row, false)
	return err
}

// reconcileBlocks compares every linked block with its event. A block whose
// event moved follows it; one whose event went is marked event_gone; one
// whose event is in conflict, became all-day or repeating is marked
// out_of_step. An event change that has not reached the server yet is the
// block's own and left alone. An out-of-step block whose event agrees with it
// again is back in step.
func (a *App) reconcileBlocks(ctx context.Context) error {
	blocks, err := a.store.LinkedTimeBlocks(ctx)
	if err != nil {
		return err
	}
	changed := false
	for i := range blocks {
		b := &blocks[i]
		if b.LinkState == storage.BlockLinkEventGone {
			continue
		}
		next := *b
		ev, err := a.blockEvent(ctx, b)
		switch {
		case err != nil:
			return err
		case ev == nil:
			next.LinkState = storage.BlockLinkEventGone
		case ev.row.Conflict, ev.recurring, ev.allDay:
			next.LinkState = storage.BlockLinkOutOfStep
		case ev.row.Pending != "":
			continue
		case b.LinkState == storage.BlockLinkOutOfStep:
			if ev.start.Equal(b.Start) && ev.end.Equal(b.End) {
				next.LinkState = storage.BlockLinkOK
			}
		default:
			next.Start, next.End = ev.start, ev.end
		}
		if next.LinkState == b.LinkState && next.Start.Equal(b.Start) && next.End.Equal(b.End) {
			continue
		}
		if err := a.store.UpdateTimeBlock(ctx, &next); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		a.emit(EventTimeBlocksChanged, nil)
	}
	return nil
}

// --- tasks ------------------------------------------------------------------------

// blocksFollowTask marks a task's blocks done when it is completed, and the
// ones still ahead open again when it is reopened.
func (a *App) blocksFollowTask(ctx context.Context, uid string, wasDone, done bool) {
	if uid == "" || wasDone == done {
		return
	}
	n, err := a.store.SetTaskTimeBlocksDone(ctx, uid, done, time.Now())
	if err != nil {
		a.log.Warn("time blocks: follow task", "task", uid, "err", err)
		return
	}
	if n > 0 {
		a.emit(EventTimeBlocksChanged, nil)
	}
}

// ReleaseTaskTimeBlocks is asked when a task is deleted: with deleteFuture,
// its blocks that have not started go (with their events) and the rest stay;
// otherwise every block stays. Blocks that stay keep the task's title and
// forget the task.
func (a *App) ReleaseTaskTimeBlocks(uid, title string, deleteFuture bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	rows, err := a.store.TaskTimeBlocks(a.ctx, []string{uid})
	if err != nil {
		return err
	}
	now := time.Now()
	for _, b := range rows {
		if deleteFuture && b.Start.After(now) {
			if err := a.deleteBlock(a.ctx, b.ID); err != nil {
				return err
			}
		}
	}
	if err := a.store.UnlinkTaskTimeBlocks(a.ctx, uid, strings.TrimSpace(title)); err != nil {
		return err
	}
	a.emit(EventTimeBlocksChanged, nil)
	return nil
}

// --- finding time -----------------------------------------------------------------

// busyBetween is everything that keeps [from, to) busy: the occurrences of
// the visible calendars (an event marked free, a cancelled one, and an
// all-day one not marked busy keep nothing busy) and every block, the
// excluded ones left out.
func (a *App) busyBetween(from, to time.Time, exclude map[int64]bool) ([]timeblock.Span, error) {
	occs, err := a.ListOccurrences(from.Format(time.RFC3339), to.Format(time.RFC3339), nil)
	if err != nil {
		return nil, err
	}
	busy := occurrenceBusy(occs, time.Local)
	blocks, err := a.store.ListTimeBlocks(a.ctx, from, to)
	if err != nil {
		return nil, err
	}
	for _, b := range blocks {
		if !exclude[b.ID] {
			busy = append(busy, timeblock.Span{Start: b.Start, End: b.End})
		}
	}
	return busy, nil
}

// occurrenceBusy turns occurrences into busy spans, all-day ones read as whole
// days in loc.
func occurrenceBusy(occs []OccurrenceDTO, loc *time.Location) []timeblock.Span {
	var out []timeblock.Span
	for _, o := range occs {
		if o.Status == ical.StatusCancelled || o.Transparency == "TRANSPARENT" {
			continue
		}
		if o.AllDay {
			if o.Transparency != "OPAQUE" {
				continue
			}
			s, err1 := time.ParseInLocation(dateLayout, o.Start, loc)
			e, err2 := time.ParseInLocation(dateLayout, o.End, loc)
			if err1 != nil || err2 != nil {
				continue
			}
			if !e.After(s) {
				e = s.AddDate(0, 0, 1)
			}
			out = append(out, timeblock.Span{Start: s, End: e})
			continue
		}
		s, err1 := time.Parse(time.RFC3339, o.Start)
		e, err2 := time.Parse(time.RFC3339, o.End)
		if err1 == nil && err2 == nil && e.After(s) {
			out = append(out, timeblock.Span{Start: s, End: e})
		}
	}
	return out
}

// taskNeed is how long a task still needs: its estimate (or the default
// length) less the time its blocks already hold. A task without an estimate
// that has a block needs nothing more.
func taskNeed(task TaskDTO, planned int, defaultMinutes int) time.Duration {
	if task.Estimate <= 0 {
		if planned > 0 {
			return 0
		}
		return time.Duration(defaultMinutes) * time.Minute
	}
	return time.Duration(max(0, task.Estimate-planned)) * time.Minute
}

// FindFreeSlots proposes times for a block: for a task, of what it still
// needs (its whole estimate when that is all planned already), with its due
// date marking the slots that come too late; else of Minutes, or the default
// length. Busy time, working hours and days, the shortest slot and the buffer
// all count. Nothing leaves the computer.
func (a *App) FindFreeSlots(req SlotRequest) ([]SlotDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s := a.blockSettings()
	from := time.Now()
	if req.From != "" {
		t, err := time.Parse(time.RFC3339, req.From)
		if err != nil {
			return nil, err
		}
		from = t
	}
	length := time.Duration(req.Minutes) * time.Minute
	var due time.Time
	if req.TaskID != 0 {
		task, err := a.GetTask(req.TaskID)
		if err != nil {
			return nil, err
		}
		due = taskDueTime(task.Due)
		if length <= 0 {
			length, err = a.taskBlockLength(task, s)
			if err != nil {
				return nil, err
			}
		}
	}
	if length <= 0 {
		length = time.Duration(s.DefaultMinutes) * time.Minute
	}
	length = min(length, maxBlockMinutes*time.Minute)
	slots, err := a.freeSlots(s, from, length, due)
	if err != nil {
		return nil, err
	}
	out := make([]SlotDTO, 0, len(slots))
	for _, sl := range slots {
		out = append(out, SlotDTO{Start: formatEventTime(sl.Start, false), End: formatEventTime(sl.End, false), AfterDue: sl.AfterDue})
	}
	return out, nil
}

// taskBlockLength is how long a new block for a task lasts: what it still
// needs, or its whole estimate or the default when nothing is left.
func (a *App) taskBlockLength(task TaskDTO, s TimeBlockSettingsDTO) (time.Duration, error) {
	rows, err := a.store.TaskTimeBlocks(a.ctx, []string{task.UID})
	if err != nil {
		return 0, err
	}
	planned := 0
	for _, p := range taskPlans(rows, time.Now()) {
		planned = p.PlannedMinutes
	}
	if need := taskNeed(task, planned, s.DefaultMinutes); need > 0 {
		return need, nil
	}
	if task.Estimate > 0 {
		return time.Duration(task.Estimate) * time.Minute, nil
	}
	return time.Duration(s.DefaultMinutes) * time.Minute, nil
}

func (a *App) freeSlots(s TimeBlockSettingsDTO, from time.Time, length time.Duration, due time.Time) ([]timeblock.Slot, error) {
	r := a.blockRules(s)
	horizon := from.AddDate(0, 0, s.SearchDays+1)
	busy, err := a.busyBetween(from.AddDate(0, 0, -1), horizon, nil)
	if err != nil {
		return nil, err
	}
	return timeblock.FindSlots(r, timeblock.SlotQuery{
		Length: length, From: from, Days: s.SearchDays, Due: due, Limit: slotsOffered, PerDay: slotsPerDay,
	}, busy), nil
}

// BlockTimeForTask blocks the first free time for a task, of what it still
// needs, and writes it to the calendar when the settings say so.
func (a *App) BlockTimeForTask(taskID int64, minutes int) (TimeBlockDTO, error) {
	if err := a.ready(); err != nil {
		return TimeBlockDTO{}, err
	}
	slots, err := a.FindFreeSlots(SlotRequest{TaskID: taskID, Minutes: minutes})
	if err != nil {
		return TimeBlockDTO{}, err
	}
	if len(slots) == 0 {
		return TimeBlockDTO{}, errBlockNoFreeTime
	}
	task, err := a.GetTask(taskID)
	if err != nil {
		return TimeBlockDTO{}, err
	}
	return a.CreateTimeBlock(TimeBlockRequest{
		TaskUID: task.UID, Start: slots[0].Start, End: slots[0].End, CalendarID: a.defaultBlockCalendar(a.blockSettings()),
	})
}

// BlockTimeForMessages makes a block for each mail, to deal with it. With at
// (rfc3339) the blocks follow each other from there; without, each takes the
// next free time of the default length. The blocks link back to their mail.
func (a *App) BlockTimeForMessages(ids []int64, at string) ([]TimeBlockDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s := a.blockSettings()
	length := time.Duration(s.DefaultMinutes) * time.Minute
	calendarID := a.defaultBlockCalendar(s)
	var reqs []TimeBlockRequest
	if at != "" {
		start, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			end := start.Add(length)
			reqs = append(reqs, TimeBlockRequest{MessageID: id, Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339), CalendarID: calendarID})
			start = end
		}
	} else {
		from := time.Now()
		r := a.blockRules(s)
		busy, err := a.busyBetween(from.AddDate(0, 0, -1), from.AddDate(0, 0, s.SearchDays+1), nil)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			slots := timeblock.FindSlots(r, timeblock.SlotQuery{Length: length, From: from, Days: s.SearchDays, Limit: 1}, busy)
			if len(slots) == 0 {
				return nil, errBlockNoFreeTime
			}
			slot := slots[0].Span
			busy = append(busy, slot)
			reqs = append(reqs, TimeBlockRequest{MessageID: id, Start: slot.Start.Format(time.RFC3339), End: slot.End.Format(time.RFC3339), CalendarID: calendarID})
		}
	}
	for i := range reqs {
		m, err := a.store.GetMessage(a.mailCtx(), reqs[i].MessageID)
		if err != nil {
			return nil, err
		}
		if !a.inMailVaultMessage(m.ID) {
			reqs[i].Title = stripSubjectPrefixes(m.Subject)
		}
	}
	return a.createBlocks(reqs)
}

// PlanDay lays the open tasks due on day (2006-01-02, empty for today) or
// before it into that day's free time from now on, as proposals. Tasks go by
// due date, then priority; each needs its estimate, or the default length,
// less what its blocks already hold. Nothing is stored until the proposals
// are accepted.
func (a *App) PlanDay(day string) (DayPlanDTO, error) {
	if err := a.ready(); err != nil {
		return DayPlanDTO{}, err
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	if day != "" {
		t, err := time.ParseInLocation(dateLayout, day, time.Local)
		if err != nil {
			return DayPlanDTO{}, err
		}
		start = t
	}
	end := start.AddDate(0, 0, 1)
	tasks, err := a.ListTasks()
	if err != nil {
		return DayPlanDTO{}, err
	}
	rows, err := a.store.AllTaskTimeBlocks(a.ctx)
	if err != nil {
		return DayPlanDTO{}, err
	}
	planned := map[string]int{}
	for _, p := range taskPlans(rows, now) {
		planned[p.TaskUID] = p.PlannedMinutes
	}
	s := a.blockSettings()
	byUID := map[string]TaskDTO{}
	var plan []timeblock.PlanTask
	for _, t := range tasks {
		due := taskDueTime(t.Due)
		if t.Done || due.IsZero() || !due.Before(end.Add(time.Second)) {
			continue
		}
		need := taskNeed(t, planned[t.UID], s.DefaultMinutes)
		if need <= 0 {
			continue
		}
		byUID[t.UID] = t
		plan = append(plan, timeblock.PlanTask{ID: t.UID, Due: due, Priority: t.Priority, Need: min(need, maxBlockMinutes*time.Minute)})
	}
	busy, err := a.busyBetween(start.AddDate(0, 0, -1), end.AddDate(0, 0, 1), nil)
	if err != nil {
		return DayPlanDTO{}, err
	}
	laid := timeblock.PlanDay(a.blockRules(s), start, now, plan, busy)
	out := DayPlanDTO{Proposals: []BlockProposalDTO{}, Short: []string{}, Unplaced: []string{}, Tasks: len(plan)}
	for _, p := range laid.Pieces {
		t := byUID[p.TaskID]
		out.Proposals = append(out.Proposals, BlockProposalDTO{
			TaskID: t.ID, TaskUID: t.UID, Title: t.Title, Start: formatEventTime(p.Start, false), End: formatEventTime(p.End, false),
			Part: p.Part, Parts: p.Parts,
		})
	}
	for _, uid := range laid.Short {
		out.Short = append(out.Short, byUID[uid].Title)
	}
	for _, uid := range laid.Unplaced {
		out.Unplaced = append(out.Unplaced, byUID[uid].Title)
	}
	return out, nil
}
