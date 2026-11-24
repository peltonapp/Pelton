// yearplan.ts is the arithmetic behind the year view: the occurrences of one
// year are spread over the days they touch in a single pass, each day gets a
// busy level, and multi-day events become bars over the rows of a month.
// everything is pure so it can be tested without a component.

import { addDays, dayKey, daysBetween, layoutRow, monthGrid, startOfDay, type Placed, type RowSpan } from './caldate'
import type { Occurrence } from './types'

/** The busy levels a day can have, 0 for nothing and 4 for the busiest. */
export const busyLevels = 4

/** The most lines of multi-day bars drawn under a week of a month. */
export const maxBarLanes = 2

/** The occurrences of a year by day: yyyy-mm-dd to the entries touching that day. */
export type DayIndex = Map<string, Placed<Occurrence>[]>

/** The last moment an entry covers: its end less a millisecond, or its start when it has no length. */
function lastMoment(p: Placed<Occurrence>): Date {
  return p.end > p.start ? new Date(p.end.getTime() - 1) : p.start
}

/**
 * Spreads occurrences over the days of a year they touch, in one pass. A
 * multi-day event is listed on each of its days; days outside the year are
 * skipped. Cancelled events are left out. Each day lists all-day entries
 * first, then by start.
 */
export function aggregateYear(list: Placed<Occurrence>[], year: number): DayIndex {
  const first = new Date(year, 0, 1)
  const last = new Date(year, 11, 31)
  const out: DayIndex = new Map()
  for (const p of list) {
    if (p.occ.status === 'CANCELLED') {
      continue
    }
    const from = startOfDay(p.start) < first ? first : startOfDay(p.start)
    const lastDay = startOfDay(lastMoment(p))
    const to = lastDay > last ? last : lastDay
    for (let d = from; d <= to; d = addDays(d, 1)) {
      const key = dayKey(d)
      const entries = out.get(key)
      if (entries) {
        entries.push(p)
      } else {
        out.set(key, [p])
      }
    }
  }
  for (const entries of out.values()) {
    entries.sort((a, b) => Number(b.occ.allDay) - Number(a.occ.allDay) || a.start.getTime() - b.start.getTime())
  }
  return out
}

/**
 * How busy a day with n entries is, 0 to busyLevels: none, one, two or three,
 * four or five, six or more.
 */
export function busyLevel(n: number): number {
  if (n <= 0) {
    return 0
  }
  if (n === 1) {
    return 1
  }
  if (n <= 3) {
    return 2
  }
  return n <= 5 ? 3 : 4
}

/** How many month columns fit a measured width: 4, 3 or 2. A width not measured yet counts as wide. */
export function yearColumns(width: number): number {
  if (width <= 0 || width >= 840) {
    return 4
  }
  return width >= 600 ? 3 : 2
}

/** Says whether bars have room: each of the eight columns of a month (the week number and seven days) is wide enough. */
export function barsFit(width: number, columns: number): boolean {
  return width <= 0 || width / columns / 8 >= 24
}

/** The ISO 8601 week number of a day, taken on the Thursday of its Monday-first week. */
export function isoWeek(d: Date): number {
  const day = startOfDay(d)
  const thursday = addDays(day, 3 - ((day.getDay() + 6) % 7))
  const jan1 = new Date(thursday.getFullYear(), 0, 1)
  return Math.floor(daysBetween(jan1, thursday) / 7) + 1
}

/** One week of a month in the year view. */
export interface YearWeek {
  /** Seven days from the week's first; days of the neighbouring months are null. */
  days: (Date | null)[]
  /** The week number the row is labelled with. */
  number: number
  /** The multi-day bars over the row's days. */
  bars: RowSpan<Occurrence>[]
}

/** True when the entry covers more than one calendar day. */
export function isMultiDay(p: Placed<Occurrence>): boolean {
  return daysBetween(startOfDay(p.start), startOfDay(lastMoment(p))) >= 1
}

/**
 * The weeks of a month for the year view: the days that belong to it, each
 * row's week number, and the bars of the multi-day entries limited to
 * maxBarLanes lines. Bars are clipped to the days of the month.
 */
export function yearMonth(year: number, month: number, weekStart: number, multi: Placed<Occurrence>[]): YearWeek[] {
  return monthGrid(new Date(year, month, 1), weekStart).map((week) => {
    const days = week.map((d) => (d.getMonth() === month ? d : null))
    const inMonth = days.flatMap((d, i) => (d ? [i] : []))
    const a = inMonth[0]
    const b = inMonth[inMonth.length - 1]
    const bars = layoutRow(multi, week.slice(a, b + 1))
      .filter((s) => s.lane < maxBarLanes)
      .map((s) => ({ ...s, col: s.col + a }))
    return { days, number: isoWeek(week[(4 - weekStart + 7) % 7]), bars }
  })
}

/** The entries to draw as bars: those that cover more than one day. */
export function multiDayEntries(list: Placed<Occurrence>[]): Placed<Occurrence>[] {
  return list.filter((p) => p.occ.status !== 'CANCELLED' && isMultiDay(p))
}

/** Steps a focused day within the year view: left and right by a day, up and down by a week, page keys by a month. */
export function stepDay(day: Date, key: string, rtl = false): Date | null {
  const back = rtl ? 'ArrowRight' : 'ArrowLeft'
  const forward = rtl ? 'ArrowLeft' : 'ArrowRight'
  switch (key) {
    case back:
      return addDays(day, -1)
    case forward:
      return addDays(day, 1)
    case 'ArrowUp':
      return addDays(day, -7)
    case 'ArrowDown':
      return addDays(day, 7)
    case 'PageUp':
      return clampMonth(day, -1)
    case 'PageDown':
      return clampMonth(day, 1)
    case 'Home':
      return new Date(day.getFullYear(), day.getMonth(), 1)
    case 'End':
      return new Date(day.getFullYear(), day.getMonth() + 1, 0)
    default:
      return null
  }
}

function clampMonth(day: Date, n: number): Date {
  const target = new Date(day.getFullYear(), day.getMonth() + n, 1)
  const last = new Date(target.getFullYear(), target.getMonth() + 1, 0).getDate()
  return new Date(target.getFullYear(), target.getMonth(), Math.min(day.getDate(), last))
}
