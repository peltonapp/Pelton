<script lang="ts">
  // the Accounts page: the mailboxes and the addresses collected from mail.
  import '../../../theme/settings2-sections.css'
  import Card from '../ui/Card.svelte'
  import MailboxesSection from '../../settings/MailboxesSection.svelte'
  import AddressBookSection from '../../settings/AddressBookSection.svelte'
  import { t } from '../../../lib/i18n'
</script>

<Card title={$t('settingsPanel.category.mailboxes')} bare>
  <div class="s2-section" data-setting="mailboxes">
    <MailboxesSection />
  </div>
</Card>

<Card title={$t('addressBook.title')} bare>
  <div class="s2-section" data-setting="address-book">
    <AddressBookSection />
  </div>
</Card>
