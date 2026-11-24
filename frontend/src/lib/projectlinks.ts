// projectlinks.ts opens what a project or a card points at: a message or a
// conversation in Mail, an event in the calendar, a person in Contacts.

import { get } from 'svelte/store'
import { findEventByUID } from './api'
import { openMailLink } from './maillink'
import { parseEventTime } from './caldate'
import { t } from './i18n'
import { calendarFocus } from '../stores/calendar'
import { contactSeed, contacts, loadContacts } from '../stores/contacts'
import { contactQuery, selectedContactId } from '../stores/contactsview'
import { openSpace } from '../stores/space'
import { sidebar } from '../stores/accounts'
import { revealMessage } from '../stores/selection'
import { errorMessage, toastError, toastInfo } from '../stores/toast'
import type { MessageSummary, ProjectLink } from './types'

/** Shows a message in the reader, in the folder it is in. */
export function openMessageInMail(m: Pick<MessageSummary, 'id' | 'accountId' | 'folderId'>): void {
  openSpace('mail')
  revealMessage(m.id, m.accountId, m.folderId, get(sidebar).data ?? null)
}

/** Opens the conversation a mail link names in Mail. */
export function openLinkedMail(link: Pick<ProjectLink, 'ref'>): void {
  if (link.ref.startsWith('#')) {
    toastInfo(get(t)('calendar.mailGone'))
    return
  }
  void openMailLink(`mid:${link.ref}`)
}

/** Shows an event in the calendar by its UID, or says it is gone. */
export async function showEvent(uid: string): Promise<void> {
  try {
    const ref = await findEventByUID(uid)
    if (!ref.found) {
      toastInfo(get(t)('tasks.eventGone'))
      return
    }
    calendarFocus.set(parseEventTime(ref.start, ref.allDay))
    openSpace('calendar')
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/**
 * Shows the contact with an address in Contacts, or starts a new contact for
 * someone who is not in the address book yet.
 */
export async function showPerson(email: string, name: string): Promise<void> {
  if (get(contacts).length === 0) {
    await loadContacts()
  }
  const address = email.toLowerCase()
  const found = get(contacts).find((c) => c.emails.some((e) => e.value.toLowerCase() === address))
  if (found) {
    contactQuery.set('')
    selectedContactId.set(found.id)
  } else {
    contactSeed.set({ name, email })
  }
  openSpace('contacts')
}
