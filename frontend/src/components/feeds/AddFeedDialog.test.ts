import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  discoverFeeds: vi.fn(),
  addFeed: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import AddFeedDialog from './AddFeedDialog.svelte'

// the dialog opens with a transition, and jsdom has no web animations. A
// finished stub lets svelte run the transition to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}
import type { FeedCandidate } from '../../lib/types'

function candidate(patch: Partial<FeedCandidate>): FeedCandidate {
  return { url: 'https://site.example/feed.xml', title: 'Site', siteUrl: 'https://site.example/', kind: 'rss', entryCount: 12, latest: ['First', 'Second'], lastPost: '2026-11-15T10:00:00Z', subscribed: false, ...patch }
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
})

afterEach(() => {
  document.documentElement.removeAttribute('data-reduce-motion')
  vi.clearAllMocks()
})

describe('adding a feed', () => {
  it('finds the feeds of a site, previews them and subscribes to the chosen one', async () => {
    api.discoverFeeds.mockResolvedValue([candidate({}), candidate({ url: 'https://site.example/comments.xml', title: 'Comments', entryCount: 3, latest: [] })])
    api.addFeed.mockResolvedValue({ id: 5 })
    const added = vi.fn()
    render(AddFeedDialog, { props: { folders: [{ id: 2, name: 'Tech', position: 0 }], folderId: 2 }, events: { added: (e: CustomEvent) => added(e.detail) } })

    const subscribe = screen.getByRole('button', { name: 'Subscribe' })
    expect(subscribe).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Website or feed address'), 'site.example{Enter}')
    expect(api.discoverFeeds).toHaveBeenCalledWith('site.example')
    expect(await screen.findByText('Found 2 feeds')).toBeInTheDocument()
    expect(screen.getByText('First')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /Site/ })).toBeChecked()

    await userEvent.click(screen.getByRole('radio', { name: /Comments/ }))
    await userEvent.type(screen.getByLabelText('Name'), 'Talk')
    await userEvent.click(subscribe)
    expect(api.addFeed).toHaveBeenCalledWith('https://site.example/comments.xml', 2, 'Talk')
    await vi.waitFor(() => expect(added).toHaveBeenCalledWith({ id: 5 }))
  })

  it('says why nothing was found', async () => {
    api.discoverFeeds.mockRejectedValue(new Error('pelton: no feed found at this address'))
    render(AddFeedDialog, { props: { folders: [] } })
    await userEvent.type(screen.getByLabelText('Website or feed address'), 'nothing.example')
    await userEvent.click(screen.getByRole('button', { name: 'Find' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('no feed found')
  })

  it('will not subscribe twice', async () => {
    api.discoverFeeds.mockResolvedValue([candidate({ subscribed: true })])
    render(AddFeedDialog, { props: { folders: [], address: 'site.example' } })
    expect(await screen.findByText(/already added/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Subscribe' })).toBeDisabled()
  })
})
