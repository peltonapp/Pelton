import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { MessageList as MessageListData, MessageSummary } from '../../lib/types'

const api = vi.hoisted(() => ({
  listViewMessages: vi.fn(),
  archiveMessage: vi.fn(),
  setSeen: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listViewMessages: api.listViewMessages,
  archiveMessage: api.archiveMessage,
  setSeen: api.setSeen,
}))

import MessageList from './MessageList.svelte'
import { messageList } from '../../stores/messages'
import { openMessageId, searchQuery, selection } from '../../stores/selection'
import { clearSelection, selectedIds, toggleSelect } from '../../stores/listselect'
import { prefs } from '../../stores/prefs'
import { idle } from '../../lib/async'
import { sidebar } from '../../stores/accounts'
import { clearHistory, history } from '../../stores/undohistory'
import { toasts } from '../../stores/toast'
import { placedScripts } from '../../stores/scripts'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

function summary(id: number, subject: string, seen = true): MessageSummary {
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
    seen,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    threadId: 0,
    answered: false,
    forwarded: false,
  }
}

function page(messages: MessageSummary[]): MessageListData {
  return { messages, total: messages.length, hasOlder: false }
}

beforeEach(() => {
  api.listViewMessages.mockReset()
  api.archiveMessage.mockReset()
  api.setSeen.mockReset().mockResolvedValue(undefined)
  api.archiveMessage.mockResolvedValue({ messageId: '<m@x>', originalFolderId: 1, destFolderId: 9, exportError: '' })
  sidebar.set(idle())
  messageList.set(idle())
  selection.set({ kind: 'view', view: 'inbox', label: 'Unified Inbox' })
  searchQuery.set('')
  openMessageId.set(null)
  clearSelection()
  clearHistory()
  toasts.set([])
  prefs.update((p) => ({
    ...p,
    design: 'new',
    rowShowAvatar: false,
    appVimMode: false,
    multiSelectEnabled: true,
    threadedList: false,
    rowActions: true,
    rowActionList: 'archive,read,pin',
    swipeEnabled: true,
    swipeLeftShort: 'archive',
    swipeLeftLong: 'none',
    swipeRightShort: 'read',
    swipeRightLong: 'none',
  }))
})

afterEach(() => {
  vi.useRealTimers()
})

async function showList(messages: MessageSummary[]): Promise<void> {
  api.listViewMessages.mockResolvedValue(page(messages))
  render(MessageList)
  await screen.findByText(messages[0].subject)
}

describe('row quick actions', () => {
  it('offers the chosen buttons, in order, as a toolbar per row', async () => {
    await showList([summary(1, 'First')])
    const bar = screen.getByRole('toolbar', { name: /First/ })
    const names = within(bar)
      .getAllByRole('button')
      .map((b) => b.getAttribute('aria-label'))
    expect(names).toEqual(['Archive', 'Mark as unread', 'Pin'])
  })

  it('acts on its row without opening or selecting it', async () => {
    await showList([summary(1, 'First'), summary(2, 'Second')])
    const bar = screen.getByRole('toolbar', { name: /Second/ })
    await userEvent.click(within(bar).getByRole('button', { name: 'Archive' }))
    await waitFor(() => expect(api.archiveMessage).toHaveBeenCalledWith(2))
    expect(get(openMessageId)).toBeNull()
    expect(get(selectedIds).size).toBe(0)
    await waitFor(() => expect(screen.queryByText('Second')).toBeNull())
    expect(get(history)[0]?.kind).toBe('archive')
  })

  it('keeps only the focused row in the tab order', async () => {
    await showList([summary(1, 'First'), summary(2, 'Second')])
    const list = screen.getByRole('listbox')
    list.focus()
    await userEvent.keyboard('{ArrowDown}')
    const first = within(screen.getByRole('toolbar', { name: /First/ })).getAllByRole('button')
    const second = within(screen.getByRole('toolbar', { name: /Second/ })).getAllByRole('button')
    expect(first.every((b) => b.tabIndex === 0)).toBe(true)
    expect(second.every((b) => b.tabIndex === -1)).toBe(true)
  })

  it('stays away while a selection is being built', async () => {
    await showList([summary(1, 'First'), summary(2, 'Second')])
    toggleSelect(1)
    await waitFor(() => expect(screen.queryByRole('toolbar')).toBeNull())
  })

  it('shows nothing when turned off', async () => {
    prefs.update((p) => ({ ...p, rowActions: false }))
    await showList([summary(1, 'First')])
    expect(screen.queryByRole('toolbar')).toBeNull()
  })

  it('is new design only', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    await showList([summary(1, 'First')])
    expect(screen.queryByRole('toolbar')).toBeNull()
  })
})

describe('row swipes', () => {
  // jsdom lays nothing out, so the row is given a width to measure the
  // swipe against.
  let restoreWidth: (() => void) | null = null
  beforeEach(() => {
    const own = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 400 })
    restoreWidth = () => {
      if (own) {
        Object.defineProperty(HTMLElement.prototype, 'clientWidth', own)
      } else {
        delete (HTMLElement.prototype as { clientWidth?: number }).clientWidth
      }
    }
  })
  afterEach(() => restoreWidth?.())

  it('runs the short swipe past the first mark and offers its undo', async () => {
    await showList([summary(1, 'First')])
    const row = screen.getByText('First').closest('[role="option"]')!
    vi.useFakeTimers()
    let t = 1000
    for (let i = 0; i < 12; i++) {
      await fireEvent.wheel(row, { deltaX: 12, deltaY: 0, deltaMode: 0, timeStamp: (t += 16) })
    }
    await vi.advanceTimersByTimeAsync(600)
    vi.useRealTimers()
    await waitFor(() => expect(api.archiveMessage).toHaveBeenCalledWith(1))
    await waitFor(() => expect(get(toasts).some((x) => x.action?.label === 'Undo')).toBe(true))
  })

  it('springs back without acting when let go early', async () => {
    await showList([summary(1, 'First')])
    const row = screen.getByText('First').closest('[role="option"]')!
    vi.useFakeTimers()
    let t = 1000
    for (let i = 0; i < 4; i++) {
      await fireEvent.wheel(row, { deltaX: 5, deltaY: 0, deltaMode: 0, timeStamp: (t += 16) })
    }
    await vi.advanceTimersByTimeAsync(600)
    expect(api.archiveMessage).not.toHaveBeenCalled()
  })

  it('leaves a vertical scroll alone', async () => {
    await showList([summary(1, 'First')])
    const row = screen.getByText('First').closest('[role="option"]')!
    vi.useFakeTimers()
    let t = 1000
    for (let i = 0; i < 20; i++) {
      await fireEvent.wheel(row, { deltaX: 3, deltaY: 14, deltaMode: 0, timeStamp: (t += 16) })
    }
    await vi.advanceTimersByTimeAsync(600)
    expect(api.archiveMessage).not.toHaveBeenCalled()
    expect(api.setSeen).not.toHaveBeenCalled()
  })

  it('labels a script swipe with the script mark', async () => {
    placedScripts.set({ 'row.swipe': [{ scriptId: 5, name: 'Summarize', description: '', options: { icon: 'star' } }] })
    prefs.update((p) => ({ ...p, swipeLeftShort: 'script:5', swipeRightShort: 'script:5' }))
    await showList([summary(1, 'First')])
    const row = screen.getByText('First').closest('[role="option"]')!
    let t = 1000
    for (let i = 0; i < 12; i++) {
      await fireEvent.wheel(row, { deltaX: 12, deltaY: 0, deltaMode: 0, timeStamp: (t += 16) })
    }
    const reveal = await waitFor(() => {
      const el = document.querySelector('.reveal.new')
      expect(el).not.toBeNull()
      return el!
    })
    expect(reveal.querySelector('[data-script-mark]')).not.toBeNull()
    expect(reveal.textContent).toContain('Script: Summarize')
    placedScripts.set({})
  })
})

describe('row script buttons', () => {
  afterEach(() => placedScripts.set({}))

  it('carry the script mark and say they run a script', async () => {
    placedScripts.set({ 'row.hover': [{ scriptId: 5, name: 'Summarize', description: '', options: { icon: 'star' } }] })
    prefs.update((p) => ({ ...p, rowActionList: 'archive,script:5' }))
    await showList([summary(1, 'First')])
    const bar = screen.getByRole('toolbar', { name: /First/ })
    const button = within(bar).getByRole('button', { name: 'Script: Summarize' })
    expect(button.querySelector('[data-script-mark]')).not.toBeNull()
    expect(within(bar).getByRole('button', { name: 'Archive' }).querySelector('[data-script-mark]')).toBeNull()
  })
})
