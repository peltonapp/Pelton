import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'

const api = vi.hoisted(() => ({
  listFolders: vi.fn(),
  moveMessage: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import MoveDialog from './MoveDialog.svelte'
import { closeMove, openMove } from '../../stores/move'
import type { Folder, MessageSummary } from '../../lib/types'

// the dialog opens with a transition, and jsdom has no web animations. A
// finished stub lets svelte run the transition to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const folder = (id: number, name: string): Folder => ({ id, accountId: 1, name }) as Folder
const message = { id: 7, accountId: 1, folderId: 1, subject: 'hi' } as MessageSummary

beforeEach(() => {
  api.listFolders.mockResolvedValue([folder(1, 'INBOX'), folder(2, 'Archive'), folder(3, 'Receipts')])
  api.moveMessage.mockResolvedValue({ messageId: 70, originalFolderId: 1 })
})

afterEach(() => {
  closeMove()
  vi.clearAllMocks()
})

async function open(): Promise<void> {
  render(MoveDialog)
  openMove(message)
  await tick()
  await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Archive' })).toBeTruthy())
}

describe('move dialog from the keyboard (#494)', () => {
  it('puts focus in the search box when it opens', async () => {
    await open()
    expect(document.activeElement).toBe(screen.getByRole('searchbox'))
  })

  it('moves to the top match on Enter', async () => {
    await open()
    const search = screen.getByRole('searchbox')
    await fireEvent.input(search, { target: { value: 'rec' } })
    await fireEvent.keyDown(search, { key: 'Enter' })

    await vi.waitFor(() => expect(api.moveMessage).toHaveBeenCalledWith(7, 3))
  })

  it('steps through the folders with the arrows and back up to the search box', async () => {
    await open()
    const search = screen.getByRole('searchbox')

    await fireEvent.keyDown(search, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Archive' }))
    await fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Receipts' }))
    await fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowUp' })
    await fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(search)
  })
})
