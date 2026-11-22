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

// The same undo stack serves archive and plain moves to any folder, so the
// confirmation must not name the archive: undoing a move out of Trash would
// otherwise say it came back from Archive.
describe('undo confirmation copy', () => {
  const catalogs = import.meta.glob<{ default: Record<string, string> }>('../lib/locales/*.ts', { eager: true })

  it('never names the archive folder, in any language', () => {
    const entries = Object.entries(catalogs)
    expect(entries.length).toBe(12)
    for (const [file, { default: cat }] of entries) {
      // a stem of the localized folder name catches inflected forms
      // ("Arşiv'den", "l'archive"); Arabic drops its article first.
      const stem = cat['folders.role.archive'].replace(/^ال/, '').toLowerCase().slice(0, 4)
      for (const key of ['undo.archiveDone', 'undo.archiveDoneCount']) {
        expect(cat[key].toLowerCase(), `${file} ${key}`).not.toContain(stem)
      }
    }
  })
  // i18n has no plural forms, and an Arabic noun after a number changes form
  // with it (dual for 2, plural for 3 to 10, singular from 11), so a count
  // must sit after a label where any n reads right.
  it('puts the Arabic counts where any number reads right', () => {
    const ar = Object.entries(catalogs).find(([file]) => file.endsWith('/ar.ts'))![1].default
    for (const key of ['undo.archiveDoneCount', 'undo.deleteDoneCount']) {
      expect(ar[key], key).toMatch(/:\s*\{n\}\.$/)
    }
  })
})
