import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MessageSummary } from '../lib/types'

vi.mock('../lib/api', () => ({ unarchiveMessage: vi.fn().mockResolvedValue(undefined) }))

import { unarchiveMessage } from '../lib/api'
import { recordArchived, triggerUndoArchive } from './undoarchive'

const summary = { id: 1 } as MessageSummary

describe('undo of a move', () => {
  beforeEach(() => {
    vi.mocked(unarchiveMessage).mockClear()
  })

  it('looks for the message in the folder the action put it in', () => {
    recordArchived(summary, { messageId: '<a@x>', destFolderId: 9, originalFolderId: 2, exportPath: '', exportError: '' })

    expect(triggerUndoArchive()).toBe(true)

    expect(unarchiveMessage).toHaveBeenCalledWith('<a@x>', 9, 2)
  })

  it('has nothing to undo for a message with no Message-ID', () => {
    recordArchived(summary, { messageId: '', destFolderId: 9, originalFolderId: 2, exportPath: '', exportError: '' })

    expect(triggerUndoArchive()).toBe(false)
  })
})
