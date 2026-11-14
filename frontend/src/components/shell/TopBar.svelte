<script lang="ts">
  // the unified top bar of the new design: it is the window's title bar, so it
  // drags the window, keeps clear of the macOS traffic lights, and carries the
  // menu, the search field and what the app is busy with.
  import PieProgress from '../common/PieProgress.svelte'
  import SpaceSwitcher from './SpaceSwitcher.svelte'
  import TopSearch from './TopSearch.svelte'
  import { outbox, syncing, syncCounts } from '../../stores/outbox'
  import { titleBarDoubleClick } from '../../lib/api'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'

  /** Shows the space switcher as tabs in the bar. */
  export let tabs: boolean = false

  $: pending = $outbox.length
  $: syncShare = $syncCounts.total > 0 ? $syncCounts.done / $syncCounts.total : null
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<header class="topbar" style="--wails-draggable:drag" on:dblclick|self={titleBarDoubleClick}>
  <div class="side start">
    <slot name="menu" />
    {#if tabs}
      <SpaceSwitcher labels />
    {/if}
  </div>

  <TopSearch />

  <div class="side end">
    {#if $syncing || pending > 0}
      <span
        class="activity"
        use:tip={{ text: $syncing ? $t('topbar.syncing') : $t('topbar.sending').replace('{count}', String(pending)), side: 'bottom' }}
      >
        <PieProgress value={$syncing ? syncShare : null} size={14} label={$syncing ? $t('topbar.syncing') : $t('topbar.sending').replace('{count}', String(pending))} />
        {#if pending > 0}
          <span class="count">{pending}</span>
        {/if}
      </span>
    {/if}
    <slot name="end" />
  </div>
</header>

<style>
  .topbar {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, var(--topsearch-width)) minmax(0, 1fr);
    align-items: center;
    gap: var(--space-4);
    height: var(--topbar-height);
    padding: 0 var(--space-3) 0 max(var(--space-3), var(--titlebar-inset));
    user-select: none;
  }

  .side {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }

  .side.end {
    justify-content: flex-end;
  }

  .activity {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - 4px);
    padding: 0 var(--space-3);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    --wails-draggable: no-drag;
  }
</style>
