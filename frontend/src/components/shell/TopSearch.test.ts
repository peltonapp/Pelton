import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  searchAddresses: vi.fn(),
  getSetting: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  searchAddresses: api.searchAddresses,
  getSetting: api.getSetting,
  setSetting: api.setSetting,
}))

import TopSearch from './TopSearch.svelte'
import { activeSearch, clearSearch, recentSearches } from '../../stores/search'
import { sidebar } from '../../stores/accounts'
import { ready } from '../../lib/async'
import type { Account, Folder } from '../../lib/types'

function folder(id: number, name: string): Folder {
  return {
    id,
    accountId: 1,
    name,
    imapPath: name,
    delimiter: '/',
    role: '',
    unreadCount: 0,
    totalCount: 0,
    attributes: [],
    pinned: false,
    roleOverride: '',
    syncExcluded: false,
  }
}

beforeEach(() => {
  api.searchAddresses.mockReset().mockResolvedValue([{ email: 'ada@example.com', name: 'Ada Lovelace', useCount: 1, lastUsed: '', createdAt: '' }])
  api.getSetting.mockReset().mockResolvedValue({ value: '', found: false })
  api.setSetting.mockReset().mockResolvedValue(undefined)
  clearSearch()
  recentSearches.set([])
  sidebar.set(
    ready({
      accounts: [{ id: 1, email: 'me@example.com', displayName: '', localLabel: '', useLocalLabel: false } as Account],
      foldersByAccount: { 1: [folder(10, 'INBOX'), folder(11, 'Archive')] },
      views: [],
      pinned: [],
    }),
  )
})

describe('TopSearch', () => {
  it('runs the typed text on enter and remembers it', async () => {
    const user = userEvent.setup()
    render(TopSearch)
    const field = screen.getByRole('combobox', { name: 'Search mail' })
    await user.type(field, 'invoice{Enter}')
    expect(get(activeSearch)).toEqual({ text: 'invoice', chips: [] })
    expect(get(recentSearches)[0]).toEqual({ text: 'invoice', chips: [] })
    expect(api.setSetting).toHaveBeenCalledWith('recent_searches', JSON.stringify([{ text: 'invoice', chips: [] }]))
  })

  it('turns typed operators into chips', async () => {
    const user = userEvent.setup()
    render(TopSearch)
    await user.type(screen.getByRole('combobox'), 'report is:unread{Enter}')
    expect(get(activeSearch)).toEqual({ text: 'report', chips: [{ field: 'is', value: 'unread' }] })
    expect(screen.getByRole('button', { name: 'Remove filter: Unread' })).toBeInTheDocument()
  })

  it('completes from: with people from the address book', async () => {
    const user = userEvent.setup()
    render(TopSearch)
    await user.type(screen.getByRole('combobox'), 'from:ad')
    const option = await screen.findByRole('option', { name: /Ada Lovelace/ })
    await user.click(option)
    expect(get(activeSearch)?.chips).toEqual([{ field: 'from', value: 'ada@example.com', label: 'Ada Lovelace' }])
    expect(screen.getByRole('combobox')).toHaveValue('')
  })

  it('completes in: with real folders and moves through them with the arrow keys', async () => {
    const user = userEvent.setup()
    render(TopSearch)
    const field = screen.getByRole('combobox')
    await user.type(field, 'in:arc')
    expect(await screen.findByRole('option', { name: /Archive/ })).toBeInTheDocument()
    await user.keyboard('{ArrowDown}')
    expect(screen.getByRole('option', { name: /Archive/ })).toHaveAttribute('aria-selected', 'true')
    await user.keyboard('{Enter}')
    expect(get(activeSearch)?.chips).toEqual([{ field: 'in', value: 'Archive', id: 11, label: 'Archive' }])
  })

  it('offers recent searches and a way to clear them', async () => {
    recentSearches.set([{ text: 'invoice', chips: [] }])
    const user = userEvent.setup()
    render(TopSearch)
    await user.click(screen.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: /Clear recent searches/ }))
    expect(get(recentSearches)).toEqual([])
    expect(api.setSetting).toHaveBeenCalledWith('recent_searches', '[]')
  })

  it('clears the search on the second escape', async () => {
    const user = userEvent.setup()
    render(TopSearch)
    const field = screen.getByRole('combobox')
    await user.type(field, 'invoice{Enter}')
    expect(get(activeSearch)).not.toBeNull()
    await user.click(field)
    await user.keyboard('{Escape}')
    expect(get(activeSearch)).not.toBeNull()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(get(activeSearch)).toBeNull())
    expect(field).toHaveValue('')
  })

  it('removes the last chip with backspace in an empty field', async () => {
    const user = userEvent.setup()
    render(TopSearch)
    const field = screen.getByRole('combobox')
    await user.type(field, 'has:attachment{Enter}')
    expect(get(activeSearch)?.chips).toHaveLength(1)
    await user.keyboard('{Backspace}')
    expect(get(activeSearch)).toBeNull()
  })
})
