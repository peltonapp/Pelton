import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  listNewsletters: vi.fn(),
  newsletterMessageIds: vi.fn(),
  deleteMessages: vi.fn(),
  undoDeletes: vi.fn(),
  archiveMessages: vi.fn(),
  restoreMessage: vi.fn(),
  unsubscribeMessage: vi.fn(),
  getMessage: vi.fn(),
  getSetting: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const browser = vi.hoisted(() => ({ openURL: vi.fn() }))
vi.mock('../../lib/browser', () => browser)

import NewsletterManager from './NewsletterManager.svelte'
import { confirmRequest } from '../../stores/confirm'
import { activeSearch, clearSearch } from '../../stores/search'
import { mailScreen } from '../../stores/mailscreen'
import { toasts } from '../../stores/toast'
import { contextMenu } from '../../stores/contextmenu'
import type { NewsletterSender } from '../../lib/mailstats'

function sender(over: Partial<NewsletterSender>): NewsletterSender {
  return {
    email: 'news@example.com',
    name: 'Weekly News',
    listId: '',
    count: 3,
    unread: 1,
    bytes: 2048,
    firstDate: '2026-09-01T00:00:00Z',
    lastDate: '2026-11-10T00:00:00Z',
    perWeek: 1,
    latestId: 30,
    unsubscribeKind: 'oneclick',
    unsubscribedAt: '',
    ...over,
  }
}

const senders = [
  sender({}),
  sender({ email: 'shop@example.org', name: 'Shop', count: 9, bytes: 10, latestId: 90, unsubscribeKind: 'link' }),
]

// answers the open confirmation once it appears, and returns its title and
// the label of its confirm button.
async function answer(ok: boolean): Promise<string> {
  await waitFor(() => expect(get(confirmRequest)).not.toBeNull())
  const request = get(confirmRequest)!
  request.resolve(ok)
  return `${request.title} | ${request.confirmLabel}`
}

function row(name: string): HTMLElement {
  return screen.getByText(name).closest('tr') as HTMLElement
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  browser.openURL.mockReset()
  api.listNewsletters.mockResolvedValue(senders)
  api.getSetting.mockResolvedValue({ value: '', found: false })
  api.setSetting.mockResolvedValue(undefined)
  confirmRequest.set(null)
  contextMenu.set(null)
  toasts.set([])
  clearSearch()
  mailScreen.set('newsletters')
})

describe('NewsletterManager', () => {
  it('lists the senders by volume and re-sorts by storage', async () => {
    const user = userEvent.setup()
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    const names = (): string[] => screen.getAllByRole('row').slice(1).map((r) => within(r).getAllByRole('cell')[1].querySelector('.name')!.textContent ?? '')
    expect(names()).toEqual(['Shop', 'Weekly News'])
    await user.click(screen.getByRole('radio', { name: 'Storage' }))
    expect(names()).toEqual(['Weekly News', 'Shop'])
    expect(screen.getByText('2 senders')).toBeInTheDocument()
  })

  it('filters by text', async () => {
    const user = userEvent.setup()
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    await user.type(screen.getByRole('textbox', { name: 'Filter senders' }), 'shop')
    expect(screen.queryByText('Weekly News')).not.toBeInTheDocument()
    expect(screen.getByText('Shop')).toBeInTheDocument()
  })

  it('unsubscribes through the existing flow after a confirmation and marks the row', async () => {
    const user = userEvent.setup()
    api.unsubscribeMessage.mockResolvedValue(undefined)
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    await user.click(within(row('Weekly News')).getByRole('button', { name: 'Unsubscribe from Weekly News' }))
    expect(await answer(true)).toBe('Unsubscribe from Weekly News? | Unsubscribe')
    await waitFor(() => expect(api.unsubscribeMessage).toHaveBeenCalledWith(30))
    expect(await within(row('Weekly News')).findByText('Unsubscribed')).toBeInTheDocument()
  })

  it('does nothing when the unsubscribe is declined', async () => {
    const user = userEvent.setup()
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    await user.click(within(row('Weekly News')).getByRole('button', { name: 'Unsubscribe from Weekly News' }))
    await answer(false)
    expect(api.unsubscribeMessage).not.toHaveBeenCalled()
  })

  it('opens a link-only unsubscribe in the browser rather than posting it', async () => {
    const user = userEvent.setup()
    api.getMessage.mockResolvedValue({ unsubscribe: { kind: 'link', target: 'https://shop.example/out', done: false } })
    render(NewsletterManager)
    await screen.findByText('Shop')
    await user.click(within(row('Shop')).getByRole('button', { name: 'Unsubscribe from Shop' }))
    await answer(true)
    await waitFor(() => expect(browser.openURL).toHaveBeenCalledWith('https://shop.example/out'))
    expect(api.unsubscribeMessage).not.toHaveBeenCalled()
  })

  it('keeps only the latest: counts, confirms with the count, deletes, and undoes', async () => {
    const user = userEvent.setup()
    api.newsletterMessageIds.mockResolvedValue([21, 22])
    api.deleteMessages.mockResolvedValue({ done: [21, 22], failed: 0, firstError: '' })
    api.undoDeletes.mockResolvedValue({ done: [21, 22], failed: 0, firstError: '' })
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    await user.click(within(row('Weekly News')).getByRole('button', { name: 'Keep only the latest from Weekly News' }))
    expect(await answer(true)).toBe('Keep only the latest and delete the rest? | Delete 2')
    expect(api.newsletterMessageIds).toHaveBeenCalledWith({ emails: ['news@example.com'], olderThanDays: 0, keepLatest: true, forArchive: false })
    await waitFor(() => expect(api.deleteMessages).toHaveBeenCalledWith([21, 22]))
    const toast = get(toasts).find((x) => x.action)
    expect(toast?.message).toBe('Deleted 2 messages')
    toast!.action!.run()
    await waitFor(() => expect(api.undoDeletes).toHaveBeenCalledWith([21, 22]))
  })

  it('deletes nothing when the confirmation is declined', async () => {
    const user = userEvent.setup()
    api.newsletterMessageIds.mockResolvedValue([21])
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    await user.click(within(row('Weekly News')).getByRole('button', { name: 'Delete older mail from Weekly News' }))
    const entry = get(contextMenu)?.entries.find((e) => typeof e === 'object' && 'label' in e && e.label === 'Older than 3 months')
    expect(entry).toBeTruthy()
    ;(entry as { action: () => void }).action()
    expect(await answer(false)).toBe('Delete mail older than 90 days? | Delete 1')
    expect(api.newsletterMessageIds).toHaveBeenCalledWith(expect.objectContaining({ olderThanDays: 90 }))
    expect(api.deleteMessages).not.toHaveBeenCalled()
  })

  it('archives a multi-selection in one call', async () => {
    const user = userEvent.setup()
    api.newsletterMessageIds.mockResolvedValue([1, 2, 3])
    api.archiveMessages.mockResolvedValue({ done: [1, 2, 3], failed: 0, firstError: '', undo: [] })
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    await user.click(screen.getByRole('checkbox', { name: 'Select all senders' }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    await user.click(within(screen.getByRole('toolbar', { name: 'Actions for the selected senders' })).getByRole('button', { name: /Archive all/ }))
    expect(await answer(true)).toBe('Archive all of this mail? | Archive 3')
    expect(api.newsletterMessageIds).toHaveBeenCalledWith(expect.objectContaining({ forArchive: true }))
    expect(api.newsletterMessageIds.mock.calls[0][0].emails.sort()).toEqual(['news@example.com', 'shop@example.org'])
    await waitFor(() => expect(api.archiveMessages).toHaveBeenCalledWith([1, 2, 3]))
  })

  it('shows the messages of a sender as a from: search and closes', async () => {
    const user = userEvent.setup()
    render(NewsletterManager)
    await screen.findByText('Weekly News')
    await user.click(within(row('Weekly News')).getByRole('button', { name: 'Show messages from Weekly News' }))
    expect(get(activeSearch)).toEqual({ text: '', chips: [{ field: 'from', value: 'news@example.com', label: 'Weekly News' }] })
    expect(get(mailScreen)).toBeNull()
  })

  it('closes on Escape', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    render(NewsletterManager, { events: { close } })
    await screen.findByText('Weekly News')
    await user.keyboard('{Escape}')
    expect(close).toHaveBeenCalled()
  })
})
