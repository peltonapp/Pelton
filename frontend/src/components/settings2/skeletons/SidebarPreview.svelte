<script lang="ts">
  // the folder pane of one mailbox: unified views, the account header and a
  // small folder tree. every part follows its own setting: indent guides on
  // nested folders, the address in the header, the count on Flagged, the mark on
  // a folder that is not syncing, and where the unified views live.
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-simple-duotone.svg?raw'
  import trayIcon from '@phosphor-icons/core/assets/duotone/tray-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import offIcon from '@phosphor-icons/core/assets/duotone/cloud-slash-duotone.svg?raw'
  import Frame from './Frame.svelte'
  import Ph from '../../common/Ph.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Draws vertical guide lines beside nested folders. */
  export let indentGuides: boolean = true
  /** Writes the address in the account header instead of its name. */
  export let accountEmail: boolean = false
  /** Counts and bolds the Flagged view. */
  export let flaggedCount: boolean = true
  /** Marks a folder that is excluded from sync. */
  export let unsynced: boolean = true
  /** Where the unified views are: in the sidebar, in a tab strip, or nowhere. */
  export let views: 'hidden' | 'sidebar' | 'tab' = 'sidebar'

  const person = sampleMessages[1]

  $: folders = [
    { key: 'inbox', label: $t('folders.role.inbox'), icon: trayIcon, depth: 0, count: 3, active: true },
    { key: 'projects', label: $t('preview.folder.projects'), icon: folderIcon, depth: 0 },
    { key: 'invoices', label: $t('preview.folder.invoices'), icon: folderIcon, depth: 1 },
    { key: 'travel', label: $t('preview.folder.travel'), icon: folderIcon, depth: 1 },
    { key: 'archive', label: $t('folders.role.archive'), icon: folderIcon, depth: 0, off: true },
  ]
</script>

<Frame sunken>
  <div class="pane">
    {#if views === 'tab'}
      <div class="tabs">
        <span class="tab on">{$t('sidebar.view.inbox')}</span>
        <span class="tab">{$t('sidebar.view.flagged')}</span>
        <span class="tab">{$t('sidebar.view.unread')}</span>
      </div>
    {/if}
    <div class="side">
      {#if views === 'sidebar'}
        <span class="head">{$t('sidebar.unifiedViews.heading')}</span>
        <span class="row">
          <span class="icon"><Ph svg={trayIcon} size={14} /></span>
          <span class="label">{$t('sidebar.view.inbox')}</span>
        </span>
        <span class="row" class:strong={flaggedCount}>
          <span class="icon"><Ph svg={flagIcon} size={14} /></span>
          <span class="label">{$t('sidebar.view.flagged')}</span>
          {#if flaggedCount}
            <span class="count">2</span>
          {/if}
        </span>
      {/if}
      <span class="head">{accountEmail ? person.email : person.name}</span>
      {#each folders as f (f.key)}
        <span class="row" class:active={f.active} class:strong={f.count} style={`--depth:${f.depth}`}>
          {#if indentGuides}
            {#each Array.from({ length: f.depth + 1 }, (_, i) => i) as level (level)}
              <span class="guide" style={`--level:${level}`}></span>
            {/each}
          {/if}
          <span class="icon"><Ph svg={f.icon} size={14} /></span>
          <span class="label">{f.label}</span>
          {#if f.off && unsynced}
            <span class="off"><Ph svg={offIcon} size={12} /></span>
          {/if}
          {#if f.count}
            <span class="count">{f.count}</span>
          {/if}
        </span>
      {/each}
    </div>
  </div>
</Frame>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .tabs {
    display: flex;
    gap: var(--space-2);
  }

  .tab {
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .tab.on {
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 1px;
    max-width: 200px;
  }

  .head {
    padding: var(--space-3) var(--space-3) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-1) var(--space-3);
    padding-inline-start: calc(var(--space-3) + var(--depth, 0) * var(--space-4));
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .row.active {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .row.strong .label {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .icon {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .active .icon {
    color: var(--accent);
  }

  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .guide {
    position: absolute;
    top: 0;
    bottom: 0;
    inset-inline-start: calc(var(--space-3) + var(--level) * var(--space-4) + var(--space-3));
    width: 1px;
    background: var(--border-default);
  }

  .off {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .count {
    padding: 0 var(--space-2);
    border-radius: 999px;
    background: var(--surface-base);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
