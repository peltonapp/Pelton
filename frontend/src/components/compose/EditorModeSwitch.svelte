<script lang="ts">
  // a segmented switch for the three compose editor modes. wysiwyg is marked as
  // basic since it is the stubbed editor for now.
  import { createEventDispatcher } from 'svelte'
  import type { EditorMode } from '../../lib/types'
  import { t } from '../../lib/i18n'

  export let mode: EditorMode

  const dispatch = createEventDispatcher<{ change: EditorMode }>()

  type ModeOption = { key: EditorMode; label: string; title: string }
  $: modes = [
    { key: 'plaintext', label: $t('compose.mode.plain'), title: $t('compose.mode.plainTitle') },
    { key: 'markdown', label: $t('compose.mode.markdown'), title: $t('compose.mode.markdownTitle') },
    { key: 'wysiwyg', label: $t('compose.mode.rich'), title: $t('compose.mode.richTitle') },
  ] as ModeOption[]
</script>

<div class="switch" role="tablist" aria-label={$t('compose.mode.ariaLabel')}>
  {#each modes as m (m.key)}
    <button
      type="button"
      role="tab"
      aria-selected={mode === m.key}
      class:active={mode === m.key}
      title={m.title}
      on:click={() => dispatch('change', m.key)}
    >
      {m.label}
    </button>
  {/each}
</div>

<style>
  .switch {
    display: inline-flex;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  button {
    border: none;
    background: var(--surface-raised);
    color: var(--text-secondary);
    padding: var(--space-1) var(--space-3);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    white-space: nowrap;
    border-inline-end: var(--hairline) solid var(--border-subtle);
  }

  button:last-child {
    border-inline-end: none;
  }

  button:hover {
    background: var(--surface-hover);
  }

  button.active {
    background: var(--selection-bg);
    color: var(--text-primary);
  }
</style>
