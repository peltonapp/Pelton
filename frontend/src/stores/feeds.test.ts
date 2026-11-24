import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { FeedEntry, FeedSidebar, MessageSummary } from '../lib/types'

const api = vi.hoisted(() => ({
  listFeedSidebar: vi.fn(),
  listFeedEntries: vi.fn(),
  listNewsletterMail: vi.fn(),
  countNewsletterMail: vi.fn(),
  setFeedEntriesRead: vi.fn(),
  setFeedEntryStarred: vi.fn(),
  setFeedEntryReadLater: vi.fn(),
  markFeedScopeRead: vi.fn(),
  refreshFeeds: vi.fn(),
  setSeen: vi.fn(),
  setFlagged: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import {
  feedList,
  feedSelected,
  feedSidebar,
  feedsUnread,
  loadMoreEntries,
  markAllRead,
  mergeNewEntries,
  newsletterUnread,
  setRowsRead,
  showScope,
  toggleReadLater,
  toggleStar,
  feedPageSize,
} from './feeds'
import { toasts } from './toast'

function entry(id: number, patch: Partial<FeedEntry> = {}): FeedEntry {
  return { id, feedId: 1, title: `Entry ${id}`, url: `https://a.example/${id}`, author: '', published: '2026-11-16T10:00:00Z', snippet: '', image: '', read: false, starred: false, readLater: false, media: [], ...patch }
}

const sidebar: FeedSidebar = {
  folders: [],
  feeds: [
    {
      id: 1,
      folderId: 0,
      url: 'https://a.example/feed',
      siteUrl: '',
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
  counts: { unread: 3, today: 3, starred: 0, readLater: 0 },
}

beforeEach(() => {
  vi.clearAllMocks()
  toasts.set([])
  feedSidebar.set(structuredClone(sidebar))
  feedSelected.set(null)
  newsletterUnread.set(0)
  feedList.set({ scope: { kind: 'unread' }, unreadOnly: false, search: '', rows: [], loading: false, more: false, error: '' })
  api.listFeedSidebar.mockResolvedValue(structuredClone(sidebar))
  api.setFeedEntriesRead.mockImplementation((ids: number[]) => Promise.resolve(ids))
})

describe('showing a scope', () => {
  it('loads the rows of a scope and names their feed', async () => {
    api.listFeedEntries.mockResolvedValue([entry(1), entry(2, { read: true })])
    await showScope({ kind: 'feed', id: 1 }, true)
    expect(api.listFeedEntries).toHaveBeenCalledWith({ kind: 'feed', id: 1 }, true, '', feedPageSize, 0)
    const state = get(feedList)
    expect(state.rows.map((r) => r.key)).toEqual(['e1', 'e2'])
    expect(state.rows[0].source).toBe('Blog')
    expect(state.more).toBe(false)
  })

  it('never narrows a list that is unread by nature', async () => {
    api.listFeedEntries.mockResolvedValue([])
    await showScope({ kind: 'today' }, true)
    expect(api.listFeedEntries).toHaveBeenCalledWith({ kind: 'today' }, false, '', feedPageSize, 0)
  })

  it('clears the selection and the filter when the scope changes', async () => {
    api.listFeedEntries.mockResolvedValue([entry(1)])
    feedList.update((s) => ({ ...s, search: 'old' }))
    feedSelected.set('e9')
    await showScope({ kind: 'starred' })
    expect(get(feedSelected)).toBeNull()
    expect(get(feedList).search).toBe('')
  })

  it('pages on and drops a stale reply', async () => {
    api.listFeedEntries.mockResolvedValueOnce(Array.from({ length: feedPageSize }, (_, i) => entry(i + 1)))
    await showScope({ kind: 'unread' })
    expect(get(feedList).more).toBe(true)
    api.listFeedEntries.mockResolvedValueOnce([entry(feedPageSize), entry(feedPageSize + 1)])
    await loadMoreEntries()
    expect(api.listFeedEntries).toHaveBeenLastCalledWith({ kind: 'unread' }, false, '', feedPageSize, feedPageSize)
    expect(get(feedList).rows).toHaveLength(feedPageSize + 1)
    expect(get(feedList).more).toBe(false)
  })

  it('lists newsletters from mail, filtered by the search text', async () => {
    const mail = [
      { id: 5, subject: 'Weekly digest', fromName: 'News', fromAddress: 'n@x.example', seen: false, flagged: false, date: '', snippet: '' },
      { id: 6, subject: 'Other', fromName: 'Shop', fromAddress: 's@x.example', seen: true, flagged: false, date: '', snippet: '' },
    ] as MessageSummary[]
    api.listNewsletterMail.mockResolvedValue(mail)
    await showScope({ kind: 'newsletters' })
    expect(get(feedList).rows.map((r) => r.key)).toEqual(['m5', 'm6'])
    feedList.update((s) => ({ ...s, search: 'digest' }))
    await showScope({ kind: 'newsletters' })
    expect(get(feedList).rows.map((r) => r.key)).toEqual(['m5'])
    expect(api.listFeedEntries).not.toHaveBeenCalled()
  })
})

describe('changing rows', () => {
  beforeEach(async () => {
    api.listFeedEntries.mockResolvedValue([entry(1), entry(2), entry(3, { read: true })])
    await showScope({ kind: 'unread' })
  })

  it('marks read at once and adjusts the counts', async () => {
    const rows = get(feedList).rows
    await setRowsRead([rows[0], rows[2]], true)
    expect(api.setFeedEntriesRead).toHaveBeenCalledWith([1], true)
    expect(get(feedList).rows.map((r) => r.read)).toEqual([true, false, true])
  })

  it('keeps a read row in the unread list until the scope changes', async () => {
    await setRowsRead([get(feedList).rows[0]], true)
    expect(get(feedList).rows).toHaveLength(3)
  })

  it('marks newsletter mail read through the mail', async () => {
    newsletterUnread.set(2)
    const mailRow = { ...get(feedList).rows[0], key: 'm40', id: 40, mail: true }
    feedList.update((s) => ({ ...s, rows: [mailRow, ...s.rows] }))
    await setRowsRead([mailRow], true)
    expect(api.setSeen).toHaveBeenCalledWith(40, true)
    expect(api.setFeedEntriesRead).not.toHaveBeenCalled()
    expect(get(newsletterUnread)).toBe(1)
  })

  it('stars, and puts back the star when the backend refuses', async () => {
    const row = get(feedList).rows[1]
    await toggleStar(row)
    expect(api.setFeedEntryStarred).toHaveBeenCalledWith(2, true)
    expect(get(feedList).rows[1].starred).toBe(true)

    api.setFeedEntryReadLater.mockRejectedValueOnce(new Error('nope'))
    await toggleReadLater(get(feedList).rows[1])
    expect(get(feedList).rows[1].readLater).toBe(false)
    expect(get(toasts).some((x) => x.kind === 'error')).toBe(true)
  })

  it('marks everything read with an undo', async () => {
    api.markFeedScopeRead.mockResolvedValue([1, 2])
    await markAllRead()
    expect(api.markFeedScopeRead).toHaveBeenCalledWith({ kind: 'unread' }, '')
    expect(get(feedList).rows.every((r) => r.read)).toBe(true)
    const toast = get(toasts).find((x) => x.action)
    expect(toast?.message).toContain('2')
    toast?.action?.run()
    await vi.waitFor(() => expect(api.setFeedEntriesRead).toHaveBeenCalledWith([1, 2], false))
    await vi.waitFor(() => expect(get(feedList).rows.map((r) => r.read)).toEqual([false, false, true]))
  })

  it('says nothing when there was nothing to mark', async () => {
    api.markFeedScopeRead.mockResolvedValue([])
    await markAllRead()
    expect(get(toasts)).toEqual([])
  })

  it('puts entries a refresh brought in front without dropping the rest', async () => {
    await setRowsRead([get(feedList).rows[0]], true)
    api.listFeedEntries.mockResolvedValue([entry(9), entry(2)])
    await mergeNewEntries()
    expect(get(feedList).rows.map((r) => r.key)).toEqual(['e9', 'e1', 'e2', 'e3'])
    expect(get(feedList).rows[1].read).toBe(true)
  })
})

describe('counts', () => {
  it('derives the rail count from the sidebar', () => {
    expect(get(feedsUnread)).toBe(3)
    feedSidebar.update((s) => ({ ...s, counts: { ...s.counts, unread: 0 } }))
    expect(get(feedsUnread)).toBe(0)
  })
})
