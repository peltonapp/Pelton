<script lang="ts">
  // the provider grid of the add-mailbox wizard, shown when the address alone
  // did not settle it or the user wants to choose. each tile seeds the auth
  // method and servers; "other" uses autodiscovery.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import { providerPresets, type ProviderPreset } from '../../lib/providers'
  import { providerIcon } from './providerIcons'
  import { t } from '../../lib/i18n'

  /** Id of the preset currently chosen, highlighted in the grid. */
  export let selected: string | null = null

  const dispatch = createEventDispatcher<{ pick: ProviderPreset }>()
</script>

<div class="grid" role="group" aria-label={$t('wizard.step.provider.lead')}>
  {#each providerPresets as preset (preset.id)}
    <button
      type="button"
      class="tile"
      class:on={selected === preset.id}
      aria-pressed={selected === preset.id}
      on:click={() => dispatch('pick', preset)}
    >
      <Ph svg={providerIcon(preset.id)} size={18} />
      <span class="label">{preset.labelKey ? $t(preset.labelKey) : preset.label}</span>
      {#if preset.kind === 'oauth'}
        <span class="tag">{$t('wizard.provider.signIn')}</span>
      {/if}
    </button>
  {/each}
</div>

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(12em, 1fr));
    gap: var(--space-2);
  }

  .tile {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    text-align: start;
    transition:
      border-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .tile:hover {
    border-color: var(--border-strong);
  }

  .tile:active {
    transform: scale(0.985);
  }

  .tile.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 var(--hairline) var(--accent);
    color: var(--accent);
  }

  .label {
    flex: 1;
    min-width: 0;
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .tag {
    flex-shrink: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
