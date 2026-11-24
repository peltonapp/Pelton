// eventphrase.ts reads a line such as "lunch with Ada tomorrow 12:30 for 1h at
// Cafe Blau" into an event. Dates and times come from chrono, which the caller
// loads and hands in; everything around them (a duration, a place, a repeat,
// a reminder, "all day", and the title that is left) is read here from
// per-language tables. It is pure: no store, no clock but the one passed in,
// and nothing typed leaves the app.

import { addDays, atMinutes, defaultSlot, minutesOfDay, sameDay, startOfDay } from './caldate'
import { buildRRule, defaultRepeat, type Freq } from './recurrence'

/** The part of a chrono result this module reads. */
export interface ChronoComponents {
  isCertain(component: 'day' | 'weekday' | 'hour' | 'month' | 'meridiem'): boolean
  date(): Date
}

/** One date or time chrono found. */
export interface ChronoResult {
  index: number
  text: string
  start: ChronoComponents
  end?: ChronoComponents | null
}

/** A chrono parser, such as chrono.de.casual. */
export interface ChronoLike {
  parse(text: string, ref: Date, option?: { forwardDate?: boolean }): ChronoResult[]
}

/** What a recognized piece of the text was read as. */
export type PartKind = 'date' | 'duration' | 'repeat' | 'reminder' | 'location' | 'allDay'

/** A recognized piece of the text, by its place in it. */
export interface PhrasePart {
  kind: PartKind
  text: string
  start: number
  end: number
}

/** The event a line describes. */
export interface PhraseResult {
  title: string
  start: Date
  end: Date
  allDay: boolean
  /** An RRULE value, empty for none. */
  rrule: string
  location: string
  /** Minutes before the start, null when the line names no reminder. */
  reminderMinutes: number | null
  /** False when nothing in the line said when; the start is then the default slot. */
  dated: boolean
  parts: PhrasePart[]
}

/** How to read a line. */
export interface PhraseOptions {
  /** The current time. */
  now: Date
  /** The day the line is read against: the day on screen, or a clicked day. */
  base: Date
  /** A clicked time slot on the base day, minutes after midnight, used when no time is typed. */
  baseMinutes?: number | null
  /** The app language; a language without a table reads with the English one. */
  lang: string
  /** Chrono parsers to try in order; the first that finds anything wins. */
  chrono: ChronoLike[]
  /** Length of an event with no end or duration, in minutes. */
  defaultMinutes?: number
}

interface RepeatSpec {
  freq: Freq
  interval: number
  byDay?: number[]
}

/** The words of one language beyond what chrono reads. */
export interface PhraseTable {
  /** Duration words to minutes per unit. Keys are lower case. */
  units: Record<string, number>
  /** Introduce a duration: "for" 1h. */
  durationLead: string[]
  /** Reminder patterns; {n} is the amount and {u} the unit. */
  reminders: string[]
  allDay: string[]
  /** Fixed repeat phrases, matched whole, lower case. */
  repeats: Record<string, RepeatSpec>
  /** "every" in front of a weekday, a unit or a number of units. */
  every: string[]
  /** Weekday names and short forms, to day numbers; they repeat after an "every". */
  weekdays: Record<string, number>
  /** Plural weekdays, "mondays", which repeat on their own. */
  weekdayPlurals: Record<string, number>
  /** Words a place follows: "at" Cafe Blau. */
  locationLead: string[]
  /** Words dropped from the edges of the title and around what was taken out. */
  fillers: string[]
  /** Reads 20.12. and 20.12.2026 as day and month, which chrono misses for some languages. */
  dayMonthDots: boolean
}

const enUnits: Record<string, number> = {
  m: 1, min: 1, mins: 1, minute: 1, minutes: 1,
  h: 60, hr: 60, hrs: 60, hour: 60, hours: 60,
  d: 1440, day: 1440, days: 1440,
  w: 10080, wk: 10080, week: 10080, weeks: 10080,
}

const deUnits: Record<string, number> = {
  m: 1, min: 1, minute: 1, minuten: 1,
  h: 60, std: 60, stunde: 60, stunden: 60,
  d: 1440, tag: 1440, tage: 1440, tagen: 1440,
  w: 10080, woche: 10080, wochen: 10080,
}

const weekly = (...days: number[]): RepeatSpec => ({ freq: 'WEEKLY', interval: 1, byDay: days })

/** The tables, by language. */
export const phraseTables: Record<string, PhraseTable> = {
  en: {
    units: enUnits,
    durationLead: ['for'],
    reminders: ['remind me {n} {u} before', 'remind me {n} {u} earlier', 'remind {n} {u} before', 'reminder {n} {u} before', '{n} {u} reminder', 'with a {n} {u} reminder'],
    allDay: ['all day', 'all-day', 'allday'],
    repeats: {
      daily: { freq: 'DAILY', interval: 1 },
      'every day': { freq: 'DAILY', interval: 1 },
      'each day': { freq: 'DAILY', interval: 1 },
      weekly: { freq: 'WEEKLY', interval: 1 },
      'every week': { freq: 'WEEKLY', interval: 1 },
      biweekly: { freq: 'WEEKLY', interval: 2 },
      'every other week': { freq: 'WEEKLY', interval: 2 },
      monthly: { freq: 'MONTHLY', interval: 1 },
      'every month': { freq: 'MONTHLY', interval: 1 },
      yearly: { freq: 'YEARLY', interval: 1 },
      annually: { freq: 'YEARLY', interval: 1 },
      'every year': { freq: 'YEARLY', interval: 1 },
      'every weekday': weekly(1, 2, 3, 4, 5),
      'every workday': weekly(1, 2, 3, 4, 5),
      weekdays: weekly(1, 2, 3, 4, 5),
      'on weekdays': weekly(1, 2, 3, 4, 5),
      'every weekend': weekly(0, 6),
    },
    every: ['every', 'each'],
    weekdays: {
      sunday: 0, sun: 0, monday: 1, mon: 1, tuesday: 2, tue: 2, tues: 2, wednesday: 3, wed: 3,
      thursday: 4, thu: 4, thur: 4, thurs: 4, friday: 5, fri: 5, saturday: 6, sat: 6,
    },
    weekdayPlurals: { sundays: 0, mondays: 1, tuesdays: 2, wednesdays: 3, thursdays: 4, fridays: 5, saturdays: 6 },
    locationLead: ['at', '@'],
    fillers: ['on', 'at', 'from', 'for', 'the', 'by', 'and', 'in', 'starting', 'until', 'till', 'to'],
    dayMonthDots: false,
  },
  de: {
    units: deUnits,
    durationLead: ['für', 'fuer', 'dauer'],
    reminders: ['erinnere mich {n} {u} vorher', 'erinnern {n} {u} vorher', 'erinnerung {n} {u} vorher', 'erinnerung {n} {u} davor', '{n} {u} vorher erinnern', 'mit erinnerung {n} {u} vorher'],
    allDay: ['ganztägig', 'ganztags', 'den ganzen tag'],
    repeats: {
      täglich: { freq: 'DAILY', interval: 1 },
      'jeden tag': { freq: 'DAILY', interval: 1 },
      wöchentlich: { freq: 'WEEKLY', interval: 1 },
      'jede woche': { freq: 'WEEKLY', interval: 1 },
      'alle zwei wochen': { freq: 'WEEKLY', interval: 2 },
      zweiwöchentlich: { freq: 'WEEKLY', interval: 2 },
      monatlich: { freq: 'MONTHLY', interval: 1 },
      'jeden monat': { freq: 'MONTHLY', interval: 1 },
      jährlich: { freq: 'YEARLY', interval: 1 },
      'jedes jahr': { freq: 'YEARLY', interval: 1 },
      'jeden werktag': weekly(1, 2, 3, 4, 5),
      werktags: weekly(1, 2, 3, 4, 5),
      'an werktagen': weekly(1, 2, 3, 4, 5),
      wochentags: weekly(1, 2, 3, 4, 5),
      'jedes wochenende': weekly(0, 6),
    },
    every: ['jeden', 'jede', 'jedes', 'alle'],
    weekdays: {
      sonntag: 0, so: 0, montag: 1, mo: 1, dienstag: 2, di: 2, mittwoch: 3, mi: 3,
      donnerstag: 4, do: 4, freitag: 5, fr: 5, samstag: 6, sa: 6, sonnabend: 6,
    },
    weekdayPlurals: { sonntags: 0, montags: 1, dienstags: 2, mittwochs: 3, donnerstags: 4, freitags: 5, samstags: 6 },
    locationLead: ['in', 'im', 'bei', 'beim', '@'],
    fillers: ['am', 'um', 'ab', 'von', 'bis', 'für', 'und', 'an', 'den', 'dem', 'der', 'die', 'das', 'im', 'in', 'zum', 'zur'],
    dayMonthDots: true,
  },
}

/** The table for an app language, English where there is none. */
export function phraseTable(lang: string): PhraseTable {
  return phraseTables[lang.toLowerCase().split(/[-_]/)[0]] ?? phraseTables.en
}

const letter = '[\\p{L}\\p{N}@]'

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// a phrase matched whole: not glued to letters on either side.
function wordRe(body: string): RegExp {
  return new RegExp(`(?<!${letter})(?:${body})(?!${letter})`, 'giu')
}

function alternatives(words: string[]): string {
  return [...words].sort((a, b) => b.length - a.length).map(escapeRe).join('|')
}

const number = '(\\d+(?:[.,]\\d+)?)'

class Masked {
  readonly taken: boolean[]
  readonly parts: PhrasePart[] = []

  constructor(readonly text: string) {
    this.taken = new Array(text.length).fill(false)
  }

  // the text with every taken character blanked, so later patterns and chrono
  // only see what is left while every index still lines up.
  get rest(): string {
    let out = ''
    for (let i = 0; i < this.text.length; i++) {
      out += this.taken[i] ? ' ' : this.text[i]
    }
    return out
  }

  take(kind: PartKind, start: number, end: number): void {
    for (let i = start; i < end; i++) {
      this.taken[i] = true
    }
    this.parts.push({ kind, text: this.text.slice(start, end), start, end })
  }

  // every match of re in what is left, handed to f, which takes what it uses.
  scan(re: RegExp, f: (m: RegExpExecArray) => void): void {
    for (const m of this.rest.matchAll(re)) {
      f(m as RegExpExecArray)
    }
  }
}

function unitMinutes(table: PhraseTable, unit: string): number {
  return table.units[unit.toLowerCase()] ?? 0
}

function amount(raw: string): number {
  return Number(raw.replace(',', '.'))
}

/** Reads a line into an event. */
export function parsePhrase(text: string, opts: PhraseOptions): PhraseResult {
  const table = phraseTable(opts.lang)
  const m = new Masked(text)
  const units = alternatives(Object.keys(table.units))

  let reminderMinutes: number | null = null
  for (const pattern of table.reminders) {
    const body = escapeRe(pattern).replace('\\{n\\}', number).replace('\\{u\\}', `\\s*(${units})`).replace(/ /g, '\\s+')
    m.scan(wordRe(body), (hit) => {
      if (reminderMinutes === null) {
        reminderMinutes = Math.round(amount(hit[1]) * unitMinutes(table, hit[2]))
        m.take('reminder', hit.index, hit.index + hit[0].length)
      }
    })
  }

  let repeat: RepeatSpec | null = null
  m.scan(wordRe(alternatives(Object.keys(table.repeats)).replace(/ /g, '\\s+')), (hit) => {
    if (!repeat) {
      repeat = { ...table.repeats[hit[0].toLowerCase().replace(/\s+/g, ' ')] }
      m.take('repeat', hit.index, hit.index + hit[0].length)
    }
  })
  if (!repeat) {
    const every = alternatives(table.every)
    const dayWords = { ...table.weekdays, ...table.weekdayPlurals }
    const days = alternatives(Object.keys(dayWords))
    // every monday, every mon and thu, mondays
    m.scan(wordRe(`(?:(${every})\\s+)?(${days})(?:\\s*(?:,|and|und|&)\\s*(?:${days}))*`), (hit) => {
      if (repeat || (!hit[1] && !(hit[2].toLowerCase() in table.weekdayPlurals))) {
        return
      }
      const words = hit[0].toLowerCase().split(/[^\p{L}]+/u).filter((w) => w in dayWords)
      repeat = weekly(...words.map((w) => dayWords[w]))
      m.take('repeat', hit.index, hit.index + hit[0].length)
    })
  }
  if (!repeat) {
    const every = alternatives(table.every)
    m.scan(wordRe(`(?:${every})\\s+(?:(\\d+)\\s+)?(${units})`), (hit) => {
      const per = unitMinutes(table, hit[2])
      const freq: Freq | null = per === 1440 ? 'DAILY' : per === 10080 ? 'WEEKLY' : null
      if (!repeat && freq) {
        repeat = { freq, interval: Math.max(1, Number(hit[1] ?? 1)) }
        m.take('repeat', hit.index, hit.index + hit[0].length)
      }
    })
  }

  let durationMinutes: number | null = null
  m.scan(wordRe(`(?:${alternatives(table.durationLead)})\\s+${number}\\s*(${units})`), (hit) => {
    if (durationMinutes === null) {
      durationMinutes = Math.round(amount(hit[1]) * unitMinutes(table, hit[2]))
      m.take('duration', hit.index, hit.index + hit[0].length)
    }
  })

  let allDayWord = false
  m.scan(wordRe(alternatives(table.allDay).replace(/ /g, '\\s+')), (hit) => {
    if (!allDayWord) {
      allDayWord = true
      m.take('allDay', hit.index, hit.index + hit[0].length)
    }
  })

  const today = sameDay(opts.base, opts.now)
  const ref = today ? opts.now : atMinutes(opts.base, opts.baseMinutes ?? 0)

  let dotted: Date | null = null
  if (table.dayMonthDots) {
    m.scan(/(?<![\d.])(\d{1,2})\.(\d{1,2})\.(\d{4}|\d{2})?(?![\d])/g, (hit) => {
      const day = Number(hit[1])
      const month = Number(hit[2]) - 1
      if (dotted || day < 1 || day > 31 || month < 0 || month > 11) {
        return
      }
      let year = hit[3] ? Number(hit[3]) : ref.getFullYear()
      if (hit[3] && hit[3].length === 2) {
        year += 2000
      }
      let d = new Date(year, month, day)
      if (!hit[3] && d < startOfDay(ref)) {
        d = new Date(year + 1, month, day)
      }
      if (d.getDate() === day) {
        dotted = d
        m.take('date', hit.index, hit.index + hit[0].length)
      }
    })
  }

  let results: ChronoResult[] = []
  const rest = m.rest
  for (const parser of opts.chrono) {
    results = parser.parse(rest, ref, { forwardDate: today }).filter((r) => r.text.trim() !== '')
    if (results.length > 0) {
      break
    }
  }
  for (const r of results) {
    m.take('date', r.index, r.index + r.text.length)
  }
  const dateSrc = results.find((r) => r.start.isCertain('day') || r.start.isCertain('weekday'))
  const timeSrc = results.find((r) => r.start.isCertain('hour'))
  const primary = timeSrc ?? dateSrc

  const location = takeLocation(m, table)
  const title = cleanTitle(m, table)

  const length = opts.defaultMinutes ?? 60
  let dated = results.length > 0 || dotted !== null || repeat !== null
  let day: Date
  if (dotted) {
    day = dotted
  } else if (dateSrc) {
    day = startOfDay(dateSrc.start.date())
  } else if (timeSrc) {
    day = startOfDay(timeSrc.start.date())
  } else {
    day = startOfDay(opts.base)
  }
  const explicitDay = dotted !== null || dateSrc !== undefined

  let allDay = allDayWord || (explicitDay && !timeSrc && (opts.baseMinutes ?? null) === null)
  let start: Date
  if (allDay) {
    start = startOfDay(day)
  } else if (timeSrc) {
    start = atMinutes(day, minutesOfDay(timeSrc.start.date()))
  } else if (opts.baseMinutes !== undefined && opts.baseMinutes !== null) {
    start = atMinutes(day, opts.baseMinutes)
  } else {
    start = defaultSlot(day, opts.now)
  }

  const spec = repeat as RepeatSpec | null
  if (spec?.byDay?.length && !explicitDay) {
    for (let i = 0; i < 7 && !spec.byDay.includes(start.getDay()); i++) {
      start = addDays(start, 1)
    }
  }

  let end: Date
  const rangeEnd = primary?.end && (primary.end.isCertain('day') || primary.end.isCertain('hour')) ? primary.end.date() : null
  if (allDay) {
    const lastDay = rangeEnd && startOfDay(rangeEnd) >= start ? startOfDay(rangeEnd) : start
    const days = durationMinutes !== null && durationMinutes >= 1440 ? Math.round(durationMinutes / 1440) : 0
    end = days > 0 ? addDays(start, days) : addDays(lastDay, 1)
  } else if (rangeEnd && timeSrc?.end?.isCertain('hour')) {
    end = atMinutes(sameDay(rangeEnd, timeSrc.start.date()) ? start : startOfDay(rangeEnd), minutesOfDay(rangeEnd))
    if (end <= start) {
      end = addDays(end, 1)
    }
  } else {
    end = new Date(start.getTime() + (durationMinutes ?? length) * 60000)
  }
  if (!dated && allDayWord) {
    dated = true
  }

  const rrule = spec ? buildRRule({ ...defaultRepeat(start, spec.freq), interval: spec.interval, byDay: spec.byDay ?? [start.getDay()] }, start) : ''
  m.parts.sort((a, b) => a.start - b.start)
  return { title, start, end, allDay, rrule, location, reminderMinutes, dated, parts: m.parts }
}

// takeLocation reads "at <place>" from what is left: the place runs from the
// lead word to the next piece that was taken or the end of a clause.
function takeLocation(m: Masked, table: PhraseTable): string {
  const rest = m.rest
  const lead = new RegExp(`(?<!${letter})(?:${alternatives(table.locationLead)})(?:\\s+|(?<=@))(?=[\\p{L}"'])`, 'giu')
  for (const hit of rest.matchAll(lead)) {
    const from = hit.index + hit[0].length
    let to = from
    while (to < rest.length && !m.taken[to] && !/[,;.!?()]/.test(rest[to])) {
      to++
    }
    const place = rest.slice(from, to).trim()
    if (place.length >= 2) {
      const endAt = from + rest.slice(from, to).trimEnd().length
      m.take('location', hit.index, endAt)
      return place.replace(/^["']|["']$/g, '')
    }
  }
  return ''
}

// cleanTitle is what is left once every recognized piece is out, without the
// little words that only joined those pieces to it.
function cleanTitle(m: Masked, table: PhraseTable): string {
  const fillers = new Set(table.fillers)
  // runs of text between taken pieces; a filler at a run's edge that faces a
  // taken piece goes.
  const runs: string[] = []
  let current = ''
  let afterTaken = false
  const flush = (beforeTaken: boolean): void => {
    let words = current.split(/\s+/).filter(Boolean)
    if (afterTaken) {
      while (words.length && fillers.has(words[0].toLowerCase())) {
        words = words.slice(1)
      }
    }
    if (beforeTaken) {
      while (words.length && fillers.has(words[words.length - 1].toLowerCase())) {
        words = words.slice(0, -1)
      }
    }
    if (words.length) {
      runs.push(words.join(' '))
    }
    current = ''
  }
  for (let i = 0; i < m.text.length; i++) {
    if (m.taken[i]) {
      if (current.trim()) {
        flush(true)
      }
      current = ''
      afterTaken = true
      continue
    }
    current += m.text[i]
  }
  flush(false)
  let title = runs.join(' ')
  title = title.replace(/\s+([,;:])/g, '$1').replace(/^[\s,;:\-–]+|[\s,;:\-]+$/g, '')
  return title.trim()
}

/** A date found in a longer text, such as a mail. */
export interface FoundDate {
  start: Date
  end: Date
  allDay: boolean
  /** The words it was read from. */
  text: string
  /** A place named right after it, if any. */
  location: string
}

/**
 * Finds the dates and times in a text such as a mail, read against when the
 * text was written. Dates with a time come before bare dates and future ones
 * before past ones; within that they keep the order they appear in.
 */
export function datesInText(text: string, opts: { ref: Date; now: Date; lang: string; chrono: ChronoLike[]; defaultMinutes?: number }): FoundDate[] {
  const table = phraseTable(opts.lang)
  const length = opts.defaultMinutes ?? 60
  const found: (FoundDate & { order: number; timed: boolean })[] = []
  const seen = new Set<string>()
  parseFirst(text, opts.ref, opts.chrono).forEach((r, order) => {
    const span = resultSpan(r, length)
    if (!span) {
      return
    }
    const { start, end, timed } = span
    const key = `${start.getTime()}|${timed}`
    if (seen.has(key)) {
      return
    }
    seen.add(key)
    found.push({ start, end, allDay: !timed, text: r.text.trim(), location: placeAfter(text, r.index + r.text.length, table), order, timed })
  })
  const nowDay = startOfDay(opts.now)
  found.sort((a, b) => {
    const futureA = (a.timed ? a.start >= opts.now : a.start >= nowDay) ? 0 : 1
    const futureB = (b.timed ? b.start >= opts.now : b.start >= nowDay) ? 0 : 1
    return Number(!a.timed) - Number(!b.timed) || futureA - futureB || a.order - b.order
  })
  return found.slice(0, 8).map(({ start, end, allDay, text: words, location }) => ({ start, end, allDay, text: words, location }))
}

// the results of the first parser that finds anything.
function parseFirst(text: string, ref: Date, chrono: ChronoLike[]): ChronoResult[] {
  for (const parser of chrono) {
    const results = parser.parse(text, ref)
    if (results.length > 0) {
      return results
    }
  }
  return []
}

// the span a result names: its times when it has an hour, else the days it
// covers. null for a result that names neither a day nor a time.
function resultSpan(r: ChronoResult, length: number): { start: Date; end: Date; timed: boolean } | null {
  const timed = r.start.isCertain('hour')
  if (!timed && !r.start.isCertain('day') && !r.start.isCertain('weekday')) {
    return null
  }
  const startAt = r.start.date()
  const start = timed ? new Date(startAt) : startOfDay(startAt)
  let end: Date
  if (timed) {
    end = r.end?.isCertain('hour') ? r.end.date() : new Date(start.getTime() + length * 60000)
    if (end <= start) {
      end = new Date(start.getTime() + length * 60000)
    }
  } else {
    const last = r.end?.isCertain('day') ? startOfDay(r.end.date()) : start
    end = addDays(last >= start ? last : start, 1)
  }
  return { start, end, timed }
}

/** A date named in a text, by where it stands in it. */
export interface DateMention {
  /** Where the words start in the text, and how long they run. */
  index: number
  length: number
  start: Date
  end: Date
  allDay: boolean
}

/**
 * Finds every date and time a text names, in the order they stand, each with
 * its place in the text, read against when the text was written. Read the
 * way a mail means them: a bare weekday is the next one rather than the last,
 * and an hour from 1 to 7 without am or pm and not written with a leading
 * zero is in the afternoon. A mention that only says
 * "now" or is shorter than three characters is left out.
 */
export function dateMentions(text: string, opts: { ref: Date; chrono: ChronoLike[]; defaultMinutes?: number }): DateMention[] {
  const length = opts.defaultMinutes ?? 60
  const out: DateMention[] = []
  let reached = 0
  for (const r of parseFirst(text, opts.ref, opts.chrono)) {
    const words = r.text.trimEnd()
    const lead = words.length - words.trimStart().length
    const index = r.index + lead
    const size = words.length - lead
    const span = resultSpan(r, length)
    if (!span || size < 3 || index < reached || /^now$/i.test(words.trim())) {
      continue
    }
    let { start, end } = span
    if (span.timed && !r.start.isCertain('meridiem') && start.getHours() >= 1 && start.getHours() <= 7 && !/(?<!\d)0\d[:.h]?\d{2}/.test(words)) {
      start = new Date(start.getTime() + 12 * 3600000)
      end = new Date(end.getTime() + 12 * 3600000)
    }
    if (!r.start.isCertain('day') && r.start.isCertain('weekday') && start < startOfDay(opts.ref)) {
      start = addDays(start, 7)
      end = addDays(end, 7)
    }
    out.push({ index, length: size, start, end, allDay: !span.timed })
    reached = index + size
  }
  return out
}

// placeAfter reads "at <Place>" directly after a date, up to the end of the
// clause, for a mail such as "on Friday at 18:00 at Cafe Blau."
function placeAfter(text: string, from: number, table: PhraseTable): string {
  const tail = text.slice(from, from + 120).split(/[\n.;!?]/)[0]
  const lead = new RegExp(`^\\s*,?\\s*(?:${alternatives(table.locationLead)})\\s+(\\p{Lu}[^,]*)`, 'u')
  const hit = tail.match(lead)
  return hit ? hit[1].trim() : ''
}
