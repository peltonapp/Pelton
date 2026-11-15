import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { MessageList as MessageListData, MessageSummary } from '../../lib/types'

const api = vi.hoisted(() => ({
  listViewMessages: vi.fn(),
  search: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listViewMessages: api.listViewMessages,
  search: api.search,
  setSetting: api.setSetting,
}))

import MessageList from './MessageList.svelte'
import { messageList } from '../../stores/messages'
import { openMessageId, searchQuery, selection } from '../../stores/selection'
import { clearSelection } from '../../stores/listselect'
import { prefs } from '../../stores/prefs'
import { idle } from '../../lib/async'
import { sidebar } from '../../stores/accounts'
import { activeSearch, clearSearch, searchDraft, submitSearch } from '../../stores/search'
import { resetQuickFilter } from '../../stores/quickfilter'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

function summary(id: number, subject: string, patch: Partial<MessageSummary> = {}): MessageSummary {
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
    threadId: 0,
    answered: false,
    forwarded: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    ...patch,
  }
}

function page(messages: MessageSummary[]): MessageListData {
  return { messages, total: messages.length, hasOlder: false }
}

function startSearch(text: string): void {
  searchDraft.set({ text, chips: [] })
  submitSearch()
}

beforeEach(() => {
  api.listViewMessages.mockReset()
  api.search.mockReset()
  api.setSetting.mockReset().mockResolvedValue(undefined)
  sidebar.set(idle())
  messageList.set(idle())
  selection.set({ kind: 'view', view: 'inbox', label: 'Unified Inbox' })
  searchQuery.set('')
  openMessageId.set(null)
  clearSelection()
  clearSearch()
  resetQuickFilter()
  prefs.update((p) => ({ ...p, rowShowAvatar: false, appVimMode: false, design: 'new', quickFilterBar: false, threadedList: false }))
})

describe('MessageList in the new design', () => {
  it('leaves searching to the top bar and names the folder in its header', async () => {
    api.listViewMessages.mockResolvedValue(page([summary(1, 'Hello')]))
    render(MessageList)
    await screen.findByText('Hello')
    expect(screen.queryByPlaceholderText('Search mail')).toBeNull()
    expect(screen.getByText('Unified Inbox')).toBeInTheDocument()
    expect(screen.getByText('1 message')).toBeInTheDocument()
  })

  it('shows results for the top bar search and refines them with the filter chips', async () => {
    api.listViewMessages.mockResolvedValue(page([summary(1, 'Normal message')]))
    api.search.mockResolvedValue({ messages: [summary(42, 'Quarterly invoice')], total: 1 })
    const user = userEvent.setup()
    render(MessageList)
    await screen.findByText('Normal message')

    startSearch('invoice')
    await screen.findByText('Search results')
    expect(api.search).toHaveBeenLastCalledWith(expect.objectContaining({ query: 'invoice', unreadOnly: false }))

    const filters = screen.getByRole('toolbar', { name: 'Refine results' })
    await user.click(within(filters).getByRole('button', { name: 'Unread' }))
    await waitFor(() =>
      expect(api.search).toHaveBeenLastCalledWith(expect.objectContaining({ query: 'invoice', unreadOnly: true })),
    )
    await user.click(within(filters).getByRole('button', { name: 'Flagged' }))
    await waitFor(() =>
      expect(api.search).toHaveBeenLastCalledWith(expect.objectContaining({ unreadOnly: true, flaggedOnly: true })),
    )
  })

  it('marks the matched words in result rows', async () => {
    api.listViewMessages.mockResolvedValue(page([]))
    api.search.mockResolvedValue({ messages: [summary(42, 'Quarterly invoice')], total: 1 })
    render(MessageList)
    startSearch('invoice')
    const row = (await screen.findByText(/Quarterly/)).closest('[role="option"]') as HTMLElement
    const mark = row.querySelector('mark')
    expect(mark?.textContent).toBe('invoice')
  })

  it('goes back to the folder when the search is closed', async () => {
    api.listViewMessages.mockResolvedValue(page([summary(1, 'Normal message')]))
    api.search.mockResolvedValue({ messages: [summary(42, 'Result')], total: 1 })
    const user = userEvent.setup()
    render(MessageList)
    await screen.findByText('Normal message')
    startSearch('result')
    await screen.findByText('Result')

    await user.click(screen.getByRole('button', { name: 'Close search' }))
    expect(get(activeSearch)).toBeNull()
    await screen.findByText('Normal message')
    expect(screen.queryByText('Search results')).toBeNull()
    expect(api.listViewMessages).toHaveBeenCalledTimes(2)
  })

  it('shows a skeleton while results load and an empty state when nothing matches', async () => {
    api.listViewMessages.mockResolvedValue(page([]))
    let finish!: (value: { messages: MessageSummary[]; total: number }) => void
    api.search.mockImplementation(() => new Promise((resolve) => (finish = resolve)))
    render(MessageList)
    await screen.findByText('Nothing left in your inbox.')
    startSearch('nothing')
    await waitFor(() => expect(api.search).toHaveBeenCalled())
    finish({ messages: [], total: 0 })
    expect(await screen.findByText('No matching messages')).toBeInTheDocument()
  })

  it('narrows the loaded list with the quick filter bar', async () => {
    api.listViewMessages.mockResolvedValue(
      page([
        summary(1, 'Read note'),
        summary(2, 'Unread note', { seen: false }),
        summary(3, 'Team lunch', { fromName: 'Bob', fromAddress: 'bob@example.com' }),
      ]),
    )
    const user = userEvent.setup()
    render(MessageList)
    await screen.findByText('Read note')

    await user.click(screen.getByRole('button', { name: 'Quick filter' }))
    const bar = await screen.findByRole('toolbar', { name: 'Quick filter' })
    expect(api.setSetting).toHaveBeenCalledWith('quick_filter_bar', 'true')

    await user.click(within(bar).getByRole('button', { name: 'Unread' }))
    await waitFor(() => expect(screen.queryByText('Read note')).toBeNull())
    expect(screen.getByText('Unread note')).toBeInTheDocument()

    await user.click(within(bar).getByRole('button', { name: 'Unread' }))
    await user.type(within(bar).getByRole('textbox', { name: 'Filter these messages' }), 'bob')
    await waitFor(() => expect(screen.queryByText('Unread note')).toBeNull())
    expect(screen.getByText('Team lunch')).toBeInTheDocument()
    expect(screen.getByText('1 of 3 shown')).toBeInTheDocument()
  })
})
