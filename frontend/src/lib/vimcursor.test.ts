import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  setSeen: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { vimCursorTo } from './vimcursor'
import { messageList } from '../stores/messages'
import { openMessageId } from '../stores/selection'
import { anchorAt, clearSelection, rangeCursor, selectedIds } from '../stores/listselect'
import { prefs } from '../stores/prefs'
import { ready } from './async'
import type { MessageSummary } from './types'

const message = (id: number, seen = false): MessageSummary => ({ id, seen, subject: `m${id}` }) as MessageSummary

let items: MessageSummary[]

function setDelay(seconds: number): void {
  prefs.update((p) => ({ ...p, markReadDelaySeconds: seconds }))
}

function selected(): number[] {
  return [...get(selectedIds)].sort((a, b) => a - b)
}

beforeEach(() => {
  vi.useFakeTimers()
  api.setSeen.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, markReadOnOpen: true, markReadDelaySeconds: 0 }))
  items = [message(1, true), message(2), message(3), message(4)]
  messageList.set(ready({ items, total: items.length, searching: false, hasOlder: false, backfilling: false }))
  clearSelection()
})

afterEach(() => {
  openMessageId.set(null)
  vi.clearAllMocks()
  vi.useRealTimers()
})

describe('vimCursorTo', () => {
  it('opens the row on a plain move, anchors there and drops the range cursor', () => {
    rangeCursor.set(4)
    vimCursorTo(items, 1, false)
    expect(get(openMessageId)).toBe(2)
    expect(get(rangeCursor)).toBeNull()
    vimCursorTo(items, 2, true)
    expect(selected()).toEqual([2, 3])
  })

  it('leaves the row a plain move opens unread while the delay is off', () => {
    vimCursorTo(items, 1, false)
    vi.advanceTimersByTime(10_000)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('marks the row a plain move opens read only once the delay is up', () => {
    setDelay(2)
    vimCursorTo(items, 1, false)
    vi.advanceTimersByTime(1999)
    expect(api.setSeen).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(api.setSeen.mock.calls).toEqual([[2, true]])
  })

  it('marks only the row a quick run of moves comes to rest on', () => {
    setDelay(2)
    for (let i = 0; i < items.length; i++) {
      vimCursorTo(items, i, false)
      vi.advanceTimersByTime(300)
    }
    vi.advanceTimersByTime(2000)
    expect(api.setSeen.mock.calls).toEqual([[4, true]])
  })

  it.each([0, 2])('selects the range on an extending move without opening or marking anything (delay %is)', (delay) => {
    setDelay(delay)
    openMessageId.set(1)
    anchorAt(1)
    vimCursorTo(items, 2, true)
    expect(get(rangeCursor)).toBe(3)
    expect(selected()).toEqual([1, 2, 3])
    expect(get(openMessageId)).toBe(1)
    vi.advanceTimersByTime(10_000)
    expect(api.setSeen).not.toHaveBeenCalled()
  })
})
