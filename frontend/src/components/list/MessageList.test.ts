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
import { messageList, patchInList } from '../../stores/messages'
import { openMessageId, searchQuery, selection } from '../../stores/selection'
import { clearSelection, rangeCursor, selectedIds } from '../../stores/listselect'
import { bindings } from '../../stores/shortcuts'
import { prefs } from '../../stores/prefs'
import { idle, ready } from '../../lib/async'
import { sidebar } from '../../stores/accounts'
import { composeSessions } from '../../stores/compose'

const defaultBindings = get(bindings)

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
    threadId: id,
    answered: false,
    forwarded: false,
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
  prefs.update((p) => ({ ...p, rowShowAvatar: false, appVimMode: false, multiSelectEnabled: true, design: 'classic' }))
  bindings.set(defaultBindings)
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

describe('MessageList keyboard selection', () => {
  // focusList lists three messages and puts focus on the list.
  async function focusList(): Promise<ReturnType<typeof userEvent.setup>> {
    api.listViewMessages.mockResolvedValue(page([summary(1, 'First'), summary(2, 'Second'), summary(3, 'Third')]))
    render(MessageList)
    await screen.findByText('First')
    screen.getByRole('listbox').focus()
    return userEvent.setup()
  }

  function selected(): number[] {
    return [...get(selectedIds)].sort((a, b) => a - b)
  }

  it('extends from the highlighted row with shift+down and back with shift+up', async () => {
    const user = await focusList()
    await user.keyboard('{ArrowDown}{Shift>}{ArrowDown}{ArrowDown}{/Shift}')
    expect(selected()).toEqual([1, 2, 3])
    await user.keyboard('{Shift>}{ArrowUp}{/Shift}')
    expect(selected()).toEqual([1, 2])
    expect(get(openMessageId)).toBeNull()
  })

  it('toggles the highlighted row with space', async () => {
    const user = await focusList()
    await user.keyboard('{ArrowDown}{ArrowDown} ')
    expect(selected()).toEqual([2])
    await user.keyboard(' ')
    expect(selected()).toEqual([])
  })

  it('clears with escape, then extends from the row still highlighted', async () => {
    const user = await focusList()
    await user.keyboard('{ArrowDown}{Shift>}{ArrowDown}{/Shift}{Escape}')
    expect(selected()).toEqual([])
    await user.keyboard('{Shift>}{ArrowDown}{/Shift}')
    expect(selected()).toEqual([2, 3])
  })

  it('leaves escape and shift+arrows to the vim motions in app vim mode', async () => {
    prefs.update((p) => ({ ...p, appVimMode: true }))
    const user = await focusList()
    await user.keyboard('{ArrowDown} {Shift>}{ArrowDown}{/Shift}')
    expect(selected()).toEqual([1])
    await user.keyboard('{Escape}')
    expect(selected()).toEqual([1])
  })

  it('stands aside for a binding the user put on its keys', async () => {
    bindings.set({ ...defaultBindings, flag: 'space', archive: 'shift+arrowdown' })
    const user = await focusList()
    await user.keyboard('{ArrowDown} {Shift>}{ArrowDown}{/Shift}')
    expect(selected()).toEqual([])
  })

  it('ignores the selection keys when multi-select is off', async () => {
    prefs.update((p) => ({ ...p, multiSelectEnabled: false }))
    const user = await focusList()
    await user.keyboard('{ArrowDown} {Shift>}{ArrowDown}{/Shift}')
    expect(selected()).toEqual([])
  })
})

describe('MessageList cursor follow', () => {
  // rows builds count messages numbered from first.
  function rows(first: number, count: number): MessageSummary[] {
    return Array.from({ length: count }, (_, i) => summary(first + i, `Message ${first + i}`))
  }

  // longList renders forty rows, more than the virtualized window holds, in a
  // list 200px tall. jsdom has no layout, so the height is stubbed.
  async function longList(total = 40): Promise<HTMLElement> {
    api.listViewMessages.mockResolvedValue({ messages: rows(1, 40), total, hasOlder: false })
    render(MessageList)
    await screen.findByText('Message 1')
    const list = screen.getByRole('listbox')
    Object.defineProperty(list, 'clientHeight', { configurable: true, value: 200 })
    return list
  }

  // settle lets any pending reactive work, including a scroll queued behind a
  // tick, run before asserting that nothing moved.
  function settle(): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, 0))
  }

  // pendingLoad holds every later list load open until the returned function
  // finishes the latest one.
  function pendingLoad(): (value: MessageListData) => void {
    let finish!: (value: MessageListData) => void
    api.listViewMessages.mockImplementation(
      () =>
        new Promise<MessageListData>((resolve) => {
          finish = resolve
        }),
    )
    return (value) => finish(value)
  }

  it('scrolls to a message opened from outside the list', async () => {
    const list = await longList()
    expect(screen.queryByText('Message 35')).toBeNull()

    openMessageId.set(35)

    await waitFor(() => expect(list.scrollTop).toBeGreaterThan(0))
    const row = (await screen.findByText('Message 35')).closest('[role="option"]')
    expect(row).toHaveAttribute('aria-selected', 'true')
  })

  it('stays where the user scrolled when the rows are replaced', async () => {
    const list = await longList()
    openMessageId.set(35)
    await waitFor(() => expect(list.scrollTop).toBeGreaterThan(0))

    list.scrollTop = 0
    list.dispatchEvent(new Event('scroll'))
    // a colour, not read state, so no sidebar count refresh outlives the test.
    patchInList(35, { flagColor: 2 })
    await settle()

    expect(list.scrollTop).toBe(0)
  })

  it('starts the next list at the top while a range cursor is out', async () => {
    const list = await longList()
    openMessageId.set(35)
    await waitFor(() => expect(list.scrollTop).toBeGreaterThan(0))
    rangeCursor.set(1)
    await waitFor(() => expect(list.scrollTop).toBe(0))

    selection.set({ kind: 'view', view: 'flagged', label: 'Flagged' })
    await waitFor(() => expect(api.listViewMessages).toHaveBeenCalledTimes(2))
    await settle()

    expect(list.scrollTop).toBe(0)
  })

  it('starts the next list at the top when a list change and an open arrive together', async () => {
    const list = await longList()
    const finish = pendingLoad()

    // what revealMessage does for a clicked new-mail notification.
    selection.set({ kind: 'view', view: 'flagged', label: 'Flagged' })
    openMessageId.set(35)
    await settle()
    expect(list.scrollTop).toBe(0)

    finish(page(rows(31, 10)))
    await screen.findByText('Message 31')
    await settle()
    expect(list.scrollTop).toBe(0)
  })

  it('follows the cursor while the next page loads', async () => {
    const list = await longList(100)
    pendingLoad()
    // jsdom reports no scroll height, so any scroll counts as near the bottom.
    list.dispatchEvent(new Event('scroll'))
    await waitFor(() => expect(get(messageList).status).toBe('loading'))

    openMessageId.set(35)

    await waitFor(() => expect(list.scrollTop).toBeGreaterThan(0))
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
