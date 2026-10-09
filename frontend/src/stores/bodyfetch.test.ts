import { describe, it, expect, vi, beforeEach } from 'vitest'
import { get } from 'svelte/store'

const { refreshMessage } = vi.hoisted(() => ({ refreshMessage: vi.fn(async (_id: number) => {}) }))
vi.mock('./message', async () => {
  const { writable } = await import('svelte/store')
  return { bodyLoading: writable(false), refreshMessage }
})

import { bodyLoading } from './message'
import { bodyFetchFailed, markBodyFetchFailed, clearBodyFetchFailed, retryBodyFetch } from './bodyfetch'

describe('body fetch failure', () => {
  beforeEach(() => {
    bodyFetchFailed.set(null)
    bodyLoading.set(false)
    refreshMessage.mockClear()
  })

  it('stops the spinner when the fetch gives up', () => {
    bodyLoading.set(true)
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
    markBodyFetchFailed(7)
    await retryBodyFetch(7)
    expect(get(bodyFetchFailed)).toBeNull()
    expect(refreshMessage).toHaveBeenCalledWith(7)
  })
})
