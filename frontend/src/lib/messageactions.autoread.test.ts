import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  setSeen: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { autoMarkRead, markSeen } from './messageactions'
import { messageList, patchInList } from '../stores/messages'
import { openMessageId } from '../stores/selection'
import { activeTabId } from '../stores/tabs'
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

// open makes a message the open one and hands it over, as a vim motion does.
function open(item: MessageSummary): void {
  openMessageId.set(item.id)
  autoMarkRead(item)
}

function setDelay(seconds: number): void {
  prefs.update((p) => ({ ...p, markReadDelaySeconds: seconds }))
}

beforeEach(() => {
  vi.useFakeTimers()
  api.setSeen.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, markReadOnOpen: true, markReadDelaySeconds: 0 }))
})

afterEach(() => {
  activeTabId.set(null)
  openMessageId.set(null)
  vi.clearAllMocks()
  vi.useRealTimers()
})

describe('autoMarkRead', () => {
  it('never marks while the delay is off', () => {
    const a = message(1)
    list(a)
    open(a)
    vi.advanceTimersByTime(10_000)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('never marks with marking on open turned off', () => {
    prefs.update((p) => ({ ...p, markReadOnOpen: false }))
    setDelay(2)
    const a = message(1)
    list(a)
    open(a)
    vi.advanceTimersByTime(10_000)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('leaves a message that is already read alone', () => {
    setDelay(2)
    const a = message(1, true)
    list(a)
    open(a)
    vi.advanceTimersByTime(10_000)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('marks only once the delay is up while the message stays open', () => {
    setDelay(2)
    const a = message(1)
    list(a)
    open(a)
    vi.advanceTimersByTime(1999)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(seenInList(1)).toBe(true)
    expect(api.setSeen.mock.calls).toEqual([[1, true]])
  })

  it.each([
    { name: 'another message', next: 2 },
    { name: 'nothing', next: null },
  ])('drops the mark when the open message changes to $name before the delay is up', ({ next }) => {
    setDelay(2)
    const a = message(1)
    list(a, message(2))
    open(a)
    vi.advanceTimersByTime(1000)
    openMessageId.set(next)
    vi.advanceTimersByTime(10_000)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('marks only the second of two opens, after its own full delay', () => {
    setDelay(2)
    const a = message(1)
    const b = message(2)
    list(a, b)
    open(a)
    vi.advanceTimersByTime(1500)
    open(b)
    vi.advanceTimersByTime(1999)
    expect(api.setSeen).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(api.setSeen.mock.calls).toEqual([[2, true]])
    expect(seenInList(1)).toBe(false)
  })

  it('does not mark a message that was left and reopened without a new open', () => {
    setDelay(2)
    const a = message(1)
    list(a, message(2))
    open(a)
    vi.advanceTimersByTime(500)
    openMessageId.set(2)
    openMessageId.set(1)
    vi.advanceTimersByTime(10_000)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('keeps a message unread that was marked unread during the wait', () => {
    setDelay(2)
    const a = message(1)
    list(a)
    open(a)
    vi.advanceTimersByTime(1000)
    void markSeen(a, false)
    vi.advanceTimersByTime(10_000)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen.mock.calls).toEqual([[1, false]])
  })

  it('drops the mark when a tab takes over the reading pane', () => {
    setDelay(2)
    const a = message(1)
    list(a, message(2))
    open(a)
    vi.advanceTimersByTime(1000)
    activeTabId.set(2)
    vi.advanceTimersByTime(10_000)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('does not restart the wait when the open message is opened again', () => {
    setDelay(2)
    const a = message(1)
    list(a)
    open(a)
    vi.advanceTimersByTime(1500)
    open(a)
    vi.advanceTimersByTime(500)
    expect(api.setSeen.mock.calls).toEqual([[1, true]])
    vi.advanceTimersByTime(10_000)
    expect(api.setSeen.mock.calls).toEqual([[1, true]])
  })

  it.each([
    { name: 'marking on open', off: { markReadOnOpen: false } },
    { name: 'the delay', off: { markReadDelaySeconds: 0 } },
  ])('does not mark when $name is turned off during the wait', ({ off }) => {
    setDelay(2)
    const a = message(1)
    list(a)
    open(a)
    vi.advanceTimersByTime(1000)
    prefs.update((p) => ({ ...p, ...off }))
    vi.advanceTimersByTime(10_000)
    expect(seenInList(1)).toBe(false)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('does not mark again a message that something else marked read during the wait', () => {
    setDelay(2)
    const a = message(1)
    list(a)
    open(a)
    vi.advanceTimersByTime(1000)
    patchInList(1, { seen: true })
    vi.advanceTimersByTime(10_000)
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('waits for a message handed over just before it becomes the open one', () => {
    setDelay(2)
    const a = message(1)
    list(a)
    autoMarkRead(a)
    openMessageId.set(1)
    vi.advanceTimersByTime(2000)
    expect(api.setSeen.mock.calls).toEqual([[1, true]])
  })

  it('does not mark a message that never became the open one', () => {
    setDelay(2)
    const a = message(1)
    list(a)
    autoMarkRead(a)
    vi.advanceTimersByTime(10_000)
    expect(api.setSeen).not.toHaveBeenCalled()
  })
})
