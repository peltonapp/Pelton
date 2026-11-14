import { describe, expect, it } from 'vitest'
import { applyQuickFilter, noQuickFilter, quickFilterActive } from './quickfilter'
import type { MessageSummary } from './types'

function row(id: number, patch: Partial<MessageSummary>): MessageSummary {
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
    ...patch,
  }
}

const rows = [
  row(1, { seen: false }),
  row(2, { flagged: true, fromAddress: 'bob@example.com', fromName: 'Bob' }),
  row(3, { hasAttachments: true, subject: 'Quarterly report' }),
]
const ids = (list: MessageSummary[]): number[] => list.map((m) => m.id)
const none = new Set<string>()

describe('applyQuickFilter', () => {
  it('lets everything through when nothing is set', () => {
    expect(quickFilterActive(noQuickFilter)).toBe(false)
    expect(applyQuickFilter(rows, noQuickFilter, none)).toBe(rows)
  })

  it('applies each toggle', () => {
    expect(ids(applyQuickFilter(rows, { ...noQuickFilter, unread: true }, none))).toEqual([1])
    expect(ids(applyQuickFilter(rows, { ...noQuickFilter, flagged: true }, none))).toEqual([2])
    expect(ids(applyQuickFilter(rows, { ...noQuickFilter, attachments: true }, none))).toEqual([3])
  })

  it('keeps senders from the address book for the contacts toggle', () => {
    const book = new Set(['bob@example.com'])
    expect(ids(applyQuickFilter(rows, { ...noQuickFilter, contacts: true }, book))).toEqual([2])
  })

  it('matches the text against sender and subject', () => {
    expect(ids(applyQuickFilter(rows, { ...noQuickFilter, text: 'BOB' }, none))).toEqual([2])
    expect(ids(applyQuickFilter(rows, { ...noQuickFilter, text: 'report' }, none))).toEqual([3])
    expect(ids(applyQuickFilter(rows, { ...noQuickFilter, text: 'report', unread: true }, none))).toEqual([])
  })
})
