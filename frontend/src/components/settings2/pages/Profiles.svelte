<script lang="ts">
  // the Profiles page: groups of mailboxes with their own sharing and palette
  // behaviour.
  import '../../../theme/settings2-sections.css'
  import Card from '../ui/Card.svelte'
  import ProfilesSection from '../../settings/ProfilesSection.svelte'
  import { t } from '../../../lib/i18n'
</script>

<Card title={$t('settingsPanel.category.profiles')} bare>
  <div class="s2-section" data-setting="profiles">
    <ProfilesSection />
  </div>
</Card>
