import { afterAll, describe, expect, it, vi } from 'vitest'
import { busySpans, formatAvailability, freeSlots, zoneLabel, type Span } from './freeslots'
import { dayKey, toRFC3339 } from './caldate'
import type { Occurrence, Task } from './types'

// the daylight saving cases need a zone that has it, whatever the machine runs in.
vi.stubEnv('TZ', 'Europe/Berlin')

afterAll(() => {
  vi.unstubAllEnvs()
})

// Monday 16 November 2026.
const at = (d: number, h: number, m = 0, month = 10): Date => new Date(2026, month, d, h, m)

function occ(start: Date, end: Date, patch: Partial<Occurrence> = {}): Occurrence {
  const allDay = patch.allDay ?? false
  return {
    eventId: 1,
    calendarId: 1,
    calendarColor: '',
    uid: 'u',
    summary: 'busy',
    location: '',
    start: allDay ? dayKey(start) : toRFC3339(start),
    end: allDay ? dayKey(end) : toRFC3339(end),
    allDay,
    recurring: false,
    recurrenceId: '',
    override: false,
    status: '',
    transparency: '',
    readOnly: false,
    hasReminders: false,
    pending: false,
    conflict: false,
    ...patch,
  }
}

const base = { minutes: 30, workStart: 9 * 60, workEnd: 17 * 60, weekends: false }

function oneDay(busy: Span[], d = 16, extra: Partial<Parameters<typeof freeSlots>[1]> = {}): [string, string][] {
  return freeSlots(busy, { ...base, from: at(d, 0), to: at(d + 1, 0), ...extra }).map((s) => [s.start.toTimeString().slice(0, 5), s.end.toTimeString().slice(0, 5)])
}

describe('freeSlots', () => {
  it('gives the whole working day when nothing is on', () => {
    expect(oneDay([])).toEqual([['09:00', '17:00']])
  })

  it('cuts the day around busy time', () => {
    const busy = busySpans([occ(at(16, 10), at(16, 11)), occ(at(16, 13), at(16, 14, 30))])
    expect(oneDay(busy)).toEqual([
      ['09:00', '10:00'],
      ['11:00', '13:00'],
      ['14:30', '17:00'],
    ])
  })

  it('merges overlapping and nested events', () => {
    const busy = busySpans([occ(at(16, 10), at(16, 12)), occ(at(16, 11), at(16, 13)), occ(at(16, 11, 30), at(16, 11, 45))])
    expect(oneDay(busy)).toEqual([
      ['09:00', '10:00'],
      ['13:00', '17:00'],
    ])
  })

  it('drops gaps shorter than the meeting and rounds starts up to the step', () => {
    const busy = busySpans([occ(at(16, 9), at(16, 10, 5)), occ(at(16, 10, 30), at(16, 16, 40))])
    expect(oneDay(busy, 16, { minutes: 30 })).toEqual([])
    expect(oneDay(busy, 16, { minutes: 15 })).toEqual([
      ['10:15', '10:30'],
      ['16:45', '17:00'],
    ])
  })

  it('leaves out events marked free, cancelled ones and tasks', () => {
    const busy = busySpans([
      occ(at(16, 10), at(16, 12), { transparency: 'TRANSPARENT' }),
      occ(at(16, 12), at(16, 13), { status: 'CANCELLED' }),
      occ(at(16, 14), at(16, 15), { task: { id: 1 } as Task }),
    ])
    expect(oneDay(busy)).toEqual([['09:00', '17:00']])
  })

  it('counts an all-day event only when it is marked busy', () => {
    const birthday = occ(at(16, 0), at(17, 0), { allDay: true })
    const leave = occ(at(17, 0), at(19, 0), { allDay: true, transparency: 'OPAQUE' })
    const slots = freeSlots(busySpans([birthday, leave]), { ...base, from: at(16, 0), to: at(20, 0) })
    expect(slots.map((s) => dayKey(s.start))).toEqual(['2026-11-16', '2026-11-19'])
  })

  it('handles an event running over midnight on both days', () => {
    const busy = busySpans([occ(at(16, 15), at(17, 10))])
    const slots = freeSlots(busy, { ...base, from: at(16, 0), to: at(18, 0) })
    expect(slots).toEqual([
      { start: at(16, 9), end: at(16, 15) },
      { start: at(17, 10), end: at(17, 17) },
    ])
  })

  it('skips weekends unless asked, and takes only the calendars asked for', () => {
    const week = { ...base, from: at(16, 0), to: at(23, 0) }
    expect(freeSlots([], week)).toHaveLength(5)
    expect(freeSlots([], { ...week, weekends: true })).toHaveLength(7)
    const busy = busySpans([occ(at(16, 9), at(16, 17), { calendarId: 2 })], new Set([1]))
    expect(busy).toEqual([])
  })

  it('drops what has already passed', () => {
    expect(oneDay([], 16, { now: at(16, 11, 7) })).toEqual([['11:15', '17:00']])
    expect(oneDay([], 16, { now: at(16, 18) })).toEqual([])
  })

  it('works to midnight when the working day ends there', () => {
    expect(oneDay([], 16, { workStart: 20 * 60, workEnd: 24 * 60 })).toEqual([['20:00', '00:00']])
  })

  it('keeps wall-clock working hours across the end of daylight saving', () => {
    // Sunday 25 October 2026 has 25 hours in Berlin.
    const slots = freeSlots([], { ...base, weekends: true, from: at(24, 0, 0, 9), to: at(27, 0, 0, 9) })
    expect(slots.map((s) => [s.start.getHours(), s.end.getHours()])).toEqual([
      [9, 17],
      [9, 17],
      [9, 17],
    ])
    expect(slots[1].end.getTime() - slots[1].start.getTime()).toBe(8 * 3600000)
  })

  it('keeps wall-clock working hours across the start of daylight saving', () => {
    // Sunday 29 March 2026 has 23 hours in Berlin; an early start lands after the gap.
    const slots = freeSlots([], { ...base, weekends: true, workStart: 60, from: new Date(2026, 2, 29), to: new Date(2026, 2, 31) })
    expect(slots.map((s) => [s.start.getHours(), s.end.getHours()])).toEqual([
      [1, 17],
      [1, 17],
    ])
    expect(slots[0].end.getTime() - slots[0].start.getTime()).toBe(15 * 3600000)
  })
})

describe('formatAvailability', () => {
  const words = { line: '{day}: {times}', range: '{from} to {to}', zone: 'Times are in {zone}.' }
  const time = (d: Date): string => d.toTimeString().slice(0, 5)

  it('writes one line per day and names the zone', () => {
    const text = formatAvailability(
      [
        { start: at(17, 14), end: at(17, 16) },
        { start: at(17, 10), end: at(17, 11, 30) },
        { start: at(19, 9), end: at(19, 12) },
      ],
      { words, locale: 'en-GB', time, timeZone: 'Europe/Berlin' },
    )
    expect(text).toBe(
      'Tue 17 Nov: 10:00 to 11:30, 14:00 to 16:00\nThu 19 Nov: 09:00 to 12:00\nTimes are in Central European Standard Time (Europe/Berlin).',
    )
  })

  it('is empty with nothing picked', () => {
    expect(formatAvailability([], { words, time })).toBe('')
  })

  it('names the zone by its long name and its id', () => {
    expect(zoneLabel(at(17, 12), 'en-GB', 'Europe/Berlin')).toBe('Central European Standard Time (Europe/Berlin)')
    expect(zoneLabel(at(17, 12), 'en-GB', 'UTC')).toContain('UTC')
  })
})
