import { afterEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const readAttachment = vi.hoisted(() => vi.fn())
vi.mock('../lib/api', () => ({
  getSetting: vi.fn(async () => ({ value: '', found: false })),
  setSetting: vi.fn(),
  readAttachment,
}))

import { composeSessions, openForward, openReply, switchReplyKind, updateCompose } from './compose'
import { toasts } from './toast'
import type { ComposeAttachment, MessageDetail } from '../lib/types'

function message(): MessageDetail {
  return {
    id: 41,
    accountId: 7,
    folderId: 1,
    fromAddress: 'ann@example.com',
    toAddresses: 'me@example.com',
    ccAddresses: '',
    subject: 'Harvest plan',
    bodyQuote: 'Plan attached.',
    messageId: '<harvest@example.com>',
    references: '',
    attachments: [
      { id: 1, filename: 'plan.pdf', contentType: 'application/pdf', sizeBytes: 4, inline: false },
      { id: 2, filename: 'logo.png', contentType: 'image/png', sizeBytes: 4, inline: true },
      { id: 3, filename: 'field.jpg', contentType: 'image/jpeg', sizeBytes: 4, inline: false },
    ],
  } as unknown as MessageDetail
}

function session(id: number) {
  return get(composeSessions).find((s) => s.id === id)!
}

function names(id: number): string[] {
  return session(id).attachments.map((att) => att.filename)
}

function serve(): void {
  readAttachment.mockImplementation(async (_messageId: number, attachmentId: number) => ({
    filename: '',
    contentType: '',
    sizeBytes: 4,
    data: `data-${attachmentId}`,
    tooLarge: false,
  }))
}

afterEach(() => {
  composeSessions.set([])
  toasts.set([])
  readAttachment.mockReset()
})

describe('forwarding attachments (#689)', () => {
  it('takes the original files along, without the inline images', async () => {
    serve()
    const id = openForward(message(), 'plaintext')
    await vi.waitFor(() => expect(names(id)).toEqual(['plan.pdf', 'field.jpg']))

    expect(readAttachment).toHaveBeenCalledWith(41, 1)
    expect(readAttachment).not.toHaveBeenCalledWith(41, 2)
    expect(session(id).attachments[0]).toEqual({
      filename: 'plan.pdf',
      contentType: 'application/pdf',
      contentBase64: 'data-1',
      inline: false,
      contentId: '',
    })
  })

  it('leaves a reply without them', async () => {
    serve()
    const id = openReply(message(), 'plaintext', false)
    await Promise.resolve()

    expect(readAttachment).not.toHaveBeenCalled()
    expect(names(id)).toEqual([])
  })

  it('adds them when a reply becomes a forward and takes them out again on the way back', async () => {
    serve()
    const own: ComposeAttachment = { filename: 'mine.txt', contentType: 'text/plain', contentBase64: 'bWluZQ==', inline: false, contentId: '' }
    const id = openReply(message(), 'plaintext', false)
    updateCompose(id, { attachments: [own] })

    switchReplyKind(id, 'forward')
    await vi.waitFor(() => expect(names(id)).toEqual(['mine.txt', 'plan.pdf', 'field.jpg']))

    switchReplyKind(id, 'reply')
    expect(names(id)).toEqual(['mine.txt'])
  })

  it('names a file that is too large and still adds the others', async () => {
    readAttachment.mockImplementation(async (_messageId: number, attachmentId: number) => ({
      filename: '',
      contentType: '',
      sizeBytes: 4,
      data: attachmentId === 1 ? '' : 'data',
      tooLarge: attachmentId === 1,
    }))
    const id = openForward(message(), 'plaintext')
    await vi.waitFor(() => expect(names(id)).toEqual(['field.jpg']))

    expect(get(toasts).map((toast) => toast.message).join(' ')).toContain('plan.pdf')
  })

  it('stops adding files once the forward is a reply again', async () => {
    let release: (value: unknown) => void = () => {}
    readAttachment.mockImplementation(() => new Promise((resolve) => {
      release = resolve
    }))
    const id = openForward(message(), 'plaintext')
    switchReplyKind(id, 'reply')
    release({ filename: '', contentType: '', sizeBytes: 4, data: 'data', tooLarge: false })
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(names(id)).toEqual([])
    expect(readAttachment).toHaveBeenCalledTimes(1)
  })
})
