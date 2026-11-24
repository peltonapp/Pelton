import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'calendar', id: 'calendar-view', labelKey: 'calendarSettings.defaultView', keywords: 'calendar view month week day agenda year open start' },
  { page: 'calendar', id: 'calendar-week-start', labelKey: 'calendarSettings.weekStart', keywords: 'calendar week start first day monday sunday saturday' },
  { page: 'calendar', id: 'calendar-working-hours', labelKey: 'calendarSettings.workingHours', keywords: 'calendar working hours work day office' },
  { page: 'calendar', id: 'calendar-time-zone', labelKey: 'calendarSettings.zone', keywords: 'calendar time zone timezone second utc travel' },
  { page: 'calendar', id: 'calendar-default-calendar', labelKey: 'calendarSettings.defaultCalendar', keywords: 'calendar default new event' },
  { page: 'calendar', id: 'calendar-default-reminder', labelKey: 'calendarSettings.defaultReminder', keywords: 'calendar reminder alarm alert notification default' },
  { page: 'calendar', id: 'calendar-reminders', labelKey: 'calendarSettings.reminders', keywords: 'calendar reminders notifications alarms' },
  { page: 'calendar', id: 'calendar-show-tasks', labelKey: 'taskSettings.inCalendar', keywords: 'calendar tasks todo show due checkbox' },
  { page: 'calendar', id: 'calendar-ics-handler', labelKey: 'calendarSettings.icsHandler', keywords: 'ics ical file open default app handler import' },
  { page: 'calendar', id: 'calendar-block-length', labelKey: 'calendarSettings.timeblock.length', keywords: 'time block blocking length default duration task' },
  { page: 'calendar', id: 'calendar-block-work-days', labelKey: 'calendarSettings.timeblock.workDays', keywords: 'time block working days week weekend' },
  { page: 'calendar', id: 'calendar-block-min-slot', labelKey: 'calendarSettings.timeblock.minSlot', keywords: 'time block shortest slot free time minimum' },
  { page: 'calendar', id: 'calendar-block-buffer', labelKey: 'calendarSettings.timeblock.buffer', keywords: 'time block buffer gap break between' },
  { page: 'calendar', id: 'calendar-block-search', labelKey: 'calendarSettings.timeblock.searchDays', keywords: 'time block find time look ahead days' },
  { page: 'calendar', id: 'calendar-block-write', labelKey: 'calendarSettings.timeblock.write', keywords: 'time block busy calendar write focus time others see' },
  { page: 'calendar', id: 'calendar-block-title', labelKey: 'calendarSettings.timeblock.eventTitle', keywords: 'time block busy event title focus time privacy' },
  { page: 'calendar', id: 'calendar-block-include-title', labelKey: 'calendarSettings.timeblock.includeTitle', keywords: 'time block task title event privacy' },
  { page: 'calendar', id: 'calendar-sync', labelKey: 'calendarSettings.sync', keywords: 'calendar sync interval background caldav' },
  { page: 'calendar', id: 'calendar-accounts', labelKey: 'calendarSettings.accounts', keywords: 'caldav account server nextcloud fastmail icloud calendar add' },
]

export default entries
