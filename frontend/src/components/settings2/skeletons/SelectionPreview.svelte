<script lang="ts">
  // a list with several rows picked. with multi select off there are no
  // checkboxes and one row at a time; on, each row has a checkbox and the header
  // can count the picked rows and say what "select all" reached.
  import Frame from './Frame.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Multi select is on: checkboxes and a header. */
  export let enabled: boolean = true
  /** The header counts the picked rows. */
  export let showCount: boolean = false
  /** What select all reached, or null for a plain selection of two rows. */
  export let scope: 'offer' | 'all' | 'loaded' | null = null

  const loaded = 50
  const total = 240

  $: picked = scope ? sampleMessages.length : 2
  $: banner =
    scope === 'all'
      ? $t('preview.selection.all').replace('{n}', String(total))
      : scope
        ? $t('list.selectAll.loadedSelected').replace('{n}', String(loaded))
        : ''
  $: offer = scope === 'offer' ? $t('list.selectAll.expand').replace('{n}', String(total)) : ''
</script>

<Frame>
  <div class="list">
    {#if enabled}
      <div class="head">
        <span class="box" class:on={scope !== null}><span class="tick"></span></span>
        <span class="all">{$t('list.selectAll.label')}</span>
        {#if showCount}
          <span class="count">{picked} {$t('messageList.selectedSuffix')}</span>
        {/if}
      </div>
      {#if banner}
        <div class="banner">
          {banner}
          {#if offer}
            <span class="offer">{offer}</span>
          {/if}
        </div>
      {/if}
    {/if}
    {#each sampleMessages as m, i (m.email)}
      <div class="row" class:picked={enabled ? i < picked : i === 0}>
        {#if enabled}
          <span class="box" class:on={i < picked}><span class="tick"></span></span>
        {/if}
        <span class="from">{m.name}</span>
        <span class="subject">{m.subject}</span>
      </div>
    {/each}
  </div>
</Frame>

<style>
  .list {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .head,
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    font-size: var(--fz-label);
  }

  .head {
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    color: var(--text-secondary);
  }

  .count {
    margin-inline-start: auto;
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .banner {
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--selection-bg);
    font-size: var(--fz-meta);
    color: var(--text-primary);
  }

  .offer {
    margin-inline-start: var(--space-2);
    color: var(--link);
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row.picked {
    background: var(--selection-bg);
  }

  .from {
    flex-shrink: 0;
    color: var(--text-primary);
  }

  .subject {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
  }

  .box {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 12px;
    height: 12px;
    flex-shrink: 0;
    border: var(--hairline) solid var(--border-strong);
    border-radius: 3px;
    background: var(--surface-raised);
  }

  .box.on {
    border-color: var(--accent);
    background: var(--accent);
  }

  .tick {
    display: none;
    width: 3px;
    height: 6px;
    margin-bottom: 1px;
    border: solid var(--accent-fg);
    border-width: 0 1.5px 1.5px 0;
    transform: rotate(45deg);
  }

  .box.on .tick {
    display: block;
  }
</style>
