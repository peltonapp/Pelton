import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { MailEventSource, TaskDraft } from './types'

const api = vi.hoisted(() => ({
  createTask: vi.fn(),
  deleteTask: vi.fn(),
  mailEventSource: vi.fn(),
  listTaskLists: vi.fn(),
  listTasks: vi.fn(),
  listTaskMailMarks: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { createTasksFromMail, draftFromMail } from './taskmail'
import { space } from '../stores/space'
import { taskFocus, taskLists } from '../stores/tasks'
import { toasts } from '../stores/toast'
import { spaceDrop } from './spaces'

const mail: MailEventSource = {
  subject: 'Invoice 42',
  text: 'Please pay by 3 December 2026.',
  date: new Date(2026, 10, 10, 9).toISOString(),
  fromName: 'Billing',
  fromAddress: 'billing@example.com',
  link: 'mid:inv42@example.com',
}

beforeEach(() => {
  vi.clearAllMocks()
  toasts.set([])
  space.set('mail')
  taskLists.set([{ id: 3, accountId: 0, accountName: '', name: '', color: '', local: true, readOnly: false, events: false, lastError: '', default: true }])
  api.mailEventSource.mockResolvedValue(mail)
  api.createTask.mockImplementation((d: TaskDraft) => Promise.resolve({ task: { ...d, id: 11 }, queued: false, conflict: false, server: {}, advanced: false }))
  api.listTaskLists.mockResolvedValue(get(taskLists))
  api.listTasks.mockResolvedValue([])
  api.listTaskMailMarks.mockResolvedValue([])
})

describe('draftFromMail', () => {
  it('takes the subject, links back to the mail and finds the due date in the text', async () => {
    const draft = await draftFromMail(mail, 3)
    expect(draft).toMatchObject({ title: 'Invoice 42', listId: 3, mailLinks: ['mid:inv42@example.com'], due: '2026-12-03' })
  })

  it('leaves the date empty when the mail names none', async () => {
    const draft = await draftFromMail({ ...mail, subject: 'Hello', text: 'Nothing to see' }, 3)
    expect(draft.due).toBe('')
  })
})

describe('createTasksFromMail', () => {
  it('makes one task per message and offers to open it', async () => {
    expect(await createTasksFromMail([1, 2])).toEqual([11, 11])
    expect(api.createTask).toHaveBeenCalledTimes(2)
    const toast = get(toasts).find((x) => x.message === '2 tasks created from the mails')
    expect(toast?.more?.[0].label).toBe('Open')
    toast?.more?.[0].run()
    expect(get(space)).toBe('tasks')
    expect(get(taskFocus)).toBe(11)
  })

  it('runs for mail dropped on the Tasks entry', async () => {
    const drop = spaceDrop('tasks')
    expect(drop.accepts?.([])).toBe(true)
    expect(drop.hint?.([{ id: 1 } as never], false)).toBe('Create a task from this mail')
    drop.onDrop([{ id: 1 } as never], { shift: false })
    await vi.waitFor(() => expect(api.createTask).toHaveBeenCalledTimes(1))
    expect(get(space)).toBe('mail')
  })
})
