import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  setSeen: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { markOpened } from './messageactions'
import { messageList } from '../stores/messages'
import { prefs } from '../stores/prefs'
import { ready } from './async'
import type { MessageSummary } from './types'

const message = (id: number, seen = false): MessageSummary => ({ id, seen, subject: `m${id}` }) as MessageSummary

// list puts rows in the message list, so a patch has a row to land on.
function list(...items: MessageSummary[]): void {
  messageList.set(ready({ items, total: items.length, searching: false, hasOlder: false, backfilling: false }))
}

function seenInList(id: number): boolean | undefined {
  return get(messageList).data?.items.find((m) => m.id === id)?.seen
}

beforeEach(() => {
  api.setSeen.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, markReadOnOpen: true }))
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('markOpened', () => {
  it('marks an unread message read', () => {
    const a = message(1)
    list(a, message(2))
    markOpened(a)
    expect(seenInList(1)).toBe(true)
    expect(seenInList(2)).toBe(false)
    expect(api.setSeen.mock.calls).toEqual([[1, true]])
  })

  it('leaves the message unread with marking on open turned off', () => {
    prefs.update((p) => ({ ...p, markReadOnOpen: false }))
    const a = message(1)
    list(a)
    markOpened(a)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('does not touch a message that is already read', () => {
    const a = message(1, true)
    list(a)
    markOpened(a)
    expect(api.setSeen).not.toHaveBeenCalled()
  })
})
