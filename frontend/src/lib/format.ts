// format.ts holds small pure display helpers: dates for the list and header,
// sender initials for the avatar, and human byte sizes for attachments. keeping
// them here keeps formatting out of component markup.

// TimeFormat is the user's clock preference: auto follows the system locale,
// 12/24 force that clock everywhere times render. Callers pass the current
// preference in (usually $prefs.timeFormat) so views re-render when it changes.
export type TimeFormat = 'auto' | '12' | '24'

// clockOf maps the preference onto Intl's clock options; an empty object lets
// the locale decide. The 24 hour clock is asked for as the h23 cycle rather
// than hour12:false, which some locales turn into h24 and show midnight as
// "24:05".
function clockOf(timeFormat: TimeFormat): Intl.DateTimeFormatOptions {
  if (timeFormat === '12') {
    return { hour12: true }
  }
  if (timeFormat === '24') {
    return { hourCycle: 'h23' }
  }
  return {}
}

// formatListDate renders a date compactly for the message list: time for today,
// weekday for the last week, otherwise a short date. empty input yields "".
export function formatListDate(iso: string, timeFormat: TimeFormat = 'auto'): string {
  if (!iso) {
    return ''
  }
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) {
    return ''
  }

  const now = new Date()
  const sameDay = date.toDateString() === now.toDateString()
  if (sameDay) {
    return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', ...clockOf(timeFormat) })
  }

  const weekMs = 7 * 24 * 60 * 60 * 1000
  if (now.getTime() - date.getTime() < weekMs) {
    return date.toLocaleDateString(undefined, { weekday: 'short' })
  }

  const sameYear = date.getFullYear() === now.getFullYear()
  return date.toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    year: sameYear ? undefined : 'numeric',
  })
}

// formatStatusClock renders the status bar clock: a short weekday and date with
// the time, in the user's clock preference.
export function formatStatusClock(date: Date, timeFormat: TimeFormat = 'auto'): string {
  const day = date.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
  const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', ...clockOf(timeFormat) })
  return `${day} ${time}`
}

// formatFullDate renders a full, unambiguous date for the message header.
export function formatFullDate(iso: string, timeFormat: TimeFormat = 'auto'): string {
  if (!iso) {
    return ''
  }
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toLocaleString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    ...clockOf(timeFormat),
  })
}

// formatWeekdayTime renders "Mon 9:30" style labels for scheduling presets
// (send later, snooze, outbox rows).
export function formatWeekdayTime(date: Date, timeFormat: TimeFormat = 'auto'): string {
  return date.toLocaleString(undefined, {
    weekday: 'short',
    hour: 'numeric',
    minute: '2-digit',
    ...clockOf(timeFormat),
  })
}

// formatDateTimeMedium renders a medium date, optionally with a short time,
// for the date picker's selection summary.
export function formatDateTimeMedium(date: Date, withTime: boolean, timeFormat: TimeFormat = 'auto'): string {
  if (!withTime) {
    return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(date)
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
    ...clockOf(timeFormat),
  }).format(date)
}

// initials derives one or two uppercase letters for an avatar from a display
// name, falling back to the email local part, then to a neutral dot.
export function initials(name: string, email: string): string {
  // a display name often still carries the address ("Foo <foo@x.com>"); strip
  // the bracketed part so its "<" never leaks into the initials (e.g. "F<"),
  // and only keep tokens that start with a letter or digit.
  const cleaned = name
    .replace(/<[^>]*>/g, ' ')
    .replace(/["']/g, '')
    .trim()
  const source = cleaned || email.split('@')[0] || ''
  const parts = source.split(/[\s._-]+/).filter((p) => /^[\p{L}\p{N}]/u.test(p))
  if (parts.length === 0) {
    return '•'
  }
  if (parts.length === 1) {
    return parts[0].slice(0, 2).toUpperCase()
  }
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}

// avatarColor derives a stable, pleasant background color for an initials avatar
// from a seed (the sender email or name). the hue is hashed from the seed so the
// same sender always gets the same color; saturation and lightness are fixed so
// white text stays readable on top in both themes.
export function avatarColor(seed: string): string {
  let hash = 0
  for (let i = 0; i < seed.length; i++) {
    hash = (hash * 31 + seed.charCodeAt(i)) >>> 0
  }
  return `hsl(${hash % 360} 48% 45%)`
}

// formatBytes renders a byte count as a human size for the attachment list.
export function formatBytes(bytes: number): string {
  if (bytes <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB']
  const exp = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, exp)
  const rounded = exp === 0 ? value : Math.round(value * 10) / 10
  return `${rounded} ${units[exp]}`
}

// formatRelative renders an epoch-ms timestamp as a short relative time for the
// status bar: "just now", "5m ago", "2h ago", otherwise a date. The wording is
// translated, so the caller passes the t() lookup rather than this reaching for
// the store itself: that keeps the label recomputing when the language changes.
export function formatRelative(epochMs: number, t: (key: string) => string): string {
  const diff = Date.now() - epochMs
  const sec = Math.floor(diff / 1000)
  if (sec < 45) {
    return t('common.time.justNow')
  }
  const min = Math.max(1, Math.floor(sec / 60))
  if (min < 60) {
    return t('common.time.minutesAgo').replace('{n}', String(min))
  }
  const hr = Math.floor(min / 60)
  if (hr < 24) {
    return t('common.time.hoursAgo').replace('{n}', String(hr))
  }
  return new Date(epochMs).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

// displayName returns the best label for a sender: name if present, else email.
export function displayName(name: string, email: string): string {
  return name.trim() || email
}

// AccountNames is the part of an Account that names it. Taking the fields
// rather than the whole account keeps this usable from the wizard, which is
// still building one.
export interface AccountNames {
  email: string
  displayName: string
  localLabel: string
  useLocalLabel: boolean
}

/**
 * accountLabel returns what to call a mailbox in this app: the local label when
 * one is set and switched on, otherwise the From name, otherwise the address.
 * The From name is what recipients see, so anything on screen that is not the
 * outgoing identity should go through here.
 */
export function accountLabel(account: AccountNames): string {
  if (account.useLocalLabel && account.localLabel.trim()) {
    return account.localLabel.trim()
  }
  return account.displayName.trim() || account.email
}

// TextSegment is one piece of linkified plain text: either literal text to
// render as-is, or a url/mailto to render as a clickable link.
export type TextSegment = { text: string; href?: string }

// urlPattern matches bare http(s) and mailto references in plain text, the
// only two schemes Pelton ever hands off to the OS default browser.
const urlPattern = /(https?:\/\/[^\s<>"')\]]+|mailto:[^\s<>"')\]]+)/gi

// trailingPunctuation trims characters a URL is unlikely to end with when
// it's actually the tail of a sentence, e.g. "see https://example.com."
const trailingPunctuation = /[.,;:!?)\]]+$/

// linkifySegments splits plain text mail into literal and link segments so
// bare URLs in a plain-text message can be rendered as real clickable links,
// the same way an html message's own <a> tags are. Sentence-trailing
// punctuation right after a URL is kept as literal text, not part of the link.
export function linkifySegments(text: string): TextSegment[] {
  const segments: TextSegment[] = []
  let lastIndex = 0
  for (const match of text.matchAll(urlPattern)) {
    const start = match.index ?? 0
    if (start > lastIndex) {
      segments.push({ text: text.slice(lastIndex, start) })
    }
    let url = match[0]
    const trailing = url.match(trailingPunctuation)?.[0] ?? ''
    if (trailing) {
      url = url.slice(0, url.length - trailing.length)
    }
    if (url) {
      segments.push({ text: url, href: url })
    }
    lastIndex = start + match[0].length
    if (trailing) {
      segments.push({ text: trailing })
    }
  }
  if (lastIndex < text.length) {
    segments.push({ text: text.slice(lastIndex) })
  }
  return segments
}
