import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({ cancelFollowUp: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import FollowUpBand from './FollowUpBand.svelte'
import { followUps } from '../../stores/followups'
import { openMessageId } from '../../stores/selection'
import type { FollowUp } from '../../lib/types'

function fired(id: number, over: Partial<FollowUp> = {}): FollowUp {
  return {
    id,
    accountId: 1,
    messageId: `m${id}@me`,
    subject: `Quote ${id}`,
    recipients: 'Ada Lovelace',
    sentAt: '2026-11-10T09:00:00Z',
    dueAt: '2026-11-13T09:00:00Z',
    firedAt: '2026-11-13T09:00:30Z',
    localId: 40 + id,
    folderId: 2,
    threadId: 7,
    ...over,
  }
}

afterEach(() => {
  vi.clearAllMocks()
  followUps.set([])
  openMessageId.set(null)
})

describe('FollowUpBand', () => {
  it('shows nothing without reminders', () => {
    render(FollowUpBand, { items: [] })
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })

  it('labels each reminder and says who has not replied', () => {
    render(FollowUpBand, { items: [fired(1)] })
    expect(screen.getByRole('list', { name: 'Follow-ups' })).toBeInTheDocument()
    expect(screen.getByText('Follow up')).toBeInTheDocument()
    expect(screen.getByText('Quote 1')).toBeInTheDocument()
    expect(screen.getByText('No reply from Ada Lovelace')).toBeInTheDocument()
  })

  it('opens the sent message in the reader', async () => {
    render(FollowUpBand, { items: [fired(1)] })
    await userEvent.click(screen.getByRole('button', { name: /Quote 1/ }))
    expect(get(openMessageId)).toBe(41)
  })

  it('cannot open a message that has not synced yet', () => {
    render(FollowUpBand, { items: [fired(1, { localId: 0 })] })
    expect(screen.getByRole('button', { name: /Quote 1/ })).toBeDisabled()
  })

  it('dismisses a reminder', async () => {
    api.cancelFollowUp.mockResolvedValue(undefined)
    followUps.set([fired(1)])
    render(FollowUpBand, { items: [fired(1)] })
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(api.cancelFollowUp).toHaveBeenCalledWith(1)
    expect(get(followUps)).toEqual([])
  })
})
