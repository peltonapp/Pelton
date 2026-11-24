// freeslots.ts finds the free time in a stretch of days, for sharing when one
// is available from the composer, and writes the times picked out as plain
// text. It is pure: the calendar's occurrences, the clock and the words come
// in as arguments, and nothing leaves the app.
//
// Days and working hours are read in local wall-clock time, so a day that
// gains or loses an hour to daylight saving still runs from the same clock
// time to the same clock time.

import { addDays, atMinutes, parseEventTime, startOfDay } from './caldate'
import type { Occurrence } from './types'

/** A stretch of time something is on, or free. */
export interface Span {
  start: Date
  end: Date
}

/** How to look for free time. */
export interface SlotOptions {
  /** The first day looked at, and the day after the last. */
  from: Date
  to: Date
  /** The shortest free time worth offering, in minutes. */
  minutes: number
  /** Working hours, minutes after midnight. */
  workStart: number
  workEnd: number
  /** Saturdays and Sundays count. */
  weekends: boolean
  /** Free time before this is dropped. */
  now?: Date
  /** Free time starts on a multiple of this many minutes past the hour. */
  step?: number
}

/**
 * The spans the given occurrences keep busy. An event marked free, a
 * cancelled one and a task's entry keep nothing busy. An all-day event keeps
 * its days busy only when it says so (OPAQUE), since a birthday or a holiday
 * usually leaves the day open; a timed event is busy unless marked free.
 */
export function busySpans(list: Occurrence[], calendarIds: ReadonlySet<number> | null = null): Span[] {
  const out: Span[] = []
  for (const occ of list) {
    if (occ.task || occ.status === 'CANCELLED' || occ.transparency === 'TRANSPARENT') {
      continue
    }
    if (calendarIds && !calendarIds.has(occ.calendarId)) {
      continue
    }
    if (occ.allDay && occ.transparency !== 'OPAQUE') {
      continue
    }
    const start = parseEventTime(occ.start, occ.allDay)
    let end = parseEventTime(occ.end, occ.allDay)
    if (end <= start) {
      end = occ.allDay ? addDays(start, 1) : start
    }
    if (end > start) {
      out.push({ start, end })
    }
  }
  return out.sort((a, b) => a.start.getTime() - b.start.getTime())
}

// roundUp moves a time forward to the next multiple of step minutes past the
// hour, keeping a time that already is one.
function roundUp(d: Date, step: number): Date {
  const out = new Date(d)
  out.setSeconds(0, 0)
  if (out < d) {
    out.setMinutes(out.getMinutes() + 1)
  }
  const extra = out.getMinutes() % step
  if (extra !== 0) {
    out.setMinutes(out.getMinutes() + step - extra)
  }
  return out
}

/**
 * The free stretches within working hours on each day of the range, each at
 * least the given length, in order. Busy spans may overlap, run across
 * midnight or over several days.
 */
export function freeSlots(busy: Span[], opts: SlotOptions): Span[] {
  const step = Math.max(1, opts.step ?? 15)
  const out: Span[] = []
  const sorted = [...busy].sort((a, b) => a.start.getTime() - b.start.getTime())
  for (let day = startOfDay(opts.from); day < opts.to; day = addDays(day, 1)) {
    const weekday = day.getDay()
    if (!opts.weekends && (weekday === 0 || weekday === 6)) {
      continue
    }
    let cursor = atMinutes(day, opts.workStart)
    const close = atMinutes(day, opts.workEnd)
    if (opts.now && cursor < opts.now) {
      cursor = opts.now
    }
    cursor = roundUp(cursor, step)
    const offer = (until: Date): void => {
      if (until.getTime() - cursor.getTime() >= opts.minutes * 60000) {
        out.push({ start: cursor, end: until })
      }
    }
    for (const b of sorted) {
      if (cursor >= close) {
        break
      }
      if (b.end <= cursor || b.start >= close) {
        continue
      }
      if (b.start > cursor) {
        offer(b.start)
      }
      if (b.end > cursor) {
        cursor = roundUp(b.end, step)
      }
    }
    if (cursor < close) {
      offer(close)
    }
  }
  return out
}

/** The words the text of shared times is built from. */
export interface AvailabilityWords {
  /** A day and its times: "{day}: {times}". */
  line: string
  /** One stretch: "{from} to {to}". */
  range: string
  /** The closing line naming the time zone: "Times are in {zone}." */
  zone: string
}

/** How to write the times. */
export interface AvailabilityFormat {
  words: AvailabilityWords
  locale?: string
  /** Writes a time of day, such as 14:00 or 2:00 PM. */
  time: (d: Date) => string
  /** The IANA zone the times are in, for its name. */
  timeZone?: string
}

/** The name of a time zone at a moment, such as "Central European Standard Time (Europe/Berlin)". */
export function zoneLabel(at: Date, locale?: string, timeZone?: string): string {
  let long = ''
  try {
    const parts = new Intl.DateTimeFormat(locale, { timeZone, timeZoneName: 'long' }).formatToParts(at)
    long = parts.find((p) => p.type === 'timeZoneName')?.value ?? ''
  } catch {
    long = ''
  }
  let id = timeZone ?? ''
  if (!id) {
    try {
      id = Intl.DateTimeFormat().resolvedOptions().timeZone ?? ''
    } catch {
      id = ''
    }
  }
  if (long && id && long !== id) {
    return `${long} (${id})`
  }
  return long || id
}

/**
 * Writes the picked free times as plain text, one line per day in order,
 * such as "Tue 18 Nov: 10:00 to 11:30, 14:00 to 16:00", with a last line
 * naming the time zone. Empty when nothing is picked.
 */
export function formatAvailability(slots: Span[], fmt: AvailabilityFormat): string {
  if (slots.length === 0) {
    return ''
  }
  const sorted = [...slots].sort((a, b) => a.start.getTime() - b.start.getTime())
  const days = new Map<string, Span[]>()
  for (const s of sorted) {
    const key = startOfDay(s.start).toDateString()
    days.set(key, [...(days.get(key) ?? []), s])
  }
  const lines: string[] = []
  for (const list of days.values()) {
    const day = list[0].start.toLocaleDateString(fmt.locale, { weekday: 'short', day: 'numeric', month: 'short' })
    const times = list.map((s) => fmt.words.range.replace('{from}', fmt.time(s.start)).replace('{to}', fmt.time(s.end))).join(', ')
    lines.push(fmt.words.line.replace('{day}', day).replace('{times}', times))
  }
  lines.push(fmt.words.zone.replace('{zone}', zoneLabel(sorted[0].start, fmt.locale, fmt.timeZone)))
  return lines.join('\n')
}
