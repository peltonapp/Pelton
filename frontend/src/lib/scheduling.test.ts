import { afterAll, describe, expect, it, vi } from 'vitest'
import { conflictsAt, suggestTimes, toBlocks, type Availability, type BusyBlock } from './scheduling'
import { toRFC3339 } from './caldate'
import type { BusyKind } from './types'

vi.stubEnv('TZ', 'Europe/Berlin')

afterAll(() => {
  vi.unstubAllEnvs()
})

// Monday 16 November 2026.
const at = (d: number, h: number, m = 0): Date => new Date(2026, 10, d, h, m)

function block(start: Date, end: Date, kind: BusyKind = 'busy'): BusyBlock {
  return { start, end, kind }
}

function row(email: string, busy: BusyBlock[], known = true): Availability {
  return { email, known, busy }
}

const hm = (d: Date): string => `${d.getDate()} ${d.toTimeString().slice(0, 5)}`

const day = { from: at(16, 0), to: at(17, 0), workStart: 9 * 60, workEnd: 17 * 60 }

describe('toBlocks', () => {
  it('reads instants whatever offset they were written with, sorted', () => {
    const blocks = toBlocks([
      { start: '2026-11-16T13:00:00Z', end: '2026-11-16T14:00:00Z', kind: 'tentative' },
      { start: '2026-11-16T03:00:00-05:00', end: '2026-11-16T04:00:00-05:00', kind: 'busy' },
      { start: '2026-11-16T10:00:00Z', end: '2026-11-16T09:00:00Z', kind: 'busy' },
    ])
    expect(blocks.map((b) => [hm(b.start), b.kind])).toEqual([
      ['16 09:00', 'busy'],
      ['16 14:00', 'tentative'],
    ])
  })
})

describe('conflictsAt', () => {
  const rows = [
    row('ada@example.com', [block(at(16, 10), at(16, 11))]),
    row('grace@example.com', [block(at(16, 10, 30), at(16, 12), 'tentative')]),
    row('alan@example.com', [block(at(16, 9), at(16, 10))]),
    row('away@example.com', [block(at(16, 0), at(17, 0), 'unavailable')]),
    row('nobody@example.org', [], false),
  ]

  it('names who is busy, who is tentative and who is unknown', () => {
    expect(conflictsAt(rows, at(16, 10), at(16, 11))).toEqual({
      busy: ['ada@example.com', 'away@example.com'],
      tentative: ['grace@example.com'],
      unknown: ['nobody@example.org'],
    })
  })

  it('treats the end as open, so back to back is no conflict', () => {
    expect(conflictsAt(rows.slice(0, 3), at(16, 11), at(16, 11, 30)).busy).toEqual([])
    expect(conflictsAt(rows.slice(0, 3), at(16, 8), at(16, 9)).busy).toEqual([])
  })

  it('compares instants, not wall clocks', () => {
    const utc = row('utc@example.com', toBlocks([{ start: '2026-11-16T09:00:00Z', end: '2026-11-16T10:00:00Z', kind: 'busy' }]))
    // 10:00 in Berlin is 09:00 UTC.
    expect(conflictsAt([utc], at(16, 10), at(16, 10, 30)).busy).toEqual(['utc@example.com'])
    expect(conflictsAt([utc], at(16, 9), at(16, 10)).busy).toEqual([])
  })
})

describe('suggestTimes', () => {
  it('offers the free times everyone shares, an event length apart', () => {
    const rows = [
      row('me', [block(at(16, 9), at(16, 10))]),
      row('ada@example.com', [block(at(16, 11), at(16, 12)), block(at(16, 13), at(16, 16))]),
    ]
    const got = suggestTimes(rows, { ...day, minutes: 60, limit: 10 })
    expect(got.map((s) => `${hm(s.start)}-${s.end.toTimeString().slice(0, 5)}`)).toEqual([
      '16 10:00-11:00',
      '16 12:00-13:00',
      '16 16:00-17:00',
    ])
  })

  it('counts tentative and away time as busy and ignores unknown rows', () => {
    const rows = [
      row('ada@example.com', [block(at(16, 9), at(16, 12), 'tentative'), block(at(16, 12), at(16, 16), 'unavailable')]),
      row('nobody@example.org', [block(at(16, 16), at(16, 17))], false),
    ]
    expect(suggestTimes(rows, { ...day, minutes: 30 }).map((s) => hm(s.start))).toEqual(['16 16:00', '16 16:30'])
  })

  it('steps at least every half hour on the quarter hour and stops at the limit', () => {
    const got = suggestTimes([row('me', [block(at(16, 9), at(16, 9, 10))])], { ...day, minutes: 15, limit: 3 })
    expect(got.map((s) => hm(s.start))).toEqual(['16 09:15', '16 09:45', '16 10:15'])
    expect(got[0].end.getTime() - got[0].start.getTime()).toBe(15 * 60000)
  })

  it('skips the past and the weekend and moves on to the next day', () => {
    const got = suggestTimes([row('me', [])], { from: at(20, 0), to: at(24, 0), workStart: 9 * 60, workEnd: 17 * 60, minutes: 60, now: at(20, 16, 20), limit: 3 })
    // Friday after 16:20 has no full hour left; Saturday and Sunday are out.
    expect(got.map((s) => hm(s.start))).toEqual(['23 09:00', '23 10:00', '23 11:00'])
  })

  it('finds nothing when the event is longer than any free stretch', () => {
    const rows = [row('me', [block(at(16, 10), at(16, 11)), block(at(16, 12), at(16, 16))])]
    expect(suggestTimes(rows, { ...day, minutes: 90 })).toEqual([])
  })

  it('keeps working hours on the wall clock across a daylight saving change', () => {
    // Sunday 29 March 2026 the clocks go forward in Berlin.
    const from = new Date(2026, 2, 30, 0, 0)
    const got = suggestTimes([row('me', [])], { from, to: new Date(2026, 2, 31), workStart: 9 * 60, workEnd: 17 * 60, minutes: 60, limit: 1 })
    expect(toRFC3339(got[0].start)).toBe('2026-03-30T09:00:00+02:00')
  })
})
