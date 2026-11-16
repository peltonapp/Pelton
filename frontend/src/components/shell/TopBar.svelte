<script lang="ts">
  // the unified top bar of the new design: it is the window's title bar, so it
  // drags the window, keeps clear of the macOS traffic lights, and carries the
  // menu, the search field and what the app is busy with.
  import SpaceSwitcher from './SpaceSwitcher.svelte'
  import TopSearch from './TopSearch.svelte'
  import SpaceSearch from './SpaceSearch.svelte'
  import { space } from '../../stores/space'
  import { spaceSearchProviders } from '../../lib/spacesearch'
  import ActivityButton from './ActivityButton.svelte'
  import FocusButton from './FocusButton.svelte'
  import HistoryButton from './HistoryButton.svelte'
  import MyDayButton from './MyDayButton.svelte'
  import { titleBarDoubleClick } from '../../lib/api'
  import { isMac } from '../../lib/i18n'
  import WindowButtons from './WindowButtons.svelte'
  import { frameIntegrated } from '../../stores/windowframe'

  /** Shows the space switcher as tabs in the bar. */
  export let tabs: boolean = false

  // a space with a search of its own takes the field over while it is showing.
  $: spaceProvider = $space === 'mail' ? undefined : $spaceSearchProviders[$space]
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

  {#if spaceProvider}
    <SpaceSearch provider={spaceProvider} />
  {:else}
    <TopSearch />
  {/if}

  <div class="side end">
    {#if $space === 'mail'}
      <MyDayButton />
    {/if}
    <FocusButton />
    <HistoryButton />
    <ActivityButton />
    <slot name="end" />
    {#if !isMac && $frameIntegrated}
      <WindowButtons />
    {/if}
  </div>
</header>

<style>
  .topbar {
    display: grid;
    /* the sides are equal while there is room, which centres the search; in a
       narrow window they keep what the menu and the window buttons need and
       the search gives way instead of covering them. */
    grid-template-columns: minmax(max-content, 1fr) minmax(var(--topsearch-min-width), var(--topsearch-width)) minmax(max-content, 1fr);
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
</style>
