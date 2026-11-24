import { describe, expect, it, beforeEach, vi } from 'vitest'
import { get } from 'svelte/store'

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  setSetting: vi.fn().mockResolvedValue(undefined),
}))

import { attachmentsOpen, attachmentFilter, openAttachments, emptyAttachmentFilter } from './attachments'
import { selection, selectView, selectFolder } from './selection'
import type { Folder } from '../lib/types'

describe('attachments browser store', () => {
  beforeEach(() => {
    attachmentsOpen.set(false)
    attachmentFilter.set(emptyAttachmentFilter())
  })

  it('opens with a filter and keeps the rest empty', () => {
    openAttachments({ sender: 'ann@example.com' })
    expect(get(attachmentsOpen)).toBe(true)
    expect(get(attachmentFilter)).toEqual({ ...emptyAttachmentFilter(), sender: 'ann@example.com' })
  })

  it('closes when a view or folder is chosen, the current one included', () => {
    openAttachments()
    selectView('inbox', 'Unified Inbox')
    expect(get(attachmentsOpen)).toBe(false)

    openAttachments()
    selectView('inbox', 'Unified Inbox')
    expect(get(attachmentsOpen)).toBe(false)

    openAttachments()
    selectFolder({ id: 4, accountId: 1, name: 'Work' } as Folder)
    expect(get(attachmentsOpen)).toBe(false)
  })

  it('stays open when only the label of the selection changes', () => {
    selectView('inbox', 'Unified Inbox')
    openAttachments()
    selection.update((s) => ({ ...s, label: 'Posteingang' }))
    expect(get(attachmentsOpen)).toBe(true)
  })
})
