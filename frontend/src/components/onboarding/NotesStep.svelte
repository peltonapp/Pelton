<script lang="ts">
  // the notes step of the setup: where the Markdown files live. the default
  // folder in Pelton's data folder, a folder of the user's own, or an existing
  // Obsidian vault. each button is the confirmation; nothing is created before.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-simple-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
  import vaultIcon from '@phosphor-icons/core/assets/duotone/vault-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import { vaultChooseFolder, vaultUseDefaultFolder } from '../../lib/api'
  import { prefs, setNotesVaultPref } from '../../stores/prefs'
  import { vaultSwitched } from '../../stores/notes'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { VaultInfo } from '../../lib/types'

  const dispatch = createEventDispatcher<{ done: string }>()

  let folder = ''
  let picked: 'default' | 'folder' | 'vault' | '' = ''
  let error = ''
  let busy = false

  async function apply(kind: 'default' | 'folder' | 'vault'): Promise<void> {
    if (busy) {
      return
    }
    busy = true
    error = ''
    try {
      const info: VaultInfo = kind === 'default' ? await vaultUseDefaultFolder() : await vaultChooseFolder()
      if (info.error) {
        error = info.error
        return
      }
      if (!info.folder) {
        return
      }
      setNotesVaultPref(kind === 'default' ? '' : info.folder)
      await vaultSwitched(info)
      folder = info.folder
      picked = kind
      dispatch('done', info.folder)
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = false
    }
  }

  $: options = [
    { key: 'default' as const, icon: folderIcon, title: $t('setup.notes.default'), hint: $t('setup.notes.default.hint') },
    { key: 'folder' as const, icon: openIcon, title: $t('setup.notes.folder'), hint: $t('setup.notes.folder.hint') },
    { key: 'vault' as const, icon: vaultIcon, title: $t('setup.notes.vault'), hint: $t('setup.notes.vault.hint') },
  ]
</script>

{#if $prefs.notesVault && !picked}
  <p class="state"><Ph svg={checkIcon} size={16} /><span>{$t('setup.notes.current')} <code>{$prefs.notesVault}</code></span></p>
{/if}

<div class="options">
  {#each options as o (o.key)}
    <button type="button" class="option" class:on={picked === o.key} aria-pressed={picked === o.key} disabled={busy} on:click={() => void apply(o.key)}>
      <Ph svg={o.icon} size={20} />
      <span class="text">
        <span class="title">{o.title}</span>
        <span class="hint">{o.hint}</span>
      </span>
      {#if picked === o.key}<Ph svg={checkIcon} size={16} />{/if}
    </button>
  {/each}
</div>

{#if folder}
  <p class="state" role="status"><span>{$t('setup.notes.using')} <code>{folder}</code></span></p>
{/if}
{#if error}
  <p class="error" role="alert"><Ph svg={warningIcon} size={13} />{error}</p>
{/if}

<style>
  .options {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .option {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-secondary);
    text-align: start;
    cursor: var(--cursor-action);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .option:hover:not(:disabled) {
    border-color: var(--border-strong);
  }

  .option.on {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .option:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .hint {
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-tertiary);
  }

  .state,
  .error {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0 0 var(--space-4);
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .options + .state,
  .options + .error {
    margin: var(--space-4) 0 0;
  }

  .state :global(.ph) {
    color: var(--success);
  }

  .error {
    color: var(--danger);
  }

  code {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }
</style>
