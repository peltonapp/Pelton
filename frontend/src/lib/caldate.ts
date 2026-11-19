// caldate.ts is the date math of the calendar screen: days and weeks in local
// wall-clock time, the ranges each view loads, the month grid, and where
// events sit in the time grid and in the rows of a month. everything here is
// pure so it can be tested without a component.
//
// days are stepped with setDate rather than by adding 24 hours, so a week that
// crosses a daylight saving change still lands on midnights.

import type { CalendarView, Occurrence } from './types'

/** Minutes in a day. */
export const dayMinutes = 24 * 60

/** How many days the agenda lists from its first day. */
export const agendaDays = 30

/** Returns local midnight of the day d falls on. */
export function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

/** Returns d moved by n calendar days, keeping its time of day. */
export function addDays(d: Date, n: number): Date {
  const out = new Date(d)
  out.setDate(out.getDate() + n)
  return out
}

/**
 * Returns d moved by n months. A day past the end of the target month is
 * clamped to its last day, so Jan 31 plus one month is the end of February.
 */
export function addMonths(d: Date, n: number): Date {
  const first = new Date(d.getFullYear(), d.getMonth() + n, 1, d.getHours(), d.getMinutes())
  const last = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate()
  first.setDate(Math.min(d.getDate(), last))
  return first
}

/** Says whether a and b fall on the same local day. */
export function sameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** Returns the local day as yyyy-mm-dd. */
export function dayKey(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** Reads a yyyy-mm-dd day as local midnight. */
export function parseDayKey(key: string): Date {
  const [y, m, d] = key.slice(0, 10).split('-').map(Number)
  return new Date(y, m - 1, d)
}

/** Whole days from a to b, both taken at their local midnight. */
export function daysBetween(a: Date, b: Date): number {
  const ms = Date.UTC(b.getFullYear(), b.getMonth(), b.getDate()) - Date.UTC(a.getFullYear(), a.getMonth(), a.getDate())
  return Math.round(ms / 86400000)
}

/** Minutes since local midnight. */
export function minutesOfDay(d: Date): number {
  return d.getHours() * 60 + d.getMinutes()
}

/** Returns local midnight of day plus the given minutes. */
export function atMinutes(day: Date, minutes: number): Date {
  const out = startOfDay(day)
  out.setMinutes(minutes)
  return out
}

/** The next full or half hour after now, which can be tomorrow's midnight. */
export function nextHalfHour(now: Date): Date {
  const out = new Date(now.getFullYear(), now.getMonth(), now.getDate(), now.getHours(), now.getMinutes() < 30 ? 30 : 60)
  return out
}

/**
 * Where a new event starts on a day when no time was picked: the next half
 * hour when the day is today, and the same clock time on any other day.
 * Late at night that clock time is the next slot only on the next day, so on
 * another day it is pulled back to keep the hour inside it.
 */
export function defaultSlot(day: Date, now: Date): Date {
  const next = nextHalfHour(now)
  if (sameDay(day, now)) {
    return next
  }
  return atMinutes(day, Math.min(minutesOfDay(next) || dayMinutes, dayMinutes - 60))
}

/** Rounds minutes to the nearest step. */
export function snapMinutes(minutes: number, step = 15): number {
  return Math.round(minutes / step) * step
}

/**
 * Formats a local time as rfc3339 with its offset, the way the backend reads
 * an event time: 2026-11-14T09:30:00+01:00.
 */
export function toRFC3339(d: Date): string {
  const offset = -d.getTimezoneOffset()
  const sign = offset >= 0 ? '+' : '-'
  const abs = Math.abs(offset)
  return `${dayKey(d)}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
}

/** Reads an event time: a yyyy-mm-dd day for an all-day event, else rfc3339. */
export function parseEventTime(value: string, allDay: boolean): Date {
  if (allDay || value.length === 10) {
    return parseDayKey(value)
  }
  return new Date(value)
}

/**
 * The first day of the week a locale uses, 0 for Sunday. Read from Intl where
 * the engine knows it; otherwise Sunday for the regions that start there,
 * Saturday for much of the Arabic speaking world, and Monday elsewhere.
 */
export function localeWeekStart(locale: string): number {
  try {
    const info = new Intl.Locale(locale) as Intl.Locale & {
      getWeekInfo?: () => { firstDay: number }
      weekInfo?: { firstDay: number }
    }
    const first = info.getWeekInfo?.().firstDay ?? info.weekInfo?.firstDay
    if (typeof first === 'number') {
      return first % 7
    }
  } catch {
    // an unknown tag falls through to the table below.
  }
  const region = locale.split('-')[1]?.toUpperCase() ?? ''
  if (['US', 'CA', 'JP', 'BR', 'MX', 'IL', 'KR', 'PH', 'TW', 'HK', 'IN', 'ZA', 'AU'].includes(region) || locale === 'en' || locale === 'pt') {
    return 0
  }
  if (locale.startsWith('ar') || ['AE', 'EG', 'SA', 'IQ', 'JO', 'KW', 'QA', 'SY'].includes(region)) {
    return 6
  }
  return 1
}

/** The week start a setting means: the setting itself, or the locale's for -1. */
export function resolveWeekStart(setting: number, locale: string): number {
  return setting >= 0 && setting <= 6 ? setting : localeWeekStart(locale)
}

/** Returns the local midnight that starts the week holding d. */
export function startOfWeek(d: Date, weekStart: number): Date {
  const day = startOfDay(d)
  const back = (day.getDay() - weekStart + 7) % 7
  return addDays(day, -back)
}

/** The seven days of the week holding d. */
export function weekDays(d: Date, weekStart: number): Date[] {
  const first = startOfWeek(d, weekStart)
  return Array.from({ length: 7 }, (_, i) => addDays(first, i))
}

/**
 * The weeks a month view shows for the month holding d: whole weeks from the
 * one holding the first of the month to the one holding its last day, so four
 * to six rows.
 */
export function monthGrid(d: Date, weekStart: number): Date[][] {
  const first = new Date(d.getFullYear(), d.getMonth(), 1)
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0)
  const weeks: Date[][] = []
  let cursor = startOfWeek(first, weekStart)
  while (cursor <= last) {
    weeks.push(Array.from({ length: 7 }, (_, i) => addDays(cursor, i)))
    cursor = addDays(cursor, 7)
  }
  return weeks
}

/** A half-open span of local time. */
export interface DateRange {
  from: Date
  to: Date
}

/** The span a view loads for its focused day, to exclusive. */
export function viewRange(view: CalendarView, focus: Date, weekStart: number): DateRange {
  switch (view) {
    case 'month': {
      const weeks = monthGrid(focus, weekStart)
      return { from: weeks[0][0], to: addDays(weeks[weeks.length - 1][6], 1) }
    }
    case 'week': {
      const from = startOfWeek(focus, weekStart)
      return { from, to: addDays(from, 7) }
    }
    case 'day': {
      const from = startOfDay(focus)
      return { from, to: addDays(from, 1) }
    }
    case 'agenda': {
      const from = startOfDay(focus)
      return { from, to: addDays(from, agendaDays) }
    }
    case 'year':
      return { from: new Date(focus.getFullYear(), 0, 1), to: new Date(focus.getFullYear() + 1, 0, 1) }
  }
}

/** Moves the focused day one page of a view back (-1) or forward (1). */
export function stepFocus(view: CalendarView, focus: Date, dir: number): Date {
  switch (view) {
    case 'month':
      return addMonths(focus, dir)
    case 'week':
      return addDays(focus, 7 * dir)
    case 'day':
      return addDays(focus, dir)
    case 'agenda':
      return addDays(focus, agendaDays * dir)
    case 'year':
      return addMonths(focus, 12 * dir)
  }
}

/** An occurrence with its times read, as the views place it. */
export interface Placed<T = Occurrence> {
  occ: T
  start: Date
  end: Date
}

/** Reads an occurrence's start and end. An end at or before the start is taken as the start. */
export function placeOccurrence<T extends { start: string; end: string; allDay: boolean }>(occ: T): Placed<T> {
  const start = parseEventTime(occ.start, occ.allDay)
  let end = parseEventTime(occ.end, occ.allDay)
  if (end <= start) {
    end = occ.allDay ? addDays(start, 1) : start
  }
  return { occ, start, end }
}

/**
 * Says whether an occurrence belongs in the all-day lane of the time grid:
 * an all-day one, or a timed one that runs for a day or more.
 */
export function isLaneEvent(p: Placed<{ allDay: boolean }>): boolean {
  return p.occ.allDay || p.end.getTime() - p.start.getTime() >= dayMinutes * 60000
}

/** One timed event placed in a day column of the time grid. */
export interface TimedBox<T> {
  placed: Placed<T>
  /** Minutes after the day's midnight the box starts and ends at, clipped to the day. */
  top: number
  bottom: number
  /** The column it takes among the events it overlaps, and how many there are. */
  col: number
  cols: number
}

/**
 * Lays out the timed events of one day side by side where they overlap, the
 * way Outlook does: events that overlap one another, directly or through a
 * chain, share the day's width in columns, and each takes the first column
 * that is free at its start. An event shorter than minMinutes is laid out as
 * if it were that long, so a five minute call stays clickable and does not
 * hide under the next one.
 */
export function layoutDay<T>(events: Placed<T>[], day: Date, minMinutes = 20): TimedBox<T>[] {
  const from = startOfDay(day)
  const to = addDays(from, 1)
  const boxes: TimedBox<T>[] = events
    .filter((p) => p.start < to && (p.end > from || (p.end.getTime() === p.start.getTime() && p.start >= from)))
    .map((p) => {
      const top = Math.max(0, (p.start.getTime() - from.getTime()) / 60000)
      const bottom = Math.min(dayMinutes, (p.end.getTime() - from.getTime()) / 60000)
      return { placed: p, top, bottom: Math.max(bottom, Math.min(dayMinutes, top + minMinutes)), col: 0, cols: 1 }
    })
    .sort((a, b) => a.top - b.top || b.bottom - a.bottom)

  let cluster: TimedBox<T>[] = []
  let columns: number[] = []
  let clusterEnd = -1
  const close = (): void => {
    for (const box of cluster) {
      box.cols = columns.length
    }
    cluster = []
    columns = []
  }
  for (const box of boxes) {
    if (box.top >= clusterEnd) {
      close()
    }
    let col = columns.findIndex((end) => end <= box.top)
    if (col === -1) {
      col = columns.length
      columns.push(box.bottom)
    } else {
      columns[col] = box.bottom
    }
    box.col = col
    cluster.push(box)
    clusterEnd = Math.max(clusterEnd, box.bottom)
  }
  close()
  return boxes
}

/** One event as a bar across a row of days: a week of the month, or the all-day lane. */
export interface RowSpan<T> {
  placed: Placed<T>
  /** The first day of the row it covers, and how many days. */
  col: number
  span: number
  /** The line it sits on, 0 at the top. */
  lane: number
  /** It started before the row's first day, or goes on past its last. */
  before: boolean
  after: boolean
}

/**
 * Lays out events over a row of consecutive days as bars, each on the first
 * line free for all its days. Longer bars go first, so a week long trip sits
 * above the single days around it.
 */
export function layoutRow<T>(events: Placed<T>[], days: Date[]): RowSpan<T>[] {
  if (days.length === 0) {
    return []
  }
  const first = startOfDay(days[0])
  const end = addDays(startOfDay(days[days.length - 1]), 1)
  const spans: RowSpan<T>[] = []
  for (const p of events) {
    const lastMoment = p.end > p.start ? new Date(p.end.getTime() - 1) : p.start
    if (p.start >= end || lastMoment < first) {
      continue
    }
    const from = Math.max(0, daysBetween(first, p.start))
    const to = Math.min(days.length - 1, daysBetween(first, lastMoment))
    spans.push({ placed: p, col: from, span: to - from + 1, lane: 0, before: p.start < first, after: lastMoment >= end })
  }
  spans.sort((a, b) => a.col - b.col || b.span - a.span || a.placed.start.getTime() - b.placed.start.getTime())
  const lanes: number[] = []
  for (const s of spans) {
    let lane = lanes.findIndex((free) => free <= s.col)
    if (lane === -1) {
      lane = lanes.length
      lanes.push(0)
    }
    lanes[lane] = s.col + s.span
    s.lane = lane
  }
  return spans
}

/**
 * How many events each day of a row cannot show when only `lanes` lines fit
 * under its date. When any day overflows, the last line goes to the "more"
 * link, so one fewer line of bars shows. Returns the lines shown and the
 * hidden count per day.
 */
export function rowOverflow<T>(spans: RowSpan<T>[], days: number, lanes: number): { shown: number; hidden: number[] } {
  const perDay = new Array<number>(days).fill(0)
  for (const s of spans) {
    for (let i = s.col; i < s.col + s.span; i++) {
      perDay[i]++
    }
  }
  const overflow = perDay.some((n, i) => n > lanes || spans.some((s) => s.lane >= lanes && s.col <= i && i < s.col + s.span))
  const shown = overflow ? Math.max(0, lanes - 1) : lanes
  const hidden = new Array<number>(days).fill(0)
  for (const s of spans) {
    if (s.lane >= shown) {
      for (let i = s.col; i < s.col + s.span; i++) {
        hidden[i]++
      }
    }
  }
  return { shown, hidden }
}

/** The occurrences that touch a day, all-day ones first, then by start. */
export function eventsOnDay<T extends { allDay: boolean }>(events: Placed<T>[], day: Date): Placed<T>[] {
  const from = startOfDay(day)
  const to = addDays(from, 1)
  return events
    .filter((p) => p.start < to && (p.end > from || p.start.getTime() === from.getTime()))
    .sort((a, b) => Number(b.occ.allDay) - Number(a.occ.allDay) || a.start.getTime() - b.start.getTime())
}

/** The minutes of the day a pointer is at, from its offset into a day column. */
export function minutesAt(offset: number, height: number, step = 15): number {
  const ratio = Math.min(1, Math.max(0, offset / Math.max(1, height)))
  return Math.min(dayMinutes, snapMinutes(ratio * dayMinutes, step))
}
