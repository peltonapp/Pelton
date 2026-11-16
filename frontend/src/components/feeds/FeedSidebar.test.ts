import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  createFeedFolder: vi.fn(),
  renameFeedFolder: vi.fn(),
  deleteFeedFolder: vi.fn(),
  moveFeeds: vi.fn(),
  reorderFeedFolders: vi.fn(),
  refreshFeed: vi.fn(),
  listFeedSidebar: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/confirm', () => ({ askConfirm: vi.fn().mockResolvedValue(true) }))

import FeedSidebar from './FeedSidebar.svelte'
import { feedSidebar, newsletterUnread } from '../../stores/feeds'
import type { Feed, FeedSidebar as Sidebar } from '../../lib/types'

function feed(id: number, patch: Partial<Feed> = {}): Feed {
  return {
    id,
    folderId: 0,
    url: `https://f${id}.example/feed`,
    siteUrl: '',
    title: `Feed ${id}`,
    feedTitle: '',
    customTitle: '',
    description: '',
    icon: '',
    error: '',
    errorCount: 0,
    lastFetched: '',
    lastSuccess: '',
    nextFetch: '',
    intervalMinutes: 0,
    loadImages: false,
    notify: false,
    position: id,
    unread: 0,
    ...patch,
  }
}

const side: Sidebar = {
  folders: [{ id: 7, name: 'Tech', position: 0 }],
  feeds: [feed(1, { unread: 4 }), feed(2, { folderId: 7, unread: 2, error: 'the feed was not found (404)' }), feed(3, { folderId: 7, unread: 1 })],
  counts: { unread: 7, today: 2, starred: 1, readLater: 0 },
}

beforeEach(() => {
  vi.clearAllMocks()
  feedSidebar.set(structuredClone(side))
  newsletterUnread.set(5)
  api.listFeedSidebar.mockResolvedValue(structuredClone(side))
  api.createFeedFolder.mockResolvedValue({ id: 8, name: 'News', position: 1 })
})

describe('the Feeds sidebar', () => {
  it('lists the smart views with their counts', () => {
    render(FeedSidebar, { props: { scope: { kind: 'unread' } } })
    expect(screen.getByRole('button', { name: /All unread\s*7/ })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('button', { name: /Today\s*2/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Newsletters\s*5/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Read later$/ })).toBeInTheDocument()
  })

  it('shows feeds in and out of folders, a folder with its total, and why a feed failed', () => {
    render(FeedSidebar, { props: { scope: { kind: 'feed', id: 2 } } })
    expect(screen.getByRole('button', { name: /^Feed 1\s*4/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Tech\s*3/ })).toBeInTheDocument()
    const failing = screen.getByRole('button', { name: /^Feed 2/ })
    expect(failing).toHaveAttribute('aria-current', 'page')
    expect(within(failing).getByLabelText('Last refresh failed: the feed was not found (404)')).toBeInTheDocument()
  })

  it('picks a scope and folds a folder', async () => {
    const scope = vi.fn()
    render(FeedSidebar, { props: { scope: { kind: 'unread' } }, events: { scope: (e: CustomEvent) => scope(e.detail) } })
    await userEvent.click(screen.getByRole('button', { name: /Starred/ }))
    expect(scope).toHaveBeenLastCalledWith({ kind: 'starred' })
    await userEvent.click(screen.getByRole('button', { name: /^Tech/ }))
    expect(scope).toHaveBeenLastCalledWith({ kind: 'folder', id: 7 })
    await userEvent.click(screen.getByRole('button', { name: 'Collapse folder' }))
    expect(screen.queryByRole('button', { name: /^Feed 3/ })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Expand folder' }))
    expect(screen.getByRole('button', { name: /^Feed 3/ })).toBeInTheDocument()
  })

  it('makes a folder', async () => {
    render(FeedSidebar, { props: { scope: { kind: 'unread' } } })
    await userEvent.click(screen.getByRole('button', { name: 'New folder' }))
    await userEvent.type(screen.getByLabelText('Folder name'), 'News')
    await userEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(api.createFeedFolder).toHaveBeenCalledWith('News')
    await vi.waitFor(() => expect(api.listFeedSidebar).toHaveBeenCalled())
  })

  it('asks for the add dialog', async () => {
    const add = vi.fn()
    render(FeedSidebar, { props: { scope: { kind: 'unread' } }, events: { add: (e: CustomEvent) => add(e.detail) } })
    await userEvent.click(screen.getAllByRole('button', { name: 'Add feed' })[0])
    expect(add).toHaveBeenCalledWith(0)
  })
})
