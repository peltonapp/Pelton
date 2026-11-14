import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { MessageSummary, ThreadList, ThreadMessage, ThreadSummary } from '../../lib/types'

const api = vi.hoisted(() => ({
  listViewMessages: vi.fn(),
  listViewThreads: vi.fn(),
  getThread: vi.fn(),
  setSetting: vi.fn(),
  setSeen: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listViewMessages: api.listViewMessages,
  listViewThreads: api.listViewThreads,
  getThread: api.getThread,
  setSetting: api.setSetting,
  setSeen: api.setSeen,
}))

import MessageList from './MessageList.svelte'
import { messageList } from '../../stores/messages'
import { openMessageId, searchQuery, selection } from '../../stores/selection'
import { clearSelection } from '../../stores/listselect'
import { prefs } from '../../stores/prefs'
import { idle } from '../../lib/async'
import { sidebar } from '../../stores/accounts'
import { clearSearch } from '../../stores/search'
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
    date: `2026-09-${String(10 + id).padStart(2, '0')}T12:00:00Z`,
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

function member(m: MessageSummary, fromMe = false): ThreadMessage {
  return { ...m, folderRole: fromMe ? 'sent' : 'inbox', fromMe }
}

function thread(head: MessageSummary, patch: Partial<ThreadSummary> = {}): ThreadSummary {
  return {
    threadId: head.threadId,
    head,
    count: 1,
    unreadCount: head.seen ? 0 : 1,
    participants: [head.fromName],
    hasAttachments: head.hasAttachments,
    flagged: head.flagged,
    answered: head.answered,
    forwarded: head.forwarded,
    messageIds: [head.id],
    ...patch,
  }
}

// a conversation of three: Ada wrote, the user answered from Sent, and Bob
// replied with something still unread. The list row is Ada's latest.
const first = summary(1, 'Trip plans', { threadId: 7, snippet: 'Shall we go in May?' })
const mine = summary(2, 'Re: Trip plans', { threadId: 7, fromName: 'Me', fromAddress: 'me@example.com', snippet: 'May works for me' })
const bob = summary(3, 'Re: Trip plans', { threadId: 7, fromName: 'Bob', fromAddress: 'bob@example.com', seen: false, snippet: 'Count me in' })
const latest = summary(4, 'Re: Trip plans', { threadId: 7, snippet: 'Booking it', answered: true })
const lone = summary(5, 'Invoice', { threadId: 9 })

function threadPage(): ThreadList {
  return {
    threads: [
      thread(latest, { count: 4, unreadCount: 1, messageIds: [1, 2, 3, 4], participants: ['Ada', 'Bob', 'Me'], answered: true }),
      thread(lone),
    ],
    total: 2,
    hasOlder: false,
  }
}

beforeEach(() => {
  api.listViewMessages.mockReset().mockResolvedValue({ messages: [latest, lone], total: 2, hasOlder: false })
  api.listViewThreads.mockReset().mockResolvedValue(threadPage())
  api.getThread.mockReset().mockResolvedValue({
    threadId: 7,
    messages: [member(first), member(mine, true), member(bob), member(latest)],
  })
  api.setSetting.mockReset().mockResolvedValue(undefined)
  api.setSeen.mockReset().mockResolvedValue(undefined)
  sidebar.set(idle())
  messageList.set(idle())
  selection.set({ kind: 'view', view: 'inbox', label: 'Unified Inbox' })
  searchQuery.set('')
  openMessageId.set(null)
  clearSelection()
  clearSearch()
  resetQuickFilter()
  prefs.update((p) => ({
    ...p,
    rowShowAvatar: false,
    appVimMode: false,
    design: 'new',
    quickFilterBar: false,
    threadedList: true,
    rowTemplate: 'compact',
  }))
})

function rowOf(text: string): HTMLElement {
  return screen.getByText(text).closest('[role="option"]') as HTMLElement
}

describe('the threaded message list', () => {
  it('shows one row per conversation with its size, unread state and reply marker', async () => {
    render(MessageList)
    await screen.findByText('Re: Trip plans')
    expect(api.listViewThreads).toHaveBeenCalled()
    expect(api.listViewMessages).not.toHaveBeenCalled()
    expect(screen.getAllByRole('option')).toHaveLength(2)
    expect(screen.getByText('2 messages')).toBeInTheDocument()

    const row = rowOf('Re: Trip plans')
    expect(within(row).getByLabelText('4 messages, 1 unread')).toHaveTextContent('4')
    expect(within(row).getByLabelText('Replied')).toBeInTheDocument()
    expect(within(rowOf('Invoice')).queryByRole('button', { name: 'Show the whole conversation' })).toBeNull()
  })

  it('expands a conversation into its replies and closes it again', async () => {
    const user = userEvent.setup()
    render(MessageList)
    await screen.findByText('Re: Trip plans')

    await user.click(screen.getByRole('button', { name: 'Show the whole conversation' }))
    expect(await screen.findByText('Bob')).toBeInTheDocument()
    expect(api.getThread).toHaveBeenCalledWith(7)
    // the row's own message is not repeated beneath it, the user's reply is.
    expect(screen.getAllByRole('option')).toHaveLength(5)
    expect(screen.getByText('Me')).toBeInTheDocument()
    expect(screen.getByTitle('Sent by you')).toBeInTheDocument()
    // newest first under the conversation, like the list around it.
    const names = screen.getAllByRole('option').map((o) => o.querySelector('.sender')?.textContent)
    expect(names).toEqual(['Ada', 'Bob', 'Me', 'Ada', 'Ada'])
    // opening it did not open the message.
    expect(api.setSeen).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Hide the conversation' }))
    await waitFor(() => expect(screen.queryByText('Bob')).toBeNull())
    expect(screen.getAllByRole('option')).toHaveLength(2)
  })

  it('opens and closes conversations from the keyboard', async () => {
    const user = userEvent.setup()
    render(MessageList)
    await screen.findByText('Re: Trip plans')
    const list = screen.getByRole('listbox')
    list.focus()

    await user.keyboard('{ArrowDown}')
    await user.keyboard('{ArrowRight}')
    expect(await screen.findByText('Bob')).toBeInTheDocument()

    // down onto a reply, then left climbs back to the conversation.
    await user.keyboard('{ArrowDown}')
    expect(list.getAttribute('aria-activedescendant')).toBe('msg-3')
    await user.keyboard('{ArrowLeft}')
    expect(list.getAttribute('aria-activedescendant')).toBe('msg-4')
    await user.keyboard('{ArrowLeft}')
    await waitFor(() => expect(screen.queryByText('Bob')).toBeNull())
  })

  it('opens a reply from inside a conversation', async () => {
    const user = userEvent.setup()
    render(MessageList)
    await screen.findByText('Re: Trip plans')
    await user.click(screen.getByRole('button', { name: 'Show the whole conversation' }))
    await user.click(await screen.findByText('Bob'))
    await waitFor(() => expect(api.setSeen).toHaveBeenCalledWith(3, true))
    expect(await screen.findByLabelText('4 messages in this conversation')).toBeInTheDocument()
  })

  it('keeps a conversation with an unread reply under the unread quick filter', async () => {
    prefs.update((p) => ({ ...p, quickFilterBar: true }))
    const user = userEvent.setup()
    render(MessageList)
    await screen.findByText('Re: Trip plans')
    const bar = await screen.findByRole('toolbar', { name: 'Quick filter' })
    await user.click(within(bar).getByRole('button', { name: 'Unread' }))
    await waitFor(() => expect(screen.queryByText('Invoice')).toBeNull())
    expect(screen.getByText('Re: Trip plans')).toBeInTheDocument()
  })

  it('lists every message when grouping is turned off', async () => {
    prefs.update((p) => ({ ...p, threadedList: false }))
    render(MessageList)
    await screen.findByText('Invoice')
    expect(api.listViewMessages).toHaveBeenCalled()
    expect(api.listViewThreads).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: 'Show the whole conversation' })).toBeNull()
  })

  it('reads the list again in the other shape when the preference changes', async () => {
    render(MessageList)
    await screen.findByText('Re: Trip plans')
    prefs.update((p) => ({ ...p, threadedList: false }))
    await waitFor(() => expect(api.listViewMessages).toHaveBeenCalled())
  })
})
