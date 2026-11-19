// calformat.ts writes the calendar's dates and times for display, in the
// system locale and the user's clock preference, and picks a calendar's color.
// ranges are joined with a plain hyphen: Intl's own range format uses an en
// dash, which Pelton's text never does.

import { addDays, sameDay } from './caldate'
import type { CalendarView } from './types'
import type { TimeFormat } from './format'

function clock(timeFormat: TimeFormat): Intl.DateTimeFormatOptions {
  if (timeFormat === '12') {
    return { hour12: true }
  }
  if (timeFormat === '24') {
    return { hourCycle: 'h23' }
  }
  return {}
}

/** A time of day, such as 09:30 or 9:30 AM. */
export function formatTime(d: Date, timeFormat: TimeFormat, locale?: string): string {
  return d.toLocaleTimeString(locale, { hour: 'numeric', minute: '2-digit', ...clock(timeFormat) })
}

/** An hour label for the time grid's gutter, such as 09:00 or 9 AM. */
export function formatHour(d: Date, timeFormat: TimeFormat, locale?: string, timeZone?: string): string {
  const opts: Intl.DateTimeFormatOptions = { hour: 'numeric', ...clock(timeFormat), timeZone }
  const twelve = new Intl.DateTimeFormat(locale, opts).resolvedOptions().hour12
  if (!twelve) {
    opts.minute = '2-digit'
  }
  return d.toLocaleTimeString(locale, opts)
}

/** A start and end time joined by a hyphen. */
export function formatTimeRange(start: Date, end: Date, timeFormat: TimeFormat, locale?: string): string {
  return `${formatTime(start, timeFormat, locale)} - ${formatTime(end, timeFormat, locale)}`
}

/** A day written out, such as Saturday, 14 November. The year shows when it is not this one. */
export function formatLongDay(d: Date, locale?: string, now = new Date()): string {
  return d.toLocaleDateString(locale, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric',
  })
}

/** A short day, such as 14 Nov. */
export function formatShortDay(d: Date, locale?: string, withYear = false): string {
  return d.toLocaleDateString(locale, { day: 'numeric', month: 'short', year: withYear ? 'numeric' : undefined })
}

/**
 * When an event happens, in one line: the day and the times, or the days of an
 * all-day or multi-day event. end is exclusive for an all-day event.
 */
export function formatWhen(start: Date, end: Date, allDay: boolean, timeFormat: TimeFormat, locale?: string, now = new Date()): string {
  if (allDay) {
    const last = addDays(end, -1)
    if (last <= start) {
      return formatLongDay(start, locale, now)
    }
    return `${formatLongDay(start, locale, now)} - ${formatLongDay(last, locale, now)}`
  }
  if (sameDay(start, end) || end.getTime() === addDays(start, 1).setHours(0, 0, 0, 0)) {
    return `${formatLongDay(start, locale, now)}, ${formatTimeRange(start, end, timeFormat, locale)}`
  }
  return `${formatLongDay(start, locale, now)} ${formatTime(start, timeFormat, locale)} - ${formatLongDay(end, locale, now)} ${formatTime(end, timeFormat, locale)}`
}

/** The heading of a view's page: the month, or the span of days it shows. */
export function formatViewTitle(view: CalendarView, from: Date, to: Date, focus: Date, locale?: string): string {
  if (view === 'month') {
    return focus.toLocaleDateString(locale, { month: 'long', year: 'numeric' })
  }
  if (view === 'year') {
    return String(focus.getFullYear())
  }
  if (view === 'day') {
    return from.toLocaleDateString(locale, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
  }
  const last = addDays(to, -1)
  if (from.getMonth() === last.getMonth() && from.getFullYear() === last.getFullYear()) {
    return `${from.toLocaleDateString(locale, { day: 'numeric' })} - ${last.toLocaleDateString(locale, { day: 'numeric', month: 'long', year: 'numeric' })}`
  }
  return `${formatShortDay(from, locale, from.getFullYear() !== last.getFullYear())} - ${formatShortDay(last, locale, true)}`
}

/** How many colors the fallback palette has, --cal-1 to --cal-8. */
export const paletteSize = 8

/** The palette as css values, in picker order. */
export const palette: string[] = Array.from({ length: paletteSize }, (_, i) => `var(--cal-${i + 1})`)

/**
 * The color a calendar shows in: its own #rrggbb, or a palette color picked by
 * its id so it stays the same from one launch to the next.
 */
export function calendarColor(cal: { id: number; color: string } | undefined): string {
  if (!cal) {
    return palette[0]
  }
  return cal.color || palette[Math.abs(cal.id) % paletteSize]
}

/** Reads a palette color's #rrggbb from the theme, to store as a calendar's color. */
export function paletteHex(index: number): string {
  return getComputedStyle(document.documentElement).getPropertyValue(`--cal-${index + 1}`).trim()
}
