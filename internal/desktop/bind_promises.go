package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/promises"
	"github.com/peltonapp/Pelton/internal/scripting/ai"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Promises: commitments found in mail, in both directions. New mail in the
// inbox and in Sent is read in the background after a sync, mail from people
// only, and what is found waits on the mail as a suggestion until the user
// accepts it (it becomes a task) or dismisses it. An accepted promise owed to
// the user closes when that person replies in the thread; one the user owes
// is offered for completion when the user replies. All of it runs in the new
// design only.

const (
	settingPromisesEnabled  = "promises_enabled"
	settingPromisesAutoMine = "promises_auto_accept_mine"
	settingPromisesNudge    = "promises_nudge"
	settingPromisesTaskList = "promises_task_list"
	// settingPromisesAccounts holds the per-account switches as a JSON object
	// of account id to promiseAccount.
	settingPromisesAccounts = "promises_accounts"
)

// EventPromisesChanged fires when promises were found, accepted, dismissed
// or closed.
const EventPromisesChanged = "promises:changed"

const (
	// promiseBatchCap is how many new messages one sync batch reads; the
	// rest of a large batch is left alone.
	promiseBatchCap = 30
	// promiseMaxAge leaves out mail older than this, so a first sync of an
	// old mailbox suggests nothing from years ago.
	promiseMaxAge = 14 * 24 * time.Hour
	// promiseAIPerBatch and promiseAIPerDay cap the AI checks.
	promiseAIPerBatch = 5
	promiseAIPerDay   = 50
	promiseAITimeout  = 60 * time.Second
)

// The task lists accepted promises go to.
var promiseTaskLists = []string{"local", "default"}

var (
	errPromiseNotSuggested = errors.New("pelton: this promise is no longer a suggestion")
	errPromiseNoTaskList   = errors.New("pelton: there is no task list to put the promise in")
	errPromiseSettings     = errors.New("pelton: these promise settings are not valid")
)

// promiseAI counts the AI checks of the day, so a busy day cannot send more
// than promiseAIPerDay mails.
var promiseAI struct {
	mu    sync.Mutex
	day   string
	count int
}

// promiseMu serializes the background passes, so two syncs that end at once
// do not read the same mail twice.
var promiseMu sync.Mutex

type promiseAccount struct {
	Off      bool   `json:"off,omitempty"`
	AI       bool   `json:"ai,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// PromiseAccountDTO is the switches of one account.
type PromiseAccountDTO struct {
	AccountID int64  `json:"accountId"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	On        bool   `json:"on"`
	// AI asks the provider named for a second reading of this account's mail.
	AI       bool   `json:"ai"`
	Provider string `json:"provider"`
}

// PromiseSettingsDTO is the promise preferences.
type PromiseSettingsDTO struct {
	Enabled bool `json:"enabled"`
	// AutoAcceptMine makes a task of a promise the user made in sent mail
	// without asking.
	AutoAcceptMine bool `json:"autoAcceptMine"`
	// Nudge offers a reminder draft for an overdue promise owed to the user.
	Nudge bool `json:"nudge"`
	// TaskList is "local" (the first list on this device) or "default" (the
	// default task list).
	TaskList string              `json:"taskList"`
	Accounts []PromiseAccountDTO `json:"accounts"`
	// AIMaxChars is how much of a mail an AI check sends at most.
	AIMaxChars int `json:"aiMaxChars"`
	// AIPerDay is how many AI checks a day runs at most.
	AIPerDay int `json:"aiPerDay"`
}

// PromiseDTO is one promise for the frontend.
type PromiseDTO struct {
	ID        int64 `json:"id"`
	AccountID int64 `json:"accountId"`
	// MailID is the Message-ID of the mail it was found in; MessageID a
	// cached copy of it, 0 when none is cached.
	MailID    string `json:"mailId"`
	MessageID int64  `json:"messageId"`
	ThreadID  int64  `json:"threadId"`
	// Direction is "mine" (the user owes it) or "theirs" (owed to the user).
	Direction   string  `json:"direction"`
	PersonName  string  `json:"personName"`
	PersonEmail string  `json:"personEmail"`
	Title       string  `json:"title"`
	Quote       string  `json:"quote"`
	Due         string  `json:"due"`
	Confidence  float64 `json:"confidence"`
	Source      string  `json:"source"`
	// State is suggested, accepted, dismissed or closed.
	State   string `json:"state"`
	TaskUID string `json:"taskUid"`
	// ReplyID is the reply that closed it (theirs) or offers to (mine), and
	// ReplyMessageID a cached copy of that reply.
	ReplyID        string `json:"replyId"`
	ReplyAt        string `json:"replyAt"`
	ReplyMessageID int64  `json:"replyMessageId"`
	MailDate       string `json:"mailDate"`
	// FollowUp is set when a follow-up reminder already watches the thread,
	// so the promise does not nudge as well.
	FollowUp bool `json:"followUp"`
}

// PromiseEditDTO is a suggestion as the user accepts it.
type PromiseEditDTO struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Due         string `json:"due"`
	Direction   string `json:"direction"`
	PersonName  string `json:"personName"`
	PersonEmail string `json:"personEmail"`
}

func (a *App) promiseStore() *promises.Store {
	return promises.NewStore(a.store.Handle())
}

func (a *App) promiseAccounts() map[string]promiseAccount {
	out := map[string]promiseAccount{}
	if raw := a.stringSetting(settingPromisesAccounts, ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			a.log.Warn("promises: read account switches", "err", err)
		}
	}
	return out
}

// promisesActive reports whether promises are read at all: the feature is on
// and the new design is in use.
func (a *App) promisesActive() bool {
	return a.stringSetting(settingDesign, "new") != "classic" && a.boolSetting(settingPromisesEnabled, true)
}

// GetPromiseSettings returns the promise preferences with every account.
func (a *App) GetPromiseSettings() (PromiseSettingsDTO, error) {
	if err := a.ready(); err != nil {
		return PromiseSettingsDTO{}, err
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return PromiseSettingsDTO{}, err
	}
	sw := a.promiseAccounts()
	out := PromiseSettingsDTO{
		Enabled:        a.boolSetting(settingPromisesEnabled, true),
		AutoAcceptMine: a.boolSetting(settingPromisesAutoMine, false),
		Nudge:          a.boolSetting(settingPromisesNudge, true),
		TaskList:       oneOf(a.stringSetting(settingPromisesTaskList, "local"), promiseTaskLists, "local"),
		Accounts:       []PromiseAccountDTO{},
		AIMaxChars:     promises.AIMaxChars,
		AIPerDay:       promiseAIPerDay,
	}
	for _, acc := range accounts {
		s := sw[strconv.FormatInt(acc.ID, 10)]
		out.Accounts = append(out.Accounts, PromiseAccountDTO{
			AccountID: acc.ID, Name: acc.DisplayName, Email: acc.Email, On: !s.Off, AI: s.AI, Provider: s.Provider,
		})
	}
	return out, nil
}

// SetPromiseSettings saves the promise preferences. An AI check needs a
// provider that is set up.
func (a *App) SetPromiseSettings(s PromiseSettingsDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !slices.Contains(promiseTaskLists, s.TaskList) {
		return errPromiseSettings
	}
	aiMu.Lock()
	recs := a.aiRecords()
	aiMu.Unlock()
	sw := map[string]promiseAccount{}
	for _, acc := range s.Accounts {
		p := promiseAccount{Off: !acc.On, AI: acc.AI, Provider: acc.Provider}
		if p.AI && !slices.ContainsFunc(recs, func(r aiRecord) bool { return r.ID == p.Provider }) {
			return errPromiseSettings
		}
		if !p.AI {
			p.Provider = ""
		}
		sw[strconv.FormatInt(acc.AccountID, 10)] = p
	}
	raw, err := json.Marshal(sw)
	if err != nil {
		return err
	}
	for _, step := range []func() error{
		func() error { return a.store.SetBool(a.ctx, settingPromisesEnabled, s.Enabled) },
		func() error { return a.store.SetBool(a.ctx, settingPromisesAutoMine, s.AutoAcceptMine) },
		func() error { return a.store.SetBool(a.ctx, settingPromisesNudge, s.Nudge) },
		func() error { return a.store.Set(a.ctx, settingPromisesTaskList, s.TaskList) },
		func() error { return a.store.Set(a.ctx, settingPromisesAccounts, string(raw)) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	a.emit(EventPromisesChanged, nil)
	return nil
}

// ListPromises returns the promises that are suggested, accepted or closed.
func (a *App) ListPromises() ([]PromiseDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	list, err := a.promiseStore().List(a.ctx, promises.StateSuggested, promises.StateAccepted, promises.StateClosed)
	if err != nil {
		return nil, err
	}
	return a.promiseDTOs(list), nil
}

// PromisesForMessage returns the promises of the mail a stored message is a
// copy of, dismissed ones left out.
func (a *App) PromisesForMessage(messageID int64) ([]PromiseDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return nil, err
	}
	mailID := strings.Trim(strings.TrimSpace(m.MessageID), "<>")
	if mailID == "" {
		return []PromiseDTO{}, nil
	}
	list, err := a.promiseStore().ForMail(a.ctx, m.AccountID, mailID)
	if err != nil {
		return nil, err
	}
	list = slices.DeleteFunc(list, func(p promises.Promise) bool { return p.State == promises.StateDismissed })
	return a.promiseDTOs(list), nil
}

// FindPromises reads one message for promises now, whether or not it was
// read before and however old it is, and returns what it holds.
func (a *App) FindPromises(messageID int64) ([]PromiseDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return nil, err
	}
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		return nil, err
	}
	promiseMu.Lock()
	_, err = a.scanForPromises(a.ctx, *m, folderRole(*folder) == roleSent, true, true)
	promiseMu.Unlock()
	if err != nil {
		return nil, err
	}
	a.emit(EventPromisesChanged, nil)
	return a.PromisesForMessage(messageID)
}

// AcceptPromise makes a task of a suggestion, with the user's edits.
func (a *App) AcceptPromise(edit PromiseEditDTO) (PromiseDTO, error) {
	if err := a.ready(); err != nil {
		return PromiseDTO{}, err
	}
	p, err := a.acceptPromise(a.ctx, edit)
	if err != nil {
		return PromiseDTO{}, err
	}
	a.emit(EventPromisesChanged, nil)
	return a.promiseDTOs([]promises.Promise{p})[0], nil
}

func (a *App) acceptPromise(ctx context.Context, edit PromiseEditDTO) (promises.Promise, error) {
	s := a.promiseStore()
	p, err := s.Get(ctx, edit.ID)
	if err != nil {
		return p, err
	}
	if p.State != promises.StateSuggested {
		return p, errPromiseNotSuggested
	}
	if t := strings.TrimSpace(edit.Title); t != "" {
		p.Title = t
	}
	p.Due = strings.TrimSpace(edit.Due)
	if edit.Direction == string(promises.Mine) || edit.Direction == string(promises.Theirs) {
		p.Direction = promises.Direction(edit.Direction)
	}
	if e := strings.TrimSpace(edit.PersonEmail); e != "" {
		p.PersonEmail, p.PersonName = e, strings.TrimSpace(edit.PersonName)
	}
	list, err := a.promiseTaskList()
	if err != nil {
		return p, err
	}
	req := TaskRequest{
		ListID:    list,
		Title:     p.Title,
		Notes:     p.Quote,
		Due:       p.Due,
		MailLinks: []string{midLink(p.MailID)},
	}
	if p.PersonEmail != "" {
		req.Contacts = []TaskContactDTO{{Name: p.PersonName, Email: p.PersonEmail}}
	}
	res, err := a.CreateTask(req)
	if err != nil {
		return p, err
	}
	p.State, p.TaskUID = promises.StateAccepted, res.Task.UID
	if err := s.Save(ctx, p); err != nil {
		return p, err
	}
	return p, nil
}

// promiseTaskList is the list an accepted promise's task goes to: the first
// writable list on this device, or the default list, as the setting says.
func (a *App) promiseTaskList() (int64, error) {
	if a.stringSetting(settingPromisesTaskList, "local") == "default" {
		cal, err := a.writableTaskList(0)
		if err != nil {
			return 0, err
		}
		return cal.ID, nil
	}
	cals, err := a.store.ListCalendars(a.ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range cals {
		if c.Local() && c.HoldsTasks() && !c.ReadOnly {
			return c.ID, nil
		}
	}
	return 0, errPromiseNoTaskList
}

// DismissPromise sets a suggestion aside for good: a resync never brings it
// back.
func (a *App) DismissPromise(id int64) error {
	return a.setPromiseState(id, promises.StateSuggested, promises.StateDismissed)
}

// RestorePromise brings a dismissed suggestion back, which is what undo of a
// dismissal does.
func (a *App) RestorePromise(id int64) error {
	return a.setPromiseState(id, promises.StateDismissed, promises.StateSuggested)
}

func (a *App) setPromiseState(id int64, from, to promises.State) error {
	if err := a.ready(); err != nil {
		return err
	}
	s := a.promiseStore()
	p, err := s.Get(a.ctx, id)
	if err != nil {
		return err
	}
	if p.State != from {
		return errPromiseNotSuggested
	}
	p.State = to
	if err := s.Save(a.ctx, p); err != nil {
		return err
	}
	a.emit(EventPromisesChanged, nil)
	return nil
}

// QuietPromiseSender stops reading a sender's mail for promises and dismisses
// the suggestions about them. It returns the ids it dismissed, for undo.
func (a *App) QuietPromiseSender(email string) ([]int64, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	ids, err := a.promiseStore().Quiet(a.ctx, bareAddress(email))
	if err != nil {
		return nil, err
	}
	a.emit(EventPromisesChanged, nil)
	if ids == nil {
		ids = []int64{}
	}
	return ids, nil
}

// UnquietPromiseSender undoes QuietPromiseSender.
func (a *App) UnquietPromiseSender(email string, ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.promiseStore().Unquiet(a.ctx, bareAddress(email), ids); err != nil {
		return err
	}
	a.emit(EventPromisesChanged, nil)
	return nil
}

// ReopenPromise undoes an automatic close or turns down an offer to complete:
// the promise is open again, its task too, and the reply that closed it is
// remembered so it does not close it again.
func (a *App) ReopenPromise(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	s := a.promiseStore()
	p, err := s.Get(a.ctx, id)
	if err != nil {
		return err
	}
	if p.State != promises.StateClosed && p.ReplyID == "" {
		return nil
	}
	if p.ReplyID != "" && !slices.Contains(p.KeepOpen, p.ReplyID) {
		p.KeepOpen = append(p.KeepOpen, p.ReplyID)
	}
	if p.State == promises.StateClosed {
		if taskID := a.promiseTaskID(p.TaskUID); taskID != 0 && a.taskDone(taskID) {
			if _, err := a.SetTaskDone(taskID, false); err != nil {
				return err
			}
		}
		p.State = promises.StateAccepted
	}
	p.ReplyID, p.ReplyAt = "", time.Time{}
	if err := s.Save(a.ctx, p); err != nil {
		return err
	}
	a.emit(EventPromisesChanged, nil)
	return nil
}

// promiseTaskID is the row of the task with this UID, 0 when it is gone.
func (a *App) promiseTaskID(uid string) int64 {
	if uid == "" {
		return 0
	}
	cals, err := a.store.ListCalendars(a.ctx)
	if err != nil {
		return 0
	}
	for _, c := range cals {
		if !c.HoldsTasks() {
			continue
		}
		if r, err := a.store.GetCalendarEventByUID(a.ctx, c.ID, uid); err == nil && r.IsTask() && r.Pending != storage.PendingDelete {
			return r.ID
		}
	}
	return 0
}

// promiseDTOs turns stored promises into DTOs, finding a cached copy of each
// mail and whether a follow-up reminder watches its thread.
func (a *App) promiseDTOs(list []promises.Promise) []PromiseDTO {
	watched := map[int64]bool{}
	if fus, err := a.store.ListFollowUps(a.ctx); err == nil {
		for _, f := range fus {
			if c, ok := a.sentCopy(f); ok && c.ThreadID != 0 {
				watched[c.ThreadID] = true
			}
		}
	}
	out := make([]PromiseDTO, 0, len(list))
	for _, p := range list {
		dto := PromiseDTO{
			ID: p.ID, AccountID: p.AccountID, MailID: p.MailID, ThreadID: p.ThreadID, Direction: string(p.Direction),
			PersonName: p.PersonName, PersonEmail: p.PersonEmail, Title: p.Title, Quote: p.Quote, Due: p.Due,
			Confidence: p.Confidence, Source: p.Source, State: string(p.State), TaskUID: p.TaskUID,
			ReplyID: p.ReplyID, ReplyAt: formatRFC3339(p.ReplyAt), MailDate: formatRFC3339(p.MailDate),
		}
		if copies, err := a.store.CopiesOf(a.ctx, p.AccountID, p.MailID); err == nil && len(copies) > 0 {
			dto.MessageID = copies[0].ID
			if dto.ThreadID == 0 {
				dto.ThreadID = copies[0].ThreadID
			}
		}
		if p.ReplyID != "" {
			if copies, err := a.store.CopiesOf(a.ctx, p.AccountID, p.ReplyID); err == nil && len(copies) > 0 {
				dto.ReplyMessageID = copies[0].ID
			}
		}
		dto.FollowUp = dto.ThreadID != 0 && watched[dto.ThreadID]
		out = append(out, dto)
	}
	return out
}

// promisesAfterSync reads the new mail of a synced folder in the background:
// replies settle open promises first, then the mail itself is read.
func (a *App) promisesAfterSync(f storage.Folder, ids []int64) {
	role := folderRole(f)
	if len(ids) == 0 || (role != roleInbox && role != roleSent) || !a.promisesActive() {
		return
	}
	goSafe("reading new mail for promises", func() {
		promiseMu.Lock()
		defer promiseMu.Unlock()
		a.readPromisesIn(ids, role == roleSent)
	})
}

func (a *App) readPromisesIn(ids []int64, sent bool) {
	if len(ids) > promiseBatchCap {
		ids = ids[len(ids)-promiseBatchCap:]
	}
	ctx := withScriptBudget(a.ctx)
	changed := false
	aiLeft := promiseAIPerBatch
	for _, id := range ids {
		if a.ctx.Err() != nil {
			return
		}
		m, err := a.store.GetMessage(a.ctx, id)
		if err != nil {
			continue
		}
		if a.settleByReply(*m, sent) {
			changed = true
		}
		if time.Since(m.Date) > promiseMaxAge {
			continue
		}
		found, err := a.scanForPromises(ctx, *m, sent, false, aiLeft > 0)
		if err != nil {
			a.log.Warn("promises: read a message", "message", id, "err", err)
			continue
		}
		if found.ai {
			aiLeft--
		}
		changed = changed || found.added > 0
	}
	if changed {
		a.emit(EventPromisesChanged, nil)
	}
}

type scanResult struct {
	added int
	ai    bool
}

// scanForPromises reads one message and stores what it finds. A message read
// before is skipped unless force is set, and so is mail that is not from a
// person or from an account that has promises off.
func (a *App) scanForPromises(ctx context.Context, m storage.Message, sent, force, aiAllowed bool) (scanResult, error) {
	var res scanResult
	mailID := strings.Trim(strings.TrimSpace(m.MessageID), "<>")
	if mailID == "" {
		return res, nil
	}
	sw := a.promiseAccounts()[strconv.FormatInt(m.AccountID, 10)]
	if sw.Off {
		return res, nil
	}
	s := a.promiseStore()
	if !force {
		done, err := s.Scanned(ctx, m.AccountID, mailID)
		if err != nil || done {
			return res, err
		}
	}
	own, err := a.ownAddresses(ctx)
	if err != nil {
		return res, err
	}
	from := bareAddress(m.FromAddress)
	person, personName := from, m.FromName
	if sent != own[from] && !force {
		return res, s.MarkScanned(ctx, m.AccountID, mailID, "skipped")
	}
	if sent {
		to := parseAddresses(m.ToAddresses)
		if len(to) == 0 {
			return res, s.MarkScanned(ctx, m.AccountID, mailID, "skipped")
		}
		person, personName = strings.ToLower(to[0].Address), to[0].Name
	} else if m.MailKind != 0 || looksAutomated(from) {
		return res, s.MarkScanned(ctx, m.AccountID, mailID, "skipped")
	}
	if quiet, err := s.IsQuiet(ctx, person); err != nil || (quiet && !force) {
		return res, err
	}
	addressed := sent || slices.ContainsFunc(parseAddresses(m.ToAddresses), func(ad *mail.Address) bool { return own[strings.ToLower(ad.Address)] })
	in := promises.Input{
		Text:     quoteText(m.BodyPlain, m.BodyHTML),
		Clock:    promises.Clock{Ref: m.Date, Location: time.Local, WeekStart: a.promiseWeekStart()},
		LangHint: a.stringSetting(settingLanguage, "en"),
	}
	found, source := promises.Detect(in), "rules"
	if sw.AI && aiAllowed && promises.HasCue(in.Text, in.LangHint) {
		if checked, err := a.checkPromisesWithAI(ctx, sw.Provider, in); err != nil {
			a.log.Warn("promises: ai check", "err", err)
		} else if checked != nil {
			found, source, res.ai = checked, "ai", true
		}
	}
	auto := sent && a.boolSetting(settingPromisesAutoMine, false)
	for _, c := range found {
		if c.Kind == promises.Request && !addressed {
			continue
		}
		p := promises.Promise{
			AccountID: m.AccountID, MailID: mailID, ThreadID: m.ThreadID, Direction: promiseDirection(c.Owner, sent),
			PersonName: personName, PersonEmail: person, Title: promiseTitle(c.Quote), Quote: strings.Join(strings.Fields(c.Quote), " "),
			Due: c.Due.String(), Confidence: c.Confidence, Source: source, MailDate: m.Date,
		}
		added, err := s.Add(ctx, &p)
		if err != nil {
			return res, err
		}
		if !added {
			continue
		}
		res.added++
		if auto && p.Direction == promises.Mine {
			if _, err := a.acceptPromise(ctx, PromiseEditDTO{ID: p.ID, Title: p.Title, Due: p.Due}); err != nil {
				a.log.Warn("promises: accept on its own", "promise", p.ID, "err", err)
			}
			continue
		}
		a.scriptEventBudgeted(ctx, triggerPromiseFound, map[string]any{"promise_id": p.ID})
	}
	return res, s.MarkScanned(ctx, m.AccountID, mailID, source)
}

// promiseDirection says who owes what a message's author wrote: the author's
// own commitment is owed by whoever wrote it, a request by the reader.
func promiseDirection(owner promises.Owner, sent bool) promises.Direction {
	if (owner == promises.OwnerAuthor) == sent {
		return promises.Mine
	}
	return promises.Theirs
}

// promiseTitle is a task title made from the sentence: on one line and at
// most 120 characters.
func promiseTitle(quote string) string {
	title := strings.Join(strings.Fields(quote), " ")
	if r := []rune(title); len(r) > 120 {
		title = strings.TrimSpace(string(r[:119])) + "…"
	}
	return title
}

func (a *App) promiseWeekStart() time.Weekday {
	if ws := a.intSetting(settingCalendarWeekStart, -1); ws >= 0 && ws <= 6 {
		return time.Weekday(ws)
	}
	return time.Monday
}

// checkPromisesWithAI asks the account's provider. It returns nil without an
// error when the day's checks are used up.
func (a *App) checkPromisesWithAI(ctx context.Context, providerID string, in promises.Input) ([]promises.Candidate, error) {
	promiseAI.mu.Lock()
	today := time.Now().Format(dateLayout)
	if promiseAI.day != today {
		promiseAI.day, promiseAI.count = today, 0
	}
	if promiseAI.count >= promiseAIPerDay {
		promiseAI.mu.Unlock()
		return nil, nil
	}
	promiseAI.count++
	promiseAI.mu.Unlock()

	aiMu.Lock()
	recs := a.aiRecords()
	aiMu.Unlock()
	i := slices.IndexFunc(recs, func(r aiRecord) bool { return r.ID == providerID })
	if i < 0 {
		return nil, ai.ErrNoProvider
	}
	rec := recs[i]
	kind, ok := ai.KindByID(rec.Kind)
	if !ok {
		return nil, ai.ErrNoProvider
	}
	key := ""
	if rec.HasKey {
		k, err := credentials.LoadAIKey(rec.ID)
		if err != nil {
			return nil, err
		}
		key = k
	}
	ctx, cancel := context.WithTimeout(ctx, promiseAITimeout)
	defer cancel()
	ep, _, err := a.aiNetwork().Open(ctx, rec.Provider, key, promiseAITimeout)
	if err != nil {
		return nil, err
	}
	chat := func(ctx context.Context, req ai.Request) (ai.Response, error) { return kind.Driver.Chat(ctx, ep, req) }
	return promises.CheckWithAI(ctx, chat, rec.Model, in)
}

// settleByReply closes the open promises a new message answers. A reply from
// the person who owes a promise closes it; a reply from the user offers to
// complete the promises they owe in that thread. It reports whether anything
// changed.
func (a *App) settleByReply(m storage.Message, sent bool) bool {
	replyID := strings.Trim(strings.TrimSpace(m.MessageID), "<>")
	if replyID == "" || m.ThreadID == 0 {
		return false
	}
	s := a.promiseStore()
	open, err := s.List(a.ctx, promises.StateAccepted)
	if err != nil || len(open) == 0 {
		return false
	}
	from := bareAddress(m.FromAddress)
	recipients := map[string]bool{}
	for _, ad := range parseAddresses(joinCSV(m.ToAddresses, m.CcAddresses)) {
		recipients[strings.ToLower(ad.Address)] = true
	}
	changed := false
	for _, p := range open {
		if p.AccountID != m.AccountID || p.MailID == replyID || slices.Contains(p.KeepOpen, replyID) || !m.Date.After(p.MailDate) {
			continue
		}
		if a.promiseThread(&p) != m.ThreadID {
			continue
		}
		switch {
		case !sent && p.Direction == promises.Theirs && strings.EqualFold(from, p.PersonEmail):
			taskID := a.promiseTaskID(p.TaskUID)
			if taskID != 0 && !a.taskDone(taskID) {
				if _, err := a.SetTaskDone(taskID, true); err != nil {
					a.log.Warn("promises: complete the task", "promise", p.ID, "err", err)
					continue
				}
			}
			p.State = promises.StateClosed
		case sent && p.Direction == promises.Mine && p.ReplyID == "" && (p.PersonEmail == "" || recipients[strings.ToLower(p.PersonEmail)]):
			if taskID := a.promiseTaskID(p.TaskUID); taskID == 0 || a.taskDone(taskID) {
				continue
			}
		default:
			continue
		}
		p.ReplyID, p.ReplyAt = replyID, m.Date
		if err := s.Save(a.ctx, p); err != nil {
			a.log.Warn("promises: settle by reply", "promise", p.ID, "err", err)
			continue
		}
		changed = true
	}
	return changed
}

// promiseThread is the conversation a promise's mail is in, looked up and
// kept when it was not known yet.
func (a *App) promiseThread(p *promises.Promise) int64 {
	if p.ThreadID != 0 {
		return p.ThreadID
	}
	if copies, err := a.store.CopiesOf(a.ctx, p.AccountID, p.MailID); err == nil {
		for _, c := range copies {
			if c.ThreadID != 0 {
				p.ThreadID = c.ThreadID
				break
			}
		}
	}
	return p.ThreadID
}
