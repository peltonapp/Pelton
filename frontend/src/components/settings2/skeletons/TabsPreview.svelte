<script lang="ts">
  // the tab strip as the app opens. with restore on, the messages that were
  // open in tabs at the last quit are back; off, only the first tab is.
  import Frame from './Frame.svelte'
  import MiniWindow from './MiniWindow.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Brings back the tabs of the last session. */
  export let restore: boolean = false

  $: tabs = restore ? sampleMessages.map((m) => m.subject) : []
</script>

<Frame sunken>
  <div class="window">
    <div class="strip">
      <span class="tab on">{$t('sidebar.view.inbox')}</span>
      {#each tabs as title (title)}
        <span class="tab">{title}</span>
      {/each}
    </div>
    <div class="body"><MiniWindow /></div>
  </div>
</Frame>

<style>
  .window {
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-base);
    overflow: hidden;
  }

  .strip {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .tab {
    min-width: 0;
    max-width: 90px;
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid transparent;
    border-bottom: none;
    border-radius: var(--radius-control) var(--radius-control) 0 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .tab.on {
    border-color: var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .body {
    height: 56px;
  }
</style>
