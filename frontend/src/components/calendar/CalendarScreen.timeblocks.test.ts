import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Calendar, CalendarSettings, Task, TimeBlock, TimeBlockRequest } from '../../lib/types'

const api = vi.hoisted(() => ({
  listCalendars: vi.fn(),
  listCalendarAccounts: vi.fn(),
  listOccurrences: vi.fn(),
  getCalendarSettings: vi.fn(),
  listAccounts: vi.fn(),
  createEvent: vi.fn(),
  listTimeBlocks: vi.fn(),
  createTimeBlocks: vi.fn(),
  deleteTimeBlocks: vi.fn(),
  updateTimeBlock: vi.fn(),
  planDay: vi.fn(),
  blockTimeForMessages: vi.fn(),
  findFreeSlots: vi.fn(),
  getTimeBlockSettings: vi.fn(),
  listTaskPlans: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

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
import { tasks } from '../../stores/tasks'
import { toasts } from '../../stores/toast'
import { proposals, timeBlocks } from './timeblocks'
import { toRFC3339 } from '../../lib/caldate'

// Saturday 14 November 2026, 10:07; the week runs Monday 9 to Sunday 15.
const now = new Date(2026, 10, 14, 10, 7)

const home = { id: 1, accountId: 0, accountName: '', name: 'Home', description: '', color: '', local: true, readOnly: false, visible: true, lastSync: '', lastError: '', default: true } as Calendar

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

const report: Task = {
  id: 3,
  listId: 2,
  listName: 'Tasks',
  listColor: '',
  uid: 't3',
  title: 'Report',
  notes: '',
  url: '',
  due: '2026-11-14',
  start: '',
  timeZone: '',
  done: false,
  completed: '',
  status: '',
  percent: 0,
  priority: 1,
  rrule: '',
  reminders: [],
  parent: '',
  categories: [],
  mailLinks: [],
  contacts: [],
  eventUids: [],
  sortOrder: 0,
  estimate: 60,
  created: '',
  readOnly: false,
  pending: false,
  conflict: false,
  syncError: '',
}

let nextId = 500

function made(req: TimeBlockRequest): TimeBlock {
  return {
    id: nextId++,
    title: req.title || 'Report',
    start: req.start,
    end: req.end,
    taskId: req.taskUid ? 3 : 0,
    taskUid: req.taskUid,
    messageId: req.messageId,
    accountId: 0,
    folderId: 0,
    hasMail: req.messageId !== 0,
    masked: false,
    done: false,
    calendarId: req.calendarId,
    eventUid: '',
    eventTitle: '',
    linkState: '',
    pending: false,
  }
}

// what the screen said to assistive technology last, from its polite live regions.
function announced(): string {
  return Array.from(document.querySelectorAll('[aria-live="polite"]'))
    .map((el) => el.textContent ?? '')
    .join('\n')
}

async function ready(): Promise<void> {
  render(CalendarScreen)
  await waitFor(() => expect(api.listTimeBlocks).toHaveBeenCalled())
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(now)
  vi.clearAllMocks()
  nextId = 500
  contextMenu.set(null)
  toasts.set([])
  proposals.set([])
  timeBlocks.set([])
  calendarSettingsLoaded.set(false)
  calendarView.set('week')
  calendarFocus.set(new Date(2026, 10, 14))
  tasks.set([report])
  api.listCalendars.mockResolvedValue([home])
  api.listCalendarAccounts.mockResolvedValue([])
  api.listOccurrences.mockResolvedValue([])
  api.getCalendarSettings.mockResolvedValue(settings)
  api.listAccounts.mockResolvedValue([])
  api.listTimeBlocks.mockResolvedValue([])
  api.listTaskPlans.mockResolvedValue([])
  api.getTimeBlockSettings.mockResolvedValue({
    defaultMinutes: 30, minSlotMinutes: 15, bufferMinutes: 5, workDays: [1, 2, 3, 4, 5], searchDays: 7,
    writeToCalendar: false, calendarId: 0, eventTitle: 'Focus time', includeTitle: false,
  })
  api.createTimeBlocks.mockImplementation((reqs: TimeBlockRequest[]) => Promise.resolve(reqs.map(made)))
  api.deleteTimeBlocks.mockResolvedValue(undefined)
  api.planDay.mockResolvedValue({
    proposals: [
      { taskId: 3, taskUid: 't3', title: 'Report', start: toRFC3339(new Date(2026, 10, 14, 10, 15)), end: toRFC3339(new Date(2026, 10, 14, 10, 45)), part: 1, parts: 2 },
      { taskId: 3, taskUid: 't3', title: 'Report', start: toRFC3339(new Date(2026, 10, 14, 11, 0)), end: toRFC3339(new Date(2026, 10, 14, 11, 30)), part: 2, parts: 2 },
    ],
    short: [],
    unplaced: ['Taxes'],
    tasks: 2,
  })
})

afterEach(() => {
  vi.useRealTimers()
  tasks.set([])
})

describe('plan my day', () => {
  it('proposes blocks, stores nothing until accepted, and undoes an accepted plan', async () => {
    await ready()
    const pane = screen.getByRole('complementary', { name: 'Plan' })
    await userEvent.click(within(pane).getByRole('button', { name: 'Plan my day' }))
    await waitFor(() => expect(api.planDay).toHaveBeenCalledWith('2026-11-14'))

    expect(await screen.findAllByRole('button', { name: /^Proposed block: Report/ })).toHaveLength(2)
    await waitFor(() => expect(announced()).toContain('2 time blocks proposed'))
    expect(within(pane).getByText(/No free time left today for: Taxes/)).toBeInTheDocument()
    expect(api.createTimeBlocks).not.toHaveBeenCalled()

    await userEvent.click(within(pane).getByRole('button', { name: 'Accept all' }))
    await waitFor(() => expect(api.createTimeBlocks).toHaveBeenCalledTimes(1))
    const reqs = api.createTimeBlocks.mock.calls[0][0] as TimeBlockRequest[]
    expect(reqs.map((r) => r.taskUid)).toEqual(['t3', 't3'])
    expect(new Date(reqs[0].start)).toEqual(new Date(2026, 10, 14, 10, 15))
    expect(get(proposals)).toHaveLength(0)
    expect(await screen.findAllByRole('button', { name: /^Time block: Report/ })).toHaveLength(2)

    const toast = get(toasts).find((x) => x.message.includes('Plan accepted'))
    expect(toast).toBeTruthy()
    toast?.action?.run()
    await waitFor(() => expect(api.deleteTimeBlocks).toHaveBeenCalledWith([500, 501]))
    await waitFor(() => expect(get(proposals)).toHaveLength(2))
  })

  it('accepts or dismisses one proposal at a time', async () => {
    await ready()
    const pane = screen.getByRole('complementary', { name: 'Plan' })
    await userEvent.click(within(pane).getByRole('button', { name: 'Plan my day' }))
    await screen.findAllByRole('button', { name: /^Proposed block/ })
    const review = within(pane).getByRole('region', { name: 'Proposed for today' })
    await userEvent.click(within(review).getAllByRole('button', { name: /^Dismiss/ })[0])
    expect(get(proposals)).toHaveLength(1)
    await waitFor(() => expect(announced()).toContain('Proposal dismissed'))
    await userEvent.click(within(review).getByRole('button', { name: /^Accept Report, 11:00/ }))
    await waitFor(() => expect(api.createTimeBlocks).toHaveBeenCalledTimes(1))
    expect((api.createTimeBlocks.mock.calls[0][0] as TimeBlockRequest[])).toHaveLength(1)
  })
})

describe('blocking time', () => {
  it('blocks the slot mail was dropped on with Alt held, instead of making an event', async () => {
    api.blockTimeForMessages.mockImplementation((ids: number[], at: string) =>
      Promise.resolve(ids.map((id) => made({ id: 0, title: 'Invoice', start: at, end: toRFC3339(new Date(new Date(at).getTime() + 1800000)), taskUid: '', messageId: id, done: false, calendarId: 0, eventTitle: '' }))),
    )
    await ready()
    const at = new Date(2026, 10, 13, 15)
    calendarSeed.set({ messageIds: [7], at, mode: 'time', shift: false, alt: true })
    await waitFor(() => expect(api.blockTimeForMessages).toHaveBeenCalledWith([7], toRFC3339(at)))
    expect(api.createEvent).not.toHaveBeenCalled()
    expect(await screen.findByRole('button', { name: /^Time block: Invoice/ })).toBeInTheDocument()
  })

  it('finds time for a task from the keyboard and blocks the slot picked', async () => {
    api.findFreeSlots.mockResolvedValue([
      { start: toRFC3339(new Date(2026, 10, 16, 9)), end: toRFC3339(new Date(2026, 10, 16, 10)), afterDue: true },
    ])
    await ready()
    const pane = screen.getByRole('complementary', { name: 'Plan' })
    within(pane).getByRole('button', { name: 'Find time for Report' }).focus()
    await userEvent.keyboard('{Enter}')
    const dialog = await screen.findByRole('dialog', { name: 'Find time for Report' })
    await waitFor(() => expect(api.findFreeSlots).toHaveBeenCalledWith(3, 60))
    const slot = await within(dialog).findByRole('button', { name: /After the due date/ })
    slot.focus()
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(api.createTimeBlocks).toHaveBeenCalledTimes(1))
    const req = (api.createTimeBlocks.mock.calls[0][0] as TimeBlockRequest[])[0]
    expect(req.taskUid).toBe('t3')
    expect(new Date(req.start)).toEqual(new Date(2026, 10, 16, 9))
    expect(req.calendarId).toBe(0)
  })

  it('deletes a block from its menu and brings it back with undo', async () => {
    api.listTimeBlocks.mockResolvedValue([made({ id: 0, title: 'Deep work', start: toRFC3339(new Date(2026, 10, 12, 9)), end: toRFC3339(new Date(2026, 10, 12, 10)), taskUid: '', messageId: 0, done: false, calendarId: 0, eventTitle: '' })])
    await ready()
    const el = await screen.findByRole('button', { name: /^Time block: Deep work/ })
    el.focus()
    await userEvent.keyboard('{Enter}')
    const menu = get(contextMenu)
    const del = menu?.entries.find((e) => typeof e === 'object' && 'label' in e && e.label === 'Delete block') as { action: () => void } | undefined
    expect(del).toBeTruthy()
    del?.action()
    await waitFor(() => expect(api.deleteTimeBlocks).toHaveBeenCalledWith([500]))
    get(toasts).find((x) => x.message === 'Time block deleted')?.action?.run()
    await waitFor(() => expect(api.createTimeBlocks).toHaveBeenCalledTimes(1))
    expect((api.createTimeBlocks.mock.calls[0][0] as TimeBlockRequest[])[0].title).toBe('Deep work')
  })
})
