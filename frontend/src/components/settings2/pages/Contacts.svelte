<script lang="ts">
  // the Contacts page: the CardDAV address books and whether addresses are
  // learned from mail.
  import '../../../theme/settings2-sections.css'
  import Card from '../ui/Card.svelte'
  import ContactsSection from '../../settings/ContactsSection.svelte'
  import { t } from '../../../lib/i18n'
</script>

<Card title={$t('settingsPanel.category.contacts')} bare>
  <div class="s2-section" data-setting="address-books">
    <ContactsSection />
  </div>
</Card>
