import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { MessageDetail } from '../lib/types'
import { idle, ready } from '../lib/async'

vi.mock('../lib/api', () => ({
  getMessage: vi.fn(),
}))

import { getMessage } from '../lib/api'
import { bodyLoading, loadMessage, messageDetail, refreshMessage } from './message'

const fetchMessage = vi.mocked(getMessage)

function detail(id: number, over: Partial<MessageDetail> = {}): MessageDetail {
  return {
    id,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: 'Hello',
    fromName: 'Ada',
    fromAddress: 'ada@example.com',
    snippet: '',
    date: '2026-09-21T12:00:00Z',
    seen: true,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    toAddresses: '',
    ccAddresses: '',
    bodyPlain: 'preview',
    bodyQuote: 'preview',
    bodyHtmlSafe: '',
    isHtml: false,
    hasRemoteContent: false,
    remoteAllowed: false,
    remoteHosts: [],
    trackingPixels: [],
    attachments: [],
    phishing: { level: 'none' },
    unsubscribe: null,
    charsetGuess: '',
    pgpState: '',
    bodyComplete: false,
    ...over,
  }
}

beforeEach(() => {
  fetchMessage.mockReset()
  messageDetail.set(idle())
  bodyLoading.set(false)
})

describe('refreshMessage', () => {
  it('keeps the preview on screen while the full body loads', async () => {
    const stub = detail(7)
    messageDetail.set(ready(stub))
    fetchMessage.mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(() => resolve(detail(7, { bodyPlain: 'full text', bodyComplete: true })), 20)
        }),
    )

    void refreshMessage(7)
    expect(get(messageDetail).data?.bodyPlain).toBe('preview')
    expect(get(bodyLoading)).toBe(true)

    await vi.waitFor(() => expect(get(bodyLoading)).toBe(false))
    expect(get(messageDetail).data?.bodyPlain).toBe('full text')
    expect(get(messageDetail).status).toBe('ready')
  })
})

describe('loadMessage', () => {
  it('marks body loading when the stub is not complete yet', async () => {
    fetchMessage.mockResolvedValue(detail(3))
    await loadMessage(3)
    expect(get(bodyLoading)).toBe(true)
  })
})
