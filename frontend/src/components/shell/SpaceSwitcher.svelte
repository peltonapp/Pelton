<script lang="ts">
  // the space switcher as a horizontal segmented control, used where there is
  // no rail: at the foot of the sidebar and as tabs in the top bar. a highlight
  // slides under the active segment.
  import Ph from '../common/Ph.svelte'
  import { spaces } from '../../lib/spaces'
  import { space, openSpace } from '../../stores/space'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'

  /** Shows each entry's name beside its icon. */
  export let labels: boolean = false

  $: activeIndex = Math.max(0, spaces.findIndex((s) => s.key === $space))
</script>

<div
  class="switcher"
  class:labels
  role="tablist"
  aria-label={$t('space.switcher')}
  style={`--count:${spaces.length};--active:${activeIndex}`}
>
  <span class="thumb" aria-hidden="true"></span>
  {#each spaces as entry (entry.key)}
    <button
      type="button"
      role="tab"
      class="segment"
      class:active={$space === entry.key}
      aria-selected={$space === entry.key}
      aria-label={$t(entry.labelKey)}
      use:tip={{ text: labels ? '' : $t(entry.labelKey), side: 'top' }}
      on:click={() => openSpace(entry.key)}
    >
      <Ph svg={entry.icon} size={16} />
      {#if labels}
        <span>{$t(entry.labelKey)}</span>
      {/if}
    </button>
  {/each}
</div>

<style>
  .switcher {
    position: relative;
    display: grid;
    grid-template-columns: repeat(var(--count), minmax(0, 1fr));
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    --wails-draggable: no-drag;
  }

  .thumb {
    position: absolute;
    top: var(--space-1);
    bottom: var(--space-1);
    inset-inline-start: var(--space-1);
    width: calc((100% - 2 * var(--space-1)) / var(--count));
    border-radius: calc(var(--radius-control) - 1px);
    background: var(--surface-raised);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
    transform: translateX(calc(var(--active) * 100%));
    transition: transform var(--motion-base) var(--ease-out);
  }

  :global([dir='rtl']) .thumb {
    transform: translateX(calc(var(--active) * -100%));
  }

  .segment {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - 6px);
    padding: 0 var(--space-4);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out);
  }

  .segment:hover,
  .segment.active {
    color: var(--text-primary);
  }
</style>
