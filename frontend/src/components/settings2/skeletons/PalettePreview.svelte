<script lang="ts">
  // the command palette with three results. with quick select on, the first
  // runs with Enter and the others carry the number that runs them.
  import Frame from './Frame.svelte'
  import Keycap from './Keycap.svelte'
  import { t } from '../../../lib/i18n'

  /** Shows the key that runs each result. */
  export let quickSelect: boolean = true

  $: results = [
    { key: 'compose', label: $t('menu.compose'), cap: '↵' },
    { key: 'sync', label: $t('menu.sync'), cap: '2' },
    { key: 'preferences', label: $t('menu.preferences'), cap: '3' },
  ]
</script>

<Frame sunken>
  <div class="palette">
    <div class="input">{$t('palette.placeholder')}</div>
    {#each results as r, i (r.key)}
      <div class="result" class:on={i === 0}>
        <span>{r.label}</span>
        {#if quickSelect}
          <Keycap>{r.cap}</Keycap>
        {/if}
      </div>
    {/each}
  </div>
</Frame>

<style>
  .palette {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  .input {
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .result {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: calc(var(--fz-label) + var(--space-5));
    padding: 0 var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .result.on {
    background: var(--selection-bg);
  }
</style>
