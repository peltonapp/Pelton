package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrCalendarAccountNotFound is returned when a calendar account id has no row.
var ErrCalendarAccountNotFound = errors.New("storage: calendar account not found")

// ErrCalendarNotFound is returned when a calendar id has no row.
var ErrCalendarNotFound = errors.New("storage: calendar not found")

// ErrCalendarEventNotFound is returned when an event has no row.
var ErrCalendarEventNotFound = errors.New("storage: calendar event not found")

// The pending states of an event changed here that the server has not taken.
const (
	PendingCreate = "create"
	PendingUpdate = "update"
	PendingDelete = "delete"
)

// CalendarAccount is one CalDAV login. Its password is in the os keyring, or
// is the one of the address book it shares a server with.
type CalendarAccount struct {
	ID int64
	// AccountID is the mail account it was discovered from, 0 for none.
	AccountID int64
	// AddressBookID is the CardDAV address book whose login it reuses, 0 for
	// an account with its own.
	AddressBookID int64
	Name          string
	URL           string
	Username      string
	LastSyncAt    string
	LastError     string
	CreatedAt     string
}

// Calendar is one calendar, synced or local.
type Calendar struct {
	ID int64
	// AccountID is the calendar account it syncs through, 0 for a local
	// calendar.
	AccountID   int64
	Path        string
	Name        string
	Description string
	Color       string
	// ColorSet is true once the user picked the color here, so a sync leaves
	// it alone.
	ColorSet   bool
	CTag       string
	SyncToken  string
	ReadOnly   bool
	Visible    bool
	Position   int
	LastSyncAt string
	LastError  string
	CreatedAt  string
}

// Local reports whether the calendar lives only in this database.
func (c Calendar) Local() bool {
	return c.AccountID == 0
}

// CalendarEvent is one calendar object resource: an event with its overrides,
// as iCalendar text plus the columns a window query needs.
type CalendarEvent struct {
	ID         int64
	CalendarID int64
	UID        string
	Path       string
	ETag       string
	Data       string
	Summary    string
	Location   string
	// RangeStart and RangeEnd bound the occurrences. A zero RangeEnd is a
	// series with no end.
	RangeStart time.Time
	RangeEnd   time.Time
	Recurring  bool
	HasAlarms  bool
	// Pending is the change the server has not taken yet, empty when none.
	Pending   string
	Conflict  bool
	SyncError string
	UpdatedAt string
}

const calendarAccountColumns = `id, coalesce(account_id, 0), coalesce(address_book_id, 0), name, url,
       username, last_sync_at, last_error, created_at`

// CreateCalendarAccount inserts a calendar account and returns its new id.
func (d *DB) CreateCalendarAccount(ctx context.Context, a *CalendarAccount) (int64, error) {
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO calendar_accounts (account_id, address_book_id, name, url, username, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		nullableAccount(a.AccountID), nullableAccount(a.AddressBookID), a.Name, a.URL, a.Username, nowText())
	if err != nil {
		return 0, fmt.Errorf("storage: insert calendar account %q: %w", a.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: calendar account insert id: %w", err)
	}
	a.ID = id
	return id, nil
}

// ListCalendarAccounts returns every calendar account by id.
func (d *DB) ListCalendarAccounts(ctx context.Context) ([]CalendarAccount, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT `+calendarAccountColumns+` FROM calendar_accounts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list calendar accounts: %w", err)
	}
	defer rows.Close()
	var out []CalendarAccount
	for rows.Next() {
		a, err := scanCalendarAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan calendar account: %w", err)
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// GetCalendarAccount returns one calendar account, or ErrCalendarAccountNotFound.
func (d *DB) GetCalendarAccount(ctx context.Context, id int64) (*CalendarAccount, error) {
	a, err := scanCalendarAccount(d.sql.QueryRowContext(ctx,
		`SELECT `+calendarAccountColumns+` FROM calendar_accounts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCalendarAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get calendar account %d: %w", id, err)
	}
	return a, nil
}

// UpdateCalendarAccount saves an account's name, location, login and the
// address book it shares a login with.
func (d *DB) UpdateCalendarAccount(ctx context.Context, a CalendarAccount) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE calendar_accounts SET name = ?, url = ?, username = ?, address_book_id = ? WHERE id = ?`,
		a.Name, a.URL, a.Username, nullableAccount(a.AddressBookID), a.ID)
	if err != nil {
		return fmt.Errorf("storage: update calendar account %d: %w", a.ID, err)
	}
	return requireOneRow(res, ErrCalendarAccountNotFound)
}

// DeleteCalendarAccount removes an account with its calendars and their
// events. Nothing is deleted on the server.
func (d *DB) DeleteCalendarAccount(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM calendar_accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete calendar account %d: %w", id, err)
	}
	return requireOneRow(res, ErrCalendarAccountNotFound)
}

// RecordCalendarAccountSync stores the outcome of an account's sync: the time
// on success, the error on failure.
func (d *DB) RecordCalendarAccountSync(ctx context.Context, id int64, failure string) error {
	query := `UPDATE calendar_accounts SET last_sync_at = ?, last_error = '' WHERE id = ?`
	arg := nowText()
	if failure != "" {
		query = `UPDATE calendar_accounts SET last_error = ? WHERE id = ?`
		arg = failure
	}
	if _, err := d.sql.ExecContext(ctx, query, arg, id); err != nil {
		return fmt.Errorf("storage: record calendar account %d sync: %w", id, err)
	}
	return nil
}

const calendarColumns = `id, coalesce(calendar_account_id, 0), path, name, description, color, color_set,
       ctag, sync_token, read_only, visible, position, last_sync_at, last_error, created_at`

// CreateCalendar inserts a calendar and returns its new id.
func (d *DB) CreateCalendar(ctx context.Context, c *Calendar) (int64, error) {
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO calendars (calendar_account_id, path, name, description, color, color_set, read_only, visible, position, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableAccount(c.AccountID), c.Path, c.Name, c.Description, c.Color, boolToInt(c.ColorSet),
		boolToInt(c.ReadOnly), boolToInt(c.Visible), c.Position, nowText())
	if err != nil {
		return 0, fmt.Errorf("storage: insert calendar %q: %w", c.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: calendar insert id: %w", err)
	}
	c.ID = id
	return id, nil
}

// ListCalendars returns every calendar: local ones first, then by account, in
// the order the user arranged them.
func (d *DB) ListCalendars(ctx context.Context) ([]Calendar, error) {
	return d.listCalendars(ctx, `SELECT `+calendarColumns+` FROM calendars
ORDER BY calendar_account_id IS NOT NULL, calendar_account_id, position, id`)
}

// ListAccountCalendars returns the calendars synced through one account.
func (d *DB) ListAccountCalendars(ctx context.Context, accountID int64) ([]Calendar, error) {
	return d.listCalendars(ctx, `SELECT `+calendarColumns+` FROM calendars
WHERE calendar_account_id = ? ORDER BY position, id`, accountID)
}

func (d *DB) listCalendars(ctx context.Context, query string, args ...any) ([]Calendar, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list calendars: %w", err)
	}
	defer rows.Close()
	var out []Calendar
	for rows.Next() {
		c, err := scanCalendar(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan calendar: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetCalendar returns one calendar, or ErrCalendarNotFound.
func (d *DB) GetCalendar(ctx context.Context, id int64) (*Calendar, error) {
	c, err := scanCalendar(d.sql.QueryRowContext(ctx, `SELECT `+calendarColumns+` FROM calendars WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCalendarNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get calendar %d: %w", id, err)
	}
	return c, nil
}

// UpdateCalendar saves a calendar's descriptive fields and flags. The sync
// markers are written by RecordCalendarSync.
func (d *DB) UpdateCalendar(ctx context.Context, c Calendar) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE calendars SET path = ?, name = ?, description = ?, color = ?, color_set = ?, read_only = ?,
       visible = ?, position = ?
WHERE id = ?`,
		c.Path, c.Name, c.Description, c.Color, boolToInt(c.ColorSet), boolToInt(c.ReadOnly),
		boolToInt(c.Visible), c.Position, c.ID)
	if err != nil {
		return fmt.Errorf("storage: update calendar %d: %w", c.ID, err)
	}
	return requireOneRow(res, ErrCalendarNotFound)
}

// DeleteCalendar removes a calendar and its events here.
func (d *DB) DeleteCalendar(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM calendars WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete calendar %d: %w", id, err)
	}
	return requireOneRow(res, ErrCalendarNotFound)
}

// RecordCalendarSync stores the outcome of a calendar's sync: the new change
// markers on success, or the error with the markers left alone so the next
// attempt resumes from the same place.
func (d *DB) RecordCalendarSync(ctx context.Context, id int64, ctag, token, failure string) error {
	var err error
	if failure != "" {
		_, err = d.sql.ExecContext(ctx, `UPDATE calendars SET last_error = ? WHERE id = ?`, failure, id)
	} else {
		_, err = d.sql.ExecContext(ctx,
			`UPDATE calendars SET ctag = ?, sync_token = ?, last_sync_at = ?, last_error = '' WHERE id = ?`,
			ctag, token, nowText(), id)
	}
	if err != nil {
		return fmt.Errorf("storage: record calendar %d sync: %w", id, err)
	}
	return nil
}

const eventColumns = `id, calendar_id, uid, path, etag, data, summary, location, range_start, range_end,
       recurring, has_alarms, pending, conflict, sync_error, updated_at`

// SaveCalendarEvent inserts or updates an event by its calendar and UID.
func (d *DB) SaveCalendarEvent(ctx context.Context, e *CalendarEvent) error {
	const query = `
INSERT INTO calendar_events (calendar_id, uid, path, etag, data, summary, location, range_start, range_end,
                             recurring, has_alarms, pending, conflict, sync_error, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(calendar_id, uid) DO UPDATE SET
    path = excluded.path, etag = excluded.etag, data = excluded.data, summary = excluded.summary,
    location = excluded.location, range_start = excluded.range_start, range_end = excluded.range_end,
    recurring = excluded.recurring, has_alarms = excluded.has_alarms, pending = excluded.pending,
    conflict = excluded.conflict, sync_error = excluded.sync_error, updated_at = excluded.updated_at`
	if _, err := d.sql.ExecContext(ctx, query,
		e.CalendarID, e.UID, e.Path, e.ETag, e.Data, e.Summary, e.Location,
		formatTime(e.RangeStart), formatTime(e.RangeEnd), boolToInt(e.Recurring), boolToInt(e.HasAlarms),
		e.Pending, boolToInt(e.Conflict), e.SyncError, nowText()); err != nil {
		return fmt.Errorf("storage: save event %q: %w", e.UID, err)
	}
	var id int64
	if err := d.sql.QueryRowContext(ctx,
		`SELECT id FROM calendar_events WHERE calendar_id = ? AND uid = ?`, e.CalendarID, e.UID).Scan(&id); err != nil {
		return fmt.Errorf("storage: event id for %q: %w", e.UID, err)
	}
	e.ID = id
	return nil
}

// GetCalendarEvent returns one event, or ErrCalendarEventNotFound.
func (d *DB) GetCalendarEvent(ctx context.Context, id int64) (*CalendarEvent, error) {
	return d.getEvent(ctx, `SELECT `+eventColumns+` FROM calendar_events WHERE id = ?`, id)
}

// GetCalendarEventByUID returns the event with a UID in a calendar, or
// ErrCalendarEventNotFound.
func (d *DB) GetCalendarEventByUID(ctx context.Context, calendarID int64, uid string) (*CalendarEvent, error) {
	return d.getEvent(ctx, `SELECT `+eventColumns+` FROM calendar_events WHERE calendar_id = ? AND uid = ?`, calendarID, uid)
}

// FindCalendarEventsByUID returns the events with a UID in any calendar, which
// is how an invitation update finds the copy an earlier answer stored.
func (d *DB) FindCalendarEventsByUID(ctx context.Context, uid string) ([]CalendarEvent, error) {
	return d.queryEvents(ctx, `SELECT `+eventColumns+` FROM calendar_events WHERE uid = ? ORDER BY id`, uid)
}

// GetCalendarEventByPath returns the event stored at a server path, or
// ErrCalendarEventNotFound.
func (d *DB) GetCalendarEventByPath(ctx context.Context, calendarID int64, path string) (*CalendarEvent, error) {
	return d.getEvent(ctx, `SELECT `+eventColumns+` FROM calendar_events WHERE calendar_id = ? AND path = ?`, calendarID, path)
}

func (d *DB) getEvent(ctx context.Context, query string, args ...any) (*CalendarEvent, error) {
	e, err := scanEvent(d.sql.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCalendarEventNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get event: %w", err)
	}
	return e, nil
}

// ListCalendarEvents returns every event of a calendar, pending deletions
// included, which is what a sync compares against the server.
func (d *DB) ListCalendarEvents(ctx context.Context, calendarID int64) ([]CalendarEvent, error) {
	return d.queryEvents(ctx, `SELECT `+eventColumns+` FROM calendar_events WHERE calendar_id = ? ORDER BY id`, calendarID)
}

// ListPendingCalendarEvents returns the events of a calendar with a change the
// server has not taken, leaving out conflicts, which wait for the user.
func (d *DB) ListPendingCalendarEvents(ctx context.Context, calendarID int64) ([]CalendarEvent, error) {
	return d.queryEvents(ctx, `SELECT `+eventColumns+` FROM calendar_events
WHERE calendar_id = ? AND pending != '' AND conflict = 0 ORDER BY id`, calendarID)
}

// EventQuery selects the events that can have an occurrence in a window.
type EventQuery struct {
	From, To time.Time
	// CalendarIDs limits the query to those calendars; empty means all.
	CalendarIDs []int64
	// VisibleOnly leaves out calendars the user has hidden.
	VisibleOnly bool
	// AlarmsOnly keeps only events that carry a reminder.
	AlarmsOnly bool
}

// QueryCalendarEvents returns the events whose span reaches [From, To),
// pending deletions left out. The caller expands them: a recurring event is
// returned once, however many occurrences it has in the window.
func (d *DB) QueryCalendarEvents(ctx context.Context, q EventQuery) ([]CalendarEvent, error) {
	query := `SELECT ` + prefixed("e.", eventColumns) + `
FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
WHERE e.pending != 'delete' AND e.range_start < ? AND (e.range_end = '' OR e.range_end >= ?)`
	args := []any{formatTime(q.To), formatTime(q.From)}
	if q.VisibleOnly {
		query += ` AND c.visible = 1`
	}
	if q.AlarmsOnly {
		query += ` AND e.has_alarms = 1`
	}
	if len(q.CalendarIDs) > 0 {
		query += ` AND e.calendar_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(q.CalendarIDs)), ",") + `)`
		for _, id := range q.CalendarIDs {
			args = append(args, id)
		}
	}
	return d.queryEvents(ctx, query+` ORDER BY e.range_start`, args...)
}

func prefixed(prefix, columns string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func (d *DB) queryEvents(ctx context.Context, query string, args ...any) ([]CalendarEvent, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list events: %w", err)
	}
	defer rows.Close()
	var out []CalendarEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan event: %w", err)
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// DeleteCalendarEvent removes an event here.
func (d *DB) DeleteCalendarEvent(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM calendar_events WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete event %d: %w", id, err)
	}
	return requireOneRow(res, ErrCalendarEventNotFound)
}

// DeleteCalendarEventByPath removes the event stored at a server path, if
// any. A local change still waiting to be written is kept: the sync that
// reported the deletion did not know about it.
func (d *DB) DeleteCalendarEventByPath(ctx context.Context, calendarID int64, path string) error {
	if _, err := d.sql.ExecContext(ctx,
		`DELETE FROM calendar_events WHERE calendar_id = ? AND path = ? AND (pending = '' OR pending = 'delete')`,
		calendarID, path); err != nil {
		return fmt.Errorf("storage: delete event at %q: %w", path, err)
	}
	return nil
}

// ClearCalendarEvents drops the synced events of a calendar ahead of a full
// reread, keeping the ones with a change still waiting to be written.
func (d *DB) ClearCalendarEvents(ctx context.Context, calendarID int64) error {
	if _, err := d.sql.ExecContext(ctx,
		`DELETE FROM calendar_events WHERE calendar_id = ? AND pending = ''`, calendarID); err != nil {
		return fmt.Errorf("storage: clear calendar %d: %w", calendarID, err)
	}
	return nil
}

func scanCalendarAccount(row rowScanner) (*CalendarAccount, error) {
	var a CalendarAccount
	if err := row.Scan(&a.ID, &a.AccountID, &a.AddressBookID, &a.Name, &a.URL, &a.Username,
		&a.LastSyncAt, &a.LastError, &a.CreatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

func scanCalendar(row rowScanner) (*Calendar, error) {
	var (
		c                           Calendar
		colorSet, readOnly, visible int
	)
	if err := row.Scan(&c.ID, &c.AccountID, &c.Path, &c.Name, &c.Description, &c.Color, &colorSet,
		&c.CTag, &c.SyncToken, &readOnly, &visible, &c.Position, &c.LastSyncAt, &c.LastError, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.ColorSet, c.ReadOnly, c.Visible = colorSet != 0, readOnly != 0, visible != 0
	return &c, nil
}

func scanEvent(row rowScanner) (*CalendarEvent, error) {
	var (
		e                 CalendarEvent
		start, end        string
		recurring, alarms int
		conflict          int
	)
	if err := row.Scan(&e.ID, &e.CalendarID, &e.UID, &e.Path, &e.ETag, &e.Data, &e.Summary, &e.Location,
		&start, &end, &recurring, &alarms, &e.Pending, &conflict, &e.SyncError, &e.UpdatedAt); err != nil {
		return nil, err
	}
	var err error
	if e.RangeStart, err = parseTime(start); err != nil {
		return nil, err
	}
	if e.RangeEnd, err = parseTime(end); err != nil {
		return nil, err
	}
	e.Recurring, e.HasAlarms, e.Conflict = recurring != 0, alarms != 0, conflict != 0
	return &e, nil
}
