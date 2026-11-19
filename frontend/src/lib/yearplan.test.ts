import { describe, expect, it } from 'vitest'
import { aggregateYear, barsFit, busyLevel, isoWeek, isMultiDay, multiDayEntries, stepDay, yearColumns, yearMonth } from './yearplan'
import { dayKey, placeOccurrence } from './caldate'
import type { Occurrence } from './types'

function occ(id: number, start: string, end: string, extra: Partial<Occurrence> = {}): Occurrence {
  return {
    eventId: id,
    calendarId: 1,
    calendarColor: '',
    uid: `u${id}`,
    summary: `Event ${id}`,
    location: '',
    start,
    end,
    allDay: false,
    recurring: false,
    recurrenceId: '',
    override: false,
    status: '',
    transparency: '',
    readOnly: false,
    hasReminders: false,
    pending: false,
    conflict: false,
    ...extra,
  }
}

const placed = (list: Occurrence[]) => list.map(placeOccurrence)

describe('aggregateYear', () => {
  it('lists an event on the day it falls on', () => {
    const idx = aggregateYear(placed([occ(1, '2026-03-04T09:00:00', '2026-03-04T10:00:00')]), 2026)
    expect([...idx.keys()]).toEqual(['2026-03-04'])
    expect(idx.get('2026-03-04')?.[0].occ.eventId).toBe(1)
  })

  it('spreads an all-day span over each day it covers, end exclusive', () => {
    const idx = aggregateYear(placed([occ(1, '2026-03-04', '2026-03-07', { allDay: true })]), 2026)
    expect([...idx.keys()].sort()).toEqual(['2026-03-04', '2026-03-05', '2026-03-06'])
  })

  it('does not count a timed event ending at midnight on the next day', () => {
    const idx = aggregateYear(placed([occ(1, '2026-03-04T22:00:00', '2026-03-05T00:00:00')]), 2026)
    expect([...idx.keys()]).toEqual(['2026-03-04'])
  })

  it('clips events crossing the year edges', () => {
    const idx = aggregateYear(placed([occ(1, '2025-12-30', '2026-01-03', { allDay: true }), occ(2, '2026-12-30', '2027-01-03', { allDay: true })]), 2026)
    expect([...idx.keys()].sort()).toEqual(['2026-01-01', '2026-01-02', '2026-12-30', '2026-12-31'])
  })

  it('skips cancelled events', () => {
    const idx = aggregateYear(placed([occ(1, '2026-03-04T09:00:00', '2026-03-04T10:00:00', { status: 'CANCELLED' })]), 2026)
    expect(idx.size).toBe(0)
  })

  it('orders a day with all-day entries first, then by start', () => {
    const idx = aggregateYear(
      placed([occ(1, '2026-03-04T15:00:00', '2026-03-04T16:00:00'), occ(2, '2026-03-04T09:00:00', '2026-03-04T10:00:00'), occ(3, '2026-03-04', '2026-03-05', { allDay: true })]),
      2026,
    )
    expect(idx.get('2026-03-04')?.map((p) => p.occ.eventId)).toEqual([3, 2, 1])
  })

  it('handles thousands of occurrences in one pass', () => {
    const list: Occurrence[] = []
    for (let i = 0; i < 6000; i++) {
      const day = new Date(2026, 0, 1 + (i % 365))
      list.push(occ(i, `${dayKey(day)}T09:00:00`, `${dayKey(day)}T10:00:00`))
    }
    const started = performance.now()
    const idx = aggregateYear(placed(list), 2026)
    expect(performance.now() - started).toBeLessThan(1000)
    expect(idx.size).toBe(365)
  })
})

describe('busyLevel', () => {
  it.each([
    [0, 0],
    [1, 1],
    [2, 2],
    [3, 2],
    [4, 3],
    [5, 3],
    [6, 4],
    [40, 4],
  ])('maps %i entries to level %i', (n, level) => {
    expect(busyLevel(n)).toBe(level)
  })
})

describe('yearColumns and barsFit', () => {
  it.each([
    [0, 4],
    [1200, 4],
    [840, 4],
    [839, 3],
    [600, 3],
    [599, 2],
    [320, 2],
  ])('gives %i px %i columns', (width, cols) => {
    expect(yearColumns(width)).toBe(cols)
  })

  it('draws bars only when a day is wide enough', () => {
    expect(barsFit(1200, 4)).toBe(true)
    expect(barsFit(320, 2)).toBe(false)
  })
})

describe('isoWeek', () => {
  it.each([
    [new Date(2026, 0, 1), 1],
    [new Date(2026, 0, 4), 1],
    [new Date(2026, 0, 5), 2],
    [new Date(2026, 11, 31), 53],
    [new Date(2027, 0, 1), 53],
    [new Date(2024, 11, 30), 1],
  ])('numbers %s as week %i', (d, week) => {
    expect(isoWeek(d)).toBe(week)
  })
})

describe('yearMonth', () => {
  it('blanks days of the neighbouring months and numbers each week', () => {
    const weeks = yearMonth(2026, 10, 1, [])
    expect(weeks).toHaveLength(6)
    expect(weeks[0].days.filter(Boolean)).toHaveLength(1)
    expect(weeks[0].days[6]?.getDate()).toBe(1)
    expect(weeks[0].number).toBe(44)
    expect(weeks[5].days.filter(Boolean)).toHaveLength(1)
    expect(weeks[5].number).toBe(49)
  })

  it('follows the week start', () => {
    const weeks = yearMonth(2026, 10, 0, [])
    expect(weeks[0].days[0]?.getDate()).toBe(1)
  })

  it('clips multi-day bars to the month and keeps two lanes', () => {
    const multi = multiDayEntries(
      placed([
        occ(1, '2026-10-28', '2026-11-04', { allDay: true }),
        occ(2, '2026-11-02', '2026-11-04', { allDay: true }),
        occ(3, '2026-11-02', '2026-11-04', { allDay: true }),
        occ(4, '2026-11-03T09:00:00', '2026-11-03T10:00:00'),
      ]),
    )
    const weeks = yearMonth(2026, 10, 1, multi)
    const first = weeks[0].bars
    expect(first).toHaveLength(1)
    expect(first[0].col).toBe(6)
    expect(first[0].span).toBe(1)
    expect(first[0].before).toBe(true)
    expect(weeks[1].bars.every((b) => b.lane < 2)).toBe(true)
    expect(weeks[1].bars).toHaveLength(2)
  })
})

describe('isMultiDay', () => {
  it('tells a one-day entry from a spanning one', () => {
    expect(isMultiDay(placeOccurrence(occ(1, '2026-03-04', '2026-03-05', { allDay: true })))).toBe(false)
    expect(isMultiDay(placeOccurrence(occ(1, '2026-03-04', '2026-03-06', { allDay: true })))).toBe(true)
    expect(isMultiDay(placeOccurrence(occ(1, '2026-03-04T22:00:00', '2026-03-05T02:00:00')))).toBe(true)
  })
})

describe('stepDay', () => {
  const day = new Date(2026, 2, 31)

  it('moves by day, week and month', () => {
    expect(dayKey(stepDay(day, 'ArrowLeft')!)).toBe('2026-03-30')
    expect(dayKey(stepDay(day, 'ArrowRight')!)).toBe('2026-04-01')
    expect(dayKey(stepDay(day, 'ArrowUp')!)).toBe('2026-03-24')
    expect(dayKey(stepDay(day, 'ArrowDown')!)).toBe('2026-04-07')
    expect(dayKey(stepDay(day, 'PageDown')!)).toBe('2026-04-30')
    expect(dayKey(stepDay(day, 'PageUp')!)).toBe('2026-02-28')
    expect(dayKey(stepDay(day, 'Home')!)).toBe('2026-03-01')
    expect(dayKey(stepDay(day, 'End')!)).toBe('2026-03-31')
  })

  it('mirrors left and right in a right-to-left layout', () => {
    expect(dayKey(stepDay(day, 'ArrowLeft', true)!)).toBe('2026-04-01')
  })

  it('ignores other keys', () => {
    expect(stepDay(day, 'a')).toBeNull()
  })
})
