import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { MessageDetail } from '../lib/types'
import { idle, ready } from '../lib/async'

vi.mock('../lib/api', () => ({
  getMessage: vi.fn(),
}))

import { getMessage } from '../lib/api'
import { bodyLoading, clearMessage, loadMessage, messageDetail, refreshMessage, setBodyHtml } from './message'

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
  clearMessage()
  fetchMessage.mockReset()
  messageDetail.set(idle())
  bodyLoading.set(false)
})

describe('refreshMessage', () => {
  it('keeps the preview on screen while the full body loads', async () => {
    const stub = detail(7)
    fetchMessage.mockResolvedValueOnce(stub)
    await loadMessage(7)
    fetchMessage.mockReset()
    const full = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(full.promise)

    const refresh = refreshMessage(7)
    expect(get(messageDetail).data?.bodyPlain).toBe('preview')
    expect(get(bodyLoading)).toBe(true)

    full.resolve(detail(7, { bodyPlain: 'full text', bodyComplete: true }))
    await refresh
    expect(get(bodyLoading)).toBe(false)
    expect(get(messageDetail).data?.bodyPlain).toBe('full text')
    expect(get(messageDetail).status).toBe('ready')
  })
})

// deferred hands back a promise and the function that resolves it, so a test
// can let getMessage answer after the user has already moved on.
function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void; reject: (reason: unknown) => void } {
  let resolve!: (v: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((r, j) => {
    resolve = r
    reject = j
  })
  return { promise, resolve, reject }
}

describe('stale responses', () => {
  it('drops a refresh of A that lands after B was opened', async () => {
    fetchMessage.mockResolvedValueOnce(detail(1))
    await loadMessage(1)
    fetchMessage.mockReset()
    const late = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(late.promise)
    const refresh = refreshMessage(1)

    fetchMessage.mockResolvedValueOnce(detail(2, { bodyComplete: true }))
    await loadMessage(2)

    late.resolve(detail(1, { bodyComplete: true }))
    await refresh
    expect(get(messageDetail).data?.id).toBe(2)
    expect(get(bodyLoading)).toBe(false)
  })

  it('drops a refresh that lands after the pane was closed', async () => {
    fetchMessage.mockResolvedValueOnce(detail(1))
    await loadMessage(1)
    fetchMessage.mockReset()
    const late = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(late.promise)
    const refresh = refreshMessage(1)

    clearMessage()
    late.resolve(detail(1, { bodyComplete: true }))
    await refresh
    expect(get(messageDetail).status).toBe('idle')
    expect(get(bodyLoading)).toBe(false)
  })

  it('drops a slow load of A that lands after B was opened', async () => {
    const slow = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(slow.promise)
    const first = loadMessage(1)

    fetchMessage.mockResolvedValueOnce(detail(2, { bodyComplete: true }))
    await loadMessage(2)

    slow.resolve(detail(1))
    await first
    expect(get(messageDetail).data?.id).toBe(2)
  })
})

describe('loadMessage', () => {
  it('marks body loading when the stub is not complete yet', async () => {
    fetchMessage.mockResolvedValue(detail(3))
    await loadMessage(3)
    expect(get(bodyLoading)).toBe(true)
  })
})

describe('detail update races', () => {
  it.each([1, 2])('rereads B once after %i updates during its initial load', async (updates) => {
    fetchMessage.mockResolvedValueOnce(detail(1, { bodyComplete: true }))
    await loadMessage(1)
    const first = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(first.promise)
    const opening = loadMessage(2)
    for (let i = 0; i < updates; i++) await refreshMessage(2)
    fetchMessage.mockResolvedValueOnce(detail(2, { bodyPlain: 'complete B', bodyComplete: true }))

    first.resolve(detail(2))
    await opening
    expect(fetchMessage).toHaveBeenCalledTimes(3)
    expect(get(messageDetail).data).toMatchObject({ id: 2, bodyPlain: 'complete B' })
    expect(get(bodyLoading)).toBe(false)
  })

  it.each(['navigate', 'close'] as const)('cancels the queued reread after %s', async (action) => {
    const first = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(first.promise)
    const opening = loadMessage(2)
    await refreshMessage(2)
    if (action === 'navigate') {
      fetchMessage.mockResolvedValueOnce(detail(3, { bodyComplete: true }))
      await loadMessage(3)
    } else clearMessage()
    const calls = fetchMessage.mock.calls.length

    first.resolve(detail(2))
    await opening
    expect(fetchMessage).toHaveBeenCalledTimes(calls)
    if (action === 'navigate') expect(get(messageDetail).data?.id).toBe(3)
    else expect(get(messageDetail).status).toBe('idle')
    expect(get(bodyLoading)).toBe(false)
  })

  it('retains the initial load error when an update was queued', async () => {
    const first = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(first.promise)
    const opening = loadMessage(2)
    await refreshMessage(2)

    first.reject(new Error('initial load failed'))
    await opening
    expect(fetchMessage).toHaveBeenCalledTimes(1)
    expect(get(messageDetail)).toMatchObject({ status: 'error', error: 'initial load failed' })
    expect(get(bodyLoading)).toBe(false)
  })

  it('keeps the later refresh result when the older stub arrives last', async () => {
    fetchMessage.mockResolvedValueOnce(detail(2))
    await loadMessage(2)
    const older = deferred<MessageDetail>()
    const newer = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const firstRefresh = refreshMessage(2)
    const secondRefresh = refreshMessage(2)

    newer.resolve(detail(2, { bodyPlain: 'complete B', bodyComplete: true }))
    await secondRefresh
    older.resolve(detail(2))
    await firstRefresh
    expect(get(messageDetail).data?.bodyPlain).toBe('complete B')
    expect(get(bodyLoading)).toBe(false)
  })

  it('keeps the newer refresh spinner when the older refresh fails', async () => {
    fetchMessage.mockResolvedValueOnce(detail(2))
    await loadMessage(2)
    const older = deferred<MessageDetail>()
    const newer = deferred<MessageDetail>()
    fetchMessage.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const firstRefresh = refreshMessage(2)
    const secondRefresh = refreshMessage(2)

    older.reject(new Error('older refresh failed'))
    await firstRefresh
    expect(get(bodyLoading)).toBe(true)
    expect(get(messageDetail).data?.bodyPlain).toBe('preview')
    newer.resolve(detail(2, { bodyPlain: 'complete B', bodyComplete: true }))
    await secondRefresh
    expect(get(messageDetail).data?.bodyPlain).toBe('complete B')
    expect(get(bodyLoading)).toBe(false)
  })
})

// loading remote images is a round trip; another message can open meanwhile.
describe('setBodyHtml', () => {
  it('swaps the body of the message it names', () => {
    messageDetail.set(ready(detail(1, { bodyHtmlSafe: '<p>blocked</p>' })))

    setBodyHtml(1, '<p>with images</p>')

    expect(get(messageDetail).data?.bodyHtmlSafe).toBe('<p>with images</p>')
  })

  it('leaves another open message alone', () => {
    messageDetail.set(ready(detail(2, { bodyHtmlSafe: '<p>second</p>' })))

    setBodyHtml(1, '<p>first with images</p>')

    expect(get(messageDetail).data?.bodyHtmlSafe).toBe('<p>second</p>')
  })
})
