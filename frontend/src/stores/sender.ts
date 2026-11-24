// sender.ts feeds the reader's sender panel: the address book looked up by
// address, and what the local mail says about a sender. Both stay in memory
// for the session; the overview is asked again when a message from the same
// sender is opened after a while, so new mail shows up.

import { writable } from 'svelte/store'
import { listContacts, senderOverview } from '../lib/api'
import type { Contact, SenderOverview } from '../lib/types'

/** Every contact across the address books, loaded on first use. */
export const allContacts = writable<Contact[]>([])

let contactsLoaded: Promise<void> | null = null

/** Loads the contacts once; `force` reads them again after an edit. */
export function loadAllContacts(force = false): Promise<void> {
  if (!contactsLoaded || force) {
    contactsLoaded = listContacts(0)
      .then((list) => allContacts.set(list ?? []))
      .catch(() => allContacts.set([]))
  }
  return contactsLoaded
}

/** The contact holding an address, or null. */
export function contactFor(address: string, contacts: Contact[]): Contact | null {
  const needle = address.trim().toLowerCase()
  if (!needle) {
    return null
  }
  return contacts.find((c) => c.emails.some((e) => e.value.trim().toLowerCase() === needle)) ?? null
}

const freshMs = 60_000
const overviews = new Map<string, { at: number; value: Promise<SenderOverview> }>()

/** What the local mail says about an address, from cache when recent. */
export function overviewFor(address: string): Promise<SenderOverview> {
  const key = address.trim().toLowerCase()
  const known = overviews.get(key)
  if (known && Date.now() - known.at < freshMs) {
    return known.value
  }
  const value = senderOverview(key)
  overviews.set(key, { at: Date.now(), value })
  value.catch(() => overviews.delete(key))
  return value
}
