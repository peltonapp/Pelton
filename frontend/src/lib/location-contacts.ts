// location-contacts.ts lets Contacts take part in back and forward: the
// contact open in the space.

import { get } from 'svelte/store'
import { registerLocationProvider, oneSideEmpty, type ContactsLocation } from './locations'
import { contacts, loadContacts } from '../stores/contacts'
import { selectedContactId } from '../stores/contactsview'
import { openSpace } from '../stores/space'

registerLocationProvider('contacts', {
  watch: [selectedContactId, contacts],
  describe(): ContactsLocation {
    const id = get(selectedContactId)
    return { space: 'contacts', contact: id ?? undefined }
  },
  label(loc) {
    if (loc.contact === undefined) {
      return ''
    }
    const found = get(contacts).find((c) => c.id === loc.contact)
    return found ? found.fullName || found.emails[0]?.value || '' : ''
  },
  async restore(loc) {
    if (loc.contact !== undefined) {
      if (get(contacts).length === 0) {
        await loadContacts()
      }
      if (!get(contacts).some((c) => c.id === loc.contact)) {
        return false
      }
    }
    selectedContactId.set(loc.contact ?? null)
    openSpace('contacts')
    return true
  },
  refines(from, to) {
    return oneSideEmpty(from.contact, to.contact)
  },
})
