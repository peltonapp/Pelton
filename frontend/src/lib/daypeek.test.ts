import { describe, expect, it } from 'vitest'
import { placeOccurrence, toRFC3339, dayKey } from './caldate'
import { isBusy, peekDay } from './daypeek'
import type { Occurrence, Task } from './types'

const day = (h: number, m = 0, d = 18): Date => new Date(2026, 10, d, h, m)

function occ(summary: string, start: Date, end: Date, patch: Partial<Occurrence> = {}): Occurrence {
  const allDay = patch.allDay ?? false
  return {
    eventId: Math.floor(Math.random() * 1e6),
    calendarId: 1,
    calendarColor: '',
    uid: summary,
    summary,
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

const placed = (list: Occurrence[]) => list.map(placeOccurrence)

describe('peekDay', () => {
  const slot = { start: day(14), end: day(15, 30), allDay: false }

  it('finds the slot free and names what comes before and after it', () => {
    const p = peekDay(placed([occ('Standup', day(9), day(9, 15)), occ('Lunch', day(12), day(13)), occ('Review', day(16), day(17))]), slot)
    expect(p.clashes).toEqual([])
    expect(p.before?.occ.summary).toBe('Lunch')
    expect(p.after?.occ.summary).toBe('Review')
    expect(p.from).toBe(12 * 60)
    expect(p.to).toBe(18 * 60)
  })

  it('reports what the slot clashes with', () => {
    const p = peekDay(placed([occ('Call', day(15), day(16)), occ('Early', day(13), day(14))]), slot)
    expect(p.clashes.map((c) => c.occ.summary)).toEqual(['Call'])
    expect(p.before?.occ.summary).toBe('Early')
  })

  it('does not count events marked free, cancelled ones or tasks', () => {
    const task = { id: 1, title: 'Send', due: toRFC3339(day(14, 30)) } as Task
    const p = peekDay(
      placed([
        occ('Focus', day(14), day(15), { transparency: 'TRANSPARENT' }),
        occ('Gone', day(14), day(15), { status: 'CANCELLED' }),
        occ('Send', day(14, 30), day(15), { task }),
      ]),
      slot,
    )
    expect(p.clashes).toEqual([])
    expect(p.timed).toHaveLength(3)
  })

  it('treats touching ends as free', () => {
    const p = peekDay(placed([occ('Before', day(13), day(14)), occ('After', day(15, 30), day(16))]), slot)
    expect(p.clashes).toEqual([])
  })

  it('splits all-day and long events from timed ones and keeps other days out', () => {
    const p = peekDay(
      placed([
        occ('Holiday', new Date(2026, 10, 18), new Date(2026, 10, 19), { allDay: true }),
        occ('Conference', day(8, 0, 17), day(8, 0, 20)),
        occ('Yesterday', day(14, 0, 17), day(15, 0, 17)),
        occ('Tomorrow', day(14, 0, 19), day(15, 0, 19)),
      ]),
      slot,
    )
    expect(p.allDay.map((x) => x.occ.summary)).toEqual(['Conference', 'Holiday'])
    expect(p.timed).toEqual([])
  })

  it('shows the working hours for a date without a time and finds no clash', () => {
    const p = peekDay(placed([occ('Call', day(10), day(11))]), { start: new Date(2026, 10, 18), end: new Date(2026, 10, 19), allDay: true }, 8 * 60, 18 * 60)
    expect(p).toMatchObject({ from: 8 * 60, to: 18 * 60, clashes: [], before: null, after: null })
    expect(p.timed).toHaveLength(1)
  })

  it('keeps the slice inside the day', () => {
    expect(peekDay([], { start: day(0, 30), end: day(1), allDay: false })).toMatchObject({ from: 0, to: 3 * 60 })
    expect(peekDay([], { start: day(23), end: day(0, 0, 19), allDay: false })).toMatchObject({ from: 21 * 60, to: 24 * 60 })
  })
})

describe('isBusy', () => {
  it('is false for free, cancelled and task entries', () => {
    expect(isBusy({ transparency: '', status: '' })).toBe(true)
    expect(isBusy({ transparency: 'TRANSPARENT', status: '' })).toBe(false)
    expect(isBusy({ transparency: '', status: 'CANCELLED' })).toBe(false)
    expect(isBusy({ transparency: '', status: '', task: {} as Task })).toBe(false)
  })
})
