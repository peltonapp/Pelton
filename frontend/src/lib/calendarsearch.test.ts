import { describe, expect, it } from 'vitest'
import { searchOccurrences } from './calendarsearch'
import type { Occurrence } from './types'

function occ(eventId: number, summary: string, start: string, location = ''): Occurrence {
  return {
    eventId,
    calendarId: 1,
    calendarColor: '',
    uid: String(eventId),
    summary,
    location,
    start,
    end: start,
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
  } as Occurrence
}

const now = new Date('2026-11-15T12:00:00Z')

describe('searchOccurrences', () => {
  it('matches title and place, every word', () => {
    const list = [
      occ(1, 'Dentist', '2026-11-20T09:00:00Z', 'Main Street'),
      occ(2, 'Team lunch', '2026-11-21T12:00:00Z'),
    ]
    expect(searchOccurrences(list, 'main dent', now).map((o) => o.eventId)).toEqual([1])
    expect(searchOccurrences(list, 'lunch', now).map((o) => o.eventId)).toEqual([2])
    expect(searchOccurrences(list, 'nothing', now)).toEqual([])
  })

  it('puts upcoming first, soonest on top, then past, latest on top', () => {
    const list = [
      occ(1, 'Sync old', '2026-10-01T09:00:00Z'),
      occ(2, 'Sync later', '2026-12-10T09:00:00Z'),
      occ(3, 'Sync soon', '2026-11-16T09:00:00Z'),
      occ(4, 'Sync recent', '2026-11-10T09:00:00Z'),
    ]
    expect(searchOccurrences(list, 'sync', now).map((o) => o.eventId)).toEqual([3, 2, 4, 1])
  })

  it('shows a repeating event once, as its next occurrence', () => {
    const list = [
      occ(7, 'Standup', '2026-11-13T09:00:00Z'),
      occ(7, 'Standup', '2026-11-16T09:00:00Z'),
      occ(7, 'Standup', '2026-11-17T09:00:00Z'),
    ]
    const found = searchOccurrences(list, 'standup', now)
    expect(found).toHaveLength(1)
    expect(found[0].start).toBe('2026-11-16T09:00:00Z')
  })
})
