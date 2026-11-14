<script lang="ts">
  // the Signatures page: reusable header and footer blocks and which one each
  // mailbox starts with.
  import '../../../theme/settings2-sections.css'
  import Card from '../ui/Card.svelte'
  import SignaturesSection from '../../settings/SignaturesSection.svelte'
  import { t } from '../../../lib/i18n'
</script>

<Card title={$t('signatures.title')} bare>
  <div class="s2-section" data-setting="signatures">
    <SignaturesSection />
  </div>
</Card>
