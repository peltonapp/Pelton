import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { AttachmentEntry, MeetingPrep, MessageSummary, Task } from '../../lib/types'

const api = vi.hoisted(() => ({
  prepareMeeting: vi.fn(),
  createTask: vi.fn(),
  listTasks: vi.fn(),
  listTaskLists: vi.fn(),
  setTaskDone: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const browser = vi.hoisted(() => ({ openURL: vi.fn() }))
vi.mock('../../lib/browser', () => browser)

const calendarLib = vi.hoisted(() => ({ openDayInCalendar: vi.fn() }))
vi.mock('../../lib/mailcalendar', () => calendarLib)

// dialogs open with transitions and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

import MeetingPrepPanel from './MeetingPrep.svelte'
import { openMessageId } from '../../stores/selection'
import { previewTarget } from '../../stores/preview'
import { space } from '../../stores/space'
import { taskFocus, taskLists, tasksLoaded } from '../../stores/tasks'
import { toasts } from '../../stores/toast'

function summary(id: number, subject: string): MessageSummary {
  return { id, accountId: 1, folderId: 4, subject, fromName: 'Ada', fromAddress: 'ada@x.example', date: '2026-11-10T09:00:00Z' } as MessageSummary
}

function openTaskRow(id: number, title: string): Task {
  return { id, listId: 1, listName: 'Tasks', listColor: '', uid: `t${id}`, title, due: '2026-11-30', done: false, readOnly: false, contacts: [], mailLinks: [], eventUids: [] } as unknown as Task
}

const file: AttachmentEntry = {
  id: 90,
  messageId: 31,
  accountId: 1,
  folderId: 4,
  threadId: 2,
  filename: 'plan.pdf',
  contentType: 'application/pdf',
  sizeBytes: 5000,
  kind: 'pdf',
  subject: 'Budget',
  fromName: 'Ada',
  fromAddress: 'ada@x.example',
  date: '2026-11-01T09:00:00Z',
  downloaded: true,
}

function prep(patch: Partial<MeetingPrep> = {}): MeetingPrep {
  return {
    eventId: 7,
    recurrenceId: '',
    uid: 'uid-7',
    title: 'Roadmap review',
    description: 'Bring the numbers.',
    location: 'Room 4',
    url: '',
    start: '2026-11-17T14:00:00+01:00',
    end: '2026-11-17T15:00:00+01:00',
    allDay: false,
    recurring: false,
    join: { url: 'https://acme.zoom.us/j/1', provider: 'Zoom' },
    attendees: [
      { email: 'me@x.example', name: 'Me', role: 'CHAIR', partStat: 'ACCEPTED', organizer: true, self: true, contact: null },
      {
        email: 'ada@x.example',
        name: 'Ada',
        role: 'REQ-PARTICIPANT',
        partStat: 'ACCEPTED',
        organizer: false,
        self: false,
        contact: { id: 1, bookId: 1, bookName: 'Personal', uid: 'a', fullName: 'Ada Lovelace', organization: 'Analytical', title: 'Countess', note: '', emails: [{ value: 'ada@x.example', label: '' }], phones: [{ value: '+31 6 1234 5678', label: '' }], readOnly: false, updated: '', extra: [] },
      },
      { email: 'bob@x.example', name: '', role: 'REQ-PARTICIPANT', partStat: 'DECLINED', organizer: false, self: false, contact: null },
    ],
    threads: [{ threadId: 2, subject: 'Budget', last: '2026-11-10T09:00:00Z', messages: 3, people: ['Ada Lovelace', 'bob@x.example'], titleMatch: false, message: summary(31, 'Budget') }],
    files: [file],
    tasks: [openTaskRow(4, 'Send Ada the numbers')],
    previous: [{ eventId: 3, recurrenceId: '', title: 'Budget talk', start: '2026-11-05T10:00:00+01:00', end: '2026-11-05T11:00:00+01:00', allDay: false, sameSeries: false, shared: ['Ada Lovelace'] }],
    ...patch,
  }
}

beforeEach(() => {
  for (const fn of [...Object.values(api), ...Object.values(browser), ...Object.values(calendarLib)]) {
    fn.mockReset()
  }
  api.prepareMeeting.mockResolvedValue(prep())
  api.listTasks.mockResolvedValue([])
  api.listTaskLists.mockResolvedValue([])
  taskLists.set([{ id: 1, name: 'Tasks', color: '', local: true, readOnly: false, default: true } as never])
  tasksLoaded.set(true)
  openMessageId.set(null)
  previewTarget.set(null)
  space.set('calendar')
  taskFocus.set(null)
  toasts.set([])
})

function mount() {
  const events = { close: vi.fn(), compose: vi.fn() }
  render(MeetingPrepPanel, { props: { target: { eventId: 7, recurrenceId: '' } }, events })
  return events
}

describe('MeetingPrep', () => {
  it('asks for the meeting and shows it', async () => {
    mount()
    expect(await screen.findByRole('dialog', { name: 'Prepare: Roadmap review' })).toBeInTheDocument()
    expect(api.prepareMeeting).toHaveBeenCalledWith(7, '')
    expect(await screen.findByText('Bring the numbers.')).toBeInTheDocument()
    expect(screen.getByText('Room 4')).toBeInTheDocument()
  })

  it('lists who is invited with their card details and reply', async () => {
    mount()
    const section = await screen.findByRole('region', { name: 'Invited' })
    expect(within(section).getByText('Ada Lovelace')).toBeInTheDocument()
    expect(within(section).getByText('Countess, Analytical')).toBeInTheDocument()
    expect(within(section).getByText('+31 6 1234 5678')).toBeInTheDocument()
    expect(within(section).getByText('You')).toBeInTheDocument()
    expect(within(section).getByText('Declined')).toBeInTheDocument()
    expect(within(section).getByText('bob@x.example', { selector: '.name' })).toBeInTheDocument()
  })

  it('joins the call in the browser', async () => {
    mount()
    await userEvent.click(await screen.findByRole('button', { name: 'Join with Zoom' }))
    expect(browser.openURL).toHaveBeenCalledWith('https://acme.zoom.us/j/1')
  })

  it('has no join button when there is no call', async () => {
    api.prepareMeeting.mockResolvedValue(prep({ join: null }))
    mount()
    await screen.findByText('Bring the numbers.')
    expect(screen.queryByRole('button', { name: /Join/ })).toBeNull()
  })

  it('opens a conversation in the reader', async () => {
    const events = mount()
    const section = await screen.findByRole('region', { name: 'Recent conversations' })
    expect(within(section).getByText('with Ada Lovelace, bob@x.example')).toBeInTheDocument()
    expect(within(section).getByText('3 messages')).toBeInTheDocument()
    await userEvent.click(within(section).getByRole('button', { name: /Budget/ }))
    expect(get(openMessageId)).toBe(31)
    expect(get(space)).toBe('mail')
    expect(events.close).toHaveBeenCalled()
  })

  it('previews a file', async () => {
    mount()
    await userEvent.click(await screen.findByRole('button', { name: /plan\.pdf/ }))
    expect(get(previewTarget)).toMatchObject({ messageId: 31, attachment: { id: 90, filename: 'plan.pdf' } })
  })

  it('opens a task in Tasks', async () => {
    mount()
    const section = await screen.findByRole('region', { name: 'Open tasks' })
    await userEvent.click(within(section).getByRole('button', { name: /Send Ada the numbers/ }))
    expect(get(taskFocus)).toBe(4)
    expect(get(space)).toBe('tasks')
  })

  it('goes to an earlier meeting in the calendar', async () => {
    const events = mount()
    const section = await screen.findByRole('region', { name: 'Earlier meetings' })
    await userEvent.click(within(section).getByRole('button', { name: /Budget talk/ }))
    expect(calendarLib.openDayInCalendar).toHaveBeenCalledWith(new Date('2026-11-05T10:00:00+01:00'))
    expect(events.close).toHaveBeenCalled()
  })

  it('says so for the sections with nothing in them', async () => {
    api.prepareMeeting.mockResolvedValue(prep({ threads: [], files: [], tasks: [], previous: [] }))
    mount()
    expect(await screen.findByText('No recent mail with them.')).toBeInTheDocument()
    expect(screen.getByText('No files exchanged lately.')).toBeInTheDocument()
    expect(screen.getByText('No open tasks linked to them.')).toBeInTheDocument()
    expect(screen.getByText('No earlier meetings with the same people.')).toBeInTheDocument()
  })

  it('writes to everyone but the user', async () => {
    const events = mount()
    await userEvent.click(await screen.findByRole('button', { name: 'Write to everyone' }))
    expect(events.compose).toHaveBeenCalledTimes(1)
    expect((events.compose.mock.calls[0][0] as CustomEvent).detail).toEqual({
      to: 'Ada Lovelace <ada@x.example>, bob@x.example',
      subject: 'Roadmap review',
    })
  })

  it('cannot write when nobody else is invited', async () => {
    api.prepareMeeting.mockResolvedValue(prep({ attendees: [prep().attendees[0]] }))
    mount()
    await screen.findByText('Nobody else is invited.')
    expect(screen.getByRole('button', { name: 'Write to everyone' })).toBeDisabled()
  })

  it('creates a task for the meeting', async () => {
    api.createTask.mockImplementation(async (draft) => ({ task: { ...openTaskRow(9, draft.title) }, queued: false, conflict: false }))
    mount()
    await userEvent.click(await screen.findByRole('button', { name: 'Create a task' }))
    await waitFor(() => expect(api.createTask).toHaveBeenCalled())
    expect(api.createTask.mock.calls[0][0]).toMatchObject({
      title: 'Prepare: Roadmap review',
      eventUids: ['uid-7'],
      contacts: [{ name: 'Ada Lovelace', email: 'ada@x.example' }, { name: '', email: 'bob@x.example' }],
    })
    await waitFor(() => expect(get(toasts).some((x) => x.message === 'Task created.')).toBe(true))
  })

  it('shows the error when gathering fails', async () => {
    api.prepareMeeting.mockRejectedValue(new Error('no such event'))
    mount()
    expect(await screen.findByRole('alert')).toHaveTextContent('no such event')
    expect(screen.getByRole('button', { name: 'Write to everyone' })).toBeDisabled()
  })

  it('closes with the close button', async () => {
    const events = mount()
    await screen.findByText('Bring the numbers.')
    await userEvent.click(screen.getByRole('button', { name: /close/i }))
    expect(events.close).toHaveBeenCalled()
  })
})
