import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Contact, MessageDetail, SenderOverview } from '../../lib/types'

const api = vi.hoisted(() => ({
  senderOverview: vi.fn(),
  listContacts: vi.fn(),
  listAddressBooks: vi.fn(),
  addVIPSender: vi.fn(),
  removeVIPSender: vi.fn(),
  unsubscribeMessage: vi.fn(),
  senderPhotos: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import SenderPanel from './SenderPanel.svelte'
import { contactSeed } from '../../stores/contacts'
import { space } from '../../stores/space'
import { activeSearch } from '../../stores/search'
import { attachmentFilter, attachmentsOpen } from '../../stores/attachments'
import { vipSenders } from '../../stores/vip'
import { editingView } from '../../stores/views'
import { loadAllContacts } from '../../stores/sender'

// the panel slides in, and jsdom has no web animations. A finished stub lets
// svelte run the transition to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

function detail(address: string, patch: Partial<MessageDetail> = {}): MessageDetail {
  return {
    id: 7,
    accountId: 1,
    folderId: 2,
    fromName: 'Ann Example',
    fromAddress: `Ann Example <${address}>`,
    subject: 'Plans',
    date: '2026-10-01T09:00:00Z',
    unsubscribe: null,
    attachments: [],
    ...patch,
  } as MessageDetail
}

function overview(address: string, patch: Partial<SenderOverview> = {}): SenderOverview {
  return {
    address,
    count: 12,
    first: '2025-03-01T00:00:00Z',
    last: '2026-10-01T09:00:00Z',
    recent: [{ id: 7, subject: 'Plans', date: '2026-10-01T09:00:00Z' } as never, { id: 3, subject: 'Older talk', date: '2026-09-01T09:00:00Z' } as never],
    files: [
      { id: 40, messageId: 3, filename: 'budget.xlsx', kind: 'spreadsheet', sizeBytes: 9000, contentType: '' } as never,
    ],
    filesTotal: 30,
    automated: false,
    ...patch,
  }
}

const contact: Contact = {
  id: 9,
  bookId: 1,
  bookName: 'Home',
  uid: 'u',
  fullName: 'Ann Contact',
  organization: 'Example Ltd',
  title: 'Engineer',
  note: '',
  emails: [
    { value: 'ann.contact@example.com', label: '' },
    { value: 'ann@work.example', label: 'work' },
  ],
  phones: [],
  matrixId: '',
  readOnly: false,
  updated: '',
  extra: [],
}

beforeEach(async () => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.senderPhotos.mockResolvedValue([])
  api.listAddressBooks.mockResolvedValue([])
  api.listContacts.mockResolvedValue([contact])
  api.addVIPSender.mockResolvedValue(undefined)
  api.setSetting.mockResolvedValue(undefined)
  vipSenders.set(new Set())
  contactSeed.set(null)
  activeSearch.set(null)
  attachmentsOpen.set(false)
  editingView.set(null)
  await loadAllContacts(true)
})

describe('SenderPanel', () => {
  it('shows what the local mail knows about a stranger and offers to add them', async () => {
    const user = userEvent.setup()
    api.senderOverview.mockResolvedValue(overview('stranger@example.com'))
    render(SenderPanel, { detail: detail('stranger@example.com') })

    expect(screen.getByRole('heading', { name: 'Ann Example' })).toBeInTheDocument()
    expect(await screen.findByText(/12 messages since/)).toBeInTheDocument()
    expect(screen.getByText('Older talk')).toBeInTheDocument()
    expect(screen.getByText('budget.xlsx')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Add to contacts' }))
    expect(get(contactSeed)).toEqual({ name: 'Ann Example', email: 'stranger@example.com' })
    expect(get(space)).toBe('contacts')
  })

  it('shows a contact with its other addresses', async () => {
    api.senderOverview.mockResolvedValue(overview('ann.contact@example.com'))
    render(SenderPanel, { detail: detail('ann.contact@example.com') })

    expect(await screen.findByRole('heading', { name: 'Ann Contact' })).toBeInTheDocument()
    expect(screen.getByText('Engineer, Example Ltd')).toBeInTheDocument()
    expect(screen.getByText('ann@work.example')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit contact' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add to contacts' })).not.toBeInTheDocument()
  })

  it('marks a sender as VIP, searches their mail and saves a view', async () => {
    const user = userEvent.setup()
    api.senderOverview.mockResolvedValue(overview('vip@example.com'))
    render(SenderPanel, { detail: detail('vip@example.com') })

    await user.click(screen.getByRole('button', { name: 'Mark as VIP' }))
    expect(api.addVIPSender).toHaveBeenCalledWith('vip@example.com')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remove from VIPs' })).toHaveAttribute('aria-pressed', 'true'))

    await user.click(screen.getByRole('button', { name: 'All mail from them' }))
    expect(get(activeSearch)).toEqual({ text: '', chips: [{ field: 'from', value: 'vip@example.com' }] })

    await user.click(screen.getByRole('button', { name: 'Save as view' }))
    expect(get(editingView)?.queryFrom).toEqual(['vip@example.com'])
  })

  it('opens every file from the sender in the attachments browser', async () => {
    const user = userEvent.setup()
    api.senderOverview.mockResolvedValue(overview('files@example.com'))
    render(SenderPanel, { detail: detail('files@example.com') })

    await user.click(await screen.findByRole('button', { name: 'All 30 files' }))
    expect(get(attachmentsOpen)).toBe(true)
    expect(get(attachmentFilter).sender).toBe('files@example.com')
  })

  it('treats a mailing list as a list, not a person', async () => {
    const user = userEvent.setup()
    api.senderOverview.mockResolvedValue(overview('news@list.example'))
    api.unsubscribeMessage.mockResolvedValue(undefined)
    render(SenderPanel, {
      detail: detail('news@list.example', { fromName: 'Weekly News', unsubscribe: { kind: 'oneclick', target: 'https://list.example/u', done: false } }),
    })

    expect(screen.getByText('Mailing list')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Write' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add to contacts' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Unsubscribe' }))
    expect(api.unsubscribeMessage).toHaveBeenCalledWith(7)
    expect(await screen.findByText('You unsubscribed from this list.')).toBeInTheDocument()
  })
})
