// bind_calendar.go is the calendar: CalDAV logins and the calendars under
// them, synced from the user's own server, and local calendars that live only
// here. Events, sync, reminders, invitations and .ics files are in the
// calendar_*.go and bind_calendar_*.go files next to this one.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/caldav"
	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/storage"
)

// calendarTimeout bounds one calendar request.
const calendarTimeout = 30 * time.Second

const (
	settingCalendarSync      = "calendar_sync_interval_minutes"
	settingCalendarReminders = "calendar_reminders"
	settingCalendarDefault   = "calendar_default_reminder_minutes"
	settingCalendarDefaultID = "calendar_default_id"
	settingCalendarView      = "calendar_default_view"
	settingCalendarWeekStart = "calendar_week_start"
	settingCalendarWorkStart = "calendar_work_start_minutes"
	settingCalendarWorkEnd   = "calendar_work_end_minutes"
	settingCalendarZoneShow  = "calendar_zone_display"
	settingCalendarZoneAlt   = "calendar_secondary_zone"
)

// The calendar views, the week starts and the time zone displays the settings
// accept. A week start of -1 follows the locale.
var (
	calendarViews        = []string{"month", "week", "day", "agenda", "year"}
	calendarZoneDisplays = []string{"off", "label", "dual"}
)

const (
	defaultWorkStart = 9 * 60
	defaultWorkEnd   = 17 * 60
	minutesPerDay    = 24 * 60
)

var (
	errCalendarNeedsURL      = errors.New("pelton: a calendar account needs a server address")
	errCalendarReadOnly      = errors.New("pelton: this calendar is read-only")
	errCalendarNotLocal      = errors.New("pelton: only a local calendar can be changed this way")
	errCalendarLastLocal     = errors.New("pelton: the last local calendar cannot be removed")
	errEventNeedsCalendar    = errors.New("pelton: choose a calendar for this event")
	errEventNeedsStart       = errors.New("pelton: an event needs a start")
	errEventEndBeforeStart   = errors.New("pelton: an event cannot end before it starts")
	errEventNoOccurrence     = errors.New("pelton: this occurrence is not part of the event")
	errEventNotInConflict    = errors.New("pelton: this event has no conflict to resolve")
	errInviteNotFound        = errors.New("pelton: this message carries no invitation")
	errInviteNoOrganizer     = errors.New("pelton: the invitation names no organizer to answer")
	errInviteUnknownResponse = errors.New("pelton: answer with accepted, tentative or declined")
	errCalendarSettings      = errors.New("pelton: these calendar settings are not valid")
)

// CalendarAccountDTO is one CalDAV login for the ui.
type CalendarAccountDTO struct {
	ID int64 `json:"id"`
	// AccountID is the mail account it was discovered from, 0 for none.
	AccountID int64 `json:"accountId"`
	// AddressBookID is the address book whose login it shares, 0 for none.
	AddressBookID int64  `json:"addressBookId"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	Username      string `json:"username"`
	// LastSync is rfc3339, empty when it never synced. LastError is the last
	// failure in the server's own words.
	LastSync      string `json:"lastSync"`
	LastError     string `json:"lastError"`
	HasPassword   bool   `json:"hasPassword"`
	CalendarCount int    `json:"calendarCount"`
}

// CalendarAccountRequest creates or updates a calendar account, and is what
// discovery starts from. An empty Password on update leaves the stored one
// alone. With an AddressBookID, the url, username and password default to
// that address book's.
type CalendarAccountRequest struct {
	ID            int64  `json:"id"`
	AccountID     int64  `json:"accountId"`
	AddressBookID int64  `json:"addressBookId"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

// DiscoveredCalendarDTO is one calendar a login can see, offered before the
// account is added.
type DiscoveredCalendarDTO struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Color    string `json:"color"`
	ReadOnly bool   `json:"readOnly"`
}

// CalendarDiscoveryDTO is what discovery found: the endpoint it resolved to,
// which is the url to add the account with, and the calendars there.
type CalendarDiscoveryDTO struct {
	URL       string                  `json:"url"`
	Username  string                  `json:"username"`
	Calendars []DiscoveredCalendarDTO `json:"calendars"`
	// Exists is true when an account for this url and login is already
	// configured.
	Exists bool `json:"exists"`
}

// CalendarDTO is one calendar for the sidebar and pickers.
type CalendarDTO struct {
	ID int64 `json:"id"`
	// AccountID is the calendar account, 0 for a local calendar.
	AccountID   int64  `json:"accountId"`
	AccountName string `json:"accountName"`
	// Name is empty for the local calendar the app starts with, which the ui
	// labels itself.
	Name        string `json:"name"`
	Description string `json:"description"`
	// Color is #rrggbb, or empty for the ui to choose.
	Color     string `json:"color"`
	Local     bool   `json:"local"`
	ReadOnly  bool   `json:"readOnly"`
	Visible   bool   `json:"visible"`
	LastSync  string `json:"lastSync"`
	LastError string `json:"lastError"`
	// Default marks the calendar new events go to unless another is chosen.
	Default bool `json:"default"`
}

// CalendarSettingsDTO holds the calendar preferences.
type CalendarSettingsDTO struct {
	// SyncIntervalMinutes is how often calendars sync in the background; 0
	// turns the periodic sync off.
	SyncIntervalMinutes int `json:"syncIntervalMinutes"`
	// RemindersEnabled turns reminder notifications on or off.
	RemindersEnabled bool `json:"remindersEnabled"`
	// DefaultReminderMinutes is the reminder a new event gets, -1 for none.
	DefaultReminderMinutes int `json:"defaultReminderMinutes"`
	// DefaultCalendarID is where new events go, 0 for the first writable one.
	DefaultCalendarID int64 `json:"defaultCalendarId"`
	// DefaultView is the view the calendar opens in: month, week, day,
	// agenda or year.
	DefaultView string `json:"defaultView"`
	// WeekStart is the first day of the week, 0 for Sunday through 6, or -1
	// to follow the locale.
	WeekStart int `json:"weekStart"`
	// WorkDayStart and WorkDayEnd are the working hours as minutes after
	// midnight; the time grid shades the hours outside them.
	WorkDayStart int `json:"workDayStart"`
	WorkDayEnd   int `json:"workDayEnd"`
	// TimeZoneDisplay is off, label (the zone's name over the hours) or dual
	// (a second column of hours in SecondaryTimeZone).
	TimeZoneDisplay   string `json:"timeZoneDisplay"`
	SecondaryTimeZone string `json:"secondaryTimeZone"`
}

// ListCalendarAccounts returns every CalDAV login.
func (a *App) ListCalendarAccounts() ([]CalendarAccountDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	accounts, err := a.store.ListCalendarAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CalendarAccountDTO, 0, len(accounts))
	for _, acct := range accounts {
		cals, err := a.store.ListAccountCalendars(a.ctx, acct.ID)
		if err != nil {
			return nil, err
		}
		password, err := a.calendarPassword(acct)
		if err != nil {
			a.log.Warn("read calendar password", "account", acct.ID, "err", err)
		}
		out = append(out, CalendarAccountDTO{
			ID:            acct.ID,
			AccountID:     acct.AccountID,
			AddressBookID: acct.AddressBookID,
			Name:          acct.Name,
			URL:           acct.URL,
			Username:      acct.Username,
			LastSync:      acct.LastSyncAt,
			LastError:     acct.LastError,
			HasPassword:   password != "",
			CalendarCount: len(cals),
		})
	}
	return out, nil
}

// DiscoverCalendars asks a server which calendars a login can see. With no
// url, an address book's url is tried first and then .well-known/caldav on
// the mail account's domain, so a server already holding the user's contacts
// or mail is one click.
func (a *App) DiscoverCalendars(req CalendarAccountRequest) (CalendarDiscoveryDTO, error) {
	if err := a.ready(); err != nil {
		return CalendarDiscoveryDTO{}, err
	}
	req, err := a.resolveCalendarRequest(req)
	if err != nil {
		return CalendarDiscoveryDTO{}, err
	}
	httpClient, err := a.contactsHTTPClient(req.AccountID)
	if err != nil {
		return CalendarDiscoveryDTO{}, err
	}
	if req.URL == "" && req.AccountID != 0 {
		account, err := a.store.GetAccount(a.ctx, req.AccountID)
		if err != nil {
			return CalendarDiscoveryDTO{}, err
		}
		ctx, cancel := context.WithTimeout(a.ctx, calendarTimeout)
		defer cancel()
		if req.URL, err = caldav.Discover(ctx, httpClient, account.Email); err != nil {
			return CalendarDiscoveryDTO{}, err
		}
	}
	if req.URL == "" {
		return CalendarDiscoveryDTO{}, errCalendarNeedsURL
	}
	client, err := caldav.Connect(caldav.Config{URL: req.URL, Username: req.Username, Password: req.Password, HTTP: httpClient})
	if err != nil {
		return CalendarDiscoveryDTO{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, calendarTimeout)
	defer cancel()
	found, err := client.Calendars(ctx)
	if err != nil {
		return CalendarDiscoveryDTO{}, err
	}
	out := CalendarDiscoveryDTO{URL: req.URL, Username: req.Username}
	for _, c := range found {
		out.Calendars = append(out.Calendars, DiscoveredCalendarDTO{Name: c.Name, Path: c.Path, Color: c.Color, ReadOnly: c.ReadOnly})
	}
	existing, err := a.store.ListCalendarAccounts(a.ctx)
	if err != nil {
		return CalendarDiscoveryDTO{}, err
	}
	for _, acct := range existing {
		if acct.URL == req.URL && acct.Username == req.Username {
			out.Exists = true
		}
	}
	return out, nil
}

// AddCalendarAccount stores a CalDAV login and starts its first sync in the
// background; the calendars arrive through calendar:changed.
func (a *App) AddCalendarAccount(req CalendarAccountRequest) (CalendarAccountDTO, error) {
	if err := a.ready(); err != nil {
		return CalendarAccountDTO{}, err
	}
	req, err := a.resolveCalendarRequest(req)
	if err != nil {
		return CalendarAccountDTO{}, err
	}
	if req.URL == "" {
		return CalendarAccountDTO{}, errCalendarNeedsURL
	}
	acct := storage.CalendarAccount{
		AccountID:     req.AccountID,
		AddressBookID: req.AddressBookID,
		Name:          req.Name,
		URL:           req.URL,
		Username:      req.Username,
	}
	id, err := a.store.CreateCalendarAccount(a.ctx, &acct)
	if err != nil {
		return CalendarAccountDTO{}, err
	}
	if req.AddressBookID == 0 || req.Password != a.addressBookPassword(req.AddressBookID) {
		if err := credentials.StoreCalendarPassword(id, req.Password); err != nil {
			_ = a.store.DeleteCalendarAccount(a.ctx, id)
			return CalendarAccountDTO{}, err
		}
	}
	a.syncCalendarsInBackground()
	return a.calendarAccountDTO(id)
}

// UpdateCalendarAccount saves an account's name, location and login. An empty
// password leaves the stored one alone.
func (a *App) UpdateCalendarAccount(req CalendarAccountRequest) (CalendarAccountDTO, error) {
	if err := a.ready(); err != nil {
		return CalendarAccountDTO{}, err
	}
	acct, err := a.store.GetCalendarAccount(a.ctx, req.ID)
	if err != nil {
		return CalendarAccountDTO{}, err
	}
	if strings.TrimSpace(req.URL) == "" {
		return CalendarAccountDTO{}, errCalendarNeedsURL
	}
	acct.Name = strings.TrimSpace(req.Name)
	acct.URL = strings.TrimSpace(req.URL)
	acct.Username = strings.TrimSpace(req.Username)
	acct.AddressBookID = req.AddressBookID
	if err := a.store.UpdateCalendarAccount(a.ctx, *acct); err != nil {
		return CalendarAccountDTO{}, err
	}
	if req.Password != "" {
		if err := credentials.StoreCalendarPassword(acct.ID, req.Password); err != nil {
			return CalendarAccountDTO{}, err
		}
	}
	a.syncCalendarsInBackground()
	return a.calendarAccountDTO(acct.ID)
}

// RemoveCalendarAccount forgets a login, its calendars and their events here.
// Nothing is deleted on the server.
func (a *App) RemoveCalendarAccount(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteCalendarAccount(a.ctx, id); err != nil {
		return err
	}
	if err := credentials.DeleteCalendarPassword(id); err != nil {
		a.log.Warn("delete calendar password", "account", id, "err", err)
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{})
	return nil
}

// ListCalendars returns every calendar that holds events, local ones first.
// Task lists are listed by ListTaskLists.
func (a *App) ListCalendars() ([]CalendarDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	cals, err := a.store.ListCalendars(a.ctx)
	if err != nil {
		return nil, err
	}
	names, err := a.calendarAccountNames()
	if err != nil {
		return nil, err
	}
	def := a.defaultCalendarID(cals)
	out := make([]CalendarDTO, 0, len(cals))
	for _, c := range cals {
		if c.HoldsEvents() {
			out = append(out, toCalendarDTO(c, names[c.AccountID], c.ID == def))
		}
	}
	return out, nil
}

// calendarAccountNames maps calendar account ids to the name the ui shows.
func (a *App) calendarAccountNames() (map[int64]string, error) {
	accounts, err := a.store.ListCalendarAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(accounts))
	for _, acct := range accounts {
		names[acct.ID] = acct.Name
		if acct.Name == "" {
			names[acct.ID] = acct.Username
		}
	}
	return names, nil
}

// SetCalendarColor sets a calendar's color here. It is not written to the
// server, and a later sync does not overwrite it.
func (a *App) SetCalendarColor(id int64, color string) error {
	if err := a.ready(); err != nil {
		return err
	}
	cal, err := a.store.GetCalendar(a.ctx, id)
	if err != nil {
		return err
	}
	cal.Color = caldav.NormalizeColor(color)
	cal.ColorSet = cal.Color != ""
	if err := a.store.UpdateCalendar(a.ctx, *cal); err != nil {
		return err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{id}})
	return nil
}

// SetCalendarVisible shows or hides a calendar's events. A hidden calendar
// still syncs, and raises no reminders.
func (a *App) SetCalendarVisible(id int64, visible bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	cal, err := a.store.GetCalendar(a.ctx, id)
	if err != nil {
		return err
	}
	cal.Visible = visible
	if err := a.store.UpdateCalendar(a.ctx, *cal); err != nil {
		return err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{id}})
	return nil
}

// CreateLocalCalendar adds a calendar that lives only in this database.
func (a *App) CreateLocalCalendar(name, color string) (CalendarDTO, error) {
	if err := a.ready(); err != nil {
		return CalendarDTO{}, err
	}
	cal := storage.Calendar{Name: strings.TrimSpace(name), Color: caldav.NormalizeColor(color), Visible: true}
	cal.ColorSet = cal.Color != ""
	if _, err := a.store.CreateCalendar(a.ctx, &cal); err != nil {
		return CalendarDTO{}, err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{cal.ID}})
	return toCalendarDTO(cal, "", false), nil
}

// RenameCalendar renames a local calendar. A synced calendar takes its name
// from the server.
func (a *App) RenameCalendar(id int64, name string) error {
	if err := a.ready(); err != nil {
		return err
	}
	cal, err := a.store.GetCalendar(a.ctx, id)
	if err != nil {
		return err
	}
	if !cal.Local() {
		return errCalendarNotLocal
	}
	cal.Name = strings.TrimSpace(name)
	if err := a.store.UpdateCalendar(a.ctx, *cal); err != nil {
		return err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{id}})
	return nil
}

// DeleteLocalCalendar removes a local calendar and its events. The last one
// stays, so there is always somewhere to put an event.
func (a *App) DeleteLocalCalendar(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	cal, err := a.store.GetCalendar(a.ctx, id)
	if err != nil {
		return err
	}
	if !cal.Local() {
		return errCalendarNotLocal
	}
	cals, err := a.store.ListCalendars(a.ctx)
	if err != nil {
		return err
	}
	locals := 0
	for _, c := range cals {
		if c.Local() && c.HoldsEvents() == cal.HoldsEvents() {
			locals++
		}
	}
	if locals <= 1 {
		return errCalendarLastLocal
	}
	if err := a.store.DeleteCalendar(a.ctx, id); err != nil {
		return err
	}
	a.emit(EventCalendarChanged, CalendarChangedEvent{CalendarIDs: []int64{id}})
	return nil
}

// GetCalendarSettings returns the calendar preferences.
func (a *App) GetCalendarSettings() (CalendarSettingsDTO, error) {
	if err := a.ready(); err != nil {
		return CalendarSettingsDTO{}, err
	}
	return CalendarSettingsDTO{
		SyncIntervalMinutes:    a.intSetting(settingCalendarSync, 15),
		RemindersEnabled:       a.boolSetting(settingCalendarReminders, true),
		DefaultReminderMinutes: a.intSetting(settingCalendarDefault, 15),
		DefaultCalendarID:      int64(a.intSetting(settingCalendarDefaultID, 0)),
		DefaultView:            oneOf(a.stringSetting(settingCalendarView, "week"), calendarViews, "week"),
		WeekStart:              a.intSetting(settingCalendarWeekStart, -1),
		WorkDayStart:           a.intSetting(settingCalendarWorkStart, defaultWorkStart),
		WorkDayEnd:             a.intSetting(settingCalendarWorkEnd, defaultWorkEnd),
		TimeZoneDisplay:        oneOf(a.stringSetting(settingCalendarZoneShow, "off"), calendarZoneDisplays, "off"),
		SecondaryTimeZone:      a.stringSetting(settingCalendarZoneAlt, ""),
	}, nil
}

// SetCalendarSettings saves the calendar preferences. An unknown view or zone
// display, a week start outside -1..6, working hours that do not fit in a day
// or end before they start, and a secondary zone Go cannot load are refused.
func (a *App) SetCalendarSettings(s CalendarSettingsDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := validCalendarView(s); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingCalendarView, s.DefaultView); err != nil {
		return err
	}
	if err := a.store.SetInt(a.ctx, settingCalendarWeekStart, s.WeekStart); err != nil {
		return err
	}
	if err := a.store.SetInt(a.ctx, settingCalendarWorkStart, s.WorkDayStart); err != nil {
		return err
	}
	if err := a.store.SetInt(a.ctx, settingCalendarWorkEnd, s.WorkDayEnd); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingCalendarZoneShow, s.TimeZoneDisplay); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingCalendarZoneAlt, s.SecondaryTimeZone); err != nil {
		return err
	}
	if err := a.store.SetInt(a.ctx, settingCalendarSync, max(s.SyncIntervalMinutes, 0)); err != nil {
		return err
	}
	if err := a.store.SetBool(a.ctx, settingCalendarReminders, s.RemindersEnabled); err != nil {
		return err
	}
	if err := a.store.SetInt(a.ctx, settingCalendarDefault, max(s.DefaultReminderMinutes, -1)); err != nil {
		return err
	}
	return a.store.SetInt(a.ctx, settingCalendarDefaultID, int(s.DefaultCalendarID))
}

// resolveCalendarRequest fills a request's url, username and password from the
// address book it names, where the request left them empty.
func (a *App) resolveCalendarRequest(req CalendarAccountRequest) (CalendarAccountRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	req.Username = strings.TrimSpace(req.Username)
	if req.AddressBookID != 0 {
		book, err := a.store.GetAddressBook(a.ctx, req.AddressBookID)
		if err != nil {
			return req, err
		}
		if req.URL == "" {
			req.URL = book.URL
		}
		if req.Username == "" {
			req.Username = book.Username
		}
		if req.Password == "" {
			req.Password = a.addressBookPassword(book.ID)
		}
		if req.AccountID == 0 {
			req.AccountID = book.AccountID
		}
	}
	if req.AccountID != 0 && req.Username == "" {
		account, err := a.store.GetAccount(a.ctx, req.AccountID)
		if err != nil {
			return req, err
		}
		req.Username = loginName(*account)
	}
	return req, nil
}

func (a *App) addressBookPassword(bookID int64) string {
	password, err := credentials.LoadAddressBookPassword(bookID)
	if err != nil {
		a.log.Warn("read address book password", "book", bookID, "err", err)
	}
	return password
}

// calendarPassword is an account's own password, or that of the address book
// it shares a login with.
func (a *App) calendarPassword(acct storage.CalendarAccount) (string, error) {
	password, err := credentials.LoadCalendarPassword(acct.ID)
	if err != nil || password != "" || acct.AddressBookID == 0 {
		return password, err
	}
	return credentials.LoadAddressBookPassword(acct.AddressBookID)
}

// calendarClient opens a connection for an account, through the same proxy
// route its contacts take.
func (a *App) calendarClient(acct storage.CalendarAccount) (*caldav.Client, error) {
	password, err := a.calendarPassword(acct)
	if err != nil {
		return nil, err
	}
	httpClient, err := a.contactsHTTPClient(acct.AccountID)
	if err != nil {
		return nil, err
	}
	return caldav.Connect(caldav.Config{URL: acct.URL, Username: acct.Username, Password: password, HTTP: httpClient})
}

func (a *App) calendarAccountDTO(id int64) (CalendarAccountDTO, error) {
	accounts, err := a.ListCalendarAccounts()
	if err != nil {
		return CalendarAccountDTO{}, err
	}
	for _, acct := range accounts {
		if acct.ID == id {
			return acct, nil
		}
	}
	return CalendarAccountDTO{}, fmt.Errorf("pelton: calendar account %d disappeared", id)
}

// defaultCalendarID is where a new event goes: the chosen default while it is
// still writable, else the first writable visible calendar.
func (a *App) defaultCalendarID(cals []storage.Calendar) int64 {
	chosen := int64(a.intSetting(settingCalendarDefaultID, 0))
	var first int64
	for _, c := range cals {
		if c.ReadOnly || !c.HoldsEvents() {
			continue
		}
		if c.ID == chosen {
			return c.ID
		}
		if first == 0 && c.Visible {
			first = c.ID
		}
	}
	return first
}

func toCalendarDTO(c storage.Calendar, accountName string, isDefault bool) CalendarDTO {
	return CalendarDTO{
		ID:          c.ID,
		AccountID:   c.AccountID,
		AccountName: accountName,
		Name:        c.Name,
		Description: c.Description,
		Color:       c.Color,
		Local:       c.Local(),
		ReadOnly:    c.ReadOnly,
		Visible:     c.Visible,
		LastSync:    c.LastSyncAt,
		LastError:   c.LastError,
		Default:     isDefault,
	}
}

// oneOf returns value when it is in allowed, else def.
func oneOf(value string, allowed []string, def string) string {
	if slices.Contains(allowed, value) {
		return value
	}
	return def
}

// validCalendarView checks the view settings of a CalendarSettingsDTO.
func validCalendarView(s CalendarSettingsDTO) error {
	switch {
	case !slices.Contains(calendarViews, s.DefaultView),
		!slices.Contains(calendarZoneDisplays, s.TimeZoneDisplay),
		s.WeekStart < -1 || s.WeekStart > 6,
		s.WorkDayStart < 0 || s.WorkDayEnd > minutesPerDay || s.WorkDayEnd <= s.WorkDayStart:
		return errCalendarSettings
	}
	if s.SecondaryTimeZone != "" {
		if _, err := time.LoadLocation(s.SecondaryTimeZone); err != nil {
			return fmt.Errorf("%w: %v", errCalendarSettings, err)
		}
	}
	return nil
}

// calendarIndex maps calendar ids to their rows.
func (a *App) calendarIndex(ctx context.Context) (map[int64]storage.Calendar, error) {
	cals, err := a.store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]storage.Calendar, len(cals))
	for _, c := range cals {
		out[c.ID] = c
	}
	return out, nil
}
