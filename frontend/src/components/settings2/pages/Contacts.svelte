<script lang="ts">
  // the Contacts page: how the contacts space shows its contacts, the CardDAV
  // address books and whether addresses are learned from mail.
  import '../../../theme/settings2-sections.css'
  import Card from '../ui/Card.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import ContactsViewSkeleton from '../skeletons/ContactsViewSkeleton.svelte'
  import ContactsSection from '../../settings/ContactsSection.svelte'
  import { prefs, setContactsView } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
  import type { ContactsView } from '../../../lib/types'

  $: classic = $prefs.design === 'classic'

  $: views = [
    { key: 'panes', label: $t('contacts.view.panes'), hint: $t('contacts.view.panes.hint') },
    { key: 'grid', label: $t('contacts.view.grid'), hint: $t('contacts.view.grid.hint') },
  ]
</script>

<Card title={$t('contacts.view.label')} description={$t('contacts.view.description')} bare>
  <div data-setting="contacts-view">
    <ChoiceCards
      label={$t('contacts.view.label')}
      value={$prefs.contactsView}
      options={views}
      minWidth={220}
      disabled={classic}
      on:change={(e) => setContactsView(e.detail as ContactsView)}
      let:option
    >
      <ContactsViewSkeleton view={option.key as ContactsView} height={120} />
    </ChoiceCards>
  </div>
</Card>

<Card title={$t('settingsPanel.category.contacts')} bare>
  <div class="s2-section" data-setting="address-books">
    <ContactsSection />
  </div>
</Card>
