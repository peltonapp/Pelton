import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { Task } from '../lib/types'

const api = vi.hoisted(() => ({
  setTaskDone: vi.fn(),
  updateTask: vi.fn(),
  resolveTaskConflict: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import { defaultListId, defaultTaskSettings, draftOf, resolveConflict, saveTask, taskBadge, taskClock, taskConflict, tasks, taskSettings, toggleDone } from './tasks'
import { toasts } from './toast'

const base: Task = {
  id: 1,
  listId: 2,
  listName: '',
  listColor: '',
  uid: 'a',
  title: 'Pay rent',
  notes: '',
  url: '',
  due: '2026-11-01',
  start: '',
  timeZone: '',
  done: false,
  completed: '',
  status: '',
  percent: 0,
  priority: 0,
  rrule: 'FREQ=MONTHLY',
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

beforeEach(() => {
  vi.clearAllMocks()
  toasts.set([])
  tasks.set([base])
  taskConflict.set(null)
  taskSettings.set(defaultTaskSettings)
  taskClock.set(new Date(2026, 10, 15, 12))
})

describe('toggleDone', () => {
  it('says when a repeating task moved on instead of closing', async () => {
    const next = { ...base, due: '2026-12-01' }
    api.setTaskDone.mockResolvedValue({ task: next, queued: false, conflict: false, server: next, advanced: true })
    await toggleDone(base)
    expect(get(tasks)[0].due).toBe('2026-12-01')
    expect(get(toasts).some((x) => x.message === '"Pay rent" moved to its next date')).toBe(true)
  })

  it('puts the task back when the save fails', async () => {
    api.setTaskDone.mockRejectedValue(new Error('offline'))
    await toggleDone(base)
    expect(get(tasks)[0].done).toBe(false)
  })
})

describe('conflicts', () => {
  it('holds a refused save until it is settled', async () => {
    const theirs = { ...base, title: 'Theirs' }
    api.updateTask.mockResolvedValue({ task: { ...base, conflict: true }, queued: false, conflict: true, server: theirs, advanced: false })
    expect(await saveTask({ ...draftOf(base), title: 'Mine' })).toBeNull()
    expect(get(taskConflict)?.server.title).toBe('Theirs')
    api.resolveTaskConflict.mockResolvedValue(theirs)
    await resolveConflict(false)
    expect(api.resolveTaskConflict).toHaveBeenCalledWith(1, false)
    expect(get(taskConflict)).toBeNull()
    expect(get(tasks)[0].title).toBe('Theirs')
  })
})

describe('badge and defaults', () => {
  it('counts overdue tasks by the setting', () => {
    expect(get(taskBadge)).toBe(1)
    taskSettings.set({ ...defaultTaskSettings, badge: 'off' })
    expect(get(taskBadge)).toBe(0)
  })

  it('picks the chosen list while it is writable, else the first one', () => {
    const lists = [
      { id: 1, accountId: 0, accountName: '', name: '', color: '', local: true, readOnly: true, events: false, lastError: '', default: false },
      { id: 2, accountId: 0, accountName: '', name: '', color: '', local: true, readOnly: false, events: false, lastError: '', default: true },
      { id: 3, accountId: 0, accountName: '', name: '', color: '', local: true, readOnly: false, events: false, lastError: '', default: false },
    ]
    expect(defaultListId(lists, { ...defaultTaskSettings, defaultListId: 3 })).toBe(3)
    expect(defaultListId(lists, { ...defaultTaskSettings, defaultListId: 1 })).toBe(2)
  })
})
