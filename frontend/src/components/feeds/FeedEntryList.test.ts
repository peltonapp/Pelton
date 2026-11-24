import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import FeedEntryList from './FeedEntryList.svelte'
import type { FeedRow } from '../../lib/feedlogic'
import type { Feed } from '../../lib/types'

function row(id: number, patch: Partial<FeedRow> = {}): FeedRow {
  return {
    key: `e${id}`,
    id,
    mail: false,
    feedId: 1,
    title: `Entry ${id}`,
    source: 'Blog',
    url: `https://a.example/${id}`,
    author: 'Ada',
    published: '2026-11-16T10:00:00Z',
    snippet: `Text of ${id}`,
    image: 'https://a.example/thumb.png',
    read: false,
    starred: false,
    readLater: false,
    media: [],
    ...patch,
  }
}

const feed: Feed = {
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
  unread: 0,
}

function mount(props: Record<string, unknown> = {}, events: Record<string, (e: CustomEvent) => void> = {}) {
  return render(FeedEntryList, {
    props: {
      rows: [row(1), row(2, { read: true, starred: true, readLater: true })],
      selectedKey: null,
      title: 'All unread',
      canFilter: true,
      unreadOnly: false,
      search: '',
      loading: false,
      more: false,
      error: '',
      feeds: [feed],
      loadImages: false,
      singleFeed: false,
      noFeeds: false,
      newsletters: false,
      progress: null,
      markOnScroll: false,
      sideToggle: false,
      sideOpen: false,
      timeFormat: '24',
      ...props,
    },
    events,
  })
}

describe('the entry list', () => {
  it('shows each row with its source, title, text and marks', () => {
    mount()
    const list = screen.getByRole('listbox', { name: 'All unread' })
    const options = within(list).getAllByRole('option')
    expect(options).toHaveLength(2)
    expect(options[0]).toHaveAccessibleName('Unread, Entry 1')
    expect(options[1]).toHaveAccessibleName('Entry 2')
    expect(within(options[0]).getByText('Blog')).toBeInTheDocument()
    expect(within(options[0]).getByText('Text of 1')).toBeInTheDocument()
    expect(within(options[1]).getByTitle('Read later')).toBeInTheDocument()
    expect(within(options[1]).getByRole('button', { name: 'Remove star' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('1 unread')).toBeInTheDocument()
  })

  it('leaves thumbnails out until the feed may load images', async () => {
    const { container, rerender } = mount()
    expect(container.querySelector('img.thumb')).toBeNull()
    await rerender({ feeds: [{ ...feed, loadImages: true }] })
    expect(container.querySelectorAll('img.thumb')).toHaveLength(2)
  })

  it('reports clicks, stars and the bar buttons', async () => {
    const select = vi.fn()
    const star = vi.fn()
    const markAll = vi.fn()
    const unreadOnly = vi.fn()
    mount({}, { select: (e) => select(e.detail.key), star: (e) => star(e.detail.key), markAll: () => markAll(), unreadOnly: (e) => unreadOnly(e.detail) })
    await userEvent.click(screen.getByText('Entry 2'))
    expect(select).toHaveBeenCalledWith('e2')
    await userEvent.click(screen.getAllByRole('button', { name: 'Star' })[0])
    expect(star).toHaveBeenCalledWith('e1')
    expect(select).toHaveBeenCalledTimes(1)
    await userEvent.click(screen.getByRole('button', { name: 'Mark all as read' }))
    expect(markAll).toHaveBeenCalledOnce()
    await userEvent.click(screen.getByRole('radio', { name: 'Unread' }))
    expect(unreadOnly).toHaveBeenCalledWith(true)
  })

  it('filters after typing stops', async () => {
    const search = vi.fn()
    mount({}, { search: (e) => search(e.detail) })
    await userEvent.type(screen.getByRole('searchbox', { name: 'Filter' }), 'rust')
    await vi.waitFor(() => expect(search).toHaveBeenLastCalledWith('rust'))
    expect(search).toHaveBeenCalledTimes(1)
  })

  it('welcomes a first visit with add and import', async () => {
    const add = vi.fn()
    const importOpml = vi.fn()
    mount({ rows: [], noFeeds: true }, { add: () => add(), importOpml: () => importOpml() })
    expect(screen.getByText('Follow sites you read')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Add feed' }))
    await userEvent.click(screen.getByRole('button', { name: 'Import OPML' }))
    expect(add).toHaveBeenCalledOnce()
    expect(importOpml).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()
  })

  it('shows the refresh as a progress pie', () => {
    mount({ progress: { running: true, done: 1, total: 4, feed: 'Blog', failed: 0 } })
    expect(screen.getByRole('progressbar', { name: 'Refreshing feeds' })).toBeInTheDocument()
    expect(screen.getByText('Refreshing 1 of 4')).toBeInTheDocument()
  })

  it('has no all or unread switch where the list is unread by nature', () => {
    mount({ canFilter: false })
    expect(screen.queryByRole('radiogroup', { name: 'Show' })).toBeNull()
  })
})
