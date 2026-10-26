import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  markSpam: vi.fn(),
  markNotSpam: vi.fn(),
  restoreMessage: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { spamState, toggleSpam } from './messageactions'
import { sidebar } from '../stores/accounts'
import { openMessageId } from '../stores/selection'
import { triggerUndoArchive } from '../stores/undoarchive'
import { ready } from './async'
import type { Folder, MessageSummary } from './types'

const folder = (id: number, accountId: number, role: string): Folder => ({ id, accountId, name: role, role }) as Folder
const message = (folderId: number, accountId = 1): MessageSummary => ({ id: 7, accountId, folderId, subject: 'hi' }) as MessageSummary

beforeEach(() => {
  sidebar.set(
    ready({
      accounts: [],
      foldersByAccount: {
        1: [folder(1, 1, 'inbox'), folder(2, 1, 'normal'), folder(3, 1, 'junk')],
        2: [folder(10, 2, 'inbox')],
      },
      views: [],
      pinned: [],
    }),
  )
})

afterEach(() => {
  vi.clearAllMocks()
  openMessageId.set(null)
})

describe('spamState', () => {
  it('knows a message in the spam folder', () => {
    expect(spamState(message(3))).toBe('spam')
  })

  it('offers the mark anywhere else in an account with a spam folder', () => {
    expect(spamState(message(1))).toBe('markable')
    expect(spamState(message(2))).toBe('markable')
  })

  it('offers nothing for an account with no spam folder', () => {
    expect(spamState(message(10, 2))).toBe('none')
  })
})

describe('toggleSpam', () => {
  it('marks a message outside the spam folder and records an undo that looks in the spam folder', async () => {
    api.markSpam.mockResolvedValue({ messageId: '<a@x>', originalFolderId: 2, destFolderId: 3, exportPath: '', exportError: '' })
    openMessageId.set(7)

    expect(await toggleSpam(message(2))).toBe(true)
    expect(api.markSpam).toHaveBeenCalledWith(7)
    expect(api.markNotSpam).not.toHaveBeenCalled()
    expect(get(openMessageId)).toBeNull()

    expect(triggerUndoArchive()).toBe(true)
    await vi.waitFor(() => expect(api.restoreMessage).toHaveBeenCalledWith('<a@x>', 3, 2))
  })

  it('rescues a message already in the spam folder', async () => {
    api.markNotSpam.mockResolvedValue({ messageId: '', originalFolderId: 3, destFolderId: 1, exportPath: '', exportError: '' })

    expect(await toggleSpam(message(3))).toBe(true)
    expect(api.markNotSpam).toHaveBeenCalledWith(7)
    expect(api.markSpam).not.toHaveBeenCalled()
  })

  it('reports a failed move without closing the message', async () => {
    api.markSpam.mockRejectedValue(new Error('over quota'))
    openMessageId.set(7)

    expect(await toggleSpam(message(1))).toBe(false)
    expect(get(openMessageId)).toBe(7)
  })

  it('does nothing for an account with no spam folder', async () => {
    expect(await toggleSpam(message(10, 2))).toBe(false)
    expect(api.markSpam).not.toHaveBeenCalled()
  })
})
