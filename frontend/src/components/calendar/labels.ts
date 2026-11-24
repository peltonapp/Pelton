// labels.ts names the calendar's reminder offsets and attendee answers for
// display, and lists the reminder choices the editor and settings offer.

/** The reminder offsets offered, in minutes before the start. */
export const reminderChoices = [0, 5, 10, 15, 30, 60, 120, 1440, 2880, 10080]

/** Says a reminder offset in words: "15 minutes before", "1 day before", "At the start". */
export function reminderLabel(minutes: number, t: (key: string) => string): string {
  if (minutes === 0) {
    return t('calendar.reminder.atStart')
  }
  if (minutes < 0) {
    return t('calendar.reminder.after').replace('{n}', String(-minutes))
  }
  if (minutes % 10080 === 0) {
    return t(minutes === 10080 ? 'calendar.reminder.week' : 'calendar.reminder.weeks').replace('{n}', String(minutes / 10080))
  }
  if (minutes % 1440 === 0) {
    return t(minutes === 1440 ? 'calendar.reminder.day' : 'calendar.reminder.days').replace('{n}', String(minutes / 1440))
  }
  if (minutes % 60 === 0) {
    return t(minutes === 60 ? 'calendar.reminder.hour' : 'calendar.reminder.hours').replace('{n}', String(minutes / 60))
  }
  return t('calendar.reminder.minutes').replace('{n}', String(minutes))
}

/** What an event block shows by color, icon or style, as words for its accessible name. */
export interface EventLabelParts {
  title: string
  when: string
  calendar: string
  status?: string
  free?: boolean
  conflict?: boolean
  recurring?: boolean
}

/** The accessible name of an event block: title, time, calendar and the states drawn on it. */
export function eventLabel(p: EventLabelParts, t: (key: string) => string): string {
  return [
    p.title,
    p.when,
    p.calendar,
    p.status === 'TENTATIVE' ? t('a11y.event.tentative') : '',
    p.status === 'CANCELLED' ? t('a11y.event.cancelled') : '',
    p.free ? t('a11y.event.free') : '',
    p.conflict ? t('a11y.event.conflict') : '',
    p.recurring ? t('a11y.event.recurring') : '',
  ]
    .filter(Boolean)
    .join(', ')
}

/** The localization key of an attendee's answer. */
export function partStatKey(partStat: string): string {
  switch (partStat) {
    case 'ACCEPTED':
      return 'calendar.partstat.accepted'
    case 'TENTATIVE':
      return 'calendar.partstat.tentative'
    case 'DECLINED':
      return 'calendar.partstat.declined'
    case 'DELEGATED':
      return 'calendar.partstat.delegated'
    default:
      return 'calendar.partstat.needsAction'
  }
}
