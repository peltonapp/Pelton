import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Calendar, MessageSummary, Occurrence, Task } from '../../lib/types'

const api = vi.hoisted(() => ({
  listOccurrences: vi.fn(),
  getEvent: vi.fn(),
  createEvent: vi.fn(),
  createTask: vi.fn(),
  setTaskDone: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const mail = vi.hoisted(() => ({ eventFromMailAt: vi.fn(), taskFromMailAt: vi.fn(), draftEventFromMail: vi.fn(), openDayInCalendar: vi.fn() }))

vi.mock('../../lib/mailcalendar', () => mail)

import MyDayPanel from './MyDayPanel.svelte'
import { calendars, calendarSettings, calendarSettingsLoaded, defaultCalendarSettings } from '../../stores/calendar'
import { taskLists, tasks, tasksLoaded } from '../../stores/tasks'
import { maildragSource } from '../../stores/maildrag'
import { prefs } from '../../stores/prefs'
import { meetingPrepTarget } from '../../stores/meetingprep'
import { toRFC3339 } from '../../lib/caldate'
import { get } from 'svelte/store'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

// Monday 16 November 2026, 10:00.
const clock = (): Date => new Date(2026, 10, 16, 10)

const work: Calendar = { id: 1, accountId: 0, accountName: '', name: 'Work', description: '', color: '', local: true, readOnly: false, visible: true, lastSync: '', lastError: '', default: true }

function occ(summary: string, start: Date, end: Date): Occurrence {
  return {
    eventId: 5,
    calendarId: 1,
    calendarColor: '',
    uid: summary,
    summary,
    location: '',
    start: toRFC3339(start),
    end: toRFC3339(end),
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
  }
}

function task(id: number, title: string, due: string): Task {
  return {
    id,
    listId: 1,
    listName: 'Tasks',
    listColor: '',
    uid: `t${id}`,
    title,
    notes: '',
    url: '',
    due,
    start: '',
    timeZone: '',
    done: false,
    completed: '',
    status: '',
    percent: 0,
    priority: 0,
    rrule: '',
    reminders: [],
    parent: '',
    categories: [],
    mailLinks: [],
    contacts: [],
    eventUids: [],
    sortOrder: 0,
    estimate: 0,
    created: '',
    readOnly: false,
    pending: false,
    conflict: false,
    syncError: '',
  }
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  for (const fn of [...Object.values(api), ...Object.values(mail)]) {
    fn.mockReset()
  }
  api.listOccurrences.mockImplementation(async (from: string) =>
    from.startsWith('2026-11-16') ? [occ('Standup', new Date(2026, 10, 16, 9, 30), new Date(2026, 10, 16, 10))] : [occ('Dentist', new Date(2026, 10, 17, 8), new Date(2026, 10, 17, 9))],
  )
  calendars.set([work])
  calendarSettings.set({ ...defaultCalendarSettings, defaultReminderMinutes: -1 })
  calendarSettingsLoaded.set(true)
  taskLists.set([{ id: 1, name: 'Tasks', color: '', local: true, readOnly: false, default: true } as never])
  tasks.set([task(1, 'Send the report', '2026-11-16'), task(2, 'Pay rent', '2026-11-12'), task(3, 'Book trip', '2026-11-17')])
  tasksLoaded.set(true)
  prefs.update((p) => ({ ...p, reduceMotion: true, timeFormat: '24' }))
})

describe('MyDayPanel', () => {
  it("shows today's agenda with the overdue and today's tasks", async () => {
    render(MyDayPanel, { props: { clock } })
    expect(await screen.findByText('Standup')).toBeInTheDocument()
    const list = within(screen.getByRole('region', { name: 'Tasks' }))
    expect(list.getAllByRole('checkbox').map((c) => c.getAttribute('aria-label'))).toEqual(['Mark as done: Pay rent', 'Mark as done: Send the report'])
    expect(list.queryByText('Book trip')).toBeNull()
    expect(api.listOccurrences).toHaveBeenCalledWith(toRFC3339(new Date(2026, 10, 16)), toRFC3339(new Date(2026, 10, 17)))
  })

  it('offers to prepare for the next meeting with others invited', async () => {
    const user = userEvent.setup()
    api.listOccurrences.mockImplementation(async (from: string) =>
      from.startsWith('2026-11-16')
        ? [
            { ...occ('Standup', new Date(2026, 10, 16, 9, 30), new Date(2026, 10, 16, 10)), hasAttendees: true },
            { ...occ('Focus time', new Date(2026, 10, 16, 10, 30), new Date(2026, 10, 16, 11)), eventId: 6 },
            { ...occ('Roadmap review', new Date(2026, 10, 16, 14), new Date(2026, 10, 16, 15)), eventId: 7, recurrenceId: '2026-11-16T13:00:00Z', hasAttendees: true },
          ]
        : [],
    )
    render(MyDayPanel, { props: { clock } })
    // standup is over at 10:00, so the next meeting is the one at 14:00.
    const next = await screen.findByRole('group', { name: 'Next meeting' })
    expect(next).toHaveTextContent('Roadmap review')
    expect(next).toHaveTextContent('14:00')
    await user.click(within(next).getByRole('button', { name: 'Prepare' }))
    expect(get(meetingPrepTarget)).toEqual({ eventId: 7, recurrenceId: '2026-11-16T13:00:00Z' })
  })

  it('shows no prepare strip when nobody else is invited', async () => {
    render(MyDayPanel, { props: { clock } })
    await screen.findByText('Standup')
    expect(screen.queryByRole('group', { name: 'Next meeting' })).toBeNull()
  })

  it('switches to tomorrow', async () => {
    const user = userEvent.setup()
    render(MyDayPanel, { props: { clock } })
    await screen.findByText('Standup')
    await user.click(screen.getByRole('radio', { name: 'Tomorrow' }))
    expect(await screen.findByText('Dentist')).toBeInTheDocument()
    const list = within(screen.getByRole('region', { name: 'Tasks' }))
    expect(list.getByText('Book trip')).toBeInTheDocument()
    expect(list.queryByText('Pay rent')).toBeNull()
  })

  it('ticks a task off', async () => {
    const user = userEvent.setup()
    api.setTaskDone.mockImplementation(async (id: number) => ({ task: { ...task(id, 'Pay rent', '2026-11-12'), done: true }, queued: false, conflict: false, advanced: false }))
    render(MyDayPanel, { props: { clock } })
    await user.click(await screen.findByRole('checkbox', { name: 'Mark as done: Pay rent' }))
    expect(api.setTaskDone).toHaveBeenCalledWith(2, true)
  })

  it('adds a task due on the day shown and an event from a line of text', async () => {
    const user = userEvent.setup()
    api.createTask.mockImplementation(async (draft) => ({ task: { ...task(9, draft.title, draft.due) }, queued: false, conflict: false }))
    api.createEvent.mockResolvedValue({ event: { id: 77 }, queued: false, conflict: false })
    render(MyDayPanel, { props: { clock } })
    await screen.findByText('Standup')

    await user.type(screen.getByRole('textbox', { name: 'Add a task' }), 'Call the bank')
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Add a task' })).toHaveValue('Call the bank'))
    await new Promise((r) => setTimeout(r, 50))
    await user.keyboard('{Enter}')
    await waitFor(() => expect(api.createTask).toHaveBeenCalled())
    expect(api.createTask.mock.calls[0][0]).toMatchObject({ title: 'Call the bank', due: '2026-11-16' })

    await user.type(screen.getByRole('textbox', { name: 'Add an event, such as lunch 12:30' }), 'Lunch with Ada 12:30')
    await waitFor(() => expect(screen.getByText(/12:30/, { selector: '.preview' })).toBeInTheDocument())
    await user.keyboard('{Enter}')
    await waitFor(() => expect(api.createEvent).toHaveBeenCalled())
    expect(api.createEvent.mock.calls[0][0]).toMatchObject({ summary: 'Lunch with Ada', start: toRFC3339(new Date(2026, 10, 16, 12, 30)), end: toRFC3339(new Date(2026, 10, 16, 13, 30)) })
  })

  it('turns mail dropped on the tasks into tasks due that day, and on the agenda into an event', async () => {
    render(MyDayPanel, { props: { clock } })
    await screen.findByText('Standup')
    const message = { id: 31, subject: 'Quarterly numbers' } as MessageSummary
    const source = document.createElement('div')
    document.body.appendChild(source)
    const drag = maildragSource(source, () => [message])

    const drop = (target: Element): void => {
      const original = document.elementFromPoint
      document.elementFromPoint = () => target
      source.dispatchEvent(new PointerEvent('pointerdown', { button: 0, clientX: 0, clientY: 0, bubbles: true }))
      window.dispatchEvent(new PointerEvent('pointermove', { clientX: 40, clientY: 40 }))
      window.dispatchEvent(new PointerEvent('pointermove', { clientX: 41, clientY: 41 }))
      window.dispatchEvent(new PointerEvent('pointerup', { clientX: 41, clientY: 41 }))
      document.elementFromPoint = original
    }

    drop(screen.getByRole('region', { name: 'Tasks' }))
    expect(mail.taskFromMailAt).toHaveBeenCalledWith([31], { start: new Date(2026, 10, 16), end: new Date(2026, 10, 17), allDay: true })

    mail.eventFromMailAt.mockResolvedValue([])
    drop(document.querySelector('.myday .col') as Element)
    await waitFor(() => expect(mail.eventFromMailAt).toHaveBeenCalled())
    const [ids, at] = mail.eventFromMailAt.mock.calls[0]
    expect(ids).toEqual([31])
    expect(at.allDay).toBe(false)
    expect(at.end.getTime() - at.start.getTime()).toBe(3600000)
    drag.destroy()
    source.remove()
  })
})
