<script lang="ts">
  // a row of message actions. with shortcut hints on, each carries the key that
  // does the same thing from the keyboard.
  import Frame from './Frame.svelte'
  import Keycap from './Keycap.svelte'
  import { t } from '../../../lib/i18n'

  /** Shows the key beside each action. */
  export let hints: boolean = true

  $: actions = [
    { key: 'reply', label: $t('action.reply'), cap: 'R' },
    { key: 'archive', label: $t('action.archive'), cap: 'E' },
    { key: 'delete', label: $t('action.delete'), cap: '⌫' },
  ]
</script>

<Frame>
  <div class="bar">
    {#each actions as a (a.key)}
      <span class="action">
        {a.label}
        {#if hints}
          <Keycap>{a.cap}</Keycap>
        {/if}
      </span>
    {/each}
  </div>
</Frame>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .action {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-hover);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }
</style>
