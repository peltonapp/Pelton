<script lang="ts">
  // a compose tab: while the active tab is one, it covers the whole content
  // area (list and reader) with the tab bar and the composer. switching to
  // another tab in the bar uncovers the panes as they were left.
  import { loadComposer } from '../../lib/lazyload'
  import TabBar from '../detail/TabBar.svelte'
  import { composeSessions } from '../../stores/compose'
  import { activeComposeId } from '../../stores/tabs'

  $: session = $activeComposeId === null ? null : ($composeSessions.find((s) => s.id === $activeComposeId) ?? null)
</script>

{#if session}
  <div class="compose-tab">
    <TabBar />
    {#key session.id}
      {#await loadComposer() then m}
        <svelte:component this={m.default} {session} layout="tab" mainWindow />
      {/await}
    {/key}
  </div>
{/if}

<style>
  .compose-tab {
    position: absolute;
    inset: 0;
    z-index: 20;
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    background: var(--surface-raised);
    border-radius: inherit;
    overflow: hidden;
  }
</style>
