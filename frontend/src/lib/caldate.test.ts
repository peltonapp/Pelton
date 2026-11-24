import { describe, expect, it } from 'vitest'
import {
  addDays,
  addMonths,
  dayKey,
  eventsOnDay,
  layoutDay,
  layoutRow,
  localeWeekStart,
  minutesAt,
  monthGrid,
  parseEventTime,
  placeOccurrence,
  resolveWeekStart,
  rowOverflow,
  startOfWeek,
  stepFocus,
  toRFC3339,
  viewRange,
  weekDays,
  type Placed,
} from './caldate'

interface Ev {
  id: string
  start: string
  end: string
  allDay: boolean
}

function ev(id: string, start: string, end: string, allDay = false): Placed<Ev> {
  return placeOccurrence({ id, start, end, allDay })
}

function local(y: number, m: number, d: number, h = 0, min = 0): Date {
  return new Date(y, m - 1, d, h, min)
}

describe('days and months', () => {
  it('clamps a month step to the last day of a shorter month', () => {
    expect(dayKey(addMonths(local(2026, 1, 31), 1))).toBe('2026-02-28')
    expect(dayKey(addMonths(local(2024, 1, 31), 1))).toBe('2024-02-29')
    expect(dayKey(addMonths(local(2026, 3, 15), -3))).toBe('2025-12-15')
  })

  it('steps days across the end of a month and a year', () => {
    expect(dayKey(addDays(local(2026, 12, 30), 3))).toBe('2027-01-02')
  })

  it('formats a local time as rfc3339 with its offset', () => {
    const d = local(2026, 11, 14, 9, 30)
    const s = toRFC3339(d)
    expect(s.startsWith('2026-11-14T09:30:00')).toBe(true)
    expect(new Date(s).getTime()).toBe(d.getTime())
  })

  it('reads an all-day date as local midnight', () => {
    expect(parseEventTime('2026-11-14', true).getTime()).toBe(local(2026, 11, 14).getTime())
  })

  it('maps a pointer offset to snapped minutes', () => {
    expect(minutesAt(0, 960)).toBe(0)
    expect(minutesAt(400, 960)).toBe(600)
    expect(minutesAt(407, 960)).toBe(615)
    expect(minutesAt(2000, 960)).toBe(1440)
  })
})

describe('weeks', () => {
  it('starts the week on the asked-for day', () => {
    // 2026-11-14 is a Saturday.
    const sat = local(2026, 11, 14)
    expect(dayKey(startOfWeek(sat, 1))).toBe('2026-11-09')
    expect(dayKey(startOfWeek(sat, 0))).toBe('2026-11-08')
    expect(dayKey(startOfWeek(sat, 6))).toBe('2026-11-14')
    expect(weekDays(sat, 1).map(dayKey)).toEqual([
      '2026-11-09',
      '2026-11-10',
      '2026-11-11',
      '2026-11-12',
      '2026-11-13',
      '2026-11-14',
      '2026-11-15',
    ])
  })

  it('follows the locale unless a day is set', () => {
    expect(localeWeekStart('de-DE')).toBe(1)
    expect(localeWeekStart('en-US')).toBe(0)
    expect(resolveWeekStart(3, 'en-US')).toBe(3)
    expect(resolveWeekStart(-1, 'de-DE')).toBe(1)
  })
})

describe('month grid', () => {
  it('covers the month in whole weeks', () => {
    const weeks = monthGrid(local(2026, 11, 14), 1)
    expect(weeks).toHaveLength(6)
    expect(dayKey(weeks[0][0])).toBe('2026-10-26')
    expect(dayKey(weeks[5][6])).toBe('2026-12-06')
    for (const week of weeks) {
      expect(week).toHaveLength(7)
    }
  })

  it('needs only four rows for a February that starts on the week start', () => {
    // 2026-02-01 is a Sunday.
    const weeks = monthGrid(local(2026, 2, 10), 0)
    expect(weeks).toHaveLength(4)
    expect(dayKey(weeks[0][0])).toBe('2026-02-01')
    expect(dayKey(weeks[3][6])).toBe('2026-02-28')
  })

  it('loads the grid the month view shows', () => {
    const r = viewRange('month', local(2026, 11, 14), 1)
    expect(dayKey(r.from)).toBe('2026-10-26')
    expect(dayKey(r.to)).toBe('2026-12-07')
  })

  it('loads one week, one day and the agenda span', () => {
    const focus = local(2026, 11, 14, 15)
    expect(dayKey(viewRange('week', focus, 1).from)).toBe('2026-11-09')
    expect(dayKey(viewRange('week', focus, 1).to)).toBe('2026-11-16')
    expect(dayKey(viewRange('day', focus, 1).to)).toBe('2026-11-15')
    expect(dayKey(viewRange('agenda', focus, 1).to)).toBe('2026-12-14')
    expect(dayKey(stepFocus('week', focus, -1))).toBe('2026-11-07')
  })
})

describe('time grid layout', () => {
  const day = local(2026, 11, 14)

  it('gives overlapping events their own columns', () => {
    const boxes = layoutDay(
      [
        ev('a', toRFC3339(local(2026, 11, 14, 9)), toRFC3339(local(2026, 11, 14, 11))),
        ev('b', toRFC3339(local(2026, 11, 14, 10)), toRFC3339(local(2026, 11, 14, 12))),
        ev('c', toRFC3339(local(2026, 11, 14, 11)), toRFC3339(local(2026, 11, 14, 12))),
        ev('d', toRFC3339(local(2026, 11, 14, 14)), toRFC3339(local(2026, 11, 14, 15))),
      ],
      day,
    )
    const by = Object.fromEntries(boxes.map((b) => [b.placed.occ.id, b]))
    expect(by.a).toMatchObject({ top: 540, bottom: 660, col: 0, cols: 2 })
    expect(by.b).toMatchObject({ col: 1, cols: 2 })
    // c starts as a ends, so it reuses the first column.
    expect(by.c).toMatchObject({ col: 0, cols: 2 })
    expect(by.d).toMatchObject({ col: 0, cols: 1 })
  })

  it('clips an event that crosses midnight to the day', () => {
    const late = ev('late', toRFC3339(local(2026, 11, 13, 22)), toRFC3339(local(2026, 11, 14, 2)))
    const [box] = layoutDay([late], day)
    expect(box).toMatchObject({ top: 0, bottom: 120 })
  })

  it('stretches a very short event to the minimum height', () => {
    const [box] = layoutDay([ev('s', toRFC3339(local(2026, 11, 14, 8)), toRFC3339(local(2026, 11, 14, 8, 5)))], day, 20)
    expect(box.bottom - box.top).toBe(20)
  })
})

describe('row layout', () => {
  const week = weekDays(local(2026, 11, 14), 1)

  it('puts longer bars first and stacks overlapping ones', () => {
    const spans = layoutRow(
      [
        ev('one', '2026-11-11', '2026-11-12', true),
        ev('trip', '2026-11-10', '2026-11-14', true),
        ev('carry', '2026-11-05', '2026-11-11', true),
      ],
      week,
    )
    const by = Object.fromEntries(spans.map((s) => [s.placed.occ.id, s]))
    expect(by.carry).toMatchObject({ col: 0, span: 2, lane: 0, before: true, after: false })
    expect(by.trip).toMatchObject({ col: 1, span: 4, lane: 1 })
    expect(by.one).toMatchObject({ col: 2, span: 1, lane: 0 })
  })

  it('counts what a day cannot show and keeps a line for the more link', () => {
    const spans = layoutRow(
      [
        ev('a', '2026-11-12', '2026-11-13', true),
        ev('b', '2026-11-12', '2026-11-13', true),
        ev('c', '2026-11-12', '2026-11-13', true),
        ev('d', '2026-11-09', '2026-11-10', true),
      ],
      week,
    )
    const { shown, hidden } = rowOverflow(spans, 7, 2)
    expect(shown).toBe(1)
    expect(hidden).toEqual([0, 0, 0, 2, 0, 0, 0])
    expect(rowOverflow(spans, 7, 3)).toEqual({ shown: 3, hidden: [0, 0, 0, 0, 0, 0, 0] })
  })

  it('lists a day with all-day events first', () => {
    const list = eventsOnDay(
      [ev('timed', toRFC3339(local(2026, 11, 14, 8)), toRFC3339(local(2026, 11, 14, 9))), ev('all', '2026-11-14', '2026-11-15', true)],
      local(2026, 11, 14),
    )
    expect(list.map((p) => p.occ.id)).toEqual(['all', 'timed'])
  })
})
