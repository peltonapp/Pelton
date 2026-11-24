import { describe, expect, it } from 'vitest'
import { eventDraftFromTask, taskDraftFromEvent } from './taskevent'
import type { CalendarEvent, Task } from './types'

const task = {
  id: 4,
  listId: 2,
  title: 'Review',
  notes: 'see the draft',
  url: '',
  due: '2026-11-20',
  rrule: '',
  categories: ['work'],
  mailLinks: ['mid:x@example.com'],
} as unknown as Task

describe('eventDraftFromTask', () => {
  it('makes an all-day event from a date and keeps the mail link in the notes', () => {
    const draft = eventDraftFromTask(task, 1, -1)
    expect(draft).toMatchObject({ calendarId: 1, summary: 'Review', allDay: true, start: '2026-11-20', end: '2026-11-21', reminders: [] })
    expect(draft.description).toBe('see the draft\nmid:x@example.com')
  })

  it('makes an hour from a due time', () => {
    const at = new Date(2026, 10, 20, 15, 0)
    const draft = eventDraftFromTask({ ...task, due: at.toISOString() }, 1, 10)
    expect(draft.allDay).toBe(false)
    expect(new Date(draft.start)).toEqual(at)
    expect(new Date(draft.end)).toEqual(new Date(2026, 10, 20, 16, 0))
    expect(draft.reminders).toHaveLength(1)
  })
})

describe('taskDraftFromEvent', () => {
  it('takes the start as the due date and the mail links from the notes', () => {
    const ev = {
      summary: 'Dentist',
      description: 'From Ada\nmid:abc@example.com',
      url: '',
      start: '2026-11-21',
      allDay: true,
      timeZone: '',
      rrule: 'FREQ=YEARLY',
      categories: [],
    } as unknown as CalendarEvent
    expect(taskDraftFromEvent(ev, 9)).toMatchObject({ listId: 9, title: 'Dentist', due: '2026-11-21', rrule: 'FREQ=YEARLY', mailLinks: ['mid:abc@example.com'] })
  })
})
