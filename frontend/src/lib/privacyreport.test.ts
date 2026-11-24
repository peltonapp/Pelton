import { describe, expect, it } from 'vitest'
import { filterSenders, sortSenders } from './privacyreport'
import type { PrivacySender } from './types'

function sender(over: Partial<PrivacySender>): PrivacySender {
  return { address: 'x@example.com', name: '', mails: 1, pixels: 0, links: 0, receipts: 0, first: '', last: '', companies: [], unsub: null, ...over }
}

const rows = [
  sender({ address: 'news@shop.example', name: 'Shop', mails: 9, pixels: 9, links: 30, last: '2026-11-01T00:00:00Z', companies: ['Mailchimp'] }),
  sender({ address: 'alerts@bank.example', name: 'Bank', mails: 20, pixels: 1, receipts: 2, last: '2026-11-10T00:00:00Z' }),
  sender({ address: 'promo@toys.example', name: 'Toys', mails: 3, pixels: 3, links: 2, last: '2026-10-01T00:00:00Z', companies: ['Klaviyo'] }),
]

describe('sortSenders', () => {
  it.each([
    // Toys and Bank tie on three, so the name decides.
    ['tracking', ['Shop', 'Bank', 'Toys']],
    ['links', ['Shop', 'Toys', 'Bank']],
    ['mails', ['Bank', 'Shop', 'Toys']],
    ['recent', ['Bank', 'Shop', 'Toys']],
    ['name', ['Bank', 'Shop', 'Toys']],
  ] as const)('by %s', (sort, want) => {
    expect(sortSenders(rows, sort).map((s) => s.name)).toEqual(want)
  })
})

describe('filterSenders', () => {
  it('matches the name, the address and the tracking company', () => {
    expect(filterSenders(rows, 'bank').map((s) => s.name)).toEqual(['Bank'])
    expect(filterSenders(rows, 'toys.example').map((s) => s.name)).toEqual(['Toys'])
    expect(filterSenders(rows, 'mailchimp').map((s) => s.name)).toEqual(['Shop'])
    expect(filterSenders(rows, '  ')).toHaveLength(3)
  })
})
