import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  getFeedEntryHtml: vi.fn(),
  updateFeed: vi.fn(),
  listFeedSidebar: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const browser = vi.hoisted(() => ({ openURL: vi.fn() }))
vi.mock('../../lib/browser', () => browser)

import FeedReader from './FeedReader.svelte'
import type { FeedRow } from '../../lib/feedlogic'
import type { Feed, FeedEntryDetail } from '../../lib/types'

const feed: Feed = {
  id: 1,
  folderId: 0,
  url: 'https://pod.example/feed',
  siteUrl: 'https://pod.example/',
  title: 'The Pod',
  feedTitle: 'The Pod',
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

const row: FeedRow = {
  key: 'e3',
  id: 3,
  mail: false,
  feedId: 1,
  title: 'Episode 3',
  source: 'The Pod',
  url: 'https://pod.example/3',
  author: 'Hosts',
  published: '2026-11-16T10:00:00Z',
  snippet: '',
  image: 'https://pod.example/cover.jpg',
  read: true,
  starred: false,
  readLater: true,
  media: [],
}

function entry(patch: Partial<FeedEntryDetail> = {}): FeedEntryDetail {
  return {
    ...row,
    feedId: 1,
    media: [
      { url: 'https://pod.example/ep3.mp3', type: 'audio/mpeg', length: 2048000, title: '' },
      { url: 'https://pod.example/slide.png', type: 'image/png', length: 0, title: 'Slide' },
    ],
    bodyHtmlSafe: '<p>Show notes</p>',
    hasRemoteContent: true,
    remoteAllowed: false,
    remoteHosts: ['pod.example'],
    trackingPixels: [],
    ...patch,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('the Feeds reader', () => {
  it('shows a note while nothing is open', () => {
    render(FeedReader, { props: { row: null } })
    expect(screen.getByText('No entry open')).toBeInTheDocument()
  })

  it('shows the entry with its source, byline and toolbar', async () => {
    const events = { star: vi.fn(), later: vi.fn(), toggleRead: vi.fn(), share: vi.fn(), openBrowser: vi.fn(), showFeed: vi.fn() }
    render(FeedReader, {
      props: { row, entry: entry(), feed },
      events: {
        star: () => events.star(),
        later: () => events.later(),
        toggleRead: () => events.toggleRead(),
        share: () => events.share(),
        openBrowser: () => events.openBrowser(),
        showFeed: (e: CustomEvent) => events.showFeed(e.detail),
      },
    })
    expect(screen.getByRole('heading', { level: 1, name: 'Episode 3' })).toBeInTheDocument()
    expect(screen.getByText('Hosts')).toBeInTheDocument()
    expect(screen.getByText('pod.example')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Star' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove from Read later' }))
    await userEvent.click(screen.getByRole('button', { name: 'Mark as unread' }))
    await userEvent.click(screen.getByRole('button', { name: 'Share by mail' }))
    await userEvent.click(screen.getByRole('link', { name: 'Episode 3' }))
    await userEvent.click(screen.getByRole('button', { name: 'The Pod' }))
    expect(events.star).toHaveBeenCalledOnce()
    expect(events.later).toHaveBeenCalledOnce()
    expect(events.toggleRead).toHaveBeenCalledOnce()
    expect(events.share).toHaveBeenCalledOnce()
    expect(events.openBrowser).toHaveBeenCalledOnce()
    expect(events.showFeed).toHaveBeenCalledWith(1)
  })

  it('plays media only on request and holds back remote images', () => {
    const { container } = render(FeedReader, { props: { row, entry: entry(), feed } })
    const audio = container.querySelector('audio')
    expect(audio).not.toBeNull()
    expect(audio?.getAttribute('preload')).toBe('none')
    expect(audio).toHaveAttribute('controls')
    expect(screen.getByText('ep3.mp3')).toBeInTheDocument()
    expect(screen.getByText('Slide')).toBeInTheDocument()
    expect(container.querySelector('img[src="https://pod.example/slide.png"]')).toBeNull()
    expect(container.querySelector('img.hero')).toBeNull()
  })

  it('shows the lead image and image files once the feed allows them', () => {
    const { container } = render(FeedReader, { props: { row, entry: entry({ remoteAllowed: true }), feed } })
    expect(container.querySelector('img.hero')).toHaveAttribute('src', 'https://pod.example/cover.jpg')
    expect(container.querySelector('img[src="https://pod.example/slide.png"]')).not.toBeNull()
  })

  it('opens a file with the browser', async () => {
    render(FeedReader, { props: { row, entry: entry(), feed } })
    await userEvent.click(screen.getAllByRole('link', { name: /Download/ })[0])
    expect(browser.openURL).toHaveBeenCalledWith('https://pod.example/ep3.mp3')
  })

  it('offers to load images once or always for the feed', async () => {
    api.getFeedEntryHtml.mockResolvedValue('<p>Show notes <img src="https://pod.example/a.png"></p>')
    api.updateFeed.mockResolvedValue(undefined)
    api.listFeedSidebar.mockResolvedValue({ folders: [], feeds: [feed], counts: { unread: 0, today: 0, starred: 0, readLater: 0 } })
    render(FeedReader, { props: { row, entry: entry(), feed } })
    await userEvent.click(screen.getByRole('button', { name: /Always for this feed/ }))
    await vi.waitFor(() => expect(api.updateFeed).toHaveBeenCalledWith(expect.objectContaining({ id: 1, loadImages: true })))
    await vi.waitFor(() => expect(api.getFeedEntryHtml).toHaveBeenCalledWith(3, true, false))
  })

  it('says when an entry has no text', () => {
    render(FeedReader, { props: { row, entry: entry({ bodyHtmlSafe: '', media: [] }), feed } })
    expect(screen.getByText(/This entry has no text/)).toBeInTheDocument()
  })
})
