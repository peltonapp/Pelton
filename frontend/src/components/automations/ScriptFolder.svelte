<script lang="ts">
  // the folder a script that declares a files permission may read and write
  // in. the user picks it in a folder dialog; it stays on this device and is
  // never part of an exported script.
  import { t } from '../../lib/i18n'
  import { clearScriptFolder, pickScriptFolder, scriptFolder } from '../../lib/api'
  import { errorMessage, toastError } from '../../stores/toast'

  /** The saved script's id. */
  export let scriptId: number

  let folder = ''
  let busy = false

  $: void load(scriptId)

  async function load(id: number): Promise<void> {
    try {
      const got = await scriptFolder(id)
      if (id === scriptId) {
        folder = got
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function pick(): Promise<void> {
    busy = true
    try {
      const got = await pickScriptFolder(scriptId)
      if (got) {
        folder = got
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function clear(): Promise<void> {
    busy = true
    try {
      await clearScriptFolder(scriptId)
      folder = ''
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }
</script>

<div class="folder">
  <span class="label">{$t('scripts.folder.label')}</span>
  {#if folder}
    <code class="path" title={folder}>{folder}</code>
  {:else}
    <span class="none">{$t('scripts.folder.none')}</span>
  {/if}
  <div class="actions">
    <button type="button" class="cal-btn" disabled={busy} on:click={() => void pick()}>
      {$t(folder ? 'scripts.folder.change' : 'scripts.folder.pick')}
    </button>
    {#if folder}
      <button type="button" class="cal-btn" disabled={busy} on:click={() => void clear()}>{$t('scripts.folder.clear')}</button>
    {/if}
  </div>
  <p class="hint">{$t('scripts.folder.hint')}</p>
</div>

<style>
  .folder {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin-top: var(--space-2);
  }

  .label {
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .path {
    overflow: hidden;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .none,
  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .actions {
    display: flex;
    gap: var(--space-2);
  }
</style>
