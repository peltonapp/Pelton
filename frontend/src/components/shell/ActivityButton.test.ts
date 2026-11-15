import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  sendOutboxNow: vi.fn(),
  retrySend: vi.fn(),
  listOutbox: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import ActivityButton from './ActivityButton.svelte'
import { outbox } from '../../stores/outbox'
import type { OutboxRow } from '../../lib/types'

function row(patch: Partial<OutboxRow>): OutboxRow {
  return {
    id: 1,
    accountId: 1,
    recipients: ['ann@example.com'],
    state: 'queued',
    attempts: 0,
    lastError: '',
    nextAttemptAt: '',
    createdAt: '',
    editable: false,
    priority: 0,
    ...patch,
  }
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.sendOutboxNow.mockResolvedValue(true)
  api.retrySend.mockResolvedValue(true)
  api.listOutbox.mockResolvedValue([])
  outbox.set([])
})

describe('ActivityButton', () => {
  it('counts what is waiting and groups it in the popover', async () => {
    const later = new Date(Date.now() + 3 * 60 * 60 * 1000).toISOString()
    outbox.set([
      row({ id: 1, state: 'sending' }),
      row({ id: 2, nextAttemptAt: later, recipients: ['bob@example.com'] }),
      row({ id: 3, state: 'failed', lastError: 'auth failed', recipients: ['carol@example.com'] }),
    ])
    render(ActivityButton)

    await userEvent.click(screen.getByRole('button', { name: '1 failed to send' }))

    const dialog = screen.getByRole('dialog', { name: 'Outbox' })
    expect(within(dialog).getByRole('region', { name: 'Sending' })).toHaveTextContent('ann@example.com')
    expect(within(dialog).getByRole('region', { name: 'Scheduled' })).toHaveTextContent('bob@example.com')
    expect(within(dialog).getByRole('region', { name: 'Failed' })).toHaveTextContent('auth failed')
  })

  it('sends a scheduled message now', async () => {
    outbox.set([row({ id: 9, nextAttemptAt: new Date(Date.now() + 86400000).toISOString() })])
    render(ActivityButton)

    await userEvent.click(screen.getByRole('button', { name: '1 waiting to send' }))
    await userEvent.click(screen.getByRole('button', { name: 'Send now' }))

    expect(api.sendOutboxNow).toHaveBeenCalledWith(9)
  })

  it('retries a failed message', async () => {
    outbox.set([row({ id: 4, state: 'failed' })])
    render(ActivityButton)

    await userEvent.click(screen.getByRole('button', { name: '1 failed to send' }))
    await userEvent.click(screen.getByRole('button', { name: 'Try sending again' }))

    expect(api.retrySend).toHaveBeenCalledWith(4)
  })
})
