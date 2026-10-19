import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Folder, MessageList as MessageListData, MessageSummary } from '../../lib/types'

const api = vi.hoisted(() => ({
  listViewMessages: vi.fn(),
  search: vi.fn(),
  openDraft: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listViewMessages: api.listViewMessages,
  search: api.search,
  openDraft: api.openDraft,
}))

import MessageList from './MessageList.svelte'
import { messageList } from '../../stores/messages'
import { openMessageId, searchQuery, selection } from '../../stores/selection'
import { clearSelection } from '../../stores/listselect'
import { prefs } from '../../stores/prefs'
import { idle, ready } from '../../lib/async'
import { sidebar } from '../../stores/accounts'
import { composeSessions } from '../../stores/compose'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

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
  }
}

function page(messages: MessageSummary[]): MessageListData {
  return { messages, total: messages.length, hasOlder: false }
}

beforeEach(() => {
  api.listViewMessages.mockReset()
  api.search.mockReset()
  api.openDraft.mockReset()
  composeSessions.set([])
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
  // openRow lists one message in a folder with the given role and clicks it.
  async function openRow(role: string): Promise<void> {
    const foldersByAccount = { 1: [{ id: 1, accountId: 1, role } as Folder] }
    sidebar.set(ready({ accounts: [], foldersByAccount, views: [], pinned: [] }))
    api.listViewMessages.mockResolvedValue(page([summary(5, 'Half written')]))
    render(MessageList)
    await userEvent.click((await screen.findByText('Half written')).closest('[role="option"]')!)
  }

  it('opens a draft Pelton saved in the composer, not the reading pane', async () => {
    const request = { to: [{ name: 'Bob', email: 'bob@example.com' }], subject: 'Half written', text: 'so far', html: '', protection: 'none', draftMessageId: '<d1@pelton>' }
    api.openDraft.mockResolvedValue({ id: 9, savedAt: '', locked: false, accountId: 1, protection: 'none', request })

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
})
