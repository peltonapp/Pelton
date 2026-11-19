<script lang="ts">
  // the middle pane of the Feeds space: what the chosen scope holds, newest
  // first. its bar names the scope and carries the filter field, the switch
  // between every entry and unread ones, mark all read and refresh, with a
  // progress pie while feeds are fetched. scrolling near the end loads the next
  // page; with the setting on, rows scrolled past the top are marked read.
  import { createEventDispatcher, tick } from 'svelte'
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import checksIcon from '@phosphor-icons/core/assets/duotone/checks-duotone.svg?raw'
  import sidebarIcon from '@phosphor-icons/core/assets/duotone/sidebar-simple-duotone.svg?raw'
  import rssIcon from '@phosphor-icons/core/assets/duotone/rss-duotone.svg?raw'
  import emptyIcon from '@phosphor-icons/core/assets/duotone/tray-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ErrorState from '../common/ErrorState.svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import FeedEntryRow from './FeedEntryRow.svelte'
  import { scrolledPast, type FeedRow } from '../../lib/feedlogic'
  import type { FeedsProgressEvent } from '../../lib/events'
  import type { TimeFormat } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import type { Feed } from '../../lib/types'

  export let rows: FeedRow[]
  export let selectedKey: string | null
  /** The name of what the list shows. */
  export let title: string
  /** The scope has an all/unread switch (feed, folder, starred, read later). */
  export let canFilter: boolean
  export let unreadOnly: boolean
  export let search: string
  export let loading: boolean
  export let more: boolean
  export let error: string
  /** Every feed, for icons and whether a row may show its thumbnail. */
  export let feeds: Feed[]
  /** The Feeds default for remote images. */
  export let loadImages: boolean
  /** The list shows one feed, so rows leave its name out. */
  export let singleFeed: boolean
  /** No feed is subscribed yet. */
  export let noFeeds: boolean
  /** The newsletter view, which has no feeds of its own. */
  export let newsletters: boolean
  export let progress: FeedsProgressEvent | null
  /** Marks rows read as they scroll past the top. */
  export let markOnScroll: boolean
  /** Shows the button that opens the folded sidebar. */
  export let sideToggle: boolean
  export let sideOpen: boolean
  export let timeFormat: TimeFormat

  const dispatch = createEventDispatcher<{
    select: FeedRow
    open: FeedRow
    star: FeedRow
    menu: { row: FeedRow; x: number; y: number }
    unreadOnly: boolean
    search: string
    markAll: void
    refresh: void
    loadMore: void
    passed: FeedRow[]
    toggleSide: void
    add: void
    importOpml: void
  }>()

  let listEl: HTMLDivElement
  let searchInput: HTMLInputElement
  let typed = search
  $: if (search !== typed && document.activeElement !== searchInput) {
    typed = search
  }

  let searchTimer: ReturnType<typeof setTimeout> | undefined
  function onSearchInput(): void {
    clearTimeout(searchTimer)
    searchTimer = setTimeout(() => dispatch('search', typed), 220)
  }

  function clearSearch(): void {
    clearTimeout(searchTimer)
    typed = ''
    dispatch('search', '')
  }

  /** Puts the cursor in the filter field. */
  export function focusSearch(): void {
    searchInput?.focus()
    searchInput?.select()
  }

  /** Moves keyboard focus into the list, onto the selected row. */
  export async function focusList(): Promise<void> {
    await tick()
    const el = listEl?.querySelector<HTMLElement>(selectedKey ? `[data-feed-row="${selectedKey}"]` : '[data-feed-row]')
    ;(el ?? listEl)?.focus()
  }

  /** Brings a row into view. */
  export async function reveal(key: string): Promise<void> {
    await tick()
    listEl?.querySelector<HTMLElement>(`[data-feed-row="${key}"]`)?.scrollIntoView({ block: 'nearest' })
  }

  $: iconOf = new Map(feeds.map((f) => [f.id, f.icon]))
  // a feed renamed, or a list loaded before the sidebar, still names its feed.
  $: titleOf = new Map(feeds.map((f) => [f.id, f.title]))
  $: shown = rows.map((r) => (r.mail ? r : { ...r, source: titleOf.get(r.feedId) ?? r.source }))
  $: imagesOf = new Map(feeds.map((f) => [f.id, f.loadImages || loadImages]))
  $: progressValue = progress && progress.total > 0 ? progress.done / progress.total : null
  $: unreadCount = rows.filter((r) => !r.read).length

  let scrollTimer: ReturnType<typeof setTimeout> | undefined
  function onScroll(): void {
    if (!listEl) {
      return
    }
    if (more && !loading && listEl.scrollTop + listEl.clientHeight > listEl.scrollHeight - listEl.clientHeight) {
      dispatch('loadMore')
    }
    if (markOnScroll) {
      clearTimeout(scrollTimer)
      scrollTimer = setTimeout(markPassed, 250)
    }
  }

  function markPassed(): void {
    if (!listEl) {
      return
    }
    const bottoms = new Map<string, number>()
    for (const el of Array.from(listEl.querySelectorAll<HTMLElement>('[data-feed-row]'))) {
      bottoms.set(el.dataset.feedRow ?? '', el.offsetTop - listEl.offsetTop + el.offsetHeight)
    }
    const passed = scrolledPast(rows, bottoms, listEl.scrollTop)
    if (passed.length) {
      dispatch('passed', passed)
    }
  }

  function onRowKey(event: KeyboardEvent, row: FeedRow): void {
    if (event.key === 'Enter') {
      event.preventDefault()
      dispatch('open', row)
    }
  }
</script>

<section class="pane" aria-label={title}>
  <header class="bar">
    <div class="titles">
      {#if sideToggle}
        <button type="button" class="cal-icon" aria-label={$t('feeds.sidebar')} aria-expanded={sideOpen} on:click={() => dispatch('toggleSide')}>
          <Ph svg={sidebarIcon} size={16} />
        </button>
      {/if}
      <h2 class="title" dir="auto">{title}</h2>
      {#if unreadCount > 0}
        <span class="unread">{$t('feeds.unreadCount').replace('{n}', String(unreadCount))}</span>
      {/if}
      <span class="spacer"></span>
      <button
        type="button"
        class="cal-icon"
        aria-label={$t('feeds.markAllRead')}
        title={$t('feeds.markAllRead')}
        disabled={rows.length === 0}
        on:click={() => dispatch('markAll')}
      >
        <Ph svg={checksIcon} size={16} />
      </button>
      <button type="button" class="cal-icon" aria-label={$t('feeds.refresh')} title={$t('feeds.refresh')} disabled={progress !== null || noFeeds} on:click={() => dispatch('refresh')}>
        {#if progress}
          <PieProgress value={progressValue} label={$t('feeds.refreshing')} />
        {:else}
          <Ph svg={syncIcon} size={16} />
        {/if}
      </button>
    </div>
    <div class="tools">
      <input
        class="cal-field filter"
        type="search"
        bind:this={searchInput}
        bind:value={typed}
        placeholder={$t('feeds.filter')}
        aria-label={$t('feeds.filter')}
        on:input={onSearchInput}
        on:keydown={(e) => {
          if (e.key === 'Escape' && typed) {
            e.stopPropagation()
            typed = ''
            dispatch('search', '')
          } else if (e.key === 'ArrowDown' || e.key === 'Enter') {
            e.preventDefault()
            void focusList()
          }
        }}
      />
      {#if canFilter}
        <div class="seg" role="radiogroup" aria-label={$t('feeds.show')}>
          <button type="button" role="radio" aria-checked={!unreadOnly} class:on={!unreadOnly} on:click={() => dispatch('unreadOnly', false)}>{$t('feeds.show.all')}</button>
          <button type="button" role="radio" aria-checked={unreadOnly} class:on={unreadOnly} on:click={() => dispatch('unreadOnly', true)}>{$t('feeds.show.unread')}</button>
        </div>
      {/if}
    </div>
    {#if progress}
      <p class="progress" aria-live="polite">{$t('feeds.progress').replace('{done}', String(progress.done)).replace('{total}', String(progress.total))}</p>
    {/if}
  </header>

  <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
  <div class="list" role="listbox" aria-label={title} tabindex="0" bind:this={listEl} on:scroll={onScroll}>
    {#if error && rows.length > 0}
      <p class="error" role="alert">{error}</p>
    {/if}
    {#if rows.length === 0 && !loading}
      {#if error}
        <ErrorState message={error} onRetry={() => dispatch('refresh')} />
      {:else if noFeeds && !newsletters}
        <EmptyState
          kind="setup"
          icon={rssIcon}
          title={$t('feeds.welcome.title')}
          detail={$t('feeds.welcome.detail')}
          actionLabel={$t('feeds.addFeed')}
          onAction={() => dispatch('add')}
          secondaryLabel={$t('feeds.importOpml')}
          onSecondary={() => dispatch('importOpml')}
        />
      {:else if search}
        <EmptyState
          kind="filtered"
          icon={searchIcon}
          title={$t('feeds.empty.search')}
          detail={$t('feeds.empty.searchDetail')}
          actionLabel={$t('common.clearSearch')}
          onAction={clearSearch}
        />
      {:else if unreadOnly}
        <EmptyState
          kind="filtered"
          icon={checksIcon}
          title={$t('feeds.empty.unread')}
          detail={$t('feeds.empty.unreadDetail')}
          actionLabel={$t('feeds.showAll')}
          onAction={() => dispatch('unreadOnly', false)}
        />
      {:else}
        <EmptyState icon={emptyIcon} title={$t('feeds.empty.title')} detail={$t(newsletters ? 'feeds.empty.newsletters' : 'feeds.empty.detail')} />
      {/if}
    {:else}
      {#each shown as row (row.key)}
        <FeedEntryRow
          {row}
          icon={iconOf.get(row.feedId) ?? ''}
          selected={row.key === selectedKey}
          thumbnail={!row.mail && (imagesOf.get(row.feedId) ?? false)}
          showSource={!singleFeed}
          {timeFormat}
          on:click={() => dispatch('select', row)}
          on:dblclick={() => dispatch('open', row)}
          on:keydown={(e) => onRowKey(e, row)}
          on:contextmenu={(e) => {
            e.preventDefault()
            dispatch('menu', { row, x: e.clientX, y: e.clientY })
          }}
          on:star={() => dispatch('star', row)}
        />
      {/each}
      {#if loading}
        <div class="loading"><PieProgress label={$t('feeds.loading')} /></div>
      {/if}
    {/if}
  </div>
</section>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    height: 100%;
    background: var(--surface-raised);
  }

  .bar {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .titles {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .title {
    margin: 0;
    min-width: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .unread {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .spacer {
    flex: 1;
  }

  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .filter {
    flex: 1 1 calc(var(--feed-thumb) * 2);
    min-width: 0;
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .seg {
    display: inline-flex;
    flex-shrink: 0;
    padding: 1px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .seg button {
    height: calc(var(--control-height) - 6px);
    padding: 0 var(--space-3);
    border: none;
    border-radius: calc(var(--radius-control) - 1px);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .seg button.on {
    background: var(--surface-raised);
    color: var(--text-primary);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
  }

  .seg button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .progress {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    position: relative;
  }

  .list:focus {
    outline: none;
  }

  .list:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .error {
    margin: var(--space-3) var(--space-4);
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-4);
    color: var(--text-tertiary);
  }
</style>
