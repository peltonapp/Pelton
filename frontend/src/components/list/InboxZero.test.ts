import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import type { MessageList as MessageListData } from '../../lib/types'

const api = vi.hoisted(() => ({
  listViewMessages: vi.fn(),
  listFolderMessages: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import MessageList from './MessageList.svelte'
import { messageList } from '../../stores/messages'
import { openMessageId, searchQuery, selection } from '../../stores/selection'
import { clearSelection } from '../../stores/listselect'
import { prefs } from '../../stores/prefs'
import { idle, ready } from '../../lib/async'
import { sidebar } from '../../stores/accounts'
import { clearSearch } from '../../stores/search'
import { resetQuickFilter } from '../../stores/quickfilter'
import type { Folder } from '../../lib/types'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

const empty: MessageListData = { messages: [], total: 0, hasOlder: false }

beforeEach(() => {
  api.listViewMessages.mockReset().mockResolvedValue(empty)
  api.listFolderMessages.mockReset().mockResolvedValue(empty)
  api.setSetting.mockReset().mockResolvedValue(undefined)
  messageList.set(idle())
  searchQuery.set('')
  openMessageId.set(null)
  clearSelection()
  clearSearch()
  resetQuickFilter()
  sidebar.set(
    ready({
      accounts: [],
      foldersByAccount: {
        1: [
          { id: 1, accountId: 1, name: 'INBOX', role: 'inbox' } as Folder,
          { id: 2, accountId: 1, name: 'Projects', role: 'normal' } as Folder,
        ],
      },
      views: [],
      pinned: [],
    }),
  )
  prefs.update((p) => ({ ...p, design: 'new', quickFilterBar: false, threadedList: false, appVimMode: false }))
})

describe('the empty inbox', () => {
  it('shows the inbox zero line in the unified inbox', async () => {
    selection.set({ kind: 'view', view: 'inbox', label: 'Unified Inbox' })
    render(MessageList)
    expect(await screen.findByRole('heading', { name: 'Nothing left in your inbox' })).toBeInTheDocument()
  })

  it('shows it in an account inbox too', async () => {
    selection.set({ kind: 'folder', folderId: 1, accountId: 1, label: 'INBOX' })
    render(MessageList)
    expect(await screen.findByRole('status')).toHaveTextContent('Nothing left in your inbox')
  })

  it('keeps the plain empty state for any other folder', async () => {
    selection.set({ kind: 'folder', folderId: 2, accountId: 1, label: 'Projects' })
    render(MessageList)
    expect(await screen.findByText('No messages here')).toBeInTheDocument()
    expect(screen.queryByText('Nothing left in your inbox')).not.toBeInTheDocument()
  })

  it('is not part of the classic design', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    selection.set({ kind: 'view', view: 'inbox', label: 'Unified Inbox' })
    render(MessageList)
    expect(await screen.findByText('No messages here')).toBeInTheDocument()
  })
})
