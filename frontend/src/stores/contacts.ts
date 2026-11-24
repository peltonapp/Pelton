// contacts.ts holds the CardDAV address books and the contacts in them (#168).
//
// Contacts are the address book the user actually maintains, on their own
// server. The harvested addresses that autocomplete has always used are a
// separate thing and stay; where the two carry the same address the contact's
// name is the one shown, because that is the one the user wrote.

import { get, writable } from 'svelte/store'
import {
  listAddressBooks,
  listContacts,
  syncContacts as syncContactsAPI,
  saveContact as saveContactAPI,
  deleteContact as deleteContactAPI,
} from '../lib/api'
import { errorMessage, toastError, toastSuccess } from './toast'
import { t } from '../lib/i18n'
import type { AddressBook, Contact, ContactConflict, ContactDraft } from '../lib/types'

/** Every configured address book. */
export const addressBooks = writable<AddressBook[]>([])

/** The contacts in the selected book, or in all of them. */
export const contacts = writable<Contact[]>([])

/** Which book the list is showing, 0 for all of them. */
export const contactBookFilter = writable<number>(0)

/** True while a load or a sync is in flight, so the screen can say so. */
export const contactsLoading = writable<boolean>(false)

/**
 * The conflict a refused save or delete came back with, or null. Both versions
 * are in it: the user is shown them side by side and picks (#168).
 */
export const contactConflict = writable<ContactConflict | null>(null)

/** Loads the books and the contacts for the current filter. */
/**
 * A person to start a new contact from, set by whatever hands one over (mail
 * dropped on Contacts) and taken by the contacts screen when it shows.
 */
export const contactSeed = writable<{ name: string; email: string } | null>(null)

export async function loadContacts(): Promise<void> {
  contactsLoading.set(true)
  try {
    addressBooks.set(await listAddressBooks())
    contacts.set(await listContacts(get(contactBookFilter)))
  } catch (err) {
    toastError(errorMessage(err))
  } finally {
    contactsLoading.set(false)
  }
}

/** Shows one book's contacts, or all of them for 0. */
export async function showBook(bookId: number): Promise<void> {
  contactBookFilter.set(bookId)
  await loadContacts()
}

/** Refreshes every address book from its server, then reloads the list. */
export async function refreshContacts(): Promise<void> {
  contactsLoading.set(true)
  try {
    await syncContactsAPI()
  } catch (err) {
    toastError(errorMessage(err))
  } finally {
    contactsLoading.set(false)
  }
  await loadContacts()
}

/**
 * Saves a contact. A refused write opens the conflict dialog and changes
 * nothing, here or on the server. Returns true when it was saved.
 */
export async function saveContact(draft: ContactDraft): Promise<boolean> {
  try {
    const result = await saveContactAPI(draft)
    if (result.conflict) {
      contactConflict.set(result)
      return false
    }
    await loadContacts()
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/**
 * Deletes a contact here and on its server. Like a save it can come back as a
 * conflict, when the contact was edited elsewhere since the last sync.
 */
export async function removeContact(id: number, force = false): Promise<boolean> {
  try {
    const result = await deleteContactAPI(id, force)
    if (result.conflict) {
      contactConflict.set(result)
      return false
    }
    toastSuccess(get(t)('contacts.deleted'))
    await loadContacts()
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** Closes the conflict dialog, leaving both versions as they are. */
export function dismissConflict(): void {
  contactConflict.set(null)
}
