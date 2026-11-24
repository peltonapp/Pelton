import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  setSetting: vi.fn(),
  importOpml: vi.fn(),
  exportOpml: vi.fn(),
  listFeedSidebar: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

import Feeds from './Feeds.svelte'
import { prefs } from '../../../stores/prefs'
import { toasts } from '../../../stores/toast'

beforeEach(() => {
  vi.clearAllMocks()
  toasts.set([])
  prefs.update((p) => ({ ...p, feedsRefreshMinutes: 30, feedsKeepDays: 90, feedsKeepCount: 500, feedsLoadImages: false, feedsMarkReadOnScroll: false }))
  api.setSetting.mockResolvedValue(undefined)
  api.listFeedSidebar.mockResolvedValue({ folders: [], feeds: [], counts: { unread: 0, today: 0, starred: 0, readLater: 0 } })
})

describe('the Feeds settings page', () => {
  it('starts from the defaults: half-hourly, images held back, no marking on scroll', () => {
    render(Feeds)
    expect(screen.getByRole('combobox', { name: 'Refresh feeds' })).toHaveTextContent('Every 30 minutes')
    expect(screen.getByRole('switch', { name: 'Load images in every feed' })).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByRole('switch', { name: 'Mark as read on scroll' })).toHaveAttribute('aria-checked', 'false')
  })

  it('saves the switches under their setting keys', async () => {
    render(Feeds)
    await userEvent.click(screen.getByRole('switch', { name: 'Load images in every feed' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Mark as read on scroll' }))
    expect(api.setSetting).toHaveBeenCalledWith('feeds_load_images', 'true')
    expect(api.setSetting).toHaveBeenCalledWith('feeds_mark_read_on_scroll', 'true')
    expect(get(prefs).feedsMarkReadOnScroll).toBe(true)
  })

  it('imports and exports OPML', async () => {
    api.importOpml.mockResolvedValue({ added: 3, skipped: 1 })
    api.exportOpml.mockResolvedValue('/tmp/feeds.opml')
    render(Feeds)
    await userEvent.click(screen.getByRole('button', { name: 'Import OPML' }))
    await vi.waitFor(() => expect(get(toasts).some((x) => x.message === 'Added 3 feeds, skipped 1')).toBe(true))
    await userEvent.click(screen.getByRole('button', { name: 'Export OPML' }))
    await vi.waitFor(() => expect(get(toasts).some((x) => x.message === 'Feeds exported')).toBe(true))
  })
})
