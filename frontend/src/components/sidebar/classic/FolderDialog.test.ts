import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'

const api = vi.hoisted(() => ({
  renameFolder: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

vi.mock('../../../stores/accounts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../stores/accounts')>()),
  refreshSidebar: vi.fn().mockResolvedValue(undefined),
}))

import ClassicFolderDialog from './FolderDialog.svelte'
import FolderDialog from '../FolderDialog.svelte'
import { closeFolderDialog, openRenameFolder } from '../../../stores/folderdialog'
import { prefs } from '../../../stores/prefs'
import type { Folder } from '../../../lib/types'

// the dialog opens with a transition, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const receipts = { id: 3, accountId: 1, name: 'Receipts' } as Folder

beforeEach(() => {
  api.renameFolder.mockResolvedValue(undefined)
})

afterEach(() => {
  closeFolderDialog()
  prefs.update((p) => ({ ...p, design: 'new' }))
  vi.clearAllMocks()
})

describe('folder dialog in the classic design', () => {
  beforeEach(() => prefs.update((p) => ({ ...p, design: 'classic' })))

  it('keeps its own frame with cancel before save', async () => {
    render(ClassicFolderDialog)
    openRenameFolder(receipts)
    await tick()

    const dialog = screen.getByRole('dialog', { name: 'Rename folder' })
    expect(dialog).toHaveClass('dialog')
    const actions = Array.from(dialog.querySelectorAll('.actions button')).map((b) => b.textContent?.trim())
    expect(actions).toEqual(['Cancel', 'Save'])
    expect(dialog.querySelector('form')).toBeNull()
  })

  it('submits on Enter from the name field', async () => {
    render(ClassicFolderDialog)
    openRenameFolder(receipts)
    await tick()

    const input = screen.getByRole('textbox')
    await fireEvent.input(input, { target: { value: 'Bills' } })
    await fireEvent.keyDown(input, { key: 'Enter' })

    await vi.waitFor(() => expect(api.renameFolder).toHaveBeenCalledWith(3, 'Bills'))
  })

  it('closes on Escape without asking, even after an edit', async () => {
    render(ClassicFolderDialog)
    openRenameFolder(receipts)
    await tick()

    await fireEvent.input(screen.getByRole('textbox'), { target: { value: 'Bills' } })
    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

describe('folder dialog in the new design', () => {
  it('asks before discarding an edit on Escape', async () => {
    render(FolderDialog)
    openRenameFolder(receipts)
    await tick()

    await fireEvent.input(screen.getByRole('textbox'), { target: { value: 'Bills' } })
    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()

    expect(screen.getByRole('dialog', { name: 'Rename folder' })).toBeInTheDocument()
    expect(screen.getByRole('dialog').querySelector('form')).not.toBeNull()
  })
})
