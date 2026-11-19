import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Calendar, CalendarSettings, EventDraft, EventSaveResult, MailEventSource } from '../../lib/types'

const api = vi.hoisted(() => ({
  listCalendars: vi.fn(),
  listCalendarAccounts: vi.fn(),
  listOccurrences: vi.fn(),
  getCalendarSettings: vi.fn(),
  setCalendarSettings: vi.fn(),
  listAccounts: vi.fn(),
  createEvent: vi.fn(),
  updateEvent: vi.fn(),
  deleteEvent: vi.fn(),
  getEvent: vi.fn(),
  createEventDraftFromMessage: vi.fn(),
  mailEventSource: vi.fn(),
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
import { contextMenu } from '../../stores/contextmenu'
import { toasts } from '../../stores/toast'
import { runSpaceAction } from '../../stores/space'
import { toRFC3339 } from '../../lib/caldate'

// Saturday 14 November 2026, 10:07.
const now = new Date(2026, 10, 14, 10, 7)

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

const mail: MailEventSource = {
  subject: 'Meet up',
  text: "Hi,\nLet's meet up on the 3rd December 2026 at 18:00.\nAda",
  date: toRFC3339(new Date(2026, 10, 7, 9)),
  fromName: 'Ada',
  fromAddress: 'ada@example.com',
  link: 'mid:abc@example.com',
}

const prefill: EventDraft = {
  id: 0,
  calendarId: 1,
  summary: 'Meet up',
  description: "Let's meet up",
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
  attendees: [{ email: 'ada@example.com', name: 'Ada', role: 'REQ-PARTICIPANT', partStat: 'NEEDS-ACTION', rsvp: true }],
  scope: '',
  recurrenceId: '',
  force: false,
}

let nextId = 100

function saved(draft: EventDraft): EventSaveResult {
  return { event: { ...draft, id: nextId++ } as unknown as EventSaveResult['event'], queued: false, conflict: false, server: {} as EventSaveResult['server'] }
}

function lastDraft(): EventDraft {
  return api.createEvent.mock.lastCall?.[0] as EventDraft
}

async function ready(): Promise<void> {
  render(CalendarScreen)
  await waitFor(() => expect(api.listCalendars).toHaveBeenCalled())
  await waitFor(() => expect(api.listOccurrences).toHaveBeenCalled())
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(now)
  vi.clearAllMocks()
  nextId = 100
  contextMenu.set(null)
  calendarSettingsLoaded.set(false)
  calendarView.set('week')
  calendarFocus.set(new Date(2026, 10, 14))
  api.listCalendars.mockResolvedValue([home])
  api.listCalendarAccounts.mockResolvedValue([])
  api.listOccurrences.mockResolvedValue([])
  api.getCalendarSettings.mockResolvedValue(settings)
  api.listAccounts.mockResolvedValue([])
  api.createEvent.mockImplementation((draft: EventDraft) => Promise.resolve(saved(draft)))
  api.deleteEvent.mockResolvedValue({ event: {}, queued: false, conflict: false, server: {} })
  api.createEventDraftFromMessage.mockResolvedValue(prefill)
  api.mailEventSource.mockResolvedValue(mail)
})

afterEach(() => {
  vi.useRealTimers()
})

describe('event editor', () => {
  it('starts at the next half hour for an hour and saves', async () => {
    await ready()
    await userEvent.click(screen.getAllByRole('button', { name: 'New event' })[0])
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    const title = within(dialog).getByRole('textbox', { name: 'Title' })
    expect(title).toHaveFocus()
    await userEvent.type(title, 'Planning')
    await userEvent.type(within(dialog).getByRole('textbox', { name: 'Location' }), 'Room 2')
    await userEvent.click(within(dialog).getByRole('button', { name: /Remove reminder/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(lastDraft()).toMatchObject({ id: 0, calendarId: 1, summary: 'Planning', location: 'Room 2', allDay: false, rrule: '', reminders: [] })
    expect(new Date(lastDraft().start)).toEqual(new Date(2026, 10, 14, 10, 30))
    expect(new Date(lastDraft().end)).toEqual(new Date(2026, 10, 14, 11, 30))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New event' })).not.toBeInTheDocument())
  })

  it('saves with Cmd or Ctrl+Enter', async () => {
    await ready()
    await userEvent.click(screen.getAllByRole('button', { name: 'New event' })[0])
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    await userEvent.type(within(dialog).getByRole('textbox', { name: 'Title' }), 'Call')
    await userEvent.keyboard('{Control>}{Meta>}{Enter}{/Meta}{/Control}')
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(lastDraft().summary).toBe('Call')
  })

  it('asks before throwing edits away', async () => {
    await ready()
    await userEvent.click(screen.getAllByRole('button', { name: 'New event' })[0])
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    await userEvent.type(within(dialog).getByRole('textbox', { name: 'Title' }), 'Draft')
    await userEvent.keyboard('{Escape}')
    await userEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New event' })).not.toBeInTheDocument())
    expect(api.createEvent).not.toHaveBeenCalled()
  })

  it('opens from the compose shortcut while the calendar is on screen', async () => {
    await ready()
    expect(runSpaceAction('calendar', 'compose')).toBe(true)
    expect(await screen.findByRole('dialog', { name: 'New event' })).toBeInTheDocument()
  })
})

describe('quick add', () => {
  async function openQuickAdd(): Promise<HTMLElement> {
    await ready()
    await userEvent.click(screen.getByRole('button', { name: 'Quick add' }))
    return screen.findByRole('dialog', { name: 'Quick add' })
  }

  it('previews what it read and creates it on Enter', async () => {
    const bar = await openQuickAdd()
    const input = within(bar).getByRole('textbox', { name: 'Quick add' })
    await userEvent.type(input, 'lunch with Ada tomorrow 12:30 for 1h at Cafe Blau')
    expect(await within(bar).findByRole('heading', { name: 'lunch with Ada' })).toBeInTheDocument()
    expect(within(bar).getByText('Cafe Blau')).toBeInTheDocument()
    expect(within(bar).getByRole('list', { name: 'Recognized' })).toHaveTextContent('tomorrow 12:30')
    await userEvent.keyboard('{Enter}')

    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(lastDraft()).toMatchObject({ summary: 'lunch with Ada', location: 'Cafe Blau', calendarId: 1 })
    expect(new Date(lastDraft().start)).toEqual(new Date(2026, 10, 15, 12, 30))
    expect(new Date(lastDraft().end)).toEqual(new Date(2026, 10, 15, 13, 30))
    const toast = get(toasts).at(-1)
    expect(toast?.message).toBe('Event added.')
    expect([toast?.action?.label, ...(toast?.more ?? []).map((a) => a.label)]).toEqual(['Undo', 'Open'])
    toast?.action?.run()
    await waitFor(() => expect(api.deleteEvent).toHaveBeenCalledWith(100, '', '', false))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Quick add' })).not.toBeInTheDocument())
  })

  it('says when it found no date and uses the next half hour', async () => {
    const bar = await openQuickAdd()
    await userEvent.type(within(bar).getByRole('textbox', { name: 'Quick add' }), 'buy milk')
    expect(await within(bar).findByText(/No date or time found/)).toBeInTheDocument()
  })

  it('opens the full editor on Shift+Enter', async () => {
    const bar = await openQuickAdd()
    await userEvent.type(within(bar).getByRole('textbox', { name: 'Quick add' }), 'dentist fri 9am')
    await within(bar).findByRole('heading', { name: 'dentist' })
    await userEvent.keyboard('{Shift>}{Enter}{/Shift}')
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    expect(within(dialog).getByRole('textbox', { name: 'Title' })).toHaveValue('dentist')
    expect(api.createEvent).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(new Date(lastDraft().start)).toEqual(new Date(2026, 10, 20, 9))
  })
})

describe('cell menu', () => {
  it('offers a new event and quick add for a right-clicked slot', async () => {
    await ready()
    const columns = document.querySelectorAll<HTMLElement>('.col')
    await fireEvent.contextMenu(columns[2], { clientX: 300, clientY: 200 })
    const menu = get(contextMenu)
    expect(menu?.entries.map((e) => (typeof e === 'object' && 'action' in e ? [e.label, e.hint] : e))).toEqual([
      ['Create event...', undefined],
      ['Quick add here...', 'Q'],
    ])

    // the slot is the default start of a line with no time in it.
    const quick = menu?.entries[1] as { action: () => void }
    quick.action()
    const bar = await screen.findByRole('dialog', { name: 'Quick add' })
    await userEvent.type(within(bar).getByRole('textbox', { name: 'Quick add' }), 'review')
    await within(bar).findByRole('heading', { name: 'review' })
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    // the week starts on Monday 9 November; the third column is Wednesday.
    expect(new Date(lastDraft().start)).toEqual(new Date(2026, 10, 11, 0, 0))
  })

  it('creates an event at a right-clicked day', async () => {
    api.getCalendarSettings.mockResolvedValue({ ...settings, defaultView: 'month' })
    await ready()
    const cell = await waitFor(() => {
      const found = [...document.querySelectorAll<HTMLElement>('.cell')].find((c) => c.querySelector('.num')?.textContent?.trim() === '20')
      expect(found).toBeTruthy()
      return found as HTMLElement
    })
    await fireEvent.contextMenu(cell, { clientX: 10, clientY: 10 })
    ;(get(contextMenu)?.entries[0] as { action: () => void }).action()
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(new Date(lastDraft().start)).toEqual(new Date(2026, 10, 20, 10, 30))
  })
})

describe('mail dropped on the calendar', () => {
  it('takes the date and time from the mail', async () => {
    await ready()
    calendarSeed.set({ messageIds: [7], at: null, mode: 'time', shift: false })
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    expect(within(dialog).getByRole('textbox', { name: 'Title' })).toHaveValue('Meet up')
    expect(within(dialog).getByRole('group', { name: 'Found in the mail' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(new Date(lastDraft().start)).toEqual(new Date(2026, 11, 3, 18))
    expect(new Date(lastDraft().end)).toEqual(new Date(2026, 11, 3, 19))
    expect(lastDraft().attendees.map((a) => a.email)).toEqual(['ada@example.com'])
  })

  it('says so when the mail has no date', async () => {
    api.mailEventSource.mockResolvedValue({ ...mail, text: 'Thanks for the files.' })
    await ready()
    calendarSeed.set({ messageIds: [7], at: null, mode: 'time', shift: false })
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    expect(within(dialog).getByText(/No date or time found in the mail/)).toBeInTheDocument()
  })

  it('keeps a chosen slot and links the mail on a Shift drop', async () => {
    await ready()
    const at = new Date(2026, 10, 17, 15, 30)
    calendarSeed.set({ messageIds: [7], at, mode: 'time', shift: true })
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    expect(within(dialog).getByRole('button', { name: 'Open mail' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(new Date(lastDraft().start)).toEqual(at)
    expect(lastDraft().description).toContain('mid:abc@example.com')
    expect(lastDraft().description).toContain('Ada <ada@example.com>')
    expect(api.createEventDraftFromMessage).not.toHaveBeenCalled()
  })

  it('makes one event per mail when several are dropped', async () => {
    await ready()
    calendarSeed.set({ messageIds: [7, 8], at: null, mode: 'time', shift: false })
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(2))
    expect(new Date((api.createEvent.mock.calls[0][0] as EventDraft).start)).toEqual(new Date(2026, 11, 3, 18))
  })

  it('makes one event linking every mail on a Shift drop of several', async () => {
    await ready()
    calendarSeed.set({ messageIds: [7, 8], at: null, mode: 'time', shift: true })
    const dialog = await screen.findByRole('dialog', { name: 'New event' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.createEvent).toHaveBeenCalledTimes(1))
    expect(lastDraft().description.match(/mid:/g)).toHaveLength(2)
  })
})

describe('views', () => {
  it('opens the views by their buttons', async () => {
    await ready()
    await userEvent.click(screen.getByRole('radio', { name: 'Month' }))
    await waitFor(() => {
      const [from, to] = api.listOccurrences.mock.lastCall as [string, string]
      // November 2026 runs Sunday to Monday: the grid is Monday 26 October to Sunday 6 December.
      expect(new Date(from)).toEqual(new Date(2026, 9, 26))
      expect(new Date(to)).toEqual(new Date(2026, 11, 7))
    })
  })

  it('loads the whole year in one request for the year view, with the y key', async () => {
    await ready()
    screen.getByRole('button', { name: 'Today' }).focus()
    await userEvent.keyboard('y')
    await waitFor(() => {
      const [from, to] = api.listOccurrences.mock.lastCall as [string, string]
      expect(new Date(from)).toEqual(new Date(2026, 0, 1))
      expect(new Date(to)).toEqual(new Date(2027, 0, 1))
    })
    expect(await screen.findByRole('group', { name: 'Year 2026' })).toBeInTheDocument()
  })

  it('leaves out the events of a calendar toggled off', async () => {
    api.listCalendars.mockResolvedValue([home, { ...home, id: 2, name: 'Work', visible: false }])
    const entry = (id: number, calendarId: number) => ({
      eventId: id, calendarId, calendarColor: '', uid: `u${id}`, summary: `E${id}`, location: '', start: '2026-11-10T09:00:00+01:00',
      end: '2026-11-10T10:00:00+01:00', allDay: false, recurring: false, recurrenceId: '', override: false, status: '', transparency: '',
      readOnly: false, hasReminders: false, pending: false, conflict: false,
    })
    api.listOccurrences.mockResolvedValue([entry(1, 1), entry(2, 2)])
    calendarSettingsLoaded.set(true)
    calendarView.set('year')
    await ready()
    const cell = await waitFor(() => {
      const el = document.querySelector<HTMLElement>('.year [data-day="2026-11-10"]')
      expect(el?.getAttribute('aria-label')).toContain('1 event')
      return el
    })
    expect(cell?.getAttribute('aria-label')).not.toContain('2 events')
  })
})
