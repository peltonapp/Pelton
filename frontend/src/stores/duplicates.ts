// duplicates.ts holds the groups of contacts that look like the same person and
// carries out merging them. The merge itself, with the CardDAV writes, is the
// backend's; this keeps the list fresh around it and offers the undo.

import { get, writable } from 'svelte/store'
import {
  dismissDuplicateContacts,
  findDuplicateContacts,
  mergeContacts as mergeContactsAPI,
  resetDismissedDuplicates as resetDismissedAPI,
  undoContactMerge as undoContactMergeAPI,
} from '../lib/api'
import { loadContacts } from './contacts'
import { selectedContactId } from './contactsview'
import { loadAllContacts } from './sender'
import { loadContactAddresses } from './quickfilter'
import { errorMessage, pushActions, toastError, toastInfo, toastSuccess } from './toast'
import { t } from '../lib/i18n'
import type { Contact, DuplicateGroup, MergeRequest } from '../lib/types'

const toastMs = 10000

/** The groups of likely duplicates, the surest first. */
export const duplicateGroups = writable<DuplicateGroup[]>([])

/** How many pairs were marked as not duplicates. */
export const dismissedPairs = writable<number>(0)

/** True while the search runs. */
export const duplicatesLoading = writable<boolean>(false)

/** The last merge that can still be undone, or null. It lasts until the next merge or a restart. */
export const lastMerge = writable<{ token: string; name: string; count: number } | null>(null)

/** Searches the address books for duplicates. */
export async function loadDuplicates(): Promise<void> {
  duplicatesLoading.set(true)
  try {
    const found = await findDuplicateContacts()
    duplicateGroups.set(found.groups ?? [])
    dismissedPairs.set(found.dismissed)
  } catch (err) {
    toastError(errorMessage(err))
  } finally {
    duplicatesLoading.set(false)
  }
}

// everything that listed contacts or looked one up by address is read again,
// so a merged contact answers for every address of the originals at once.
async function refreshAfterChange(): Promise<void> {
  await Promise.all([loadContacts(), loadAllContacts(true), loadContactAddresses(), loadDuplicates()])
}

/**
 * Merges contacts and offers to undo it. Returns true when it went through. A
 * contact the server would not delete stays where it is and is reported.
 */
export async function mergeGroup(req: MergeRequest, count: number): Promise<boolean> {
  try {
    const result = await mergeContactsAPI(req)
    const label = get(t)
    const name = result.contact.fullName || result.contact.emails[0]?.value || ''
    lastMerge.set({ token: result.token, name, count })
    selectedContactId.set(result.contact.id)
    await refreshAfterChange()
    pushActions(
      'success',
      label('contacts.merge.done').replace('{n}', String(count)).replace('{name}', name),
      [{ label: label('contacts.merge.undo'), run: () => void undoLastMerge() }],
      toastMs,
    )
    if (result.kept > 0) {
      toastInfo(label('contacts.merge.kept').replace('{n}', String(result.kept)))
    }
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** Puts the contacts of the last merge back. */
export async function undoLastMerge(): Promise<boolean> {
  const merge = get(lastMerge)
  if (!merge) {
    return false
  }
  try {
    const restored: Contact[] = await undoContactMergeAPI(merge.token)
    lastMerge.set(null)
    selectedContactId.set(restored[0]?.id ?? null)
    await refreshAfterChange()
    toastSuccess(get(t)('contacts.merge.undone'))
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** Marks a group as not duplicates for good and drops it from the list. */
export async function dismissGroup(group: DuplicateGroup): Promise<void> {
  try {
    await dismissDuplicateContacts(group.contacts.map((c) => c.id))
    duplicateGroups.update((list) => list.filter((g) => g !== group))
    await loadDuplicates()
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Forgets every not-duplicates answer and searches again. */
export async function showDismissedAgain(): Promise<void> {
  try {
    await resetDismissedAPI()
    await loadDuplicates()
  } catch (err) {
    toastError(errorMessage(err))
  }
}
