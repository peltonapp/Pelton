package desktop

import (
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Reminders: a loop that wakes every half minute, finds the VALARMs whose
// trigger fell in the time since it last looked, and raises a desktop
// notification for each through the same path new mail takes. Only visible
// calendars remind, and only display and audio alarms: an EMAIL alarm is the
// server's job.

// reminderTick is how often the reminder loop looks.
const reminderTick = 30 * time.Second

// reminderReach is how far an alarm can sit from its occurrence and still be
// found: a reminder a week ahead is common, further is not.
const reminderReach = 8 * 24 * time.Hour

// calendarText holds the localizable text the Go side writes for the
// calendar: reminder notifications and the subject of an invitation reply.
// Like notifyStrings it is keyed by the language setting.
type calendarText struct {
	untitled  string
	allDay    string
	accepted  string
	tentative string
	declined  string
}

var calendarLocales = map[string]calendarText{
	"en":    {untitled: "(untitled event)", allDay: "All day", accepted: "Accepted: %s", tentative: "Tentative: %s", declined: "Declined: %s"},
	"de":    {untitled: "(Termin ohne Titel)", allDay: "Ganztägig", accepted: "Angenommen: %s", tentative: "Vorläufig: %s", declined: "Abgelehnt: %s"},
	"fr":    {untitled: "(événement sans titre)", allDay: "Toute la journée", accepted: "Accepté : %s", tentative: "Provisoire : %s", declined: "Refusé : %s"},
	"nl":    {untitled: "(afspraak zonder titel)", allDay: "Hele dag", accepted: "Geaccepteerd: %s", tentative: "Voorlopig: %s", declined: "Afgewezen: %s"},
	"es":    {untitled: "(evento sin título)", allDay: "Todo el día", accepted: "Aceptado: %s", tentative: "Provisional: %s", declined: "Rechazado: %s"},
	"it":    {untitled: "(evento senza titolo)", allDay: "Tutto il giorno", accepted: "Accettato: %s", tentative: "Provvisorio: %s", declined: "Rifiutato: %s"},
	"pl":    {untitled: "(wydarzenie bez tytułu)", allDay: "Cały dzień", accepted: "Zaakceptowano: %s", tentative: "Wstępnie: %s", declined: "Odrzucono: %s"},
	"ru":    {untitled: "(событие без названия)", allDay: "Весь день", accepted: "Принято: %s", tentative: "Под вопросом: %s", declined: "Отклонено: %s"},
	"tr":    {untitled: "(başlıksız etkinlik)", allDay: "Tüm gün", accepted: "Kabul edildi: %s", tentative: "Belirsiz: %s", declined: "Reddedildi: %s"},
	"pt":    {untitled: "(evento sem título)", allDay: "Dia inteiro", accepted: "Aceite: %s", tentative: "Provisório: %s", declined: "Recusado: %s"},
	"ar":    {untitled: "(حدث بلا عنوان)", allDay: "طوال اليوم", accepted: "مقبول: %s", tentative: "مبدئي: %s", declined: "مرفوض: %s"},
	"zh-CN": {untitled: "（无标题日程）", allDay: "全天", accepted: "已接受：%s", tentative: "暂定：%s", declined: "已拒绝：%s"},
}

// calendarTextFor returns the calendar text for lang, English for an unknown
// code.
func calendarTextFor(lang string) calendarText {
	if s, ok := calendarLocales[lang]; ok {
		return s
	}
	return calendarLocales["en"]
}

// dueReminder is one alarm whose trigger has come.
type dueReminder struct {
	row storage.CalendarEvent
	occ ical.Occurrence
	at  time.Time
}

// key identifies the reminder across ticks, so one is never raised twice.
func (r dueReminder) key() string {
	return fmt.Sprintf("%d|%d|%d", r.row.ID, r.occ.RecurrenceID.Unix(), r.at.Unix())
}

// runReminderLoop raises reminders until the app quits.
func (a *App) runReminderLoop() {
	ticker := time.NewTicker(reminderTick)
	defer ticker.Stop()
	last := time.Now().Add(-reminderTick)
	fired := map[string]time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		if a.store != nil && a.boolSetting(settingCalendarReminders, true) {
			for _, r := range a.dueReminders(last, now) {
				if _, done := fired[r.key()]; done {
					continue
				}
				fired[r.key()] = now
				a.raiseReminder(r)
			}
		}
		last = now
		for k, at := range fired {
			if now.Sub(at) > 24*time.Hour {
				delete(fired, k)
			}
		}
	}
}

// dueReminders returns the alarms that trigger in (from, to].
func (a *App) dueReminders(from, to time.Time) []dueReminder {
	rows, err := a.store.QueryCalendarEvents(a.ctx, storage.EventQuery{
		From:        from.Add(-reminderReach),
		To:          to.Add(reminderReach),
		VisibleOnly: true,
		AlarmsOnly:  true,
	})
	if err != nil {
		a.log.Warn("look for reminders", "err", err)
		return nil
	}
	var out []dueReminder
	for _, row := range rows {
		obj, err := parseStoredEvent(row)
		if err != nil {
			continue
		}
		for _, occ := range obj.Expand(from.Add(-reminderReach), to.Add(reminderReach)) {
			if occ.Event.Status == ical.StatusCancelled {
				continue
			}
			for _, alarm := range occ.Event.Alarms {
				if alarm.Action == "EMAIL" || alarm.Action == "PROCEDURE" {
					continue
				}
				at := triggerTime(obj, occ, alarm)
				if at.After(from) && !at.After(to) {
					out = append(out, dueReminder{row: row, occ: occ, at: at})
				}
			}
		}
	}
	return out
}

// triggerTime is when an alarm fires for an occurrence.
func triggerTime(obj *ical.Object, occ ical.Occurrence, alarm ical.Alarm) time.Time {
	if !alarm.Absolute.IsZero() {
		return obj.Zones.Instant(alarm.Absolute)
	}
	if alarm.RelatedEnd {
		return occ.End.Add(alarm.Trigger)
	}
	return occ.Start.Add(alarm.Trigger)
}

// raiseReminder posts the notification and tells the ui.
func (a *App) raiseReminder(r dueReminder) {
	text := calendarTextFor(a.stringSetting(settingLanguage, "en"))
	title := strings.TrimSpace(r.occ.Event.Summary)
	if title == "" {
		title = text.untitled
	}
	a.sendNotification(notification{title: title, body: reminderBody(r.occ, text, a.stringSetting(settingTimeFormat, "auto"))})
	a.emit(EventCalendarReminder, CalendarReminderEvent{
		EventID:      r.row.ID,
		CalendarID:   r.row.CalendarID,
		RecurrenceID: formatRecurrenceID(r.occ.RecurrenceID),
		Summary:      r.occ.Event.Summary,
		Location:     r.occ.Event.Location,
		Start:        formatEventTime(r.occ.Start, r.occ.AllDay),
		End:          formatEventTime(r.occ.End, r.occ.AllDay),
		AllDay:       r.occ.AllDay,
	})
}

// reminderBody is the time span and the place, in the user's zone and clock.
func reminderBody(occ ical.Occurrence, text calendarText, timeFormat string) string {
	when := text.allDay
	if !occ.AllDay {
		layout := "15:04"
		if timeFormat == "12" {
			layout = "3:04 PM"
		}
		when = occ.Start.In(time.Local).Format(layout)
		if occ.End.After(occ.Start) {
			when += " - " + occ.End.In(time.Local).Format(layout)
		}
	}
	if loc := strings.TrimSpace(occ.Event.Location); loc != "" {
		return when + "\n" + loc
	}
	return when
}
