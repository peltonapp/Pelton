// taskphrase.ts reads a quick entry line such as "call Ada tomorrow 3pm" or
// "pay rent every month on the 1st" into a task, through the same phrase
// reading the calendar's quick add uses. A task differs from an event in one
// way here: a day without a time stays a date, unless the settings give due
// dates a default time.

import { atMinutes, dayKey } from './caldate'
import { parsePhrase, type ChronoLike, type PhrasePart } from './eventphrase'

/** The task a line describes. */
export interface TaskPhrase {
  title: string
  /** A date (2026-11-15), an rfc3339 instant, or empty when the line names no day. */
  due: string
  /** An RRULE value, empty for none. */
  rrule: string
  /** Minutes before the due time, null when the line names no reminder. */
  reminderMinutes: number | null
  parts: PhrasePart[]
}

/** How to read a line. */
export interface TaskPhraseOptions {
  now: Date
  lang: string
  chrono: ChronoLike[]
  /** The time of day a bare date gets, minutes after midnight, or -1 to keep it a date. */
  defaultDueMinutes: number
}

const monthNames = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

// a bare day of the month, "on the 1st", which chrono does not read: it is
// written out as the next such date, in English, which every chrono list
// the app loads reads as its fallback.
function spellDayOfMonth(text: string, now: Date): string {
  return text.replace(/\b(?:on\s+)?the\s+(\d{1,2})(?:st|nd|rd|th)\b(?!\s+of\b)/i, (whole, digits: string) => {
    const day = Number(digits)
    if (day < 1 || day > 31) {
      return whole
    }
    let month = now.getMonth()
    let year = now.getFullYear()
    for (let i = 0; i < 13; i++) {
      const d = new Date(year, month, day)
      if (d.getDate() === day && d >= new Date(now.getFullYear(), now.getMonth(), now.getDate())) {
        return `on ${monthNames[month]} ${day} ${year}`
      }
      month += 1
      if (month > 11) {
        month = 0
        year += 1
      }
    }
    return whole
  })
}

/** Reads a quick entry line into a task. */
export function parseTaskPhrase(text: string, opts: TaskPhraseOptions): TaskPhrase {
  const r = parsePhrase(spellDayOfMonth(text, opts.now), { now: opts.now, base: opts.now, lang: opts.lang, chrono: opts.chrono })
  const title = r.title || text.trim()
  if (!r.dated) {
    return { title, due: '', rrule: '', reminderMinutes: r.reminderMinutes, parts: r.parts }
  }
  const hasTime = !r.allDay && r.parts.some((p) => p.kind === 'date')
  let due: string
  if (hasTime) {
    due = r.start.toISOString()
  } else if (opts.defaultDueMinutes >= 0) {
    due = atMinutes(r.start, opts.defaultDueMinutes).toISOString()
  } else {
    due = dayKey(r.start)
  }
  return { title, due, rrule: r.rrule, reminderMinutes: r.reminderMinutes, parts: r.parts }
}
