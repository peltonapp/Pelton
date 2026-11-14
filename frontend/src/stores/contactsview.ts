// contactsview.ts holds what the contacts space remembers while the user moves
// between spaces: the search, the order, the organization filter and which
// contact is open. The contacts themselves live in contacts.ts.

import { derived, writable } from 'svelte/store'
import { contacts } from './contacts'
import { matchesQuery, sortContacts, type ContactSort } from '../lib/contactlist'

/** The text typed into the contact search. */
export const contactQuery = writable<string>('')

/** How the list is ordered. */
export const contactSort = writable<ContactSort>('name')

/** The organization the list is narrowed to, or null for all of them. */
export const contactOrg = writable<string | null>(null)

/** The id of the contact open in the detail pane, or null. */
export const selectedContactId = writable<number | null>(null)

/** The contacts that pass the search and the organization filter, in order. */
export const visibleContacts = derived(
  [contacts, contactQuery, contactSort, contactOrg],
  ([$contacts, $query, $sort, $org]) =>
    sortContacts(
      $contacts.filter(
        (c) =>
          ($org === null || c.organization.trim().toLocaleLowerCase() === $org.toLocaleLowerCase()) &&
          matchesQuery(c, $query),
      ),
      $sort,
    ),
)
