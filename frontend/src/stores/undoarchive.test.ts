import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MessageSummary } from '../lib/types'

vi.mock('../lib/api', () => ({ restoreMessage: vi.fn().mockResolvedValue(undefined) }))

import { restoreMessage } from '../lib/api'
import { recordArchived, recordArchivedBatch } from './undoarchive'
import { clearHistory, undoLatest } from './undohistory'

const summary = { id: 1, subject: 's' } as MessageSummary

describe('undo of a move', () => {
  beforeEach(() => {
    vi.mocked(restoreMessage).mockClear()
    clearHistory()
  })

  it('looks for the message in the folder the action put it in', async () => {
    recordArchived(summary, { messageId: '<a@x>', destFolderId: 9, originalFolderId: 2, exportPath: '', exportError: '' })

    expect(await undoLatest()).toBe(true)

    expect(restoreMessage).toHaveBeenCalledWith('<a@x>', 9, 2)
  })

  it('undoes a batch from the last move to the first', async () => {
    recordArchivedBatch([
      { summary, messageId: '<dup@x>', destFolderId: 9, originalFolderId: 2 },
      { summary: { ...summary, id: 2 }, messageId: '<dup@x>', destFolderId: 9, originalFolderId: 3 },
    ])

    expect(await undoLatest()).toBe(true)

    expect(vi.mocked(restoreMessage).mock.calls).toEqual([
      ['<dup@x>', 9, 3],
      ['<dup@x>', 9, 2],
    ])
  })

  it('has nothing to undo for a message with no Message-ID', async () => {
    recordArchived(summary, { messageId: '', destFolderId: 9, originalFolderId: 2, exportPath: '', exportError: '' })

    expect(await undoLatest()).toBe(false)
  })
})
