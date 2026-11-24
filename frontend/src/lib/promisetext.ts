// promisetext.ts words the promises the reader and the Tasks space show: who
// owes what, and dates as a person would say them.

import { parseDayKey } from './caldate'
import type { MailPromise } from './types'

/** A due value or a time as a short date, with the time of day when it has one; '' for none. */
export function shortDate(value: string, locale: string): string {
  if (!value) {
    return ''
  }
  const timed = value.length > 10
  const d = timed ? new Date(value) : parseDayKey(value)
  if (Number.isNaN(d.getTime())) {
    return ''
  }
  const opts: Intl.DateTimeFormatOptions = { weekday: 'short', day: 'numeric', month: 'short' }
  if (timed) {
    opts.hour = 'numeric'
    opts.minute = '2-digit'
  }
  return d.toLocaleString(locale || undefined, opts)
}

/** The name a promise's person goes by. */
export function personOf(p: Pick<MailPromise, 'personName' | 'personEmail'>): string {
  return p.personName.trim() || p.personEmail
}

/** The locale key of the line that says who owes a promise. */
export function ownerKey(p: Pick<MailPromise, 'direction'>): string {
  return p.direction === 'mine' ? 'promises.owner.mine' : 'promises.owner.theirs'
}
