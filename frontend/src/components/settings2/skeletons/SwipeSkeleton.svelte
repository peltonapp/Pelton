<script lang="ts">
  // a small picture of a list row slid aside to show the action underneath,
  // which is what a trackpad swipe does. a row dragged to the left shows its
  // action on the right edge; dragged to the right, on the left edge.
  import noneIcon from '@phosphor-icons/core/assets/duotone/prohibit-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import { isRowAction, rowActionView } from '../../../lib/rowactions'
  import { placed } from '../../../lib/scriptplacements'

  const swipeScripts = placed('row.swipe')

  /** Which way the row is dragged. */
  export let direction: 'left' | 'right' = 'left'
  /** Swipe action: a row action id, or none. */
  export let action: string = 'none'
  /** Name of the action, written next to its icon. */
  export let label: string = ''
  /** Height of the picture in px. */
  export let height: number = 52
  /** How far the row is slid aside, as a share of its width. */
  export let reach: number = 0.38

  const state = { seen: false, flagged: false, pinned: false, muted: false, inSpam: false }

  // the colours the row's own reveal fills with once a swipe is armed.
  $: view = isRowAction(action) ? rowActionView(action, state, $swipeScripts) : null
  $: current = view
    ? { icon: view.icon, color: view.danger ? 'var(--danger)' : 'var(--accent)' }
    : { icon: noneIcon, color: 'var(--text-tertiary)' }
</script>

<div class="stage" class:left={direction === 'left'} style={`height:${height}px;--reach:${reach * 100}%`} aria-hidden="true">
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
    inset-inline-start: var(--reach);
    inset-inline-end: calc(var(--reach) * -1);
    display: flex;
    align-items: center;
    padding: 0 8px;
    background: var(--surface-raised);
    border-inline-start: var(--hairline) solid var(--border-default);
    box-shadow: var(--shadow-overlay);
  }

  .left .row {
    inset-inline-start: calc(var(--reach) * -1);
    inset-inline-end: var(--reach);
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
