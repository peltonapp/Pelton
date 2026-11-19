import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  cancelOutboxSend: vi.fn(),
  prioritiseOutboxSend: vi.fn(),
  listOutbox: vi.fn(),
}))

const compose = vi.hoisted(() => ({
  openRequestSession: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/compose', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/compose')>()),
  ...compose,
}))

import OutboxList from './OutboxList.svelte'
import { toasts } from '../../stores/toast'
import { get } from 'svelte/store'
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

function sendingRows(): HTMLElement[] {
  const region = screen.getByRole('region', { name: 'Sending' })
  return within(region)
    .getAllByText(/@example\.com/)
    .map((label) => label.closest('.row') as HTMLElement)
}

function names(el: HTMLElement): string[] {
  return within(el)
    .queryAllByRole('button')
    .map((b) => b.getAttribute('aria-label') ?? '')
}

beforeEach(() => {
  for (const fn of [...Object.values(api), ...Object.values(compose)]) {
    fn.mockReset()
  }
  api.listOutbox.mockResolvedValue([])
  api.prioritiseOutboxSend.mockResolvedValue(true)
})

describe('OutboxList', () => {
  it('offers cancel on every row going out and send first only once more than two wait', () => {
    render(OutboxList, {
      props: {
        now: Date.now(),
        rows: [
          row({ id: 1, state: 'sending', recipients: ['a@example.com'] }),
          row({ id: 2, recipients: ['b@example.com'] }),
          row({ id: 3, recipients: ['c@example.com'] }),
        ],
      },
    })

    const rows = sendingRows()
    expect(rows).toHaveLength(3)
    for (const r of rows) {
      expect(names(r)).toEqual(['Cancel send'])
    }
  })

  it('lets every waiting row but the first go first, in the order the worker takes them', () => {
    render(OutboxList, {
      props: {
        now: Date.now(),
        rows: [
          row({ id: 1, state: 'sending', recipients: ['a@example.com'] }),
          row({ id: 2, recipients: ['b@example.com'] }),
          row({ id: 3, recipients: ['c@example.com'] }),
          row({ id: 4, recipients: ['d@example.com'], priority: 2 }),
        ],
      },
    })

    const rows = sendingRows()
    expect(rows.map((r) => within(r).getByText(/@example\.com/).textContent)).toEqual([
      'a@example.com',
      'd@example.com',
      'b@example.com',
      'c@example.com',
    ])
    expect(names(rows[0])).toEqual(['Cancel send'])
    expect(names(rows[1])).toEqual(['Cancel send'])
    expect(names(rows[2])).toEqual(['Send first', 'Cancel send'])
    expect(names(rows[3])).toEqual(['Send first', 'Cancel send'])
  })

  it('keeps scheduled and failed rows to their own actions', () => {
    const later = new Date(Date.now() + 3 * 60 * 60 * 1000).toISOString()
    render(OutboxList, {
      props: {
        now: Date.now(),
        rows: [
          row({ id: 5, nextAttemptAt: later, editable: true }),
          row({ id: 6, state: 'failed', lastError: 'cancelled', recipients: ['f@example.com'] }),
        ],
      },
    })

    const scheduled = screen.getByRole('region', { name: 'Scheduled' })
    expect(names(scheduled)).toEqual(['Edit', 'Send now', 'Cancel scheduled send'])
    const failed = screen.getByRole('region', { name: 'Failed' })
    expect(failed).toHaveTextContent('Cancelled')
    expect(names(failed)).toEqual(['Try sending again', 'Discard'])
    expect(screen.queryByRole('region', { name: 'Sending' })).toBeNull()
  })

  it('moves a waiting message to the front', async () => {
    render(OutboxList, {
      props: {
        now: Date.now(),
        rows: [row({ id: 1 }), row({ id: 2 }), row({ id: 3 })],
      },
    })

    await userEvent.click(within(sendingRows()[2]).getByRole('button', { name: 'Send first' }))

    expect(api.prioritiseOutboxSend).toHaveBeenCalledWith(3)
  })

  it('opens a cancelled message in a composer when it could not go back to Drafts', async () => {
    const request = { accountId: 1, subject: 'kept' }
    api.cancelOutboxSend.mockResolvedValue({ outcome: 'compose', request })
    render(OutboxList, { props: { now: Date.now(), rows: [row({ id: 7, state: 'sending' })] } })

    await userEvent.click(screen.getByRole('button', { name: 'Cancel send' }))

    expect(api.cancelOutboxSend).toHaveBeenCalledWith(7)
    expect(compose.openRequestSession).toHaveBeenCalledWith(request)
  })

  it('says so when it is too late to cancel', async () => {
    api.cancelOutboxSend.mockResolvedValue({ outcome: 'tooLate', request: {} })
    render(OutboxList, { props: { now: Date.now(), rows: [row({ id: 8, state: 'sending' })] } })

    await userEvent.click(screen.getByRole('button', { name: 'Cancel send' }))

    expect(get(toasts).map((toast) => toast.message)).toContain('Too late to cancel, the server has already accepted the message.')
    expect(compose.openRequestSession).not.toHaveBeenCalled()
  })
})
