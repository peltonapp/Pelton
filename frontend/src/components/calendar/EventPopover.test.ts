import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { CalendarEvent, Occurrence } from '../../lib/types'

const api = vi.hoisted(() => ({ getEvent: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import EventPopover from './EventPopover.svelte'
import { meetingPrepTarget } from '../../stores/meetingprep'

const occ: Occurrence = {
  eventId: 7,
  calendarId: 1,
  calendarColor: '',
  uid: 'u7',
  summary: 'Roadmap review',
  location: '',
  start: '2026-11-17T14:00:00+01:00',
  end: '2026-11-17T15:00:00+01:00',
  allDay: false,
  recurring: true,
  recurrenceId: '2026-11-17T13:00:00Z',
  override: false,
  status: '',
  transparency: '',
  readOnly: true,
  hasReminders: false,
  hasAttendees: true,
  pending: false,
  conflict: false,
}

function eventWith(attendees: CalendarEvent['attendees']): CalendarEvent {
  return {
    id: 7,
    summary: 'Roadmap review',
    description: '',
    location: '',
    url: '',
    start: occ.start,
    end: occ.end,
    allDay: false,
    rrule: '',
    reminders: [],
    organizer: { email: 'me@x.example', name: '', role: '', partStat: '', rsvp: false },
    attendees,
    categories: [],
    extra: [],
    syncError: '',
  } as unknown as CalendarEvent
}

function mount() {
  const anchor = document.createElement('button')
  document.body.appendChild(anchor)
  const events = { close: vi.fn() }
  render(EventPopover, { props: { occ, anchor, calendarName: 'Work', color: 'var(--cal-1)', timeFormat: '24' as const }, events })
  return events
}

beforeEach(() => {
  api.getEvent.mockReset()
  meetingPrepTarget.set(null)
})

describe('EventPopover prepare', () => {
  it('opens the prepare panel for the occurrence, even on a read-only calendar', async () => {
    api.getEvent.mockResolvedValue(eventWith([{ email: 'ada@x.example', name: 'Ada', role: '', partStat: 'ACCEPTED', rsvp: false }]))
    const events = mount()
    await userEvent.click(await screen.findByRole('button', { name: 'Prepare' }))
    expect(get(meetingPrepTarget)).toEqual({ eventId: 7, recurrenceId: '2026-11-17T13:00:00Z' })
    expect(events.close).toHaveBeenCalled()
  })

  it('has no prepare button when nobody is invited', async () => {
    api.getEvent.mockResolvedValue(eventWith([]))
    mount()
    await screen.findByText('Work')
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByRole('button', { name: 'Prepare' })).toBeNull()
  })
})
