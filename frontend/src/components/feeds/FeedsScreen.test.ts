import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  listFeedSidebar: vi.fn(),
  listFeedEntries: vi.fn(),
  countNewsletterMail: vi.fn(),
  getFeedEntry: vi.fn(),
  getFeedEntryHtml: vi.fn(),
  setFeedEntriesRead: vi.fn(),
  setFeedEntryStarred: vi.fn(),
  setFeedEntryReadLater: vi.fn(),
  searchFeedEntries: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const browser = vi.hoisted(() => ({ openURL: vi.fn() }))
vi.mock('../../lib/browser', () => browser)

import FeedsScreen from './FeedsScreen.svelte'
import { feedList, feedSelected } from '../../stores/feeds'
import { runSpaceAction } from '../../stores/space'
import { spaceSearchProviders } from '../../lib/spacesearch'
import type { FeedEntry, FeedEntryDetail, FeedSidebar } from '../../lib/types'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const sidebar: FeedSidebar = {
  folders: [],
  feeds: [
    {
      id: 1,
      folderId: 0,
      url: 'https://a.example/feed',
      siteUrl: 'https://a.example/',
      title: 'Blog',
      feedTitle: 'Blog',
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
      position: 0,
      unread: 3,
    },
  ],
  counts: { unread: 3, today: 0, starred: 0, readLater: 0 },
}

function entry(id: number, patch: Partial<FeedEntry> = {}): FeedEntry {
  return { id, feedId: 1, title: `Entry ${id}`, url: `https://a.example/${id}`, author: '', published: '2026-11-16T10:00:00Z', snippet: '', image: '', read: false, starred: false, readLater: false, media: [], ...patch }
}

function detail(id: number): FeedEntryDetail {
  return { ...entry(id), bodyHtmlSafe: `<p>Body ${id}</p>`, hasRemoteContent: false, remoteAllowed: false, remoteHosts: [], trackingPixels: [] }
}

beforeEach(() => {
  vi.clearAllMocks()
  feedSelected.set(null)
  feedList.set({ scope: { kind: 'unread' }, unreadOnly: false, search: '', rows: [], loading: false, more: false, error: '' })
  api.listFeedSidebar.mockResolvedValue(structuredClone(sidebar))
  api.countNewsletterMail.mockResolvedValue(0)
  api.listFeedEntries.mockResolvedValue([entry(1), entry(2, { read: true }), entry(3)])
  api.getFeedEntry.mockImplementation((id: number) => Promise.resolve(detail(id)))
  api.setFeedEntriesRead.mockImplementation((ids: number[]) => Promise.resolve(ids))
  api.setFeedEntryStarred.mockResolvedValue(undefined)
  api.setFeedEntryReadLater.mockResolvedValue(undefined)
})

async function ready() {
  const view = render(FeedsScreen)
  await screen.findByText('Entry 3')
  return view
}

describe('the Feeds screen', () => {
  it('opens an entry in the reader and marks it read', async () => {
    await ready()
    await userEvent.click(screen.getByText('Entry 1'))
    expect(await screen.findByRole('heading', { level: 1, name: 'Entry 1' })).toBeInTheDocument()
    await vi.waitFor(() => expect(api.setFeedEntriesRead).toHaveBeenCalledWith([1], true))
    expect(get(feedList).rows[0].read).toBe(true)
  })

  it('moves with j and k, and to the next unread with n', async () => {
    await ready()
    const section = screen.getByRole('region', { name: 'Feeds' })
    await fireEvent.keyDown(section, { key: 'j' })
    await vi.waitFor(() => expect(get(feedSelected)).toBe('e1'))
    await fireEvent.keyDown(section, { key: 'n' })
    await vi.waitFor(() => expect(get(feedSelected)).toBe('e3'))
    await fireEvent.keyDown(section, { key: 'k' })
    await vi.waitFor(() => expect(get(feedSelected)).toBe('e2'))
    await fireEvent.keyDown(section, { key: 'v' })
    expect(browser.openURL).toHaveBeenCalledWith('https://a.example/2')
  })

  it('answers the mail shortcuts for the open entry', async () => {
    await ready()
    await userEvent.click(screen.getByText('Entry 1'))
    await screen.findByRole('heading', { level: 1, name: 'Entry 1' })
    expect(runSpaceAction('feeds', 'flag')).toBe(true)
    await vi.waitFor(() => expect(api.setFeedEntryStarred).toHaveBeenCalledWith(1, true))
    expect(runSpaceAction('feeds', 'snooze')).toBe(true)
    await vi.waitFor(() => expect(api.setFeedEntryReadLater).toHaveBeenCalledWith(1, true))
    expect(runSpaceAction('feeds', 'mark-unread')).toBe(true)
    await vi.waitFor(() => expect(api.setFeedEntriesRead).toHaveBeenLastCalledWith([1], false))
    expect(runSpaceAction('feeds', 'reply')).toBe(true)
  })

  it('shares the open entry by mail', async () => {
    const compose = vi.fn()
    render(FeedsScreen, { events: { compose: (e: CustomEvent) => compose(e.detail) } })
    await screen.findByText('Entry 3')
    await userEvent.click(screen.getByText('Entry 3'))
    await userEvent.click(await screen.findByRole('button', { name: 'Share by mail' }))
    expect(compose).toHaveBeenCalledWith(expect.objectContaining({ subject: 'Entry 3' }))
    expect(compose.mock.calls[0][0].body).toContain('https://a.example/3')
  })

  it('searches every feed from the top bar', async () => {
    api.searchFeedEntries.mockResolvedValue([entry(7, { title: 'Found it' })])
    await ready()
    const provider = get(spaceSearchProviders).feeds
    expect(provider).toBeDefined()
    const hits = await provider!.search('found')
    expect(api.searchFeedEntries).toHaveBeenCalledWith('found', 20)
    expect(hits).toEqual([expect.objectContaining({ id: '7', title: 'Found it', detail: 'Blog' })])
  })
})
