import { describe, it, expect, vi, beforeEach } from 'vitest'
import { get } from 'svelte/store'
import type { MessageDetail } from '../lib/types'

// only the backend is faked: the message and bodyfetch stores are the real ones,
// so the order in which a failure event and a detail reply land is what counts.
vi.mock('../lib/api', () => ({ getMessage: vi.fn() }))

import { getMessage } from '../lib/api'
import { bodyLoading, clearMessage, loadMessage, messageDetail } from './message'
import { bodyFetchFailed, markBodyFetchFailed, clearBodyFetchFailed, retryBodyFetch } from './bodyfetch'

const fetchMessage = vi.mocked(getMessage)

function detail(id: number, bodyComplete: boolean): MessageDetail {
  return { id, bodyComplete, bodyPlain: 'preview' } as unknown as MessageDetail
}

/** A getMessage reply the test releases by hand. */
function deferred(value: MessageDetail): { promise: Promise<MessageDetail>; resolve: () => void } {
  let resolve!: () => void
  const promise = new Promise<MessageDetail>((r) => {
    resolve = () => r(value)
  })
  return { promise, resolve }
}

describe('body fetch failure', () => {
  beforeEach(() => {
    clearMessage()
    fetchMessage.mockReset()
  })

  it('stops the spinner when the fetch gives up', async () => {
    fetchMessage.mockResolvedValueOnce(detail(7, false))
    await loadMessage(7)
    expect(get(bodyLoading)).toBe(true)
    markBodyFetchFailed(7)
    expect(get(bodyLoading)).toBe(false)
    expect(get(bodyFetchFailed)).toBe(7)
  })

  it('clears only the failure of the message that got its body', () => {
    markBodyFetchFailed(7)
    clearBodyFetchFailed(8)
    expect(get(bodyFetchFailed)).toBe(7)
    clearBodyFetchFailed(7)
    expect(get(bodyFetchFailed)).toBeNull()
  })

  it('retries by reloading the message', async () => {
    fetchMessage.mockResolvedValueOnce(detail(7, false))
    await loadMessage(7)
    markBodyFetchFailed(7)
    fetchMessage.mockResolvedValueOnce(detail(7, false))
    await retryBodyFetch(7)
    expect(fetchMessage).toHaveBeenCalledTimes(2)
    expect(get(bodyFetchFailed)).toBeNull()
    expect(get(bodyLoading)).toBe(true)
  })

  it('keeps the retry when the failure beats the first detail reply', async () => {
    const reply = deferred(detail(7, false))
    fetchMessage.mockReturnValueOnce(reply.promise)
    const load = loadMessage(7)
    markBodyFetchFailed(7)
    reply.resolve()
    await load
    expect(get(messageDetail).data?.id).toBe(7)
    expect(get(bodyFetchFailed)).toBe(7)
    expect(get(bodyLoading)).toBe(false)
  })

  it('keeps the retry when the failure beats the retry reply', async () => {
    fetchMessage.mockResolvedValueOnce(detail(7, false))
    await loadMessage(7)
    markBodyFetchFailed(7)
    const reply = deferred(detail(7, false))
    fetchMessage.mockReturnValueOnce(reply.promise)
    const retry = retryBodyFetch(7)
    expect(get(bodyFetchFailed)).toBeNull()
    expect(get(bodyLoading)).toBe(true)
    markBodyFetchFailed(7)
    reply.resolve()
    await retry
    expect(get(bodyFetchFailed)).toBe(7)
    expect(get(bodyLoading)).toBe(false)
  })

  it('a complete reply clears the failure and the spinner', async () => {
    const reply = deferred(detail(7, true))
    fetchMessage.mockReturnValueOnce(reply.promise)
    const load = loadMessage(7)
    markBodyFetchFailed(7)
    reply.resolve()
    await load
    expect(get(bodyFetchFailed)).toBeNull()
    expect(get(bodyLoading)).toBe(false)
  })

  it('opening the message again forgets an old failure', async () => {
    fetchMessage.mockResolvedValueOnce(detail(7, false))
    await loadMessage(7)
    markBodyFetchFailed(7)
    clearMessage()
    fetchMessage.mockResolvedValueOnce(detail(7, false))
    await loadMessage(7)
    expect(get(bodyFetchFailed)).toBeNull()
    expect(get(bodyLoading)).toBe(true)
  })

  it('a failure for another message leaves the open spinner alone', async () => {
    fetchMessage.mockResolvedValueOnce(detail(7, false))
    await loadMessage(7)
    markBodyFetchFailed(8)
    expect(get(bodyLoading)).toBe(true)
  })
})
