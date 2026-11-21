import { describe, it, expect, vi, beforeEach } from 'vitest'
import { get } from 'svelte/store'

vi.mock('../lib/api', () => ({ getMessage: vi.fn() }))

import { getMessage } from '../lib/api'
import { messageDetail, loadMessage, clearMessage, setBodyHtml } from './message'
import type { MessageDetail } from '../lib/types'

const fetch = vi.mocked(getMessage)

// deferred hands back a load that finishes only when the test says so, so the
// order responses arrive in can be chosen.
function deferred(): { promise: Promise<MessageDetail>; resolve: (id: number) => void; reject: (e: Error) => void } {
  let resolve!: (id: number) => void
  let reject!: (e: Error) => void
  const promise = new Promise<MessageDetail>((res, rej) => {
    resolve = (id) => res({ id, subject: `message ${id}`, bodyHtmlSafe: '' } as unknown as MessageDetail)
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('loadMessage', () => {
  beforeEach(() => {
    fetch.mockReset()
    clearMessage()
  })

  // the bug: open A, then B; A's load is slower and lands last. The pane
  // showed A while B was selected, and clicking B again did nothing.
  it('keeps the message opened last when an earlier load finishes after it', async () => {
    const a = deferred()
    const b = deferred()
    fetch.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)

    const loadA = loadMessage(1)
    const loadB = loadMessage(2)
    b.resolve(2)
    await loadB
    a.resolve(1)
    await loadA

    expect(get(messageDetail).data?.id).toBe(2)
    expect(get(messageDetail).status).toBe('ready')
  })

  it('ignores a late failure of a load that was superseded', async () => {
    const a = deferred()
    const b = deferred()
    fetch.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)

    const loadA = loadMessage(1)
    const loadB = loadMessage(2)
    b.resolve(2)
    await loadB
    a.reject(new Error('gone'))
    await loadA

    expect(get(messageDetail).status).toBe('ready')
    expect(get(messageDetail).data?.id).toBe(2)
  })

  it('does not fill a cleared pane with a load still on its way', async () => {
    const a = deferred()
    fetch.mockReturnValueOnce(a.promise)

    const loadA = loadMessage(1)
    clearMessage()
    a.resolve(1)
    await loadA

    expect(get(messageDetail).status).toBe('idle')
  })

  it('puts remote images only into the message they were loaded for', async () => {
    fetch.mockResolvedValueOnce({ id: 2, bodyHtmlSafe: 'plain' } as unknown as MessageDetail)
    await loadMessage(2)

    setBodyHtml(1, 'other message')
    expect(get(messageDetail).data?.bodyHtmlSafe).toBe('plain')
    setBodyHtml(2, 'with images')
    expect(get(messageDetail).data?.bodyHtmlSafe).toBe('with images')
  })
})
