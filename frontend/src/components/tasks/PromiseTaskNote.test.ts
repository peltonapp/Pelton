import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  reopenPromise: vi.fn(),
  listPromises: vi.fn(),
  getMessage: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  reopenPromise: api.reopenPromise,
  listPromises: api.listPromises,
  getMessage: api.getMessage,
}))

import PromiseTaskNote from './PromiseTaskNote.svelte'
import { promises, promiseSettings, defaultPromiseSettings } from '../../stores/promises'
import { expectAccessible } from '../../test/a11y'
import type { MailPromise, Task } from '../../lib/types'

const task = {
  id: 5,
  uid: 'u1',
  title: 'Contract from Anna',
  due: '2026-11-10',
  done: false,
  listId: 1,
} as Task

function promise(over: Partial<MailPromise>): MailPromise {
  return {
    id: 1,
    accountId: 1,
    mailId: 'm1@example.com',
    messageId: 7,
    threadId: 3,
    direction: 'theirs',
    personName: 'Anna',
    personEmail: 'anna@example.com',
    title: 'Contract from Anna',
    quote: "I'll send the contract by Friday.",
    due: '2026-11-10',
    confidence: 0.8,
    source: 'rules',
    state: 'accepted',
    taskUid: 'u1',
    replyId: '',
    replyAt: '',
    replyMessageId: 0,
    mailDate: '2026-11-09T09:00:00Z',
    followUp: false,
    ...over,
  }
}

const now = new Date('2026-11-18T10:00:00')

beforeEach(() => {
  api.reopenPromise.mockReset().mockResolvedValue(undefined)
  api.listPromises.mockReset().mockResolvedValue([])
  promiseSettings.set(defaultPromiseSettings)
})

describe('PromiseTaskNote', () => {
  it('says nothing for a task that is not a promise', () => {
    promises.set([])
    render(PromiseTaskNote, { props: { task, now } })
    expect(screen.queryByRole('region')).not.toBeInTheDocument()
  })

  it('offers a reminder draft for an overdue promise owed to the user', () => {
    promises.set([promise({})])
    render(PromiseTaskNote, { props: { task, now } })
    expect(screen.getByRole('region', { name: 'Promise from mail' })).toBeInTheDocument()
    expect(screen.getByText(/Still waiting on Anna since/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Write a reminder' })).toBeInTheDocument()
    expectAccessible()
  })

  it('does not nudge while a follow-up reminder watches the thread, or when nudges are off', () => {
    promises.set([promise({ followUp: true })])
    const { unmount } = render(PromiseTaskNote, { props: { task, now } })
    expect(screen.queryByRole('button', { name: 'Write a reminder' })).not.toBeInTheDocument()
    unmount()
    promises.set([promise({})])
    promiseSettings.set({ ...defaultPromiseSettings, nudge: false })
    render(PromiseTaskNote, { props: { task, now } })
    expect(screen.queryByRole('button', { name: 'Write a reminder' })).not.toBeInTheDocument()
  })

  it('says which reply closed it and undoes the close', async () => {
    const user = userEvent.setup()
    promises.set([promise({ state: 'closed', replyId: 'r@example.com', replyAt: '2026-11-17T08:00:00Z', replyMessageId: 9 })])
    render(PromiseTaskNote, { props: { task: { ...task, done: true }, now } })
    expect(screen.getByText(/Closed by the reply from Anna/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open the reply' })).toBeInTheDocument()
    expectAccessible()
    await user.click(screen.getByRole('button', { name: 'Undo' }))
    await waitFor(() => expect(api.reopenPromise).toHaveBeenCalledWith(1))
    expect(screen.getByText('Open again.')).toBeInTheDocument()
  })

  it('offers to complete a promise the user owes once they replied', async () => {
    const user = userEvent.setup()
    promises.set([promise({ direction: 'mine', replyId: 'a@example.com', replyAt: '2026-11-17T08:00:00Z' })])
    render(PromiseTaskNote, { props: { task, now } })
    expect(screen.getByText(/You replied in this conversation/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Not yet' }))
    await waitFor(() => expect(api.reopenPromise).toHaveBeenCalledWith(1))
  })
})
