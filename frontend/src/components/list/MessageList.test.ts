import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Folder, MessageList as MessageListData, MessageSummary, Profile } from '../../lib/types'

const api = vi.hoisted(() => ({
  listViewMessages: vi.fn(),
  search: vi.fn(),
  openDraft: vi.fn(),
  signerFingerprint: vi.fn(),
  listPGPKeys: vi.fn(),
  unlockPGPKey: vi.fn(),
  unsealDraft: vi.fn(),
  draftSubjects: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import MessageList from './MessageList.svelte'
import PassphraseDialog from '../settings/PassphraseDialog.svelte'
import { toasts } from '../../stores/toast'
import { messageList } from '../../stores/messages'
import { openMessageId, searchQuery, selection } from '../../stores/selection'
import { clearSelection } from '../../stores/listselect'
import { prefs } from '../../stores/prefs'
import { idle, ready } from '../../lib/async'
import { sidebar } from '../../stores/accounts'
import { composeSessions } from '../../stores/compose'
import { currentProfile } from '../../stores/profiles'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

// the passphrase dialog closes with a transition, and jsdom has no web
// animations. A finished stub lets svelte run the transition to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

function summary(id: number, subject: string): MessageSummary {
  return {
    id,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject,
    fromName: 'Ada',
    fromAddress: 'ada@example.com',
    snippet: '',
    date: '2026-09-21T12:00:00Z',
    seen: true,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    messageId: '',
  }
}

function page(messages: MessageSummary[]): MessageListData {
  return { messages, total: messages.length, hasOlder: false }
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.draftSubjects.mockResolvedValue({})
  composeSessions.set([])
  currentProfile.set({ id: 1, name: 'Home' } as Profile)
  toasts.set([])
  sidebar.set(idle())
  messageList.set(idle())
  selection.set({ kind: 'view', view: 'inbox', label: 'Unified Inbox' })
  searchQuery.set('')
  openMessageId.set(null)
  clearSelection()
  prefs.update((p) => ({ ...p, rowShowAvatar: false }))
})

describe('MessageList search selection', () => {
  it('drops a stale row highlight when clearing search changes the list', async () => {
    const normal = summary(1, 'Normal message')
    const result = summary(42, 'Search result')
    let finishReload!: (value: MessageListData) => void
    const reload = new Promise<MessageListData>((resolve) => {
      finishReload = resolve
    })

    api.listViewMessages.mockResolvedValueOnce(page([normal])).mockImplementation(() => reload)
    api.search.mockResolvedValue({ messages: [result], total: 1 })

    render(MessageList)
    await screen.findByText('Normal message')

    const user = userEvent.setup()
    await user.type(screen.getByPlaceholderText('Search mail'), 'needle')
    await waitFor(() => expect(api.search).toHaveBeenCalled())
    const resultRow = (await screen.findByText('Search result')).closest('[role="option"]')
    expect(resultRow).not.toBeNull()
    await user.click(resultRow!)
    expect(get(openMessageId)).toBe(42)
    expect(resultRow).toHaveAttribute('aria-selected', 'true')

    await user.click(screen.getByRole('button', { name: 'Clear search' }))
    finishReload(page([normal]))

    await waitFor(() => {
      const normalRow = screen.getByText('Normal message').closest('[role="option"]')
      expect(normalRow).toHaveAttribute('aria-selected', 'false')
    })
    // Clearing a filter should not also discard the message being read; it only
    // stops a different row at the old numeric index from looking selected.
    expect(get(openMessageId)).toBe(42)
  })
})

describe('MessageList drafts', () => {
  const saved = {
    id: 9,
    savedAt: '',
    locked: false,
    accountId: 1,
    protection: 'none',
    request: { to: [{ name: 'Bob', email: 'bob@example.com' }], subject: 'Half written', text: 'so far', html: '', protection: 'none', draftMessageId: '<d1@pelton>' },
  }

  // listIn shows the given rows as the contents of folder 1 with the given role.
  async function listIn(role: string, rows = [summary(5, 'Half written')]): Promise<void> {
    const foldersByAccount = { 1: [{ id: 1, accountId: 1, role } as Folder] }
    sidebar.set(ready({ accounts: [], foldersByAccount, views: [], pinned: [] }))
    api.listViewMessages.mockResolvedValue(page(rows))
    render(MessageList)
  }

  async function openRow(role: string): Promise<void> {
    await listIn(role)
    await userEvent.click((await screen.findByText('Half written')).closest('[role="option"]')!)
  }

  it('opens a draft Pelton saved in the composer, not the reading pane', async () => {
    api.openDraft.mockResolvedValue(saved)

    await openRow('drafts')

    await waitFor(() => expect(get(composeSessions)).toHaveLength(1))
    expect(api.openDraft).toHaveBeenCalledWith(5)
    expect(get(composeSessions)[0]).toMatchObject({
      to: 'Bob <bob@example.com>',
      subject: 'Half written',
      body: 'so far',
      mode: 'plaintext',
      draftId: 9,
      draftMessageId: '<d1@pelton>',
      saved: true,
    })
    expect(get(openMessageId)).toBeNull()
  })

  it.each([
    { name: 'a draft saved elsewhere', role: 'drafts', lookups: [[5]] },
    { name: 'a message outside the Drafts folder', role: 'normal', lookups: [] },
  ])('reads $name in the reading pane', async ({ role, lookups }) => {
    api.openDraft.mockResolvedValue({ id: 0 })

    await openRow(role)

    await waitFor(() => expect(get(openMessageId)).toBe(5))
    expect(api.openDraft.mock.calls).toEqual(lookups)
    expect(get(composeSessions)).toEqual([])
  })

  // An encrypted draft's server copy is titled "...". The subject this device
  // can read takes its place, in the Drafts folder only.
  it.each([
    { role: 'drafts', want: ['Real subject', '...'], lookups: [[1]] },
    { role: 'normal', want: ['...', '...'], lookups: [] },
  ])('shows readable encrypted draft subjects in a $role folder', async ({ role, want, lookups }) => {
    api.draftSubjects.mockResolvedValue({ 'd1@pelton': 'Real subject' })

    await listIn(role, [
      { ...summary(5, '...'), messageId: 'd1@pelton' },
      { ...summary(6, '...'), messageId: 'd2@pelton' },
    ])

    await waitFor(() => expect(api.draftSubjects.mock.calls).toEqual(lookups))
    await waitFor(() =>
      expect(screen.getAllByRole('option').map((row) => (row.textContent?.includes('Real subject') ? 'Real subject' : '...'))).toEqual(want),
    )
  })

  // The list outlives a profile switch, so subjects one profile looked up and
  // got back late must not show up in the next.
  it('drops the encrypted draft subjects of a profile it has left', async () => {
    let answerHome!: (subjects: Record<string, string>) => void
    api.draftSubjects.mockReturnValueOnce(new Promise((resolve) => (answerHome = resolve)))

    await listIn('drafts', [{ ...summary(5, '...'), messageId: 'd1@pelton' }])
    await waitFor(() => expect(api.draftSubjects).toHaveBeenCalledTimes(1))
    currentProfile.set({ id: 2, name: 'Work' } as Profile)
    await waitFor(() => expect(api.draftSubjects).toHaveBeenCalledTimes(2))
    answerHome({ 'd1@pelton': 'Home secret' })

    await new Promise((r) => setTimeout(r, 0))
    expect(screen.queryByText('Home secret')).toBeNull()
  })

  describe('sealed on this device', () => {
    beforeEach(() => {
      api.openDraft.mockResolvedValue({ ...saved, locked: true, protection: 'encrypt', request: { draftMessageId: '<d1@pelton>' } })
      api.signerFingerprint.mockResolvedValue('ABCD')
      api.listPGPKeys.mockResolvedValue([{ fingerprint: 'ABCD', name: 'Me', email: 'me@example.com' }])
      api.unsealDraft.mockResolvedValue({ ...saved, protection: 'encrypt' })
      render(PassphraseDialog)
    })

    async function unlockWith(passphrase: string): Promise<void> {
      await openRow('drafts')
      await userEvent.type(await screen.findByLabelText('Passphrase'), passphrase)
      await userEvent.click(screen.getByRole('button', { name: 'Unlock' }))
    }

    it('asks for the passphrase, then opens the composer', async () => {
      api.unlockPGPKey.mockResolvedValue(undefined)

      await unlockWith('hunter2')

      await waitFor(() => expect(get(composeSessions)).toHaveLength(1))
      expect(api.unlockPGPKey).toHaveBeenCalledWith('ABCD', 'hunter2', false)
      expect(api.unsealDraft).toHaveBeenCalledWith(9, '')
      // the subjects it now holds the passphrase for are fetched again.
      await waitFor(() => expect(api.draftSubjects).toHaveBeenCalledTimes(2))
      expect(get(composeSessions)[0]).toMatchObject({ subject: 'Half written', draftId: 9 })
      expect(get(openMessageId)).toBeNull()
    })

    it.each([
      [
        'a wrong passphrase',
        async () => {
          api.unlockPGPKey.mockRejectedValue(new Error('wrong passphrase'))
          await unlockWith('nope')
          await waitFor(() => expect(get(toasts).map((t) => t.message)).toContain('wrong passphrase'))
        },
      ],
      [
        'cancel',
        async () => {
          await openRow('drafts')
          await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
          await waitFor(() => expect(screen.queryByLabelText('Passphrase')).toBeNull())
        },
      ],
    ])('opens nothing on %s', async (_, refuse) => {
      await refuse()

      expect(api.unsealDraft).not.toHaveBeenCalled()
      expect(get(composeSessions)).toEqual([])
      expect(get(openMessageId)).toBeNull()
    })
  })
})
