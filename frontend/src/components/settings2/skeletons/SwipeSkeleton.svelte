<script lang="ts">
  // a small picture of a list row slid aside to show the action underneath,
  // which is what a trackpad swipe does. a row dragged to the left shows its
  // action on the right edge; dragged to the right, on the left edge.
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-open-duotone.svg?raw'
  import unreadIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import archiveIcon from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
  import snoozeIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import noneIcon from '@phosphor-icons/core/assets/duotone/prohibit-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'

  /** Which way the row is dragged. */
  export let direction: 'left' | 'right' = 'left'
  /** Swipe action key: none, delete, read, unread, flag, archive or snooze. */
  export let action: string = 'none'
  /** Name of the action, written next to its icon. */
  export let label: string = ''
  /** Height of the picture in px. */
  export let height: number = 52

  const meta: Record<string, { icon: string; color: string }> = {
    none: { icon: noneIcon, color: 'var(--text-tertiary)' },
    delete: { icon: trashIcon, color: 'var(--danger)' },
    read: { icon: openIcon, color: 'var(--accent)' },
    unread: { icon: unreadIcon, color: 'var(--accent)' },
    flag: { icon: flagIcon, color: 'var(--warning)' },
    archive: { icon: archiveIcon, color: 'var(--text-tertiary)' },
    snooze: { icon: snoozeIcon, color: 'var(--accent)' },
  }

  $: current = meta[action] ?? meta.none
</script>

<div class="stage" class:left={direction === 'left'} style={`height:${height}px`} aria-hidden="true">
  <div class="reveal" style={`background:${current.color}`}>
    <Ph svg={current.icon} size={14} />
    {#if label}
      <span>{label}</span>
    {/if}
  </div>
  <div class="row">
    <span class="lines">
      <span class="bar w45 strong"></span>
      <span class="bar w80"></span>
    </span>
  </div>
</div>

<style>
  .stage {
    position: relative;
    overflow: hidden;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
  }

  .reveal {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-3);
    color: var(--text-inverse);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
  }

  .left .reveal {
    justify-content: flex-end;
  }

  .row {
    position: absolute;
    top: 0;
    bottom: 0;
    inset-inline-start: 38%;
    inset-inline-end: -38%;
    display: flex;
    align-items: center;
    padding: 0 8px;
    background: var(--surface-raised);
    border-inline-start: var(--hairline) solid var(--border-default);
    box-shadow: var(--shadow-overlay);
  }

  .left .row {
    inset-inline-start: -38%;
    inset-inline-end: 38%;
    border-inline-start: none;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .lines {
    display: flex;
    flex-direction: column;
    gap: 5px;
    flex: 1;
    min-width: 0;
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .bar.strong {
    height: 4px;
    background: var(--text-tertiary);
  }

  .w45 { width: 45%; }
  .w80 { width: 80%; }
</style>
