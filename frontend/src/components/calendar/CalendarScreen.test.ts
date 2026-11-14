import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Calendar, CalendarSettings, EventDraft, EventSaveResult } from '../../lib/types'

const api = vi.hoisted(() => ({
  listCalendars: vi.fn(),
  listCalendarAccounts: vi.fn(),
  listOccurrences: vi.fn(),
  getCalendarSettings: vi.fn(),
  setCalendarSettings: vi.fn(),
  listAccounts: vi.fn(),
  createEvent: vi.fn(),
  updateEvent: vi.fn(),
  getEvent: vi.fn(),
  createEventDraftFromMessage: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

// views slide in and dialogs open with transitions, and jsdom has no web
// animations. A finished stub lets svelte run each to its end.
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

import CalendarScreen from './CalendarScreen.svelte'
import { calendarFocus, calendarSeed, calendarSettingsLoaded, calendarView } from '../../stores/calendar'
import { toRFC3339 } from '../../lib/caldate'

const home: Calendar = {
  id: 1,
  accountId: 0,
  accountName: '',
  name: 'Home',
  description: '',
  color: '',
  local: true,
  readOnly: false,
  visible: true,
  lastSync: '',
  lastError: '',
  default: true,
}

const settings: CalendarSettings = {
  syncIntervalMinutes: 15,
  remindersEnabled: true,
  defaultReminderMinutes: 10,
  defaultCalendarId: 0,
  defaultView: 'week',
  weekStart: 1,
  workDayStart: 9 * 60,
  workDayEnd: 17 * 60,
  timeZoneDisplay: 'off',
  secondaryTimeZone: '',
}

function saved(draft: EventDraft): EventSaveResult {
  return { event: { ...draft } as unknown as EventSaveResult['event'], queued: false, conflict: false, server: {} as EventSaveResult['server'] }
}

beforeEach(() => {
  vi.clearAllMocks()
  calendarSettingsLoaded.set(false)
  calendarView.set('week')
  calendarFocus.set(new Date(2031, 10, 14))
  api.listCalendars.mockResolvedValue([home])
  api.listCalendarAccounts.mockResolvedValue([])
  api.listOccurrences.mockResolvedValue([])
  api.getCalendarSettings.mockResolvedValue(settings)
  api.listAccounts.mockResolvedValue([])
  api.createEvent.mockImplementation((draft: EventDraft) => Promise.resolve(saved(draft)))
})

describe('CalendarScreen', () => {
  it('creates an event from the editor', async () => {
    render(CalendarScreen)
    await waitFor(() => expect(api.listCalendars).toHaveBeenCalled())
    await waitFor(() => expect(api.listOccurrences).toHaveBeenCalled())

    await userEvent.click(screen.getAllByRole('button', { name: 'New event' })[0])
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    await userEvent.type(within(dialog).getByRole('textbox', { name: 'Title' }), 'Planning')
    await userEvent.type(within(dialog).getByRole('textbox', { name: 'Location' }), 'Room 2')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    const draft = api.createEvent.mock.calls[0][0] as EventDraft
    expect(draft).toMatchObject({ id: 0, calendarId: 1, summary: 'Planning', location: 'Room 2', allDay: false, rrule: '' })
    expect(draft.reminders).toEqual([{ minutesBefore: 10, relatedEnd: false, absolute: '', action: '' }])
    // a day other than today starts at the beginning of the working hours.
    expect(new Date(draft.start)).toEqual(new Date(2031, 10, 14, 9))
    expect(new Date(draft.end)).toEqual(new Date(2031, 10, 14, 10))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New event' })).not.toBeInTheDocument())
  })

  it('opens the views by their buttons', async () => {
    render(CalendarScreen)
    await waitFor(() => expect(api.listOccurrences).toHaveBeenCalled())
    await userEvent.click(screen.getByRole('radio', { name: 'Month' }))
    await waitFor(() => {
      const [from, to] = api.listOccurrences.mock.lastCall as [string, string]
      // November 2031 runs Saturday to Sunday: the grid is Monday 27 October to Sunday 30 November.
      expect(new Date(from)).toEqual(new Date(2031, 9, 27))
      expect(new Date(to)).toEqual(new Date(2031, 11, 1))
    })
  })

  it('starts an event from mail dropped on a time slot', async () => {
    const at = new Date(2026, 10, 17, 15, 30)
    api.createEventDraftFromMessage.mockResolvedValue({
      id: 0,
      calendarId: 1,
      summary: 'Quarterly numbers',
      description: 'See attached.',
      location: '',
      url: '',
      status: '',
      transparency: '',
      categories: [],
      start: toRFC3339(new Date(2026, 10, 14, 11)),
      end: toRFC3339(new Date(2026, 10, 14, 12)),
      allDay: false,
      timeZone: '',
      rrule: '',
      reminders: [],
      organizer: { email: '', name: '', role: '', partStat: '', rsvp: false },
      attendees: [{ email: 'ann@example.com', name: 'Ann', role: 'REQ-PARTICIPANT', partStat: 'NEEDS-ACTION', rsvp: true }],
      scope: '',
      recurrenceId: '',
      force: false,
    } satisfies EventDraft)

    render(CalendarScreen)
    await waitFor(() => expect(api.listCalendars).toHaveBeenCalled())
    calendarSeed.set({ messageId: 7, at, mode: 'time' })

    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    expect(api.createEventDraftFromMessage).toHaveBeenCalledWith(7)
    expect(within(dialog).getByRole('textbox', { name: 'Title' })).toHaveValue('Quarterly numbers')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    const draft = api.createEvent.mock.calls[0][0] as EventDraft
    expect(new Date(draft.start)).toEqual(at)
    expect(new Date(draft.end)).toEqual(new Date(2026, 10, 17, 16, 30))
    expect(draft.attendees.map((a) => a.email)).toEqual(['ann@example.com'])
  })
})
