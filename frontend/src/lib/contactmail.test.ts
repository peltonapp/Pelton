import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MessageSummary } from './types'

const api = vi.hoisted(() => ({ search: vi.fn() }))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  search: api.search,
}))

import { recentMailWith } from './contactmail'

function message(id: number, date: string): MessageSummary {
  return { id, date, subject: `m${id}` } as MessageSummary
}

beforeEach(() => {
  api.search.mockReset()
})

describe('recentMailWith', () => {
  it('merges sent and received mail, newest first, each message once', async () => {
    api.search.mockImplementation(async (req: { from: string; to: string }) =>
      req.from
        ? { messages: [message(1, '2026-01-01T00:00:00Z'), message(2, '2026-03-01T00:00:00Z')], total: 2 }
        : { messages: [message(2, '2026-03-01T00:00:00Z'), message(3, '2026-02-01T00:00:00Z')], total: 2 },
    )
    const out = await recentMailWith(['ada@example.com'], 5)
    expect(out.map((m) => m.id)).toEqual([2, 3, 1])
    expect(api.search).toHaveBeenCalledWith(expect.objectContaining({ from: 'ada@example.com', to: '' }))
    expect(api.search).toHaveBeenCalledWith(expect.objectContaining({ from: '', to: 'ada@example.com' }))
  })

  it('cuts the result to the limit', async () => {
    api.search.mockResolvedValue({
      messages: [message(1, '2026-01-01T00:00:00Z'), message(2, '2026-02-01T00:00:00Z'), message(3, '2026-03-01T00:00:00Z')],
      total: 3,
    })
    expect((await recentMailWith(['a@b.c'], 2)).map((m) => m.id)).toEqual([3, 2])
  })

  it('treats a failing search as no mail and skips blank addresses', async () => {
    api.search.mockRejectedValue(new Error('search is not available'))
    expect(await recentMailWith(['a@b.c'], 5)).toEqual([])
    api.search.mockClear()
    expect(await recentMailWith(['  '], 5)).toEqual([])
    expect(api.search).not.toHaveBeenCalled()
  })
})
