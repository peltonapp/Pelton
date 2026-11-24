import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { AttachmentEntry, AttachmentQuery } from '../../lib/types'

const api = vi.hoisted(() => ({
  queryAttachments: vi.fn(),
  fetchAttachment: vi.fn(),
  readAttachment: vi.fn(),
  saveAttachmentsTo: vi.fn(),
  openAttachmentFile: vi.fn(),
  saveAttachment: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import AttachmentsBrowser from './AttachmentsBrowser.svelte'
import { attachmentFilter, attachmentsOpen, emptyAttachmentFilter } from '../../stores/attachments'
import { openMessageId } from '../../stores/selection'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

function entry(id: number, patch: Partial<AttachmentEntry> = {}): AttachmentEntry {
  return {
    id,
    messageId: 100 + id,
    accountId: 1,
    folderId: 2,
    threadId: 0,
    filename: `file-${id}.pdf`,
    contentType: 'application/pdf',
    sizeBytes: 2048 * id,
    kind: 'pdf',
    subject: `Subject ${id}`,
    fromName: 'Ann',
    fromAddress: 'ann@example.com',
    date: '2026-10-01T09:00:00Z',
    downloaded: true,
    ...patch,
  }
}

function lastQuery(): AttachmentQuery {
  const calls = api.queryAttachments.mock.calls
  return calls[calls.length - 1][0]
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.readAttachment.mockResolvedValue({ filename: '', contentType: '', sizeBytes: 0, data: '', tooLarge: false })
  attachmentFilter.set(emptyAttachmentFilter())
  attachmentsOpen.set(true)
})

describe('AttachmentsBrowser', () => {
  it('lists the first page with its total and narrows by type', async () => {
    const user = userEvent.setup()
    api.queryAttachments.mockResolvedValue({ entries: [entry(1), entry(2, { filename: 'notes.txt', kind: 'document' })], total: 2 })
    render(AttachmentsBrowser)

    const list = await screen.findByRole('listbox', { name: 'Attachments' })
    expect(within(list).getAllByRole('option')).toHaveLength(2)
    expect(screen.getByText('2 files')).toBeInTheDocument()
    expect(lastQuery()).toMatchObject({ offset: 0, withTotal: true, kinds: [] })

    await user.click(screen.getByRole('button', { name: /PDFs/ }))
    await waitFor(() => expect(lastQuery().kinds).toEqual(['pdf']))
    expect(screen.getByRole('button', { name: /PDFs/ })).toHaveAttribute('aria-pressed', 'true')
  })

  it('says which tiny images it leaves out', async () => {
    api.queryAttachments.mockResolvedValue({ entries: [], total: 0 })
    render(AttachmentsBrowser)
    expect(await screen.findByText('No attachments in the mail stored here.')).toBeInTheDocument()
    expect(screen.getByText(/images under 2 KB and inline images under 20 KB/)).toBeInTheDocument()
  })

  it('marks a file that is not downloaded and fetches it on request', async () => {
    const user = userEvent.setup()
    const remote = entry(3, { downloaded: false })
    api.queryAttachments.mockResolvedValue({ entries: [remote], total: 1 })
    api.fetchAttachment.mockResolvedValue({ ...remote, downloaded: true })
    render(AttachmentsBrowser)

    await screen.findByRole('listbox', { name: 'Attachments' })
    expect(screen.getAllByText('Not downloaded').length).toBeGreaterThan(0)
    expect(api.readAttachment).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Download from server' }))
    expect(api.fetchAttachment).toHaveBeenCalledWith(3)
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Download from server' })).not.toBeInTheDocument())
  })

  it('saves several selected files to a folder', async () => {
    const user = userEvent.setup()
    api.queryAttachments.mockResolvedValue({ entries: [entry(1), entry(2), entry(3)], total: 3 })
    api.saveAttachmentsTo.mockResolvedValue('/tmp/out')
    render(AttachmentsBrowser)

    const options = await screen.findAllByRole('option')
    await user.click(options[0])
    await user.keyboard('{Shift>}')
    await user.click(options[2])
    await user.keyboard('{/Shift}')
    expect(screen.getByText('3 selected')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Save to folder' }))
    expect(api.saveAttachmentsTo).toHaveBeenCalledWith([
      { messageId: 101, attachmentId: 1 },
      { messageId: 102, attachmentId: 2 },
      { messageId: 103, attachmentId: 3 },
    ])
  })

  it('shows the message a file came with', async () => {
    const user = userEvent.setup()
    api.queryAttachments.mockResolvedValue({ entries: [entry(5)], total: 1 })
    render(AttachmentsBrowser)

    await screen.findByRole('listbox', { name: 'Attachments' })
    await user.click(screen.getByRole('button', { name: 'Show message' }))
    expect(get(attachmentsOpen)).toBe(false)
    expect(get(openMessageId)).toBe(105)
  })
})
