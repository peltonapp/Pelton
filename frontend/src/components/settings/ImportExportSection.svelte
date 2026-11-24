<script lang="ts">
  // Import/export replaces the removed folder-based config sync with a plain,
  // local file: the user picks what to export, and on import picks a file and
  // chooses what to bring in. Pelton only reads and writes the file the user
  // chooses; nothing leaves the machine on its own.
  import { IconDownload, IconUpload, IconFileImport } from '@tabler/icons-svelte'
  import { t } from '../../lib/i18n'
  import { loadImportDialog, loadGuidedImport } from '../../lib/lazyload'

  /** Opens the New design's guided import instead of the import dialog. */
  export let guided = false

  let exportOpen = false
  let importOpen = false
</script>

<div class="section">
  <div class="block">
    <div class="block-head">
      <IconUpload size={16} stroke={1.7} />
      <h4>{$t('importExport.exportTitle')}</h4>
    </div>
    <p class="hint">{$t('importExport.exportHint')}</p>
    <button type="button" class="action-btn primary" on:click={() => (exportOpen = true)}>
      <IconDownload size={14} stroke={1.8} />
      {$t('importExport.exportButton')}
    </button>
  </div>

  <!-- a Thunderbird profile, mail files and a Pelton backup all come in
       through the one import dialog. -->
  <div class="block">
    <div class="block-head">
      <IconFileImport size={16} stroke={1.7} />
      <h4>{$t('importExport.importTitle')}</h4>
    </div>
    <p class="hint">{$t('import.entry.sub')}</p>
    <button type="button" class="action-btn" on:click={() => (importOpen = true)}>
      {$t('importExport.importButton')}
    </button>
  </div>
</div>

<!-- the modals are code-split so their logic loads only on demand. -->
{#if exportOpen}
  {#await import('./ExportModal.svelte') then m}
    <svelte:component this={m.default} on:close={() => (exportOpen = false)} />
  {/await}
{/if}

{#if importOpen && guided}
  {#await loadGuidedImport() then m}
    <svelte:component this={m.default} on:close={() => (importOpen = false)} />
  {/await}
{:else if importOpen}
  {#await loadImportDialog() then m}
    <svelte:component this={m.default} on:close={() => (importOpen = false)} />
  {/await}
{/if}

<style>
  .section {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    align-items: flex-start;
  }

  .block-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-secondary);
  }
  h4 {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .action-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-1);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }
  .action-btn:hover {
    background: var(--surface-hover);
  }
  .action-btn.primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }
</style>
