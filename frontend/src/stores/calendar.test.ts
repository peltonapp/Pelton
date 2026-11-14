import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { EventDraft, EventSaveResult, Occurrence } from '../lib/types'

const listOccurrences = vi.fn()
const createEvent = vi.fn()
const updateEvent = vi.fn()
const deleteEvent = vi.fn()
const resolveEventConflict = vi.fn()
vi.mock('../lib/api', () => ({
  listCalendars: vi.fn(async () => []),
  listCalendarAccounts: vi.fn(async () => []),
  listOccurrences: (...args: unknown[]) => listOccurrences(...args),
  syncCalendars: vi.fn(async () => {}),
  createEvent: (d: EventDraft) => createEvent(d),
  updateEvent: (d: EventDraft) => updateEvent(d),
  deleteEvent: (...args: unknown[]) => deleteEvent(...args),
  resolveEventConflict: (...args: unknown[]) => resolveEventConflict(...args),
  setCalendarVisible: vi.fn(async () => {}),
  setCalendarColor: vi.fn(async () => {}),
}))

let changed: (() => void) | null = null
vi.mock('../lib/events', () => ({
  onCalendarChanged: (cb: () => void) => {
    changed = cb
    return () => {
      changed = null
    }
  },
  onCalendarSync: () => () => {},
}))

const store = await import('./calendar')

function occ(eventId: number): Occurrence {
  return { eventId, summary: `e${eventId}` } as Occurrence
}

function result(over: Partial<EventSaveResult> = {}): EventSaveResult {
  return { event: { id: 7 }, queued: false, conflict: false, server: {}, ...over } as EventSaveResult
}

beforeEach(() => {
  for (const fn of [listOccurrences, createEvent, updateEvent, deleteEvent, resolveEventConflict]) {
    fn.mockReset()
  }
  listOccurrences.mockResolvedValue([])
  store.occurrences.set([])
  store.eventConflict.set(null)
  store.calendarRange.set({ from: '', to: '' })
})

describe('showRange', () => {
  it('loads the occurrences of the window', async () => {
    listOccurrences.mockResolvedValue([occ(1), occ(2)])
    await store.showRange('2026-03-01T00:00:00Z', '2026-04-01T00:00:00Z')
    expect(listOccurrences).toHaveBeenCalledWith('2026-03-01T00:00:00Z', '2026-04-01T00:00:00Z')
    expect(get(store.occurrences).map((o) => o.eventId)).toEqual([1, 2])
  })

  it('does nothing before a range is set', async () => {
    await store.loadOccurrences()
    expect(listOccurrences).not.toHaveBeenCalled()
  })
})

describe('saveEvent', () => {
  it('creates for id 0 and updates otherwise', async () => {
    createEvent.mockResolvedValue(result())
    updateEvent.mockResolvedValue(result())
    await store.saveEvent({ id: 0 } as EventDraft)
    await store.saveEvent({ id: 3 } as EventDraft)
    expect(createEvent).toHaveBeenCalledTimes(1)
    expect(updateEvent).toHaveBeenCalledTimes(1)
  })

  // a refused write keeps both versions for the user to choose between.
  it('opens the conflict instead of returning', async () => {
    updateEvent.mockResolvedValue(result({ conflict: true }))
    expect(await store.saveEvent({ id: 3 } as EventDraft)).toBeNull()
    expect(get(store.eventConflict)?.conflict).toBe(true)

    resolveEventConflict.mockResolvedValue({})
    await store.resolveConflict(true)
    expect(resolveEventConflict).toHaveBeenCalledWith(7, true)
    expect(get(store.eventConflict)).toBeNull()
  })
})

describe('removeEvent', () => {
  it('passes the scope and occurrence through', async () => {
    deleteEvent.mockResolvedValue(result())
    expect(await store.removeEvent(4, 'this', '2026-03-02T08:00:00Z')).toBe(true)
    expect(deleteEvent).toHaveBeenCalledWith(4, 'this', '2026-03-02T08:00:00Z', false)
  })
})

describe('watchCalendar', () => {
  it('reloads the window when the backend says something changed', async () => {
    store.calendarRange.set({ from: 'a', to: 'b' })
    const off = store.watchCalendar()
    changed?.()
    await Promise.resolve()
    expect(listOccurrences).toHaveBeenCalledWith('a', 'b')
    off()
    expect(changed).toBeNull()
  })
})
