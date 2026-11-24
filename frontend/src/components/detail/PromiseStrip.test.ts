import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  acceptPromise: vi.fn(),
  dismissPromise: vi.fn(),
  restorePromise: vi.fn(),
  quietPromiseSender: vi.fn(),
  listPromises: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  acceptPromise: api.acceptPromise,
  dismissPromise: api.dismissPromise,
  restorePromise: api.restorePromise,
  quietPromiseSender: api.quietPromiseSender,
  listPromises: api.listPromises,
}))

import PromiseStrip from './PromiseStrip.svelte'
import { promiseHighlight, promises } from '../../stores/promises'
import { expectAccessible } from '../../test/a11y'
import type { MailPromise } from '../../lib/types'

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
    title: "I'll send the contract by Friday.",
    quote: "I'll send the contract by Friday.",
    due: '2026-11-20',
    confidence: 0.8,
    source: 'rules',
    state: 'suggested',
    taskUid: '',
    replyId: '',
    replyAt: '',
    replyMessageId: 0,
    mailDate: '2026-11-18T09:00:00Z',
    followUp: false,
    ...over,
  }
}

const props = { messageId: 7, accountId: 1, mailId: '<m1@example.com>' }

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.listPromises.mockResolvedValue([])
  promises.set([])
  promiseHighlight.set(null)
})

describe('PromiseStrip', () => {
  it('shows nothing but its live region without suggestions', () => {
    const { container } = render(PromiseStrip, { props })
    expect(screen.queryByRole('region')).not.toBeInTheDocument()
    expect(container.querySelector('[aria-live="polite"]')).not.toBeNull()
  })

  it('accepts a suggestion and marks its sentence while focused', async () => {
    const user = userEvent.setup()
    const p = promise({})
    promises.set([p])
    api.acceptPromise.mockResolvedValue({ ...p, state: 'accepted', taskUid: 'u1' })
    render(PromiseStrip, { props })
    expect(screen.getByRole('region', { name: 'Promises in this mail' })).toBeInTheDocument()
    expect(screen.getByText('Anna owes you')).toBeInTheDocument()
    expectAccessible()

    const accept = screen.getByRole('button', { name: `Accept as a task: ${p.title}` })
    accept.focus()
    expect(get(promiseHighlight)).toEqual({ messageId: 7, quote: p.quote })
    await user.keyboard('{Enter}')
    await waitFor(() => expect(api.acceptPromise).toHaveBeenCalledWith(expect.objectContaining({ id: 1, title: p.title, due: '2026-11-20' })))
    await waitFor(() => expect(screen.queryByRole('region')).not.toBeInTheDocument())
    expect(screen.getByText(`Accepted: ${p.title}`)).toBeInTheDocument()
  })

  it('edits who owes it and the text before accepting, and Escape cancels', async () => {
    const user = userEvent.setup()
    const p = promise({})
    promises.set([p])
    api.acceptPromise.mockResolvedValue({ ...p, state: 'accepted' })
    render(PromiseStrip, { props })
    await user.click(screen.getByRole('button', { name: `Edit before accepting: ${p.title}` }))
    const title = screen.getByRole('textbox', { name: 'Task' })
    await waitFor(() => expect(title).toHaveFocus())
    expectAccessible()
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('textbox', { name: 'Task' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: `Edit before accepting: ${p.title}` }))
    const field = screen.getByRole('textbox', { name: 'Task' })
    await user.clear(field)
    await user.type(field, 'Contract from Anna')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Who owes it' }), 'mine')
    await user.click(screen.getByRole('button', { name: 'Accept' }))
    await waitFor(() =>
      expect(api.acceptPromise).toHaveBeenCalledWith(expect.objectContaining({ id: 1, title: 'Contract from Anna', direction: 'mine' })),
    )
  })

  it('folds several into one line and dismisses with an undo', async () => {
    const user = userEvent.setup()
    promises.set([promise({}), promise({ id: 2, direction: 'mine', title: 'Can you confirm the address?', quote: 'Can you confirm the address?' })])
    api.dismissPromise.mockResolvedValue(undefined)
    render(PromiseStrip, { props })
    const fold = screen.getByRole('button', { name: /2 promises in this mail/ })
    expect(fold).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('You owe Anna')).not.toBeInTheDocument()
    await user.click(fold)
    expect(screen.getByText('You owe Anna')).toBeInTheDocument()
    expectAccessible()
    await user.click(screen.getByRole('button', { name: 'Dismiss: Can you confirm the address?' }))
    await waitFor(() => expect(api.dismissPromise).toHaveBeenCalledWith(2))
    expect(screen.queryByText('You owe Anna')).not.toBeInTheDocument()
  })

  it('leaves out suggestions of other mail and ones already accepted', () => {
    promises.set([promise({ mailId: 'other@example.com' }), promise({ id: 3, state: 'accepted' })])
    render(PromiseStrip, { props })
    expect(screen.queryByRole('region')).not.toBeInTheDocument()
  })

  it('dismisses everything from a sender', async () => {
    const user = userEvent.setup()
    promises.set([promise({})])
    api.quietPromiseSender.mockResolvedValue([1])
    render(PromiseStrip, { props })
    await user.click(screen.getByRole('button', { name: 'Dismiss all from Anna' }))
    await waitFor(() => expect(api.quietPromiseSender).toHaveBeenCalledWith('anna@example.com'))
  })
})
