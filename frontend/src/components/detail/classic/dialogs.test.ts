import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'

const api = vi.hoisted(() => ({
  listFolders: vi.fn(),
  moveMessage: vi.fn(),
  getMessageSource: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

import ClassicMoveDialog from './MoveDialog.svelte'
import ClassicSnoozeDialog from './SnoozeDialog.svelte'
import ClassicSourceModal from './SourceModal.svelte'
import ClassicInfoModal from './InfoModal.svelte'
import MoveDialog from '../MoveDialog.svelte'
import SnoozeDialog from '../SnoozeDialog.svelte'
import SourceModal from '../SourceModal.svelte'
import InfoModal from '../InfoModal.svelte'
import { closeMove, openMove } from '../../../stores/move'
import { closeSnooze, openSnooze } from '../../../stores/snooze'
import { prefs } from '../../../stores/prefs'
import type { Folder, MessageDetail, MessageSummary } from '../../../lib/types'

// the dialogs open with a transition, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const folder = (id: number, name: string): Folder => ({ id, accountId: 1, name }) as Folder
const message = { id: 7, accountId: 1, folderId: 1, subject: 'hi' } as MessageSummary
const detail = {
  id: 7,
  subject: 'Quarterly numbers',
  fromName: 'Ada',
  fromAddress: 'ada@example.com',
  toAddresses: 'me@example.com',
  ccAddresses: '',
  date: '2026-11-01T10:00:00Z',
  accountEmail: 'me@example.com',
  folderName: 'INBOX',
  isHtml: false,
  hasRemoteContent: false,
  pgp: 'none',
  attachments: [],
} as unknown as MessageDetail

beforeEach(() => {
  api.listFolders.mockResolvedValue([folder(1, 'INBOX'), folder(2, 'Archive')])
  api.getMessageSource.mockRejectedValue(new Error('connection refused'))
})

afterEach(() => {
  closeMove()
  closeSnooze()
  prefs.update((p) => ({ ...p, design: 'new' }))
  vi.clearAllMocks()
})

async function openMoveIn(component: typeof MoveDialog): Promise<void> {
  render(component)
  openMove(message)
  await tick()
  await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Archive' })).toBeTruthy())
}

describe('move dialog', () => {
  it('keeps the classic frame, focus and plain empty line', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    await openMoveIn(ClassicMoveDialog)

    const dialog = screen.getByRole('dialog', { name: 'Move to folder' })
    expect(dialog).toHaveClass('dialog')
    expect(dialog.querySelector('.body')).toBeNull()
    expect(document.activeElement).toBe(screen.getByRole('searchbox'))

    await fireEvent.input(screen.getByRole('searchbox'), { target: { value: 'zzz' } })
    const empty = dialog.querySelector('li.empty')
    expect(empty).toHaveTextContent('No matching folders.')
    expect(empty?.querySelector('[data-kind]')).toBeNull()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('sits in the shared modal in the new design', async () => {
    await openMoveIn(MoveDialog)

    const dialog = screen.getByRole('dialog', { name: 'Move to folder' })
    expect(dialog.querySelector('.body')).not.toBeNull()

    await fireEvent.input(screen.getByRole('searchbox'), { target: { value: 'zzz' } })
    expect(dialog.querySelector('li.empty [data-kind="filtered"]')).not.toBeNull()
  })
})

describe('snooze dialog', () => {
  it('keeps the classic frame with the subject under the title', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    render(ClassicSnoozeDialog)
    openSnooze(7, 'Quarterly numbers')
    await tick()

    const dialog = screen.getByRole('dialog', { name: 'Snooze message' })
    expect(dialog.querySelector('p.subject')).toHaveTextContent('Quarterly numbers')
    expect(dialog.querySelector('button.go')).toHaveTextContent('Snooze')

    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('sits in the shared modal in the new design', async () => {
    render(SnoozeDialog)
    openSnooze(7, 'Quarterly numbers')
    await tick()

    const dialog = screen.getByRole('dialog', { name: 'Snooze' })
    expect(dialog.querySelector('p.hint')).toHaveTextContent('Quarterly numbers')
    expect(dialog.querySelector('button.go')).toBeNull()
  })
})

describe('source modal', () => {
  it('keeps the copy button in the classic header and a plain error line', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    const onClose = vi.fn()
    render(ClassicSourceModal, { props: { messageId: 7 }, events: { close: onClose } })

    const dialog = screen.getByRole('dialog', { name: 'Message source' })
    const header = dialog.querySelector('header')
    expect(header?.querySelector('button[aria-label="Copy source"]')).not.toBeNull()
    await vi.waitFor(() => expect(dialog.querySelector('.state.error')).not.toBeNull())
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument()

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('shows the shared error state with a retry in the new design', async () => {
    render(SourceModal, { props: { messageId: 7 } })

    expect(await screen.findByText('Something went wrong')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
    expect(screen.getByRole('dialog').querySelector('footer button')).toHaveTextContent('Copy source')
  })
})

describe('info modal', () => {
  it('keeps the classic frame and label', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    const onClose = vi.fn()
    render(ClassicInfoModal, { props: { detail }, events: { close: onClose } })

    const dialog = screen.getByRole('dialog', { name: 'Message information' })
    expect(dialog).toHaveClass('modal')
    expect(dialog.querySelector('footer button.primary')).toHaveTextContent('Copy as Markdown')

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('sits in the shared modal in the new design', () => {
    render(InfoModal, { props: { detail } })

    const dialog = screen.getByRole('dialog', { name: 'Message info' })
    expect(dialog.querySelector('footer button.cal-btn')).toHaveTextContent('Copy as Markdown')
  })
})
