import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Attendee, Calendar, EventDraft, FreeBusy, FreeBusyQuery } from '../../lib/types'

const api = vi.hoisted(() => ({ lookupFreeBusy: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

// dialogs open with transitions, and jsdom has no web animations. A finished
// stub lets svelte run each to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

import SchedulingAssistant from './SchedulingAssistant.svelte'
import EventEditor from './EventEditor.svelte'
import { calendarSettings, calendarSettingsLoaded, defaultCalendarSettings } from '../../stores/calendar'
import { toRFC3339 } from '../../lib/caldate'

// Monday 16 November 2026.
const at = (d: number, h: number, m = 0): Date => new Date(2026, 10, d, h, m)

const people: Attendee[] = [
  { email: 'ada@example.com', name: 'Ada Lovelace', role: 'REQ-PARTICIPANT', partStat: 'NEEDS-ACTION', rsvp: true },
  { email: 'grace@example.com', name: '', role: 'REQ-PARTICIPANT', partStat: 'NEEDS-ACTION', rsvp: true },
  { email: 'nobody@example.org', name: '', role: 'REQ-PARTICIPANT', partStat: 'NEEDS-ACTION', rsvp: true },
]

const busy = (from: Date, to: Date, kind: 'busy' | 'tentative' | 'unavailable' = 'busy') => ({ start: toRFC3339(from), end: toRFC3339(to), kind })

const answer: FreeBusy = {
  mine: [busy(at(16, 9), at(16, 9, 30))],
  attendees: [
    { email: 'ada@example.com', known: true, reason: '', status: '2.0;Success', busy: [busy(at(16, 10, 30), at(16, 11, 30))] },
    { email: 'grace@example.com', known: true, reason: '', status: '2.0;Success', busy: [busy(at(16, 13), at(16, 14), 'tentative')] },
    { email: 'nobody@example.org', known: false, reason: 'unanswered', status: '3.7;Invalid calendar user', busy: [] },
  ],
}

type Call = Promise<FreeBusy> & { cancel: ReturnType<typeof vi.fn> }

function reply(result: Promise<FreeBusy>): Call {
  return Object.assign(result, { cancel: vi.fn() })
}

function mount(props: Record<string, unknown> = {}, events: Record<string, (e: CustomEvent) => void> = {}) {
  return render(SchedulingAssistant, {
    props: {
      start: at(16, 10),
      end: at(16, 11),
      attendees: people,
      calendarId: 7,
      organizer: 'me@example.com',
      eventId: 3,
      timeFormat: '24' as const,
      now: () => at(16, 8),
      ...props,
    },
    events,
  })
}

// jsdom lays nothing out. The timeline's tracks are given a box 1200 wide,
// which for the 07:00 to 19:00 stretch the event's day shows is 100 per hour.
let restore: () => void = () => {}
beforeEach(() => {
  // early on the event's day, so its working hours are still ahead.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(at(16, 6))
  api.lookupFreeBusy.mockReset()
  api.lookupFreeBusy.mockImplementation(() => reply(Promise.resolve(answer)))
  calendarSettingsLoaded.set(false)
  calendarSettings.set(defaultCalendarSettings)
  const original = Element.prototype.getBoundingClientRect
  Element.prototype.getBoundingClientRect = function (this: Element): DOMRect {
    if (this.classList.contains('tracks')) {
      return { x: 0, y: 0, left: 0, right: 1200, top: 0, bottom: 200, width: 1200, height: 200, toJSON: () => ({}) } as DOMRect
    }
    return original.call(this)
  }
  restore = () => {
    Element.prototype.getBoundingClientRect = original
  }
})

afterEach(() => {
  restore()
  vi.useRealTimers()
})

const x = (h: number, m = 0): number => (h - 7) * 100 + (m / 60) * 100

describe('SchedulingAssistant', () => {
  it('asks once for the event day and the week after, in absolute time', async () => {
    mount()
    await screen.findByText('Busy at this time: Ada Lovelace')
    expect(api.lookupFreeBusy).toHaveBeenCalledTimes(1)
    const query = api.lookupFreeBusy.mock.calls[0][0] as FreeBusyQuery
    expect(query).toEqual({
      calendarId: 7,
      from: toRFC3339(at(16, 0)),
      to: toRFC3339(at(23, 0)),
      organizer: 'me@example.com',
      attendees: ['ada@example.com', 'grace@example.com', 'nobody@example.org'],
      excludeEventId: 3,
    })
  })

  it('draws a labelled row per person, and no information quietly', async () => {
    mount()
    expect(await screen.findByRole('img', { name: 'You: busy 9:00 - 9:30' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Ada Lovelace: busy 10:30 - 11:30' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'grace@example.com: tentative 13:00 - 14:00' })).toBeInTheDocument()
    const nobody = screen.getByRole('img', { name: 'nobody@example.org: No information' })
    expect(within(nobody).getByText('No information')).toBeInTheDocument()
    expect(nobody.querySelector('.block')).toBeNull()
    expect(document.querySelectorAll('.block.tentative')).toHaveLength(1)
  })

  it('names who the proposed time runs into', async () => {
    mount()
    expect(await screen.findByText('Busy at this time: Ada Lovelace')).toBeInTheDocument()
    expect(screen.getByText('No information for nobody@example.org')).toBeInTheDocument()
    expect(screen.queryByText(/Tentative at this time/)).toBeNull()
  })

  it('says everyone known is free when nobody clashes', async () => {
    mount({ start: at(16, 12), end: at(16, 13) })
    expect(await screen.findByText('Everyone with known availability is free.')).toBeInTheDocument()
  })

  it('offers the next shared free times and picks one', async () => {
    const change = vi.fn()
    mount({}, { change })
    const group = await screen.findByRole('group', { name: 'Suggested times' })
    const chips = within(group).getAllByRole('button').map((b) => b.textContent?.trim())
    expect(chips).toEqual(['8:00 - 9:00', '9:30 - 10:30', '11:30 - 12:30', '14:00 - 15:00', '15:00 - 16:00', '16:00 - 17:00'])
    await userEvent.click(within(group).getByRole('button', { name: '11:30 - 12:30' }))
    expect(change.mock.calls[0][0].detail).toEqual({ start: at(16, 11, 30), end: at(16, 12, 30) })
  })

  it('uses the working hours from the calendar settings', async () => {
    calendarSettings.set({ ...defaultCalendarSettings, workDayStart: 10 * 60, workDayEnd: 16 * 60 })
    calendarSettingsLoaded.set(true)
    mount()
    const group = await screen.findByRole('group', { name: 'Suggested times' })
    expect(within(group).getAllByRole('button')[0].textContent?.trim()).toBe('11:30 - 12:30')
  })

  it('moves the band with the arrow keys, and its end with shift', async () => {
    const change = vi.fn()
    mount({}, { change })
    const band = screen.getByRole('slider', { name: 'Proposed time' })
    expect(band).toHaveAttribute('aria-valuetext', '10:00 - 11:00')
    band.focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(change.mock.calls[0][0].detail).toEqual({ start: at(16, 10, 15), end: at(16, 11, 15) })
    await userEvent.keyboard('{Shift>}{ArrowLeft}{/Shift}')
    expect(change.mock.calls[1][0].detail).toEqual({ start: at(16, 10), end: at(16, 10, 45) })
  })

  it('drags the band and its edges on the quarter hour', async () => {
    const change = vi.fn()
    mount({}, { change })
    const band = screen.getByRole('slider', { name: 'Proposed time' })
    await fireEvent.pointerDown(band, { button: 0, clientX: x(10, 20) })
    await fireEvent.pointerMove(window, { clientX: x(11, 27) })
    await fireEvent.pointerUp(window)
    expect(change.mock.calls[0][0].detail).toEqual({ start: at(16, 11, 15), end: at(16, 12, 15) })

    await fireEvent.pointerDown(band.querySelector('.grip.end') as HTMLElement, { button: 0, clientX: x(11) })
    await fireEvent.pointerMove(window, { clientX: x(12, 35) })
    await fireEvent.pointerUp(window)
    expect(change.mock.calls[1][0].detail).toEqual({ start: at(16, 10), end: at(16, 12, 30) })
  })

  it('steps to the next day and asks again, cancelling the old lookup', async () => {
    const change = vi.fn()
    const first = reply(new Promise<FreeBusy>(() => {}))
    api.lookupFreeBusy.mockImplementationOnce(() => first)
    const { rerender } = mount({}, { change })
    expect(screen.getByRole('progressbar', { name: 'Checking availability' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Next day' }))
    expect(change.mock.calls[0][0].detail).toEqual({ start: at(17, 10), end: at(17, 11) })
    await rerender({ start: at(17, 10), end: at(17, 11) })
    expect(first.cancel).toHaveBeenCalled()
    expect((api.lookupFreeBusy.mock.calls[1][0] as FreeBusyQuery).from).toBe(toRFC3339(at(17, 0)))
    await waitFor(() => expect(screen.queryByRole('progressbar')).toBeNull())
  })

  it('cancels the lookup when it closes', () => {
    const call = reply(new Promise<FreeBusy>(() => {}))
    api.lookupFreeBusy.mockImplementationOnce(() => call)
    const { unmount } = mount()
    unmount()
    expect(call.cancel).toHaveBeenCalled()
  })

  it('shows a failed lookup inline and retries it', async () => {
    api.lookupFreeBusy.mockImplementationOnce(() => reply(Promise.reject(new Error('offline'))))
    mount()
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not check availability.')
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Busy at this time: Ada Lovelace')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(api.lookupFreeBusy).toHaveBeenCalledTimes(2)
  })
})

const calendar: Calendar = {
  id: 7,
  accountId: 2,
  accountName: 'Server',
  name: 'Work',
  description: '',
  color: '',
  local: false,
  readOnly: false,
  visible: true,
  lastSync: '',
  lastError: '',
  default: true,
}

function draft(attendees: Attendee[]): EventDraft {
  return {
    id: 0,
    calendarId: 7,
    summary: 'Planning',
    description: '',
    location: '',
    url: '',
    status: '',
    transparency: '',
    categories: [],
    start: toRFC3339(at(16, 10)),
    end: toRFC3339(at(16, 11)),
    allDay: false,
    timeZone: '',
    rrule: '',
    reminders: [],
    organizer: { email: '', name: '', role: '', partStat: '', rsvp: false },
    attendees,
    scope: '',
    recurrenceId: '',
    force: false,
  }
}

describe('EventEditor scheduling view', () => {
  it('only offers the scheduling view once someone is invited', () => {
    render(EventEditor, { props: { draft: draft([]), calendars: [calendar] } })
    expect(screen.queryByRole('radiogroup', { name: 'Editor view' })).toBeNull()
  })

  it('moves the event to a suggested time and saves it', async () => {
    const save = vi.fn()
    render(EventEditor, {
      props: { draft: draft(people.slice(0, 1)), calendars: [calendar], identity: { name: 'Me', email: 'me@example.com' }, timeFormat: '24' as const },
      events: { save },
    })
    await userEvent.click(screen.getByRole('radio', { name: 'Scheduling' }))
    expect(screen.getByRole('slider', { name: 'Proposed time' })).toBeInTheDocument()
    expect((api.lookupFreeBusy.mock.calls[0][0] as FreeBusyQuery).organizer).toBe('me@example.com')
    const group = await screen.findByRole('group', { name: 'Suggested times' })
    await userEvent.click(within(group).getAllByRole('button')[0])
    await waitFor(() => expect(screen.getByRole('slider', { name: 'Proposed time' })).toHaveAttribute('aria-valuetext', '8:00 - 9:00'))
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    const saved = save.mock.calls[0][0].detail as EventDraft
    expect([saved.start, saved.end]).toEqual([toRFC3339(at(16, 8)), toRFC3339(at(16, 9))])
  })
})
