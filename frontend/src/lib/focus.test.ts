import { describe, expect, it } from 'vitest'
import { focusEnd, formatLeft, minutesLeft, untilClock } from './focus'
import { summaryText } from '../stores/focus'

const en: Record<string, string> = {
  'focus.left.minutes': '{m} min',
  'focus.left.hours': '{h} h {m} min',
  'focus.ended.title': 'Focus ended: {summary}',
  'focus.ended.nothing': 'Focus ended. Nothing new arrived.',
  'focus.ended.one': '1 new message',
  'focus.ended.many': '{n} new messages',
  'focus.ended.vipOne': '1 from a VIP',
  'focus.ended.vipMany': '{n} from VIPs',
  'focus.ended.withVip': '{count}, {vip}',
}
const t = (key: string): string => en[key] ?? key

describe('focus times', () => {
  const now = new Date(2026, 10, 16, 10, 0, 0)

  it('ends a chosen length from now, or never', () => {
    expect(focusEnd('25m', now)?.getTime()).toBe(now.getTime() + 25 * 60_000)
    expect(focusEnd('1h', now)?.getTime()).toBe(now.getTime() + 60 * 60_000)
    expect(focusEnd('off', now)).toBeNull()
  })

  it('runs until a clock time today, or tomorrow once it has passed', () => {
    expect(untilClock('14:30', now)).toEqual(new Date(2026, 10, 16, 14, 30))
    expect(untilClock('09:15', now)).toEqual(new Date(2026, 10, 17, 9, 15))
    expect(untilClock('10:00', now)).toEqual(new Date(2026, 10, 17, 10, 0))
    expect(untilClock('25:00', now)).toBeNull()
    expect(untilClock('soon', now)).toBeNull()
  })

  it('reads the time left the way the top bar shows it', () => {
    const in24 = new Date(now.getTime() + 23 * 60_000 + 30_000).toISOString()
    expect(minutesLeft(in24, now)).toBe(24)
    expect(formatLeft(in24, t, now)).toBe('24 min')
    const in65 = new Date(now.getTime() + 65 * 60_000).toISOString()
    expect(formatLeft(in65, t, now)).toBe('1 h 5 min')
    expect(formatLeft('', t, now)).toBe('')
    expect(minutesLeft(new Date(now.getTime() - 60_000).toISOString(), now)).toBe(0)
  })
})

describe('the summary toast', () => {
  it('says what arrived and how much of it was from VIPs', () => {
    expect(summaryText({ total: 0, vip: 0 }, t)).toBe('Focus ended. Nothing new arrived.')
    expect(summaryText({ total: 1, vip: 0 }, t)).toBe('Focus ended: 1 new message')
    expect(summaryText({ total: 7, vip: 0 }, t)).toBe('Focus ended: 7 new messages')
    expect(summaryText({ total: 7, vip: 2 }, t)).toBe('Focus ended: 7 new messages, 2 from VIPs')
    expect(summaryText({ total: 3, vip: 1 }, t)).toBe('Focus ended: 3 new messages, 1 from a VIP')
  })
})
