// location-calendar.ts lets the calendar take part in back and forward: the
// date in focus and the view it is shown in.

import { get } from 'svelte/store'
import { registerLocationProvider, parseYmd, ymd, type CalendarLocation } from './locations'
import { calendarFocus, calendarView } from '../stores/calendar'
import { openSpace } from '../stores/space'
import { t } from './i18n'

/** A short name for a date as a calendar view shows it. */
export function calendarLabel(loc: CalendarLocation, translate: (key: string) => string): string {
  const date = loc.date ? parseYmd(loc.date) : null
  if (!date) {
    return ''
  }
  if (loc.view === 'month') {
    return date.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
  }
  if (loc.view === 'year') {
    return String(date.getFullYear())
  }
  const day = date.toLocaleDateString(undefined, { dateStyle: 'medium' })
  return loc.view === 'week' ? translate('navHistory.calendar.week').replace('{date}', day) : day
}

registerLocationProvider('calendar', {
  watch: [calendarFocus, calendarView],
  describe(): CalendarLocation {
    return { space: 'calendar', view: get(calendarView), date: ymd(get(calendarFocus)) }
  },
  label(loc) {
    return calendarLabel(loc, get(t))
  },
  async restore(loc) {
    const date = loc.date ? parseYmd(loc.date) : null
    if (loc.view) {
      calendarView.set(loc.view)
    }
    if (date) {
      calendarFocus.set(date)
    }
    openSpace('calendar')
    return true
  },
  refines(from, to) {
    // picking another day of the month on show stays on the same page.
    return from.view === 'month' && to.view === 'month' && (from.date ?? '').slice(0, 7) === (to.date ?? '').slice(0, 7)
  },
})
