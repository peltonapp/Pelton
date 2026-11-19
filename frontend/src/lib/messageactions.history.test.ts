import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  setSeen: vi.fn(),
  setFlagged: vi.fn(),
  setFlagColor: vi.fn(),
  deleteMessage: vi.fn(),
  undoDelete: vi.fn(),
  archiveMessage: vi.fn(),
  moveMessage: vi.fn(),
  restoreMessage: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { archive, bulkMarkFlagged, bulkMarkSeen, markColor, markFlagged, markSeen, moveToFolder, trashMessage } from './messageactions'
import { clearHistory, history, redoLatest, undoLatest } from '../stores/undohistory'
import { messageList } from '../stores/messages'
import { toasts } from '../stores/toast'
import { ready } from './async'
import type { Folder, MessageSummary } from './types'

const message = (id: number, patch: Partial<MessageSummary> = {}): MessageSummary =>
  ({ id, accountId: 1, folderId: 1, subject: `s${id}`, seen: false, flagged: false, flagColor: 0, threadId: 0, ...patch }) as MessageSummary

function load(items: MessageSummary[]): void {
  messageList.set(ready({ items, total: items.length, searching: false } as never))
}

const rows = () => (get(messageList).data?.items ?? []) as MessageSummary[]

beforeEach(() => {
  clearHistory()
  for (const fn of Object.values(api)) {
    fn.mockReset().mockResolvedValue(undefined)
  }
})

afterEach(() => {
  messageList.set({ status: 'idle', data: null } as never)
})

describe('read, flag and color changes', () => {
  it('records a read mark and undoes and redoes it on the list', async () => {
    load([message(1)])
    await markSeen(message(1), true)
    expect(rows()[0].seen).toBe(true)
    expect(get(history)[0]).toMatchObject({ kind: 'seen', label: 'Marked read', detail: 's1', count: 1 })

    await undoLatest()
    expect(api.setSeen).toHaveBeenLastCalledWith(1, false)
    expect(rows()[0].seen).toBe(false)

    await redoLatest()
    expect(api.setSeen).toHaveBeenLastCalledWith(1, true)
    expect(rows()[0].seen).toBe(true)
  })

  it('records nothing when the value did not change', async () => {
    await markSeen(message(1, { seen: true }), true)
    expect(get(history)).toHaveLength(0)
  })

  it('records a failed change as nothing and puts the row back', async () => {
    load([message(1)])
    api.setFlagged.mockRejectedValue(new Error('offline'))
    await markFlagged(message(1), true)
    expect(rows()[0].flagged).toBe(false)
    expect(get(history)).toHaveLength(0)
  })

  it('makes one entry of a bulk change and restores each message to its own value', async () => {
    load([message(1, { seen: true }), message(2), message(3)])
    await bulkMarkSeen([message(1, { seen: true }), message(2), message(3)], true)
    const [entry] = get(history)
    expect(get(history)).toHaveLength(1)
    expect(entry).toMatchObject({ count: 2, detail: '2 messages' })

    await undoLatest()
    expect(api.setSeen).toHaveBeenCalledWith(2, false)
    expect(api.setSeen).toHaveBeenCalledWith(3, false)
    expect(api.setSeen).not.toHaveBeenLastCalledWith(1, false)
  })

  it('puts back each message color on undo', async () => {
    load([message(1, { flagColor: 2 }), message(2, { flagColor: 5 })])
    await markColor(message(1, { flagColor: 2 }), 0)
    expect(get(history)[0].label).toBe('Cleared color label')
    await undoLatest()
    expect(api.setFlagColor).toHaveBeenLastCalledWith(1, 2)
  })

  it('blocks the earlier of two flag changes on the same message until the later is undone', async () => {
    load([message(1)])
    await markFlagged(message(1), true)
    await markFlagged(message(1, { flagged: true }), false)
    const [later, earlier] = get(history)
    expect(later.canUndo).toBe(true)
    expect(earlier).toMatchObject({ canUndo: false, reason: 'later' })
    await undoLatest()
    expect(get(history).find((r) => r.id === earlier.id)?.canUndo).toBe(true)
  })

  it('bulk flag changes on other messages stay independent', async () => {
    load([message(1), message(2)])
    await markFlagged(message(1), true)
    await bulkMarkFlagged([message(2)], true)
    expect(get(history).every((r) => r.canUndo)).toBe(true)
  })
})

describe('moves and deletes', () => {
  const undo = { messageId: '<a@x>', originalFolderId: 1, destFolderId: 9, exportPath: '', exportError: '' }

  it('records an archive and reverses it by Message-ID, then replays it', async () => {
    load([message(1)])
    api.archiveMessage.mockResolvedValue(undo)
    await archive(message(1))
    expect(get(history)[0]).toMatchObject({ kind: 'archive', label: 'Archived', detail: 's1' })

    await undoLatest()
    expect(api.restoreMessage).toHaveBeenLastCalledWith('<a@x>', 9, 1)
    await redoLatest()
    expect(api.restoreMessage).toHaveBeenLastCalledWith('<a@x>', 1, 9)
  })

  it('does not record a move that has no Message-ID to find it by', async () => {
    api.archiveMessage.mockResolvedValue({ ...undo, messageId: '' })
    await archive(message(1))
    expect(get(history)).toHaveLength(0)
  })

  it('names the folder a message was moved to', async () => {
    api.moveMessage.mockResolvedValue(undo)
    await moveToFolder([message(1), message(2)], { id: 9, accountId: 1, name: 'Receipts', role: '' } as Folder)
    expect(get(history)[0]).toMatchObject({ kind: 'move', label: 'Moved to Receipts', count: 2 })
  })

  it('offers the move back from its toast', async () => {
    api.moveMessage.mockResolvedValue(undo)
    toasts.set([])
    await moveToFolder([message(1)], { id: 9, accountId: 1, name: 'Receipts', role: '' } as Folder)
    const toast = get(toasts).at(-1)
    expect(toast?.action?.label).toBe('Undo')
    toast?.action?.run()
    await vi.waitFor(() => expect(api.restoreMessage).toHaveBeenCalled())
    await vi.waitFor(() => expect(get(history)[0].state).toBe('undone'))
  })

  it('brings a deleted message back into the list and deletes it again on redo', async () => {
    load([message(1)])
    await trashMessage(message(1))
    expect(rows()).toHaveLength(0)
    expect(get(history)[0]).toMatchObject({ kind: 'delete', label: 'Deleted' })

    await undoLatest()
    expect(api.undoDelete).toHaveBeenCalledWith(1)
    expect(rows().map((m) => m.id)).toEqual([1])

    await redoLatest()
    expect(api.deleteMessage).toHaveBeenCalledTimes(2)
    expect(rows()).toHaveLength(0)
  })

  it('reports a delete that can no longer be undone and marks it dead', async () => {
    await trashMessage(message(1))
    api.undoDelete.mockRejectedValue(new Error('already expunged'))
    expect(await undoLatest()).toBe(false)
    expect(get(history)[0]).toMatchObject({ state: 'failed', canUndo: false })
  })

  it('disables the earlier flag once the message was archived', async () => {
    load([message(1)])
    api.archiveMessage.mockResolvedValue(undo)
    await markFlagged(message(1), true)
    await archive(message(1))
    const [, flag] = get(history)
    expect(flag).toMatchObject({ canUndo: false, reason: 'later' })
  })
})
