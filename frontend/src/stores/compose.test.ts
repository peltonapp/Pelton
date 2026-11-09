import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

vi.mock('../lib/api', () => ({ getSetting: vi.fn(async () => ({ value: '', found: false })), setSetting: vi.fn() }))

import { composeSessions, openReply } from './compose'
import type { MessageDetail } from '../lib/types'

function detail(overrides: Partial<MessageDetail>): MessageDetail {
  return {
    accountId: 1,
    accountEmail: 'me@example.com',
    fromName: 'Ann',
    fromAddress: 'ann@example.com',
    toAddresses: 'me@example.com, bob@example.com',
    ccAddresses: 'carol@example.com',
    subject: 'Plans',
    date: '2026-10-01',
    bodyQuote: 'hello',
    ...overrides,
  } as MessageDetail
}

function last() {
  const list = get(composeSessions)
  return list[list.length - 1]
}

describe('reply recipients', () => {
  beforeEach(() => composeSessions.set([]))

  it('addresses a plain sender by name and address', () => {
    openReply(detail({}), 'plaintext', false)
    expect(last().to).toBe('Ann <ann@example.com>')
  })

  it('quotes a sender name with a comma', () => {
    openReply(detail({ fromName: 'Doe, Jane', fromAddress: 'jane@example.com' }), 'plaintext', false)
    expect(last().to).toBe('"Doe, Jane" <jane@example.com>')
  })

  it('escapes a quote in the sender name', () => {
    openReply(detail({ fromName: 'Jane "JD" Doe', fromAddress: 'jane@example.com' }), 'plaintext', false)
    expect(last().to).toBe('"Jane \\"JD\\" Doe" <jane@example.com>')
  })

  it('quotes a comma name in the reply-all cc', () => {
    openReply(detail({ ccAddresses: 'Doe, Jane <jane@example.com>, bob@example.com' }), 'plaintext', true)
    expect(last().cc).toBe('"Doe, Jane" <jane@example.com>, bob@example.com')
  })
})

describe('reply quote', () => {
  beforeEach(() => composeSessions.set([]))

  it('quotes as html in the rich editor, so its lines do not run together', () => {
    openReply(detail({ bodyQuote: 'line one\nline two\n> older' }), 'wysiwyg', false)
    const body = last().body
    expect(body).not.toContain('&gt; ')
    expect(body).toContain('<blockquote><p>line one<br>line two</p><blockquote><p>older</p></blockquote></blockquote>')
  })

  it('keeps the plain "> " quote outside the rich editor', () => {
    openReply(detail({ bodyQuote: 'line one\nline two' }), 'plaintext', false)
    expect(last().body).toContain('> line one\n> line two')
  })

  it('writes a readable date in the attribution, not the raw timestamp', () => {
    openReply(detail({ date: '2026-10-05T16:03:44Z' }), 'plaintext', false)
    expect(last().body).not.toContain('2026-10-05T16:03:44Z')
    expect(last().body).toMatch(/On .*2026.*, Ann wrote:/)
  })
})
