<script lang="ts">
  // the unified top bar of the new design: it is the window's title bar, so it
  // drags the window, keeps clear of the macOS traffic lights, and carries the
  // menu, the search field and what the app is busy with.
  import { createEventDispatcher } from 'svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import SpaceSwitcher from './SpaceSwitcher.svelte'
  import { outbox, syncing, syncCounts } from '../../stores/outbox'
  import { shortcutTitle, bindings, menuHint } from '../../stores/shortcuts'
  import { titleBarDoubleClick } from '../../lib/api'
  import { tip } from '../../lib/tip'
  import { t, isMac } from '../../lib/i18n'
  import WindowButtons from './WindowButtons.svelte'
  import { frameIntegrated } from '../../stores/windowframe'

  /** Shows the space switcher as tabs in the bar. */
  export let tabs: boolean = false

  const dispatch = createEventDispatcher<{ search: void }>()

  $: pending = $outbox.length
  $: syncShare = $syncCounts.total > 0 ? $syncCounts.done / $syncCounts.total : null
  $: searchKeys = $bindings.search ? menuHint('search') : ''
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<header
  class="topbar"
  class:buttons={!isMac && $frameIntegrated}
  style="--wails-draggable:drag"
  on:dblclick|self={titleBarDoubleClick}
>
  <div class="side start">
    <slot name="menu" />
    {#if tabs}
      <SpaceSwitcher labels />
    {/if}
  </div>

  <button
    type="button"
    class="search"
    title={$shortcutTitle($t('shortcut.search'), 'search')}
    on:click={() => dispatch('search')}
  >
    <Ph svg={searchIcon} size={15} />
    <span class="placeholder">{$t('topbar.search')}</span>
    {#if searchKeys}
      <kbd>{searchKeys}</kbd>
    {/if}
  </button>

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
    {#if !isMac && $frameIntegrated}
      <WindowButtons />
    {/if}
  </div>
</header>

<style>
  .topbar {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 420px) minmax(0, 1fr);
    align-items: center;
    gap: var(--space-4);
    height: var(--topbar-height);
    padding: 0 var(--space-3) 0 max(var(--space-3), var(--titlebar-inset));
    user-select: none;
  }

  /* the window buttons run to the corner of the window. */
  .topbar.buttons {
    padding-inline-end: 0;
  }

  .side {
    display: flex;
    align-self: stretch;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }

  .side.end {
    justify-content: flex-end;
  }

  .search {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    height: calc(var(--control-height) - 4px);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    cursor: text;
    --wails-draggable: no-drag;
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out);
  }

  .search:hover {
    border-color: var(--border-strong);
    color: var(--text-secondary);
  }

  .placeholder {
    flex: 1;
    text-align: start;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  kbd {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
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
