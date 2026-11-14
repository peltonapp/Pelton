// quickfilter.ts narrows the loaded message list without asking the backend:
// the quick filter bar's toggles and its text box.

import type { MessageSummary } from './types'

/** The quick filter bar's state. */
export interface QuickFilter {
  unread: boolean
  flagged: boolean
  attachments: boolean
  contacts: boolean
  text: string
}

/** A quick filter that lets everything through. */
export const noQuickFilter: QuickFilter = { unread: false, flagged: false, attachments: false, contacts: false, text: '' }

/** Reports whether any part of the quick filter is set. */
export function quickFilterActive(f: QuickFilter): boolean {
  return f.unread || f.flagged || f.attachments || f.contacts || f.text.trim() !== ''
}

/**
 * Keeps the rows that pass every part of the filter. contacts holds the
 * lowercased addresses of the address book; the text matches the sender's
 * name or address and the subject, ignoring case.
 */
export function applyQuickFilter(items: MessageSummary[], f: QuickFilter, contacts: ReadonlySet<string>): MessageSummary[] {
  if (!quickFilterActive(f)) {
    return items
  }
  const needle = f.text.trim().toLowerCase()
  return items.filter((m) => {
    if (f.unread && m.seen) {
      return false
    }
    if (f.flagged && !m.flagged) {
      return false
    }
    if (f.attachments && !m.hasAttachments) {
      return false
    }
    if (f.contacts && !contacts.has(m.fromAddress.trim().toLowerCase())) {
      return false
    }
    if (needle !== '') {
      const hay = `${m.fromName} ${m.fromAddress} ${m.subject}`.toLowerCase()
      if (!hay.includes(needle)) {
        return false
      }
    }
    return true
  })
}
