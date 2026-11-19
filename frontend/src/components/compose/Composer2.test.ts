import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  saveDraft: vi.fn(),
  deleteDraft: vi.fn(),
  uploadDraft: vi.fn(),
  composeProtectionStatus: vi.fn(),
  showMessageInMainWindow: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/outbox', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/outbox')>()),
  loadOutbox: vi.fn(),
}))

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 0 }),
}))

import Composer2 from './Composer2.svelte'
import { composeSessions, getSession, openCompose, openReply, updateCompose, type ComposeSession } from '../../stores/compose'
import { prefs } from '../../stores/prefs'
import type { MessageDetail } from '../../lib/types'

let unsubscribe: (() => void) | null = null

// mount renders a composer re-rendered from the store, the way its hosts do.
function mount(id: number, layout: 'window' | 'docked' | 'tab' = 'window'): void {
  updateCompose(id, { signaturesApplied: true })
  const session = getSession(id) as ComposeSession
  const { rerender } = render(Composer2, { props: { session, layout } })
  unsubscribe = composeSessions.subscribe((list) => {
    const next = list.find((s) => s.id === id)
    if (next) {
      void rerender({ session: next, layout })
    }
  })
}

function original(): MessageDetail {
  return {
    id: 42,
    accountId: 1,
    folderId: 20,
    accountEmail: 'me@example.com',
    fromName: 'Ada Lovelace',
    fromAddress: 'ada@example.com',
    toAddresses: 'me@example.com, charles@example.com',
    ccAddresses: '',
    subject: 'Engine notes',
    date: '2026-10-01',
    bodyQuote: 'the notes',
    messageId: '<n1@x>',
    references: [],
    inReplyTo: [],
  } as unknown as MessageDetail
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.sendMessage.mockResolvedValue(7)
  api.saveDraft.mockResolvedValue(3)
  api.uploadDraft.mockResolvedValue('<d@x>')
  api.showMessageInMainWindow.mockResolvedValue(undefined)
  api.composeProtectionStatus.mockResolvedValue({ canSign: false, canEncrypt: false, suggested: 'none', recipients: [] })
  prefs.update((p) => ({ ...p, reduceMotion: true, composeVimMode: false, sendDelaySeconds: 0 }))
})

afterEach(() => {
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
})

describe('Composer2', () => {
  it('sends from the toolbar and closes', async () => {
    const id = openCompose(1, 'wysiwyg')
    updateCompose(id, { to: 'ann@example.com', subject: 'Hi' })
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    await vi.waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    await vi.waitFor(() => expect(getSession(id)).toBeUndefined())
  })

  it('refuses to send with no recipient and stays open', async () => {
    const id = openCompose(1, 'wysiwyg')
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    expect(api.sendMessage).not.toHaveBeenCalled()
    expect(getSession(id)).toBeDefined()
    expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled()
  })

  it('has no editor mode switch', () => {
    const id = openCompose(1, 'wysiwyg')
    mount(id)
    expect(screen.queryByRole('radiogroup', { name: 'Editor mode' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Markdown' })).toBeNull()
  })

  it('reveals Cc from the To row', async () => {
    const id = openCompose(1, 'wysiwyg')
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: 'Cc' }))

    expect(getSession(id)?.showCc).toBe(true)
  })

  it('shows what a reply answers and folds the quote', () => {
    const id = openReply(original(), 'wysiwyg', false)
    mount(id)

    expect(screen.getByText('Replying to Ada Lovelace - Engine notes')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show quoted text' })).toBeInTheDocument()
  })

  it('switches a reply to reply all in place', async () => {
    const id = openReply(original(), 'wysiwyg', false)
    mount(id)

    await userEvent.click(screen.getByRole('radio', { name: 'Reply all' }))

    expect(getSession(id)?.to).toBe('Ada Lovelace <ada@example.com>, charles@example.com')
    expect(getSession(id)?.origin?.kind).toBe('replyAll')
  })

  it('shows the original from a window through the main window', async () => {
    const id = openReply(original(), 'wysiwyg', false)
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: 'Show original' }))

    expect(api.showMessageInMainWindow).toHaveBeenCalledWith(42)
  })

  it('asks before closing unsaved text', async () => {
    const id = openCompose(1, 'wysiwyg')
    updateCompose(id, { subject: 'Draft' })
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: 'Close compose' }))

    expect(screen.getByRole('alertdialog', { name: 'Save or discard this message' })).toBeInTheDocument()
    expect(getSession(id)).toBeDefined()
  })

  it('opens a vim mode session in the plain text editor', () => {
    prefs.update((p) => ({ ...p, composeVimMode: true }))
    const id = openCompose(1, 'wysiwyg')
    mount(id)
    expect(get(composeSessions).find((s) => s.id === id)?.mode).toBe('plaintext')
  })
})
