// mailstats.ts holds the shapes and the pure helpers of the newsletter manager
// and the insights page: sorting and filtering senders, wording how often one
// writes, binning daily volume into bars, and the weekday and hour grid. The
// numbers themselves come from the backend, which aggregates them in sql.

/** How a sender's newest message can be unsubscribed from, '' for not at all. */
export type UnsubscribeKind = '' | 'oneclick' | 'mailto' | 'link'

/** One sender of List-Unsubscribe mail. Dates are RFC 3339. */
export interface NewsletterSender {
  email: string
  name: string
  listId: string
  count: number
  unread: number
  bytes: number
  firstDate: string
  lastDate: string
  /** Messages a week over the last 90 days. */
  perWeek: number
  /** The newest message, which an unsubscribe acts on. */
  latestId: number
  unsubscribeKind: UnsubscribeKind
  /** When Pelton unsubscribed from this sender, '' if never. */
  unsubscribedAt: string
}

/** What the newsletter table sorts by. */
export type NewsletterSort = 'volume' | 'recency' | 'unread' | 'storage'

/** The sort keys in the order the control lists them. */
export const newsletterSorts: NewsletterSort[] = ['volume', 'recency', 'unread', 'storage']

/** Returns the senders sorted by key, largest or newest first, ties by name. */
export function sortNewsletters(list: NewsletterSender[], key: NewsletterSort): NewsletterSender[] {
  const value = (s: NewsletterSender): number | string => {
    switch (key) {
      case 'volume':
        return s.count
      case 'recency':
        return s.lastDate
      case 'unread':
        return s.unread
      case 'storage':
        return s.bytes
    }
  }
  return [...list].sort((a, b) => {
    const x = value(a)
    const y = value(b)
    if (x !== y) {
      return x < y ? 1 : -1
    }
    return senderLabel(a).localeCompare(senderLabel(b))
  })
}

/** Keeps the senders whose name, address or list id contains the text. */
export function filterNewsletters(list: NewsletterSender[], text: string): NewsletterSender[] {
  const needle = text.trim().toLowerCase()
  if (!needle) {
    return list
  }
  return list.filter((s) => `${s.name}\n${s.email}\n${s.listId}`.toLowerCase().includes(needle))
}

/** The name to show for a sender: the display name, else the address. */
export function senderLabel(s: { name: string; email: string }): string {
  return s.name.trim() || s.email
}

/** Words how often a sender writes, from a weekly rate. */
export function frequencyLabel(perWeek: number, t: (key: string) => string): string {
  if (perWeek <= 0) {
    return t('newsletters.freq.none')
  }
  if (perWeek >= 6.5) {
    return t('newsletters.freq.daily').replace('{n}', String(Math.max(1, Math.round(perWeek / 7))))
  }
  if (perWeek >= 0.95) {
    return t('newsletters.freq.weekly').replace('{n}', String(Math.round(perWeek)))
  }
  const perMonth = (perWeek * 365) / 12 / 7
  if (perMonth >= 0.95) {
    return t('newsletters.freq.monthly').replace('{n}', String(Math.round(perMonth)))
  }
  return t('newsletters.freq.rare')
}

/** One account's share of the store, folders largest first. */
export interface InsightsAccount {
  id: number
  email: string
  label: string
  local: boolean
  count: number
  bytes: number
  folders: { id: number; name: string; role: string; count: number; bytes: number }[]
}

/** A sender or recipient with a count, and a size where it applies. */
export interface InsightsPerson {
  email: string
  name: string
  count: number
  bytes: number
}

/** One stored attachment and the message holding it. */
export interface InsightsAttachment {
  id: number
  messageId: number
  filename: string
  contentType: string
  bytes: number
  subject: string
  from: string
  date: string
}

/** The storage section of the insights page. */
export interface InsightsStorage {
  accounts: InsightsAccount[]
  senders: InsightsPerson[]
  attachments: InsightsAttachment[]
}

/** Mail of one local day, YYYY-MM-DD. */
export interface InsightsDay {
  day: string
  received: number
  sent: number
}

/** Received mail of one local weekday (0 is Sunday) and hour. */
export interface InsightsHour {
  weekday: number
  hour: number
  count: number
}

/** The volume section for a range. */
export interface InsightsVolume {
  since: string
  days: InsightsDay[]
  hours: InsightsHour[]
  hasSent: boolean
}

/** One side's median reply time; leave it out unless available. */
export interface InsightsReply {
  available: boolean
  count: number
  medianSeconds: number
}

/** The people section for a range. */
export interface InsightsPeople {
  writesTo: InsightsPerson[]
  writesYou: InsightsPerson[]
  myReplies: InsightsReply
  theirReplies: InsightsReply
}

/** The ranges the volume and people sections can cover. */
export type InsightsRange = '30' | '90' | '365' | 'all'

/** The ranges in display order. */
export const insightsRanges: InsightsRange[] = ['30', '90', '365', 'all']

/** Days the range covers, 0 for all mail. */
export function rangeDays(range: InsightsRange): number {
  return range === 'all' ? 0 : Number(range)
}

/** One bar of the volume chart: a day, a week or a month of mail. */
export interface VolumeBucket {
  /** First day the bar covers, YYYY-MM-DD. */
  start: string
  /** Last day the bar covers, YYYY-MM-DD. */
  end: string
  received: number
  sent: number
}

const dayMs = 24 * 60 * 60 * 1000

function parseDay(day: string): number {
  const [y, m, d] = day.split('-').map(Number)
  return Date.UTC(y, m - 1, d)
}

function formatDay(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10)
}

/**
 * Bins daily volume into bars and fills the days without mail: one bar a day
 * for 30 and 90 days, a week (from Monday) for a year, a month for all mail,
 * which starts at the first day that has any. Bars run oldest first and end on
 * today.
 */
export function volumeBuckets(days: InsightsDay[], range: InsightsRange, today: string): VolumeBucket[] {
  const end = parseDay(today)
  let starts: number[] = []
  if (range === '30' || range === '90') {
    const n = Number(range)
    starts = Array.from({ length: n }, (_, i) => end - (n - 1 - i) * dayMs)
  } else if (range === '365') {
    const first = end - 364 * dayMs
    const weekday = (new Date(first).getUTCDay() + 6) % 7
    for (let s = first - weekday * dayMs; s <= end; s += 7 * dayMs) {
      starts.push(s)
    }
  } else {
    if (days.length === 0) {
      return []
    }
    const first = new Date(parseDay(days[0].day))
    let y = first.getUTCFullYear()
    let m = first.getUTCMonth()
    while (Date.UTC(y, m, 1) <= end) {
      starts.push(Date.UTC(y, m, 1))
      m += 1
      if (m === 12) {
        m = 0
        y += 1
      }
    }
  }
  const buckets: VolumeBucket[] = starts.map((s, i) => ({
    start: formatDay(s),
    end: formatDay(Math.min(i + 1 < starts.length ? starts[i + 1] - dayMs : end, end)),
    received: 0,
    sent: 0,
  }))
  for (const d of days) {
    const at = parseDay(d.day)
    let lo = 0
    let hi = starts.length - 1
    if (hi < 0 || at < starts[0] || at > end) {
      continue
    }
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1
      if (starts[mid] <= at) {
        lo = mid
      } else {
        hi = mid - 1
      }
    }
    buckets[lo].received += d.received
    buckets[lo].sent += d.sent
  }
  return buckets
}

/** Weekdays in grid order, Monday first, as Date.getDay numbers. */
export const gridWeekdays = [1, 2, 3, 4, 5, 6, 0]

/** The grid as rows of 24 hours, in gridWeekdays order. */
export function hourGrid(hours: InsightsHour[]): number[][] {
  const grid = gridWeekdays.map(() => new Array<number>(24).fill(0))
  for (const h of hours) {
    const row = gridWeekdays.indexOf(h.weekday)
    if (row >= 0 && h.hour >= 0 && h.hour < 24) {
      grid[row][h.hour] += h.count
    }
  }
  return grid
}

/** The busiest weekday (a Date.getDay number) and hour, or null without mail. */
export function busiest(hours: InsightsHour[]): { weekday: number; hour: number } | null {
  const byDay = new Map<number, number>()
  const byHour = new Map<number, number>()
  for (const h of hours) {
    byDay.set(h.weekday, (byDay.get(h.weekday) ?? 0) + h.count)
    byHour.set(h.hour, (byHour.get(h.hour) ?? 0) + h.count)
  }
  const top = (m: Map<number, number>): number => [...m.entries()].sort((a, b) => b[1] - a[1] || a[0] - b[0])[0][0]
  if (byDay.size === 0) {
    return null
  }
  return { weekday: top(byDay), hour: top(byHour) }
}

/** Words a duration in seconds: minutes, hours and minutes, or days. */
export function formatDuration(seconds: number, t: (key: string) => string): string {
  const minutes = Math.max(1, Math.round(seconds / 60))
  if (minutes < 60) {
    return t('insights.duration.minutes').replace('{n}', String(minutes))
  }
  const hours = Math.floor(minutes / 60)
  if (hours < 48) {
    const rest = minutes % 60
    return rest === 0
      ? t('insights.duration.hours').replace('{n}', String(hours))
      : t('insights.duration.hoursMinutes').replace('{h}', String(hours)).replace('{m}', String(rest))
  }
  return t('insights.duration.days').replace('{n}', String(Math.round(hours / 24)))
}
