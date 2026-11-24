package desktop

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/caldav"
	"github.com/peltonapp/Pelton/internal/dav"
	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Calendar sync. It runs on its own goroutine, periodically and on demand,
// never on the ui thread. Each run first writes the changes made here that the
// server has not taken (the offline queue), then reads what changed on the
// server: through sync-collection when the server has it, else by comparing
// etags. A calendar whose ctag has not moved is not read at all.

// calendarSyncMu serializes sync runs, so a periodic run and the sync button
// never write the same calendar at once.
var calendarSyncMu sync.Mutex

// calendarPushMu serializes writes of single events to the server; see
// pushEvent.
var calendarPushMu sync.Mutex

// calendarAccountBudget bounds one account's sync run as a whole.
const calendarAccountBudget = 5 * time.Minute

// SyncCalendars syncs every calendar account now, for the button in the ui. It
// returns when the run is done, with the first failure.
func (a *App) SyncCalendars() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.syncAllCalendars(a.sessionCtx())
}

// syncCalendarsInBackground starts a run off the calling goroutine.
func (a *App) syncCalendarsInBackground() {
	goSafe("syncing calendars", func() {
		if err := a.syncAllCalendars(a.sessionCtx()); err != nil {
			a.log.Warn("calendar sync", "err", err)
		}
	})
}

// runCalendarSyncLoop syncs calendars at the interval the user set. A short
// base tick lets a changed interval or low-power mode take effect promptly.
func (a *App) runCalendarSyncLoop() {
	const baseTick = 30 * time.Second
	ticker := time.NewTicker(baseTick)
	defer ticker.Stop()
	var lastRun time.Time
	for {
		interval := time.Duration(a.intSetting(settingCalendarSync, 15)) * time.Minute
		if interval > 0 && !a.lowPowerMode() && time.Since(lastRun) >= interval {
			lastRun = time.Now()
			if err := a.syncAllCalendars(a.sessionCtx()); err != nil && a.ctx.Err() == nil {
				a.log.Warn("calendar sync", "err", err)
			}
		}
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// syncAllCalendars runs one sync over every account. One account failing does
// not stop the others; the first failure is returned.
func (a *App) syncAllCalendars(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	calendarSyncMu.Lock()
	defer calendarSyncMu.Unlock()

	accounts, err := a.store.ListCalendarAccounts(ctx)
	if err != nil || len(accounts) == 0 {
		return err
	}
	cals, err := a.store.ListCalendars(ctx)
	if err != nil {
		return err
	}
	progress := CalendarSyncEvent{Running: true, Total: len(cals)}
	a.emit(EventCalendarSync, progress)

	var changed []int64
	var failed error
	for _, acct := range accounts {
		if ctx.Err() != nil {
			break
		}
		ids, err := a.syncCalendarAccount(ctx, acct, func(cal storage.Calendar) {
			progress.CalendarID, progress.Calendar = cal.ID, cal.Name
			a.emit(EventCalendarSync, progress)
			progress.Done++
		})
		changed = append(changed, ids...)
		if err != nil {
			a.log.Warn("calendar account sync", "account", acct.ID, "err", err)
			if failed == nil {
				failed = err
			}
		}
	}
	done := CalendarSyncEvent{Done: progress.Done, Total: progress.Done}
	if failed != nil {
		done.Error = failed.Error()
	}
	a.emit(EventCalendarSync, done)
	if len(changed) > 0 {
		a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: changed})
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return failed
}

// syncCalendarAccount brings one account's calendars in line with the server:
// new calendars are added, ones gone from the server are dropped, and each is
// synced. It returns the ids of calendars that changed.
func (a *App) syncCalendarAccount(ctx context.Context, acct storage.CalendarAccount, report func(storage.Calendar)) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, calendarAccountBudget)
	defer cancel()
	ctx = withScriptBudget(ctx)
	client, err := a.calendarClient(acct)
	if err != nil {
		return nil, a.recordAccountFailure(ctx, acct, err)
	}
	remote, err := client.Calendars(ctx)
	if err != nil {
		return nil, a.recordAccountFailure(ctx, acct, err)
	}
	local, err := a.store.ListAccountCalendars(ctx, acct.ID)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]storage.Calendar, len(local))
	for _, c := range local {
		byPath[c.Path] = c
	}

	var changed []int64
	var failed error
	seen := map[string]bool{}
	for i, rc := range remote {
		seen[rc.Path] = true
		cal, exists := byPath[rc.Path]
		if !exists {
			cal = storage.Calendar{AccountID: acct.ID, Path: rc.Path, Visible: true, Position: i}
		}
		cal.Name, cal.Description, cal.ReadOnly = rc.Name, rc.Description, rc.ReadOnly
		cal.Components = rc.Components()
		if !cal.ColorSet {
			cal.Color = rc.Color
		}
		if exists {
			err = a.store.UpdateCalendar(ctx, cal)
		} else {
			_, err = a.store.CreateCalendar(ctx, &cal)
			changed = append(changed, cal.ID)
		}
		if err != nil {
			return changed, err
		}
		report(cal)
		didChange, err := a.syncCalendar(ctx, client, cal, rc)
		if didChange {
			changed = append(changed, cal.ID)
		}
		if err != nil {
			if ctx.Err() != nil {
				return changed, ctx.Err()
			}
			failed = err
			if recErr := a.store.RecordCalendarSync(ctx, cal.ID, "", "", err.Error()); recErr != nil {
				return changed, recErr
			}
		}
	}
	for _, c := range local {
		if !seen[c.Path] {
			if err := a.store.DeleteCalendar(ctx, c.ID); err != nil {
				return changed, err
			}
			changed = append(changed, c.ID)
		}
	}
	if failed != nil {
		return changed, a.recordAccountFailure(ctx, acct, failed)
	}
	return changed, a.store.RecordCalendarAccountSync(ctx, acct.ID, "")
}

func (a *App) recordAccountFailure(ctx context.Context, acct storage.CalendarAccount, err error) error {
	if ctx.Err() == nil {
		if recErr := a.store.RecordCalendarAccountSync(a.ctx, acct.ID, err.Error()); recErr != nil {
			return recErr
		}
	}
	return err
}

// syncCalendar pushes a calendar's queued changes and pulls the server's.
func (a *App) syncCalendar(ctx context.Context, client *caldav.Client, cal storage.Calendar, remote caldav.Calendar) (bool, error) {
	pending, err := a.store.ListPendingCalendarEvents(ctx, cal.ID)
	if err != nil {
		return false, err
	}
	changed := false
	for i := range pending {
		err := a.pushEvent(ctx, client, cal, &pending[i], false)
		switch {
		case err == nil, errors.Is(err, caldav.ErrConflict):
			changed = true
		case isNetworkError(err) || ctx.Err() != nil:
			return changed, err
		default:
			a.log.Warn("write calendar event", "event", pending[i].ID, "err", err)
		}
	}
	if len(pending) == 0 && cal.CTag != "" && remote.CTag == cal.CTag && remote.SyncToken == cal.SyncToken {
		return changed, nil
	}

	if remote.SyncToken != "" {
		pulled, err := a.pullBySyncToken(ctx, client, cal)
		if err == nil {
			if err := a.store.RecordCalendarSync(ctx, cal.ID, remote.CTag, pulled.token, ""); err != nil {
				return true, err
			}
			return changed || pulled.changed, nil
		}
		var status *dav.StatusError
		if !errors.As(err, &status) {
			return changed, err
		}
		// a server that lists a sync token but refuses the report is read by
		// etag instead.
	}
	pulledChange, err := a.pullByETag(ctx, client, cal)
	if err != nil {
		return changed, err
	}
	if err := a.store.RecordCalendarSync(ctx, cal.ID, remote.CTag, "", ""); err != nil {
		return true, err
	}
	return changed || pulledChange, nil
}

type pullResult struct {
	token   string
	changed bool
}

// pullBySyncToken reads what changed since the stored token. A token the
// server no longer accepts is answered with a full read, which also drops
// local events the server no longer has.
func (a *App) pullBySyncToken(ctx context.Context, client *caldav.Client, cal storage.Calendar) (pullResult, error) {
	token := cal.SyncToken
	changes, err := client.Sync(ctx, cal.Path, token)
	if errors.Is(err, caldav.ErrSyncTokenInvalid) {
		token = ""
		changes, err = client.Sync(ctx, cal.Path, "")
	}
	if err != nil {
		return pullResult{}, err
	}
	out := pullResult{token: changes.Token}
	present := map[string]bool{}
	for _, res := range changes.Updated {
		present[res.Path] = true
		stored, err := a.storeResource(ctx, cal, res)
		if err != nil {
			return out, err
		}
		out.changed = out.changed || stored
	}
	for _, gone := range changes.Deleted {
		if err := a.store.DeleteCalendarEventByPath(ctx, cal.ID, gone); err != nil {
			return out, err
		}
		out.changed = true
	}
	if token == "" {
		removed, err := a.dropMissing(ctx, cal, present)
		if err != nil {
			return out, err
		}
		out.changed = out.changed || removed
	}
	return out, nil
}

// pullByETag compares the server's etags with the local ones and fetches only
// what differs. A calendar holding events and tasks is listed once for each.
func (a *App) pullByETag(ctx context.Context, client *caldav.Client, cal storage.Calendar) (bool, error) {
	etags := map[string]string{}
	for _, kind := range []string{storage.KindEvent, storage.KindTask} {
		if (kind == storage.KindEvent && !cal.HoldsEvents()) || (kind == storage.KindTask && !cal.HoldsTasks()) {
			continue
		}
		listed, err := client.List(ctx, cal.Path, kind)
		if err != nil {
			return false, err
		}
		for p, etag := range listed {
			etags[p] = etag
		}
	}
	local, err := a.store.ListCalendarEvents(ctx, cal.ID)
	if err != nil {
		return false, err
	}
	have := make(map[string]string, len(local))
	for _, ev := range local {
		have[ev.Path] = ev.ETag
	}
	var need []string
	present := make(map[string]bool, len(etags))
	for p, etag := range etags {
		present[p] = true
		if current, ok := have[p]; !ok || current != etag || etag == "" {
			need = append(need, p)
		}
	}
	resources, err := client.Multiget(ctx, cal.Path, need)
	if err != nil {
		return false, err
	}
	changed := false
	for _, res := range resources {
		stored, err := a.storeResource(ctx, cal, res)
		if err != nil {
			return changed, err
		}
		changed = changed || stored
	}
	removed, err := a.dropMissing(ctx, cal, present)
	return changed || removed, err
}

// dropMissing removes synced events the server no longer lists. Events with a
// change still waiting to be written are kept.
func (a *App) dropMissing(ctx context.Context, cal storage.Calendar, present map[string]bool) (bool, error) {
	local, err := a.store.ListCalendarEvents(ctx, cal.ID)
	if err != nil {
		return false, err
	}
	removed := false
	for _, ev := range local {
		if ev.Path == "" || ev.Pending != "" || present[ev.Path] {
			continue
		}
		if err := a.store.DeleteCalendarEvent(ctx, ev.ID); err != nil {
			return removed, err
		}
		removed = true
	}
	return removed, nil
}

// storeResource writes one fetched resource. An event with a local change the
// server has not taken is left alone: the change is written next, against
// the etag it started from, and a conflict is reported then. It returns
// whether anything was stored.
func (a *App) storeResource(ctx context.Context, cal storage.Calendar, res caldav.Resource) (bool, error) {
	var (
		uid  string
		fill func(*storage.CalendarEvent)
	)
	if ical.HasTodo(res.Data) {
		obj, err := ical.ParseTodoObject(res.Data, time.Local)
		if err != nil {
			a.log.Warn("unreadable task resource", "path", res.Path, "err", err)
			return false, nil
		}
		uid = obj.Todo.UID
		fill = func(row *storage.CalendarEvent) { fillTaskRow(row, obj) }
	} else {
		obj, err := ical.ParseObject(res.Data, time.Local)
		if err != nil {
			a.log.Warn("unreadable calendar resource", "path", res.Path, "err", err)
			return false, nil
		}
		uid = obj.UID()
		fill = func(row *storage.CalendarEvent) { fillEventRow(row, obj) }
	}
	row, err := a.store.GetCalendarEventByPath(ctx, cal.ID, res.Path)
	if errors.Is(err, storage.ErrCalendarEventNotFound) {
		row, err = a.store.GetCalendarEventByUID(ctx, cal.ID, uid)
	}
	switch {
	case errors.Is(err, storage.ErrCalendarEventNotFound):
		row = &storage.CalendarEvent{CalendarID: cal.ID}
	case err != nil:
		return false, err
	case row.Pending != "":
		return false, nil
	case row.ETag == res.ETag && res.ETag != "":
		return false, nil
	}
	created, wasDone := row.ID == 0, row.TaskCompleted != ""
	fill(row)
	row.Data = string(res.Data)
	row.Path, row.ETag = res.Path, res.ETag
	row.Pending, row.Conflict, row.SyncError = "", false, ""
	if err := a.store.SaveCalendarEvent(ctx, row); err != nil {
		return false, err
	}
	a.scriptResourceStored(ctx, cal, *row, created, wasDone)
	if row.IsTask() {
		a.blocksFollowTask(ctx, row.UID, wasDone, row.TaskCompleted != "")
	}
	return true, nil
}

// pushEvent writes one event's pending change to the server under the etag it
// started from, or regardless when force is set. A refusal marks the event as
// a conflict and returns caldav.ErrConflict; any other failure is recorded on
// the event and returned, and the change stays queued.
//
// Pushes are serialized and the row is read again under the lock, so an edit
// written straight away and a sync draining the queue never send the same
// change twice, the second time against an etag the first one replaced.
func (a *App) pushEvent(ctx context.Context, client *caldav.Client, cal storage.Calendar, row *storage.CalendarEvent, force bool) error {
	calendarPushMu.Lock()
	defer calendarPushMu.Unlock()
	current, err := a.store.GetCalendarEvent(ctx, row.ID)
	if errors.Is(err, storage.ErrCalendarEventNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	*row = *current
	switch {
	case row.Pending == "":
		return nil
	case row.Pending == storage.PendingDelete:
		ifMatch := row.ETag
		if force {
			ifMatch = ""
		}
		err := client.Delete(ctx, row.Path, ifMatch)
		if err == nil || errors.Is(err, caldav.ErrNotFound) {
			return a.store.DeleteCalendarEvent(ctx, row.ID)
		}
		return a.markPushFailure(ctx, row, err)
	}

	p := row.Path
	pre := dav.PreconditionMatch
	if p == "" {
		p = caldav.ResourcePath(cal.Path, resourceName(row.UID))
	}
	if row.ETag == "" {
		pre = dav.PreconditionNew
	}
	if force {
		pre = dav.PreconditionNone
	}
	etag, err := client.Put(ctx, p, []byte(row.Data), pre, row.ETag)
	if err != nil {
		return a.markPushFailure(ctx, row, err)
	}
	if etag == "" {
		// a server that sends no etag on write has to be asked, or the next
		// edit would carry a stale precondition and be refused.
		if fetched, err := client.Get(ctx, p); err == nil {
			etag = fetched.ETag
		}
	}
	row.Path, row.ETag = p, etag
	row.Pending, row.Conflict, row.SyncError = "", false, ""
	return a.store.SaveCalendarEvent(ctx, row)
}

func (a *App) markPushFailure(ctx context.Context, row *storage.CalendarEvent, err error) error {
	if errors.Is(err, caldav.ErrConflict) {
		row.Conflict = true
	}
	row.SyncError = err.Error()
	if saveErr := a.store.SaveCalendarEvent(ctx, row); saveErr != nil {
		return saveErr
	}
	return err
}

var plainName = regexp.MustCompile(`^[A-Za-z0-9@._-]{1,200}$`)

// resourceName is the file name a new event gets: its UID when that is a
// plain name, else a hash of it, since some UIDs carry characters servers
// mangle in a path.
func resourceName(uid string) string {
	if plainName.MatchString(uid) {
		return uid
	}
	sum := sha1.Sum([]byte(uid))
	return hex.EncodeToString(sum[:])
}

// calendarClientFor opens the client for a synced calendar's account.
func (a *App) calendarClientFor(ctx context.Context, cal storage.Calendar) (*caldav.Client, error) {
	acct, err := a.store.GetCalendarAccount(ctx, cal.AccountID)
	if err != nil {
		return nil, err
	}
	return a.calendarClient(*acct)
}

// commitEvent stores an edited object and, for a synced calendar, writes it to
// the server right away. rid names the occurrence the result describes, zero
// for the event as a whole.
func (a *App) commitEvent(ctx context.Context, cal storage.Calendar, row storage.CalendarEvent, obj *ical.Object, rid time.Time, force bool) (EventSaveResultDTO, error) {
	fillEventRow(&row, obj)
	row.SyncError = ""
	if cal.Local() {
		row.Pending, row.Conflict = "", false
	} else if row.Pending != storage.PendingCreate {
		row.Pending = storage.PendingUpdate
		if row.Path == "" {
			row.Pending = storage.PendingCreate
		}
	}
	if err := a.store.SaveCalendarEvent(ctx, &row); err != nil {
		return EventSaveResultDTO{}, err
	}

	var result EventSaveResultDTO
	if !cal.Local() {
		pushErr := a.pushNow(ctx, cal, &row, force, &result)
		if pushErr != nil {
			return EventSaveResultDTO{}, pushErr
		}
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	saved, err := a.store.GetCalendarEventByUID(ctx, cal.ID, row.UID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	savedObj, err := parseStoredEvent(*saved)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	if result.Event, err = occurrenceDTO(*saved, cal, savedObj, rid); err != nil {
		result.Event, err = occurrenceDTO(*saved, cal, savedObj, time.Time{})
	}
	return result, err
}

// pushNow tries to write a change straight away. An unreachable server queues
// it; a conflict fills in the server's version. Only a failure that is
// neither is returned, with the change still queued.
func (a *App) pushNow(ctx context.Context, cal storage.Calendar, row *storage.CalendarEvent, force bool, result *EventSaveResultDTO) error {
	out, err := a.pushRowNow(ctx, cal, row, force)
	if err != nil {
		return err
	}
	result.Queued, result.Conflict = out.queued, out.conflict
	if out.server != nil {
		if obj, err := ical.ParseObject(out.server.Data, time.Local); err == nil {
			shown := *row
			shown.Data, shown.ETag = string(out.server.Data), out.server.ETag
			result.Server, _ = occurrenceDTO(shown, cal, obj, time.Time{})
		}
	}
	return nil
}

// pushOutcome is what writing one change straight away came to: queued for
// the next sync, or refused as a conflict with the server's copy when it could
// be read.
type pushOutcome struct {
	queued   bool
	conflict bool
	server   *caldav.Resource
}

// pushRowNow writes an event's or a task's change straight away, the way
// pushNow describes, and reports the outcome without building a DTO.
func (a *App) pushRowNow(ctx context.Context, cal storage.Calendar, row *storage.CalendarEvent, force bool) (pushOutcome, error) {
	client, err := a.calendarClientFor(ctx, cal)
	if err != nil {
		return pushOutcome{}, err
	}
	pushCtx, cancel := context.WithTimeout(ctx, calendarTimeout)
	defer cancel()
	err = a.pushEvent(pushCtx, client, cal, row, force)
	switch {
	case err == nil:
		return pushOutcome{}, nil
	case errors.Is(err, caldav.ErrConflict):
		out := pushOutcome{conflict: true}
		if server, err := client.Get(pushCtx, row.Path); err == nil {
			out.server = &server
		}
		return out, nil
	case isNetworkError(err), errors.Is(err, context.DeadlineExceeded):
		return pushOutcome{queued: true}, nil
	}
	return pushOutcome{}, err
}

// deleteEventRow deletes an event here and on the server.
func (a *App) deleteEventRow(ctx context.Context, cal storage.Calendar, row storage.CalendarEvent, force bool) (EventSaveResultDTO, error) {
	if cal.Local() || row.Path == "" {
		if err := a.store.DeleteCalendarEvent(ctx, row.ID); err != nil {
			return EventSaveResultDTO{}, err
		}
		a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
		return EventSaveResultDTO{}, nil
	}
	row.Pending = storage.PendingDelete
	if err := a.store.SaveCalendarEvent(ctx, &row); err != nil {
		return EventSaveResultDTO{}, err
	}
	var result EventSaveResultDTO
	if err := a.pushNow(ctx, cal, &row, force, &result); err != nil {
		return EventSaveResultDTO{}, err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	return result, nil
}

// moveEvent puts an edited event in another calendar: created there, then
// deleted from where it was.
func (a *App) moveEvent(row storage.CalendarEvent, from storage.Calendar, toID int64, obj *ical.Object) (EventSaveResultDTO, error) {
	to, err := a.store.GetCalendar(a.ctx, toID)
	if err != nil {
		return EventSaveResultDTO{}, err
	}
	if to.ReadOnly {
		return EventSaveResultDTO{}, errCalendarReadOnly
	}
	result, err := a.commitEvent(a.ctx, *to, newEventRow(*to, obj), obj, time.Time{}, false)
	if err != nil {
		return result, err
	}
	if _, err := a.deleteEventRow(a.ctx, from, row, false); err != nil {
		return result, err
	}
	return result, nil
}
