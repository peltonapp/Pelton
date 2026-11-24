import { afterEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  listFollowUps: vi.fn(),
  cancelFollowUp: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import { bareId, dropFollowUp, dueFollowUps, followUpFor, followUps, followUpsIn, loadFollowUps, pendingFollowUps } from './followups'
import type { FollowUp } from '../lib/types'

function followUp(id: number, over: Partial<FollowUp> = {}): FollowUp {
  return {
    id,
    accountId: 1,
    messageId: `m${id}@me`,
    subject: `subject ${id}`,
    recipients: 'Ada',
    sentAt: '2026-11-10T09:00:00Z',
    dueAt: '2026-11-13T09:00:00Z',
    firedAt: '',
    localId: 0,
    folderId: 0,
    threadId: 0,
    ...over,
  }
}

afterEach(() => {
  vi.clearAllMocks()
  followUps.set([])
})

describe('followUps', () => {
  it('splits waiting reminders from fired ones, the latest fired first', () => {
    followUps.set([
      followUp(1),
      followUp(2, { firedAt: '2026-11-13T09:00:00Z' }),
      followUp(3, { firedAt: '2026-11-14T09:00:00Z' }),
    ])
    expect(get(pendingFollowUps).map((f) => f.id)).toEqual([1])
    expect(get(dueFollowUps).map((f) => f.id)).toEqual([3, 2])
  })

  it('loads the list from the backend', async () => {
    api.listFollowUps.mockResolvedValue([followUp(4)])
    await loadFollowUps()
    expect(get(followUps).map((f) => f.id)).toEqual([4])
  })

  it('keeps the last list when a load fails', async () => {
    followUps.set([followUp(1)])
    api.listFollowUps.mockRejectedValue(new Error('down'))
    await loadFollowUps()
    expect(get(followUps).map((f) => f.id)).toEqual([1])
  })

  it('drops a reminder at once and puts it back when the backend refuses', async () => {
    followUps.set([followUp(1), followUp(2)])
    api.cancelFollowUp.mockResolvedValueOnce(undefined)
    await dropFollowUp(1)
    expect(api.cancelFollowUp).toHaveBeenCalledWith(1)
    expect(get(followUps).map((f) => f.id)).toEqual([2])

    api.cancelFollowUp.mockRejectedValueOnce(new Error('gone'))
    await dropFollowUp(2)
    expect(get(followUps).map((f) => f.id)).toEqual([2])
  })
})

describe('followUpFor', () => {
  it('finds a reminder by Message-ID with or without brackets', () => {
    const list = [followUp(1), followUp(2)]
    expect(followUpFor(list, '<m2@me>')?.id).toBe(2)
    expect(followUpFor(list, ' m1@me ')?.id).toBe(1)
    expect(followUpFor(list, 'other@me')).toBeNull()
    expect(followUpFor(list, '')).toBeNull()
  })

  it('strips brackets and space', () => {
    expect(bareId(' <a@b> ')).toBe('a@b')
  })
})

describe('followUpsIn', () => {
  it('keeps every account for the unified inbox and one for an account inbox', () => {
    const list = [followUp(1, { accountId: 1 }), followUp(2, { accountId: 2 })]
    expect(followUpsIn(list, 0).map((f) => f.id)).toEqual([1, 2])
    expect(followUpsIn(list, 2).map((f) => f.id)).toEqual([2])
  })
})
