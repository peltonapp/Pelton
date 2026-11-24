// contactmail.ts finds the mail the user has exchanged with a contact, from the
// search index that is already built, so the contacts space needs no backend of
// its own for it.

import { search } from './api'
import type { MessageSummary } from './types'

/** Most addresses of one contact that are searched. */
const maxAddresses = 3

/**
 * Returns the newest messages sent by or to any of the addresses, newest first,
 * each message once. A failed search counts as no mail: this is a convenience
 * under a contact, not something worth an error toast.
 */
export async function recentMailWith(addresses: string[], limit: number): Promise<MessageSummary[]> {
  const wanted = addresses.map((a) => a.trim()).filter(Boolean).slice(0, maxAddresses)
  const base = { query: '', afterUnix: 0, beforeUnix: 0, limit, offset: 0, subject: '', hasAttachment: false, unreadOnly: false, sort: 'newest' as const }
  const pages = await Promise.all(
    wanted.flatMap((address) => [
      search({ ...base, from: address, to: '' }).catch(() => null),
      search({ ...base, from: '', to: address }).catch(() => null),
    ]),
  )
  const seen = new Map<number, MessageSummary>()
  for (const page of pages) {
    for (const message of page?.messages ?? []) {
      seen.set(message.id, message)
    }
  }
  return [...seen.values()].sort((a, b) => Date.parse(b.date) - Date.parse(a.date)).slice(0, limit)
}
