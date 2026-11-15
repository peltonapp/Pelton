import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  archiveMessage: vi.fn(),
  getThread: vi.fn(),
  restoreMessage: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))
vi.mock('../stores/sidebarcounts', () => ({ refreshCountsSoon: vi.fn() }))

import { archivable, conversationOf, doneTargets, markDone } from './triage'
import { leaveList, nextAfterLeaving } from './messageactions'
import { messageList } from '../stores/messages'
import { openMessageId } from '../stores/selection'
import { prefs } from '../stores/prefs'
import { invalidateThreads } from '../stores/threads'
import { triggerUndoArchive } from '../stores/undoarchive'
import { ready } from './async'
import type { MessageSummary, ThreadMessage, ThreadSummary } from './types'

function summary(id: number, over: Partial<MessageSummary> = {}): MessageSummary {
  return {
    id,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: `message ${id}`,
    fromName: 'Ada',
    fromAddress: 'ada@example.com',
    snippet: '',
    date: `2026-09-${String(30 - id).padStart(2, '0')}T12:00:00Z`,
    seen: false,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    threadId: 0,
    answered: false,
    forwarded: false,
    ...over,
  }
}

function member(id: number, threadId: number, folderRole: string): ThreadMessage {
  return { ...summary(id, { threadId }), folderRole, fromMe: folderRole === 'sent' }
}

function thread(head: MessageSummary, messageIds: number[]): ThreadSummary {
  return {
    threadId: head.threadId,
    head,
    count: messageIds.length,
    unreadCount: 0,
    participants: [],
    hasAttachments: false,
    flagged: false,
    answered: false,
    forwarded: false,
    messageIds,
  }
}

function load(items: MessageSummary[], threads?: Record<number, ThreadSummary>): void {
  messageList.set(ready({ items, total: items.length, searching: false, hasOlder: false, backfilling: false, threads }))
}

function ids(): number[] {
  return get(messageList).data?.items.map((m) => m.id) ?? []
}

const undo = (id: number) => ({ messageId: `<${id}@x>`, originalFolderId: 1, destFolderId: 9, exportPath: '', exportError: '' })

beforeEach(() => {
  prefs.update((p) => ({ ...p, design: 'new', threadedList: false, afterMove: 'older' }))
  api.archiveMessage.mockImplementation(async (id: number) => undo(id))
})

afterEach(() => {
  vi.clearAllMocks()
  openMessageId.set(null)
  invalidateThreads()
  while (triggerUndoArchive()) {
    // drain the undo stack between tests
  }
})

describe('archivable', () => {
  it('moves inbox and folder mail but leaves sent, drafts and what is already filed', () => {
    expect(archivable({ folderRole: 'inbox' })).toBe(true)
    expect(archivable({ folderRole: 'normal' })).toBe(true)
    for (const role of ['sent', 'drafts', 'archive', 'trash', 'junk']) {
      expect(archivable({ folderRole: role })).toBe(false)
    }
  })
})

describe('conversationOf', () => {
  it('finds a conversation by its head and by its thread', () => {
    const head = summary(1, { threadId: 50 })
    const threads = { 1: thread(head, [1, 2]) }
    expect(conversationOf(head, threads)?.threadId).toBe(50)
    expect(conversationOf(summary(2, { threadId: 50 }), threads)?.threadId).toBe(50)
  })

  it('has none in an ungrouped list or for a message without a thread', () => {
    expect(conversationOf(summary(1, { threadId: 50 }), undefined)).toBeNull()
    expect(conversationOf(summary(1), { 1: thread(summary(1), [1]) })).toBeNull()
  })
})

describe('doneTargets', () => {
  it('takes a single message as it is', async () => {
    const members = vi.fn()
    const out = await doneTargets([summary(1)], undefined, members)
    expect(out.map((m) => m.id)).toEqual([1])
    expect(members).not.toHaveBeenCalled()
  })

  it('expands a conversation to its members outside Sent and the filed folders', async () => {
    const head = summary(3, { threadId: 50 })
    const members = vi.fn(async () => [member(1, 50, 'inbox'), member(2, 50, 'sent'), member(3, 50, 'inbox'), member(4, 50, 'archive')])
    const out = await doneTargets([head], { 3: thread(head, [1, 2, 3, 4]) }, members)
    expect(out.map((m) => m.id)).toEqual([1, 3])
    expect(members).toHaveBeenCalledWith(50)
  })

  it('keeps the row itself when a conversation has nothing left to move', async () => {
    const head = summary(3, { threadId: 50 })
    const out = await doneTargets([head], { 3: thread(head, [2, 3]) }, async () => [member(2, 50, 'sent')])
    expect(out.map((m) => m.id)).toEqual([3])
  })

  it('lists a message reached twice once', async () => {
    const a = summary(3, { threadId: 50 })
    const b = summary(1, { threadId: 50 })
    const threads = { 3: thread(a, [1, 3]) }
    const out = await doneTargets([a, b], threads, async () => [member(1, 50, 'inbox'), member(3, 50, 'inbox')])
    expect(out.map((m) => m.id)).toEqual([1, 3])
  })
})

describe('nextAfterLeaving', () => {
  beforeEach(() => load([1, 2, 3, 4].map((id) => summary(id))))

  it('goes to the older message below by default and to the newer one above when asked', () => {
    expect(nextAfterLeaving(2, new Set([2]), false)).toBe(3)
    prefs.update((p) => ({ ...p, afterMove: 'newer' }))
    expect(nextAfterLeaving(2, new Set([2]), false)).toBe(1)
  })

  it('falls back to the other side at either end', () => {
    expect(nextAfterLeaving(4, new Set([4]), false)).toBe(3)
    prefs.update((p) => ({ ...p, afterMove: 'newer' }))
    expect(nextAfterLeaving(1, new Set([1]), false)).toBe(2)
  })

  it('goes back to the list when asked', () => {
    prefs.update((p) => ({ ...p, afterMove: 'list' }))
    expect(nextAfterLeaving(2, new Set([2]), false)).toBeNull()
  })

  it('reads older from the dates in a list sorted oldest first', () => {
    load([4, 3, 2, 1].map((id) => summary(id)))
    expect(nextAfterLeaving(3, new Set([3]), false)).toBe(4)
  })

  it('keeps the classic design as it was: a delete moves on, anything else closes', () => {
    prefs.update((p) => ({ ...p, design: 'classic', afterMove: 'newer' }))
    expect(nextAfterLeaving(2, new Set([2]), true)).toBe(3)
    expect(nextAfterLeaving(2, new Set([2]), false)).toBeNull()
  })
})

describe('leaveList', () => {
  it('moves the reader past the rest of a batch', () => {
    load([1, 2, 3, 4].map((id) => summary(id)))
    openMessageId.set(2)
    leaveList([2], new Set([2, 3]))
    expect(ids()).toEqual([1, 3, 4])
    expect(get(openMessageId)).toBe(4)
  })

  it('leaves the reader alone when it shows something else', () => {
    load([1, 2, 3].map((id) => summary(id)))
    openMessageId.set(3)
    leaveList([1])
    expect(get(openMessageId)).toBe(3)
  })
})

describe('markDone', () => {
  it('archives the open message and opens the next one at once', async () => {
    load([1, 2, 3].map((id) => summary(id)))
    openMessageId.set(1)
    const done = markDone([summary(1)])
    await vi.waitFor(() => expect(get(openMessageId)).toBe(2))
    await done
    expect(ids()).toEqual([2, 3])
    expect(api.archiveMessage).toHaveBeenCalledWith(1)
  })

  it('archives a whole conversation and records it as one undo step', async () => {
    prefs.update((p) => ({ ...p, threadedList: true }))
    const head = summary(1, { threadId: 50 })
    load([head, summary(2, { threadId: 60 })], { 1: thread(head, [1, 5, 6]) })
    api.getThread.mockResolvedValue({ threadId: 50, messages: [member(5, 50, 'inbox'), member(6, 50, 'sent'), member(1, 50, 'inbox')] })
    openMessageId.set(6)

    await markDone([head])

    expect(api.archiveMessage.mock.calls.map((c) => c[0])).toEqual([5, 1])
    expect(ids()).toEqual([2])
    expect(get(openMessageId)).toBe(2)
    expect(triggerUndoArchive()).toBe(true)
    await vi.waitFor(() => expect(api.restoreMessage).toHaveBeenCalledTimes(2))
  })

  it('goes back to the list when that is the choice, leaving the list empty', async () => {
    prefs.update((p) => ({ ...p, afterMove: 'list' }))
    load([summary(1)])
    openMessageId.set(1)
    await markDone([summary(1)])
    expect(ids()).toEqual([])
    expect(get(openMessageId)).toBeNull()
  })

  it('puts a row back whose move failed', async () => {
    load([1, 2].map((id) => summary(id)))
    api.archiveMessage.mockRejectedValueOnce(new Error('offline'))
    await markDone([summary(1)])
    expect(ids()).toContain(1)
  })
})
