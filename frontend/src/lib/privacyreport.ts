// privacyreport.ts sorts and filters the tracking report's senders. Kept out
// of the component so the order can be tested without rendering it.

import type { PrivacySender } from './types'

/** What the report can be sorted by. */
export type ReportSort = 'tracking' | 'links' | 'mails' | 'recent' | 'name'

/** Every sort, in the order the switch offers them. */
export const reportSorts: ReportSort[] = ['tracking', 'links', 'mails', 'recent', 'name']

function label(s: PrivacySender): string {
  return (s.name || s.address).toLowerCase()
}

/** The senders in the order sort asks for; ties fall back to the name. */
export function sortSenders(rows: readonly PrivacySender[], sort: ReportSort): PrivacySender[] {
  const key = (s: PrivacySender): number => {
    switch (sort) {
      case 'tracking':
        return s.pixels + s.receipts
      case 'links':
        return s.links
      case 'mails':
        return s.mails
      case 'recent':
        return Date.parse(s.last || s.unsub?.at || '') || 0
    }
    return 0
  }
  return [...rows].sort((a, b) => key(b) - key(a) || label(a).localeCompare(label(b)))
}

/** The senders whose name, address or tracking company contains text. */
export function filterSenders(rows: readonly PrivacySender[], text: string): PrivacySender[] {
  const q = text.trim().toLowerCase()
  if (q === '') {
    return [...rows]
  }
  return rows.filter((s) => label(s).includes(q) || s.address.includes(q) || s.companies.some((c) => c.toLowerCase().includes(q)))
}
