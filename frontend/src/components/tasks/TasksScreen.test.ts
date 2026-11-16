import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Task, TaskDraft, TaskList, TaskSaveResult, TaskSettings } from '../../lib/types'

const api = vi.hoisted(() => ({
  listTaskLists: vi.fn(),
  listTasks: vi.fn(),
  listTaskMailMarks: vi.fn(),
  getTaskSettings: vi.fn(),
  setTaskSettings: vi.fn(),
  createTask: vi.fn(),
  updateTask: vi.fn(),
  setTaskDone: vi.fn(),
  setTaskDue: vi.fn(),
  deleteTask: vi.fn(),
  restoreTasks: vi.fn(),
  reorderTasks: vi.fn(),
  getCalendarSettings: vi.fn(),
  searchAddresses: vi.fn(),
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

// rows animate into their new place with flip, which asks for the running
// animations first.
Element.prototype.getAnimations ??= function () {
  return []
}

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

import TasksScreen from './TasksScreen.svelte'
import TaskDetail from './TaskDetail.svelte'
import MonthView from '../calendar/MonthView.svelte'
import { tasks, taskLists, taskSettings, defaultTaskSettings, taskClock } from '../../stores/tasks'
import { runSpaceAction } from '../../stores/space'
import { spaceSearchProviders } from '../../lib/spacesearch'
import { toasts } from '../../stores/toast'
import { monthGrid, placeOccurrence } from '../../lib/caldate'
import { taskOccurrences } from '../calendar/taskocc'

// Sunday 15 November 2026, 10:07.
const now = new Date(2026, 10, 15, 10, 7)

const inbox: TaskList = { id: 7, accountId: 0, accountName: '', name: 'Errands', color: '', local: true, readOnly: false, events: false, lastError: '', default: true }

const settings: TaskSettings = { ...defaultTaskSettings }

let nextId = 1
function task(over: Partial<Task> = {}): Task {
  const id = nextId++
  return {
    id,
    listId: 7,
    listName: 'Errands',
    listColor: '',
    uid: `u${id}`,
    title: `Task ${id}`,
    notes: '',
    url: '',
    due: '',
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
    created: '',
    readOnly: false,
    pending: false,
    conflict: false,
    syncError: '',
    ...over,
  }
}

function result(t: Task): TaskSaveResult {
  return { task: t, queued: false, conflict: false, server: t, advanced: false }
}

let stored: Task[] = []

async function ready(): Promise<void> {
  render(TasksScreen)
  await waitFor(() => expect(api.listTasks).toHaveBeenCalled())
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(now)
  taskClock.set(now)
  vi.clearAllMocks()
  nextId = 1
  stored = [
    task({ title: 'Buy milk', due: '2026-11-15' }),
    task({ title: 'File taxes', due: '2026-11-10' }),
    task({ title: 'Plan trip', due: '2026-11-20' }),
    task({ title: 'Read book' }),
  ]
  tasks.set([])
  taskLists.set([])
  taskSettings.set(settings)
  toasts.set([])
  api.listTaskLists.mockResolvedValue([inbox])
  api.listTasks.mockImplementation(() => Promise.resolve(stored))
  api.listTaskMailMarks.mockResolvedValue([])
  api.getTaskSettings.mockResolvedValue(settings)
  api.getCalendarSettings.mockResolvedValue({ weekStart: 1 })
  api.searchAddresses.mockResolvedValue([])
  api.createTask.mockImplementation((draft: TaskDraft) => {
    const made = task({ ...draft, id: 0 } as unknown as Partial<Task>)
    const saved = { ...made, id: nextId++, title: draft.title, due: draft.due, rrule: draft.rrule }
    stored = [...stored, saved]
    return Promise.resolve(result(saved))
  })
  api.setTaskDone.mockImplementation((id: number, done: boolean) => {
    const t = { ...stored.find((x) => x.id === id)!, done }
    stored = stored.map((x) => (x.id === id ? t : x))
    return Promise.resolve(result(t))
  })
  api.updateTask.mockImplementation((draft: TaskDraft) => {
    const t = { ...stored.find((x) => x.id === draft.id)!, ...draft } as unknown as Task
    stored = stored.map((x) => (x.id === draft.id ? t : x))
    return Promise.resolve(result(t))
  })
  api.deleteTask.mockImplementation((id: number) => {
    const gone = stored.find((x) => x.id === id)!
    stored = stored.filter((x) => x.id !== id)
    return Promise.resolve({ removed: [{ listId: gone.listId, data: `uid:${gone.uid}` }], queued: false })
  })
  api.restoreTasks.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.useRealTimers()
})

describe('smart lists', () => {
  it('opens on Today and moves between lists', async () => {
    await ready()
    expect(await screen.findByText('Buy milk')).toBeInTheDocument()
    expect(screen.queryByText('File taxes')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Overdue/ }))
    expect(await screen.findByText('File taxes')).toBeInTheDocument()
    expect(screen.queryByText('Buy milk')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /No date/ }))
    expect(await screen.findByText('Read book')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /^Errands/ }))
    expect(await screen.findByText('Plan trip')).toBeInTheDocument()
    expect(screen.getByText('File taxes')).toBeInTheDocument()
  })
})

describe('quick entry', () => {
  it('reads the day and time out of the line', async () => {
    await ready()
    const entry = screen.getByRole('textbox', { name: 'New task' })
    await userEvent.type(entry, 'call Ada tomorrow 3pm{Enter}')
    await waitFor(() => expect(api.createTask).toHaveBeenCalledTimes(1))
    const draft = api.createTask.mock.lastCall?.[0] as TaskDraft
    expect(draft.title).toBe('call Ada')
    expect(draft.listId).toBe(7)
    expect(new Date(draft.due)).toEqual(new Date(2026, 10, 16, 15, 0))
  })

  it('puts a task typed in Today on today', async () => {
    await ready()
    await userEvent.type(screen.getByRole('textbox', { name: 'New task' }), 'water plants{Enter}')
    await waitFor(() => expect(api.createTask).toHaveBeenCalledTimes(1))
    expect((api.createTask.mock.lastCall?.[0] as TaskDraft).due).toBe('2026-11-15')
  })

  it('takes the compose shortcut while Tasks is on screen', async () => {
    await ready()
    expect(runSpaceAction('tasks', 'compose')).toBe(true)
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'New task' })).toHaveFocus())
  })
})

describe('checking off and keys', () => {
  it('completes a task from its checkbox', async () => {
    await ready()
    const box = await screen.findByRole('checkbox', { name: 'Done: Buy milk' })
    expect(box).toHaveAttribute('aria-checked', 'false')
    await userEvent.click(box)
    await waitFor(() => expect(api.setTaskDone).toHaveBeenCalledWith(1, true))
  })

  it('toggles with space, moves on, and deletes with undo', async () => {
    stored = [...stored, task({ title: 'Call bank', due: '2026-11-15' })]
    await ready()
    const row = (await screen.findByText('Buy milk')).closest('li') as HTMLElement
    await userEvent.click(row)
    row.focus()
    await userEvent.keyboard(' ')
    await waitFor(() => expect(api.setTaskDone).toHaveBeenCalledWith(1, true))
    const next = (await screen.findByText('Call bank')).closest('li') as HTMLElement
    await waitFor(() => expect(next).toHaveFocus())

    await userEvent.keyboard('{Delete}')
    await waitFor(() => expect(api.deleteTask).toHaveBeenCalledWith(5))
    const toast = get(toasts).find((x) => x.message === 'Task deleted')
    expect(toast).toBeDefined()
    toast?.action?.run()
    await waitFor(() => expect(api.restoreTasks).toHaveBeenCalledWith([{ listId: 7, data: 'uid:u5' }]))
  })
})

describe('top bar search', () => {
  it('searches titles, notes and list names while Tasks shows', async () => {
    stored = [...stored, task({ title: 'Call plumber', notes: 'about the leak' })]
    await ready()
    await waitFor(() => expect(get(tasks).length).toBe(5))
    const provider = get(spaceSearchProviders).tasks
    expect(provider).toBeDefined()
    const hits = await provider!.search('leak')
    expect(hits.map((h) => h.title)).toEqual(['Call plumber'])
    expect((await provider!.search('errands')).length).toBe(5)
  })
})

describe('detail pane', () => {
  it('saves a priority and shows contacts as chips', async () => {
    const t = task({ title: 'Send report', contacts: [{ name: 'Ada', email: 'ada@example.com' }] })
    stored = [t]
    tasks.set([t])
    taskLists.set([inbox])
    render(TaskDetail, { task: t, lists: [inbox] })
    expect(screen.getByText('Ada')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove Ada' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Remove Ada' }))
    await waitFor(() => expect(api.updateTask).toHaveBeenCalled())
    expect((api.updateTask.mock.lastCall?.[0] as TaskDraft).contacts).toEqual([])
  })
})

describe('tasks in the calendar', () => {
  it('draws a due task with a checkbox that completes it', async () => {
    const t = task({ title: 'Pay rent', due: '2026-11-17' })
    const onToggle = vi.fn()
    const occs = taskOccurrences([t], [inbox], new Date(2026, 10, 1), new Date(2026, 11, 1))
    render(MonthView, {
      props: {
        weeks: monthGrid(new Date(2026, 10, 15), 1),
        month: 10,
        placed: occs.map(placeOccurrence),
        colors: new Map(),
        now,
        weekStart: 1,
        focus: now,
      },
      events: { toggleTask: (e: CustomEvent) => onToggle(e.detail) },
    })
    const box = screen.getByRole('checkbox', { name: 'Mark as done: Pay rent' })
    await userEvent.click(box)
    expect(onToggle).toHaveBeenCalledWith(expect.objectContaining({ task: t }))
  })

  it('leaves out tasks outside the window and without a date', () => {
    const inside = task({ due: '2026-11-17' })
    const outside = task({ due: '2027-01-02' })
    const undated = task()
    expect(taskOccurrences([inside, outside, undated], [inbox], new Date(2026, 10, 1), new Date(2026, 11, 1)).map((o) => o.task?.id)).toEqual([inside.id])
  })
})

describe('the list in a rendered screen', () => {
  it('nests subtasks under their parent', async () => {
    const parent = task({ title: 'Move house', due: '2026-11-15' })
    const child = task({ title: 'Book van', due: '2026-11-15', parent: parent.uid })
    stored = [parent, child]
    await ready()
    const rows = within(await screen.findByRole('listbox', { name: 'Tasks' })).getAllByRole('option')
    expect(rows.map((r) => r.textContent?.includes('Book van'))).toEqual([false, true])
    expect(rows[1].getAttribute('style')).toMatch(/--depth: ?1/)
  })
})
