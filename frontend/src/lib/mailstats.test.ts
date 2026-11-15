import { describe, expect, it } from 'vitest'
import {
  busiest,
  filterNewsletters,
  formatDuration,
  frequencyLabel,
  hourGrid,
  sortNewsletters,
  volumeBuckets,
  type NewsletterSender,
} from './mailstats'

const t = (key: string): string =>
  ({
    'newsletters.freq.none': 'none',
    'newsletters.freq.daily': '{n}/day',
    'newsletters.freq.weekly': '{n}/week',
    'newsletters.freq.monthly': '{n}/month',
    'newsletters.freq.rare': 'rare',
    'insights.duration.minutes': '{n} min',
    'insights.duration.hours': '{n} h',
    'insights.duration.hoursMinutes': '{h} h {m} min',
    'insights.duration.days': '{n} days',
  })[key] ?? key

function sender(over: Partial<NewsletterSender>): NewsletterSender {
  return {
    email: 'a@example.com',
    name: '',
    listId: '',
    count: 1,
    unread: 0,
    bytes: 0,
    firstDate: '2026-01-01T00:00:00Z',
    lastDate: '2026-01-01T00:00:00Z',
    perWeek: 0,
    latestId: 1,
    unsubscribeKind: 'oneclick',
    unsubscribedAt: '',
    ...over,
  }
}

describe('newsletter list helpers', () => {
  const list = [
    sender({ email: 'b@x', name: 'Bravo', count: 5, unread: 1, bytes: 10, lastDate: '2026-03-01T00:00:00Z' }),
    sender({ email: 'a@x', name: 'Alpha', count: 2, unread: 4, bytes: 900, lastDate: '2026-05-01T00:00:00Z', listId: '<deals.shop>' }),
    sender({ email: 'c@x', name: '', count: 5, unread: 0, bytes: 50, lastDate: '2026-01-01T00:00:00Z' }),
  ]

  it('sorts by each key, largest or newest first, ties by name', () => {
    expect(sortNewsletters(list, 'volume').map((s) => s.email)).toEqual(['b@x', 'c@x', 'a@x'])
    expect(sortNewsletters(list, 'recency').map((s) => s.email)).toEqual(['a@x', 'b@x', 'c@x'])
    expect(sortNewsletters(list, 'unread').map((s) => s.email)).toEqual(['a@x', 'b@x', 'c@x'])
    expect(sortNewsletters(list, 'storage').map((s) => s.email)).toEqual(['a@x', 'c@x', 'b@x'])
  })

  it('filters on name, address and list id', () => {
    expect(filterNewsletters(list, 'brav').map((s) => s.email)).toEqual(['b@x'])
    expect(filterNewsletters(list, 'DEALS').map((s) => s.email)).toEqual(['a@x'])
    expect(filterNewsletters(list, '  ')).toHaveLength(3)
  })

  it('words how often a sender writes', () => {
    expect(frequencyLabel(0, t)).toBe('none')
    expect(frequencyLabel(14, t)).toBe('2/day')
    expect(frequencyLabel(3.2, t)).toBe('3/week')
    expect(frequencyLabel(0.5, t)).toBe('2/month')
    expect(frequencyLabel(0.1, t)).toBe('rare')
  })
})

describe('volume buckets', () => {
  const days = [
    { day: '2026-11-01', received: 3, sent: 1 },
    { day: '2026-11-15', received: 2, sent: 0 },
  ]

  it('gives one bar per day for 30 days and fills the gaps', () => {
    const bars = volumeBuckets(days, '30', '2026-11-15')
    expect(bars).toHaveLength(30)
    expect(bars[0].start).toBe('2026-10-17')
    expect(bars[29]).toEqual({ start: '2026-11-15', end: '2026-11-15', received: 2, sent: 0 })
    expect(bars.find((b) => b.start === '2026-11-01')).toMatchObject({ received: 3, sent: 1 })
    expect(bars.reduce((n, b) => n + b.received, 0)).toBe(5)
  })

  it('gives weeks from Monday for a year', () => {
    const bars = volumeBuckets(days, '365', '2026-11-15')
    expect(bars.length).toBeGreaterThanOrEqual(53)
    expect(new Date(`${bars[0].start}T00:00:00Z`).getUTCDay()).toBe(1)
    expect(bars[bars.length - 1].end).toBe('2026-11-15')
    expect(bars.reduce((n, b) => n + b.received + b.sent, 0)).toBe(6)
  })

  it('gives months from the first mail for all mail, and nothing without mail', () => {
    const bars = volumeBuckets([{ day: '2026-09-20', received: 1, sent: 2 }, ...days], 'all', '2026-11-15')
    expect(bars.map((b) => b.start)).toEqual(['2026-09-01', '2026-10-01', '2026-11-01'])
    expect(bars[2]).toMatchObject({ received: 5, sent: 1, end: '2026-11-15' })
    expect(volumeBuckets([], 'all', '2026-11-15')).toEqual([])
  })
})

describe('hour grid', () => {
  const hours = [
    { weekday: 2, hour: 9, count: 5 },
    { weekday: 2, hour: 14, count: 1 },
    { weekday: 0, hour: 9, count: 2 },
  ]

  it('lays the counts out Monday first', () => {
    const grid = hourGrid(hours)
    expect(grid).toHaveLength(7)
    expect(grid[1][9]).toBe(5)
    expect(grid[6][9]).toBe(2)
    expect(grid[0].every((n) => n === 0)).toBe(true)
  })

  it('finds the busiest weekday and hour, or nothing', () => {
    expect(busiest(hours)).toEqual({ weekday: 2, hour: 9 })
    expect(busiest([])).toBeNull()
  })
})

describe('formatDuration', () => {
  it('picks minutes, hours or days', () => {
    expect(formatDuration(20, t)).toBe('1 min')
    expect(formatDuration(35 * 60, t)).toBe('35 min')
    expect(formatDuration(2 * 3600, t)).toBe('2 h')
    expect(formatDuration(3 * 3600 + 15 * 60, t)).toBe('3 h 15 min')
    expect(formatDuration(5 * 86400, t)).toBe('5 days')
  })
})
