<script lang="ts">
  // shows a script or library file before it is imported: its name, a
  // script's trigger and the permissions it asks for, its code, and the
  // libraries it needs that are not here. nothing is stored until the user
  // says so, and an imported script arrives switched off.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import PermissionList from './PermissionList.svelte'
  import { t } from '../../lib/i18n'
  import { entryLabel, type ScriptCatalog, type ScriptFile } from '../../lib/scripting'
  import '../../theme/controls.css'

  /** The file read for import. */
  export let file: ScriptFile
  /** The registry. */
  export let catalog: ScriptCatalog

  const dispatch = createEventDispatcher<{ import: void; close: void }>()

  $: library = file.library
  $: name = library ? library.name : file.script.name
  $: trigger = catalog.triggers.find((tr) => tr.id === file.script.trigger.type)
  $: code = library ? library.source : file.script.kind === 'lua' ? file.script.source : file.text
</script>

<Modal
  title={$t(library ? 'scripts.importDialog.libraryTitle' : 'scripts.importDialog.title').replace('{name}', name)}
  hint={$t(library ? 'scripts.importDialog.libraryHint' : 'scripts.importDialog.hint')}
  size="large"
  on:close
>
  <div class="body">
    {#if library}
      {#if library.description}<p class="desc">{library.description}</p>{/if}
    {:else}
      {#if file.script.description}<p class="desc">{file.script.description}</p>{/if}
      <p class="fact">
        <span class="label">{$t('scripts.trigger')}</span>
        {trigger ? entryLabel($t, trigger) : file.script.trigger.type}
      </p>
      <h3 class="heading">{$t('scripts.permissions')}</h3>
      <PermissionList {catalog} declared={file.script.permissions} approved={file.script.permissions} />
    {/if}
    {#if file.missingLibraries.length > 0}
      <p class="warn" role="note">{$t('scripts.importDialog.missingLibraries').replace('{list}', file.missingLibraries.join(', '))}</p>
    {/if}
    <h3 class="heading">{$t(library || file.script.kind === 'lua' ? 'scripts.code' : 'scripts.importDialog.file')}</h3>
    <pre class="code">{code}</pre>
  </div>
  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('scripts.cancel')}</button>
    <button type="button" class="cal-btn primary" on:click={() => dispatch('import')}>{$t('scripts.importDialog.confirm')}</button>
  </svelte:fragment>
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .desc,
  .fact {
    margin: 0;
    font-size: var(--fz-label);
  }

  .label {
    margin-inline-end: var(--space-2);
    color: var(--text-secondary);
  }

  .heading {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .warn {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-label);
  }

  .code {
    margin: 0;
    max-height: var(--automation-steps-max);
    overflow: auto;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    white-space: pre-wrap;
  }
</style>
