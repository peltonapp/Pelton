// recurrence.ts turns the event editor's repeat choice into an RRULE value and
// back. the editor offers what people actually pick (every day, weekdays,
// weekly on some days, monthly, yearly, every n of those, until a day or for a
// number of times); a rule written elsewhere that says more than that is kept
// as it is and shown as a custom rule.

import { dayKey } from './caldate'

/** How often a series repeats. */
export type Freq = 'DAILY' | 'WEEKLY' | 'MONTHLY' | 'YEARLY'

/** How a series ends. */
export type RepeatEnd = 'never' | 'count' | 'until'

/** The editor's repeat form. */
export interface Repeat {
  freq: Freq
  interval: number
  /** Weekdays for a weekly rule, 0 for Sunday. */
  byDay: number[]
  /** A monthly rule repeats on the date (14th) or the weekday (second Tuesday). */
  monthly: 'date' | 'weekday'
  end: RepeatEnd
  count: number
  /** yyyy-mm-dd, inclusive. */
  until: string
}

const dayCodes = ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA']

/** The repeat form a new rule starts from for an event starting at start. */
export function defaultRepeat(start: Date, freq: Freq = 'WEEKLY'): Repeat {
  return { freq, interval: 1, byDay: [start.getDay()], monthly: 'date', end: 'never', count: 10, until: '' }
}

/** Which week of its month a day falls in, 1 to 4, or -1 for the last. */
export function weekOfMonth(d: Date): number {
  const nth = Math.ceil(d.getDate() / 7)
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate()
  return d.getDate() + 7 > last && nth >= 4 ? -1 : nth
}

/** Writes the repeat form as an RRULE value for an event starting at start. */
export function buildRRule(r: Repeat, start: Date): string {
  const parts = [`FREQ=${r.freq}`]
  if (r.interval > 1) {
    parts.push(`INTERVAL=${Math.floor(r.interval)}`)
  }
  if (r.freq === 'WEEKLY') {
    const days = [...new Set(r.byDay)].sort((a, b) => a - b)
    if (days.length > 0 && !(days.length === 1 && days[0] === start.getDay())) {
      parts.push(`BYDAY=${days.map((d) => dayCodes[d]).join(',')}`)
    }
  }
  if (r.freq === 'MONTHLY' && r.monthly === 'weekday') {
    parts.push(`BYDAY=${weekOfMonth(start)}${dayCodes[start.getDay()]}`)
  }
  if (r.end === 'count' && r.count > 0) {
    parts.push(`COUNT=${Math.floor(r.count)}`)
  }
  if (r.end === 'until' && r.until) {
    parts.push(`UNTIL=${r.until.replaceAll('-', '')}T235959Z`)
  }
  return parts.join(';')
}

/**
 * Reads an RRULE value into the repeat form, or null when it says something
 * the form cannot hold (BYSETPOS, several monthly weekdays, hourly rules),
 * which the editor then keeps untouched.
 */
export function parseRRule(rule: string, start: Date): Repeat | null {
  if (!rule) {
    return null
  }
  const fields = new Map<string, string>()
  for (const part of rule.replace(/^RRULE:/i, '').split(';')) {
    const [k, v] = part.split('=')
    if (k && v !== undefined) {
      fields.set(k.toUpperCase(), v.toUpperCase())
    }
  }
  const freq = fields.get('FREQ') as Freq | undefined
  if (!freq || !['DAILY', 'WEEKLY', 'MONTHLY', 'YEARLY'].includes(freq)) {
    return null
  }
  const known = new Set(['FREQ', 'INTERVAL', 'BYDAY', 'COUNT', 'UNTIL', 'WKST'])
  if (freq === 'MONTHLY') {
    known.add('BYMONTHDAY')
  }
  if (freq === 'YEARLY') {
    known.add('BYMONTH').add('BYMONTHDAY')
  }
  for (const k of fields.keys()) {
    if (!known.has(k)) {
      return null
    }
  }
  const r = defaultRepeat(start, freq)
  r.interval = Math.max(1, Number(fields.get('INTERVAL') ?? 1) || 1)
  const byDay = fields.get('BYDAY')
  if (byDay) {
    if (freq === 'WEEKLY' || freq === 'DAILY') {
      const days = byDay.split(',').map((c) => dayCodes.indexOf(c))
      if (days.some((d) => d < 0)) {
        return null
      }
      if (freq === 'DAILY') {
        if (r.interval !== 1 || !isWeekdays(days)) {
          return null
        }
        r.freq = 'WEEKLY'
      }
      r.byDay = days
    } else if (freq === 'MONTHLY' && /^-?[1-5](SU|MO|TU|WE|TH|FR|SA)$/.test(byDay)) {
      r.monthly = 'weekday'
    } else {
      return null
    }
  }
  if (fields.has('COUNT')) {
    r.end = 'count'
    r.count = Number(fields.get('COUNT')) || 1
  } else if (fields.has('UNTIL')) {
    const u = fields.get('UNTIL') ?? ''
    r.end = 'until'
    r.until = `${u.slice(0, 4)}-${u.slice(4, 6)}-${u.slice(6, 8)}`
  }
  return r
}

function isWeekdays(days: number[]): boolean {
  const set = new Set(days)
  return set.size === 5 && [1, 2, 3, 4, 5].every((d) => set.has(d))
}

/** The preset a rule matches in the repeat menu, or custom. */
export type RepeatPreset = 'none' | 'daily' | 'weekdays' | 'weekly' | 'monthly' | 'yearly' | 'custom'

/** Names the preset an RRULE value is, for the editor's menu. */
export function presetOf(rule: string, start: Date): RepeatPreset {
  if (!rule) {
    return 'none'
  }
  const r = parseRRule(rule, start)
  if (!r || r.interval !== 1 || r.end !== 'never') {
    return 'custom'
  }
  if (r.freq === 'DAILY') {
    return 'daily'
  }
  if (r.freq === 'WEEKLY') {
    if (isWeekdays(r.byDay)) {
      return 'weekdays'
    }
    return r.byDay.length === 1 && r.byDay[0] === start.getDay() ? 'weekly' : 'custom'
  }
  if (r.freq === 'MONTHLY') {
    return r.monthly === 'date' ? 'monthly' : 'custom'
  }
  return 'yearly'
}

/** The RRULE value of a preset for an event starting at start; '' for none. */
export function presetRule(preset: Exclude<RepeatPreset, 'custom'>, start: Date): string {
  switch (preset) {
    case 'none':
      return ''
    case 'daily':
      return buildRRule(defaultRepeat(start, 'DAILY'), start)
    case 'weekdays':
      return buildRRule({ ...defaultRepeat(start, 'WEEKLY'), byDay: [1, 2, 3, 4, 5] }, start)
    case 'weekly':
      return buildRRule(defaultRepeat(start, 'WEEKLY'), start)
    case 'monthly':
      return buildRRule(defaultRepeat(start, 'MONTHLY'), start)
    case 'yearly':
      return buildRRule(defaultRepeat(start, 'YEARLY'), start)
  }
}

/**
 * Describes a rule in words through the given translator, such as "Every 2
 * weeks on Mon, Thu, 10 times". locale names the weekdays.
 */
export function describeRRule(rule: string, start: Date, t: (key: string) => string, locale: string): string {
  const r = parseRRule(rule, start)
  if (!r) {
    return rule ? t('calendar.repeat.custom') : ''
  }
  const unit = { DAILY: 'day', WEEKLY: 'week', MONTHLY: 'month', YEARLY: 'year' }[r.freq]
  let text = r.interval === 1 ? t(`calendar.repeat.every.${unit}`) : t(`calendar.repeat.everyN.${unit}`).replace('{n}', String(r.interval))
  if (r.freq === 'WEEKLY') {
    if (isWeekdays(r.byDay) && r.interval === 1) {
      text = t('calendar.repeat.weekdays')
    } else {
      const names = r.byDay
        .slice()
        .sort((a, b) => a - b)
        .map((d) => weekdayName(d, locale, 'short'))
      text += ` ${t('calendar.repeat.on').replace('{days}', names.join(', '))}`
    }
  }
  if (r.freq === 'MONTHLY' && r.monthly === 'weekday') {
    const nth = weekOfMonth(start)
    text += ` ${t(`calendar.repeat.nth.${nth === -1 ? 'last' : nth}`).replace('{day}', weekdayName(start.getDay(), locale, 'long'))}`
  }
  if (r.end === 'count') {
    text += `, ${t('calendar.repeat.times').replace('{n}', String(r.count))}`
  } else if (r.end === 'until' && r.until) {
    const until = new Date(`${r.until}T00:00:00`)
    text += `, ${t('calendar.repeat.until').replace('{date}', until.toLocaleDateString(locale, { day: 'numeric', month: 'short', year: 'numeric' }))}`
  }
  return text
}

/** The name of a weekday, 0 for Sunday, in a locale. */
export function weekdayName(day: number, locale: string, width: 'short' | 'long' | 'narrow'): string {
  // 2023-01-01 was a Sunday.
  return new Date(2023, 0, 1 + day).toLocaleDateString(locale, { weekday: width })
}

/** The until day a series ends on, as yyyy-mm-dd, for an end picked a month after start. */
export function defaultUntil(start: Date): string {
  return dayKey(new Date(start.getFullYear(), start.getMonth() + 1, start.getDate()))
}
