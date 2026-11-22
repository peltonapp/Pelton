<script lang="ts">
  // the Backup page: the external tool server, command line access, and
  // exporting and importing settings and mail.
  import '../../../theme/settings2-sections.css'
  import Card from '../ui/Card.svelte'
  import ExternalSection from '../../settings/ExternalSection.svelte'
  import ImportExportSection from '../../settings/ImportExportSection.svelte'
  import { loadCliCard } from '../../../lib/lazyload'
  import { t } from '../../../lib/i18n'
</script>

<Card title={$t('settingsPanel.category.external')} bare>
  <div class="s2-section" data-setting="external">
    <ExternalSection />
  </div>
</Card>

{#await loadCliCard() then cli}
  <svelte:component this={cli.default} />
{/await}

<Card title={$t('settingsPanel.category.importExport')} bare>
  <div class="s2-section" data-setting="import-export">
    <ImportExportSection guided />
  </div>
</Card>
