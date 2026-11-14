import { describe, expect, it } from 'vitest'
import { buildRRule, defaultRepeat, describeRRule, parseRRule, presetOf, presetRule, weekOfMonth } from './recurrence'

// 2026-11-10 is the second Tuesday of November.
const start = new Date(2026, 10, 10, 9)

const strings: Record<string, string> = {
  'calendar.repeat.every.week': 'Weekly',
  'calendar.repeat.everyN.week': 'Every {n} weeks',
  'calendar.repeat.every.month': 'Monthly',
  'calendar.repeat.on': 'on {days}',
  'calendar.repeat.times': '{n} times',
  'calendar.repeat.nth.2': 'on the second {day}',
  'calendar.repeat.weekdays': 'Every weekday',
  'calendar.repeat.custom': 'Custom',
}
const t = (key: string): string => strings[key] ?? key

describe('recurrence rules', () => {
  it('writes the presets', () => {
    expect(presetRule('none', start)).toBe('')
    expect(presetRule('daily', start)).toBe('FREQ=DAILY')
    expect(presetRule('weekdays', start)).toBe('FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR')
    expect(presetRule('weekly', start)).toBe('FREQ=WEEKLY')
    expect(presetRule('monthly', start)).toBe('FREQ=MONTHLY')
  })

  it('writes intervals, days and ends', () => {
    const r = { ...defaultRepeat(start), interval: 2, byDay: [4, 2], end: 'count' as const, count: 6 }
    expect(buildRRule(r, start)).toBe('FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,TH;COUNT=6')
    const monthly = { ...defaultRepeat(start, 'MONTHLY'), monthly: 'weekday' as const, end: 'until' as const, until: '2027-03-01' }
    expect(buildRRule(monthly, start)).toBe('FREQ=MONTHLY;BYDAY=2TU;UNTIL=20270301T235959Z')
  })

  it('reads back what it writes', () => {
    const rule = 'FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,TH;COUNT=6'
    const r = parseRRule(rule, start)
    expect(r).toMatchObject({ freq: 'WEEKLY', interval: 2, byDay: [2, 4], end: 'count', count: 6 })
    expect(buildRRule(r!, start)).toBe(rule)
    expect(parseRRule('FREQ=MONTHLY;UNTIL=20270301T235959Z', start)).toMatchObject({ end: 'until', until: '2027-03-01' })
  })

  it('leaves rules the form cannot hold alone', () => {
    expect(parseRRule('FREQ=MONTHLY;BYDAY=MO,TU;BYSETPOS=-1', start)).toBeNull()
    expect(parseRRule('FREQ=HOURLY', start)).toBeNull()
    expect(presetOf('FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1', start)).toBe('custom')
  })

  it('names the preset a rule is', () => {
    expect(presetOf('', start)).toBe('none')
    expect(presetOf('FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR', start)).toBe('weekdays')
    expect(presetOf('FREQ=WEEKLY;BYDAY=TU', start)).toBe('weekly')
    expect(presetOf('FREQ=WEEKLY;BYDAY=WE', start)).toBe('custom')
    expect(presetOf('FREQ=YEARLY', start)).toBe('yearly')
    expect(presetOf('FREQ=DAILY;COUNT=3', start)).toBe('custom')
  })

  it('finds the week of the month, with the last one as -1', () => {
    expect(weekOfMonth(start)).toBe(2)
    expect(weekOfMonth(new Date(2026, 10, 24))).toBe(-1)
    expect(weekOfMonth(new Date(2026, 10, 1))).toBe(1)
  })

  it('describes a rule in words', () => {
    expect(describeRRule('FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,TH;COUNT=6', start, t, 'en')).toBe('Every 2 weeks on Tue, Thu, 6 times')
    expect(describeRRule('FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR', start, t, 'en')).toBe('Every weekday')
    expect(describeRRule('FREQ=MONTHLY;BYDAY=2TU', start, t, 'en')).toBe('Monthly on the second Tuesday')
    expect(describeRRule('FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1', start, t, 'en')).toBe('Custom')
  })
})
