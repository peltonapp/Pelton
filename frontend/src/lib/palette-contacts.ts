// palette-contacts.ts puts contacts into the command palette: making a
// contact, finding one by name, organisation or address, and switching how the
// contacts space lays them out. the entries work from any space.

import { get } from 'svelte/store'
import { IconAddressBook, IconUserPlus, IconUserSearch, IconLayoutColumns, IconLayoutGrid, IconSettings, IconUser } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { CommandStep, PaletteCommand } from './commands'
import { contacts, loadContacts } from '../stores/contacts'
import { selectedContactId } from '../stores/contactsview'
import { setContactsView } from '../stores/prefs'
import { openSpace, runInSpace } from '../stores/space'
import { openSettingsAt } from '../stores/settingsnav'

// a step listing every contact; picking one opens it in the contacts space.
function findStep(t: (key: string) => string): CommandStep {
  return {
    label: t('palette.contacts.find'),
    placeholder: t('contacts.searchPlaceholder'),
    items: get(contacts).map((contact) => ({
      id: `contacts:open:${contact.id}`,
      group: 'contacts',
      label: contact.fullName || contact.emails[0]?.value || contact.organization,
      keywords: [contact.organization, ...contact.emails.map((e) => e.value)].join(' '),
      hint: contact.emails[0]?.value ?? contact.organization,
      icon: IconUser,
      run: () => {
        openSpace('contacts')
        selectedContactId.set(contact.id)
      },
    })),
  }
}

registerPaletteSource('contacts', (t) => {
  // the list is only loaded once the contacts space has been opened; asking
  // here has it ready by the time the find step is chosen.
  if (get(contacts).length === 0) {
    void loadContacts()
  }
  const out: PaletteCommand[] = [
    {
      id: 'contacts:open',
      group: 'contacts',
      label: t('palette.contacts.open'),
      icon: IconAddressBook,
      run: () => openSpace('contacts'),
    },
    {
      id: 'contacts:new',
      group: 'contacts',
      label: t('contacts.new'),
      keywords: t('palette.contacts.newKeywords'),
      icon: IconUserPlus,
      run: () => runInSpace('contacts', 'compose'),
    },
    {
      id: 'contacts:find',
      group: 'contacts',
      label: t('palette.contacts.find'),
      keywords: t('palette.contacts.findKeywords'),
      icon: IconUserSearch,
      run: () => findStep(t),
    },
    {
      id: 'contacts:view:panes',
      group: 'contacts',
      label: t('palette.contacts.view').replace('{view}', t('contacts.view.panes')),
      icon: IconLayoutColumns,
      run: () => {
        setContactsView('panes')
        openSpace('contacts')
      },
    },
    {
      id: 'contacts:view:grid',
      group: 'contacts',
      label: t('palette.contacts.view').replace('{view}', t('contacts.view.grid')),
      icon: IconLayoutGrid,
      run: () => {
        setContactsView('grid')
        openSpace('contacts')
      },
    },
    {
      id: 'contacts:manage',
      group: 'contacts',
      label: t('contacts.manageBooks'),
      icon: IconSettings,
      run: () => openSettingsAt('contacts'),
    },
  ]
  return out
})
