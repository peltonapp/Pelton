// focus.ts holds the time arithmetic of focus mode, free of stores: when a
// chosen length ends, when "until" a clock time ends, and how the time left
// reads in the top bar.

/** The lengths focus can be started for from the menu and the palette. */
export type FocusLength = '25m' | '1h' | 'off'

/** When focus of the given length ends, or null for "until I turn it off". */
export function focusEnd(length: FocusLength, now: Date = new Date()): Date | null {
  switch (length) {
    case '25m':
      return new Date(now.getTime() + 25 * 60_000)
    case '1h':
      return new Date(now.getTime() + 60 * 60_000)
    case 'off':
      return null
  }
}

/**
 * The next time the clock shows hhmm ("14:30"): today, or tomorrow when that
 * time has passed. Null for anything that is not a time.
 */
export function untilClock(hhmm: string, now: Date = new Date()): Date | null {
  const match = /^(\d{1,2}):(\d{2})$/.exec(hhmm.trim())
  if (!match) {
    return null
  }
  const hours = Number(match[1])
  const minutes = Number(match[2])
  if (hours > 23 || minutes > 59) {
    return null
  }
  const at = new Date(now)
  at.setHours(hours, minutes, 0, 0)
  if (at.getTime() <= now.getTime()) {
    at.setDate(at.getDate() + 1)
  }
  return at
}

/** Whole minutes left until an RFC 3339 end, rounded up; null without an end. */
export function minutesLeft(until: string, now: Date = new Date()): number | null {
  if (!until) {
    return null
  }
  const end = Date.parse(until)
  if (Number.isNaN(end)) {
    return null
  }
  return Math.max(0, Math.ceil((end - now.getTime()) / 60_000))
}

/**
 * The time left as the top bar shows it, through the given translator:
 * "24 min" under an hour, "1 h 5 min" above it. Empty without an end.
 */
export function formatLeft(until: string, t: (key: string) => string, now: Date = new Date()): string {
  const left = minutesLeft(until, now)
  if (left === null) {
    return ''
  }
  if (left < 60) {
    return t('focus.left.minutes').replace('{m}', String(left))
  }
  return t('focus.left.hours')
    .replace('{h}', String(Math.floor(left / 60)))
    .replace('{m}', String(left % 60))
}
