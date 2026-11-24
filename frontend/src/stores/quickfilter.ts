// quickfilter.ts holds the quick filter bar's state and the address book
// addresses its Contacts toggle checks senders against.

import { writable } from 'svelte/store'
import { listContacts } from '../lib/api'
import { noQuickFilter, type QuickFilter } from '../lib/quickfilter'
import { errorMessage, toastError } from './toast'

/** The quick filter applied to the loaded list. */
export const quickFilter = writable<QuickFilter>({ ...noQuickFilter })

/** Every address in the address books, lowercased. */
export const contactAddresses = writable<ReadonlySet<string>>(new Set())

/** Lets everything through again. */
export function resetQuickFilter(): void {
  quickFilter.set({ ...noQuickFilter })
}

/** Reads the address books' addresses for the Contacts toggle. */
export async function loadContactAddresses(): Promise<void> {
  try {
    const list = await listContacts(0)
    const set = new Set<string>()
    for (const c of list) {
      for (const e of c.emails ?? []) {
        set.add(e.value.trim().toLowerCase())
      }
    }
    contactAddresses.set(set)
  } catch (err) {
    toastError(errorMessage(err))
  }
}
