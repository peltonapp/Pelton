import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'calendar', id: 'calendar-view', labelKey: 'calendarSettings.defaultView', keywords: 'calendar view month week day agenda open start' },
  { page: 'calendar', id: 'calendar-week-start', labelKey: 'calendarSettings.weekStart', keywords: 'calendar week start first day monday sunday saturday' },
  { page: 'calendar', id: 'calendar-working-hours', labelKey: 'calendarSettings.workingHours', keywords: 'calendar working hours work day office' },
  { page: 'calendar', id: 'calendar-time-zone', labelKey: 'calendarSettings.zone', keywords: 'calendar time zone timezone second utc travel' },
  { page: 'calendar', id: 'calendar-default-calendar', labelKey: 'calendarSettings.defaultCalendar', keywords: 'calendar default new event' },
  { page: 'calendar', id: 'calendar-default-reminder', labelKey: 'calendarSettings.defaultReminder', keywords: 'calendar reminder alarm alert notification default' },
  { page: 'calendar', id: 'calendar-reminders', labelKey: 'calendarSettings.reminders', keywords: 'calendar reminders notifications alarms' },
  { page: 'calendar', id: 'calendar-ics-handler', labelKey: 'calendarSettings.icsHandler', keywords: 'ics ical file open default app handler import' },
  { page: 'calendar', id: 'calendar-sync', labelKey: 'calendarSettings.sync', keywords: 'calendar sync interval background caldav' },
  { page: 'calendar', id: 'calendar-accounts', labelKey: 'calendarSettings.accounts', keywords: 'caldav account server nextcloud fastmail icloud calendar add' },
]

export default entries
