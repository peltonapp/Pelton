<script lang="ts">
  // the Feeds space of the new design, three panes like mail: the smart views,
  // folders and feeds in a sidebar, the entries of the chosen one in the
  // middle, the open entry in a reader beside it. panes fold in a narrow
  // window: the reader first lies over the list, then the sidebar becomes a
  // drawer. opening an entry marks it read. the app's shortcuts act here the
  // way they do on mail: the star key stars, mark read and unread mark, the
  // snooze key puts an entry on the read-later list, forward shares it by mail,
  // compose adds a feed, sync refreshes. j and k (or the arrows) move through
  // the list, n and p to the next and previous unread entry, v opens the entry
  // in the browser, shift+a marks the list read. code-split like the other
  // spaces, it loads when first opened.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import '../../theme/controls.css'
  import FeedSidebar from './FeedSidebar.svelte'
  import FeedEntryList from './FeedEntryList.svelte'
  import FeedReader from './FeedReader.svelte'
  import AddFeedDialog from './AddFeedDialog.svelte'
  import FeedSettingsDialog from './FeedSettingsDialog.svelte'
  import {
    feedFocus,
    feedList,
    feedProgress,
    feedSelected,
    feedSidebar,
    loadEntries,
    loadFeedSidebar,
    loadMoreEntries,
    markAllRead,
    refreshAllFeeds,
    searchEntries,
    setRowsRead,
    showScope,
    startFeeds,
    toggleReadLater,
    toggleStar,
  } from '../../stores/feeds'
  import { getFeedEntry, getMessage, importOpml, searchFeedEntries } from '../../lib/api'
  import { entryRow, neighbour, sameScope, scopeIsUnread, scopeTitle, shareMail, type FeedRow } from '../../lib/feedlogic'
  import { focusSpaceSearch, registerSpaceSearch } from '../../lib/spacesearch'
  import { registerSpaceActions } from '../../stores/space'
  import { openContextMenu, type MenuItem } from '../../stores/contextmenu'
  import { popupIsOpen } from '../../lib/popups'
  import { openURL } from '../../lib/browser'
  import { resolveLink } from '../../lib/linkclean'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { formatListDate, type TimeFormat } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import { isEditableTarget } from '../../lib/editable'
  import type { EditorMode, FeedEntryDetail, FeedScope, MessageDetail } from '../../lib/types'

  const dispatch = createEventDispatcher<{ compose: { subject: string; body: string } }>()

  const narrowReader = 1040
  const narrowSide = 720

  let width = 0
  let sideOpen = false
  let listPane: FeedEntryList
  let adding: { folderId: number } | null = null
  let settingsFor: number | null = null

  $: foldReader = width > 0 && width < narrowReader
  $: foldSide = width > 0 && width < narrowSide
  $: if (!foldSide) {
    sideOpen = false
  }
  $: side = $feedSidebar
  $: list = $feedList
  $: scope = list.scope
  $: rows = list.rows
  $: selected = rows.find((r) => r.key === $feedSelected) ?? null
  $: title = scopeTitle(scope, side, $t)
  $: canFilter = scope.kind === 'feed' || scope.kind === 'folder' || scope.kind === 'starred' || scope.kind === 'later' || scope.kind === 'newsletters'
  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: settingsFeed = settingsFor === null ? undefined : side.feeds.find((f) => f.id === settingsFor)
  $: readerFeed = selected && !selected.mail ? side.feeds.find((f) => f.id === selected?.feedId) : undefined
  $: showReader = selected !== null && (!foldReader || readerOpen)

  // --- the reader ---------------------------------------------------------------------

  let readerOpen = false
  let entry: FeedEntryDetail | null = null
  let mail: MessageDetail | null = null
  let readerLoading = false
  let readerError = ''
  let readerRun = 0

  async function openRow(row: FeedRow, reveal = true): Promise<void> {
    feedSelected.set(row.key)
    readerOpen = true
    if (reveal) {
      void listPane?.reveal(row.key)
    }
    const run = ++readerRun
    readerLoading = true
    readerError = ''
    entry = null
    mail = null
    try {
      if (row.mail) {
        const detail = await getMessage(row.id)
        if (run === readerRun) {
          mail = detail
        }
      } else {
        const detail = await getFeedEntry(row.id)
        if (run === readerRun) {
          entry = detail
        }
      }
    } catch (err) {
      if (run === readerRun) {
        readerError = errorMessage(err)
      }
    } finally {
      if (run === readerRun) {
        readerLoading = false
      }
    }
    if (run === readerRun && !row.read) {
      await setRowsRead([row], true)
    }
  }

  function closeReader(): void {
    readerOpen = false
    void listPane?.focusList()
  }

  $: if ($feedSelected === null) {
    entry = null
    mail = null
    readerRun++
  }

  // --- scope ----------------------------------------------------------------------------

  async function setScope(next: FeedScope): Promise<void> {
    if (foldSide) {
      sideOpen = false
    }
    readerOpen = false
    await showScope(next, sameScope(next, scope) ? list.unreadOnly : false)
  }

  // --- moving through the list -------------------------------------------------------

  function step(dir: 1 | -1, unreadOnly = false): void {
    const next = neighbour(rows, $feedSelected, dir, unreadOnly)
    if (next) {
      void openRow(next)
    } else if (dir === 1 && list.more) {
      void loadMoreEntries()
    }
  }

  function current(): FeedRow | null {
    return selected
  }

  function withRow(run: (row: FeedRow) => void): () => void {
    return () => {
      const row = current()
      if (row && !popupIsOpen() && !adding && settingsFor === null) {
        run(row)
      }
    }
  }

  // --- actions --------------------------------------------------------------------------

  function openInBrowser(row: FeedRow): void {
    if (row.url) {
      openURL(resolveLink(row.url, $prefs.cleanLinks).url)
      if (!row.read) {
        void setRowsRead([row], true)
      }
    }
  }

  async function copyLink(row: FeedRow): Promise<void> {
    if (!row.url) {
      return
    }
    try {
      await navigator.clipboard.writeText(row.url)
      toastSuccess($t('feeds.linkCopied'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function share(row: FeedRow): void {
    dispatch('compose', shareMail(row.title, row.url, $prefs.defaultEditorMode as EditorMode))
  }

  function rowMenu(row: FeedRow, x: number, y: number): void {
    feedSelected.set(row.key)
    const entries: MenuItem[] = [
      { label: $t('feeds.menu.open'), action: () => void openRow(row, false) },
      { label: row.read ? $t('feeds.markUnread') : $t('feeds.markRead'), action: () => void setRowsRead([row], !row.read) },
      { label: row.starred ? $t('feeds.unstar') : $t('feeds.star'), action: () => void toggleStar(row) },
    ]
    if (!row.mail) {
      entries.push({ label: row.readLater ? $t('feeds.readLater.remove') : $t('feeds.readLater.add'), action: () => void toggleReadLater(row) })
    }
    if (row.url) {
      entries.push(
        { label: $t('feeds.openBrowser'), action: () => openInBrowser(row) },
        { label: $t('feeds.copyLink'), action: () => void copyLink(row) },
        { label: $t('feeds.share'), action: () => share(row) },
      )
    }
    openContextMenu(x, y, entries, row.mail ? undefined : { place: 'feeds.item', items: [{ id: String(row.id) }] })
  }

  async function runImport(): Promise<void> {
    try {
      const res = await importOpml()
      if (res.added > 0 || res.skipped > 0) {
        toastSuccess($t('feeds.imported').replace('{added}', String(res.added)).replace('{skipped}', String(res.skipped)))
      }
      await loadFeedSidebar()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onAdded(feedId: number): Promise<void> {
    adding = null
    await loadFeedSidebar()
    await setScope({ kind: 'feed', id: feedId })
  }

  async function onRemoved(feedId: number): Promise<void> {
    settingsFor = null
    await loadFeedSidebar()
    if (scope.kind === 'feed' && scope.id === feedId) {
      await setScope({ kind: 'unread' })
    } else {
      await loadEntries()
    }
  }

  // shows an entry wherever it is: in the list when it is there, else in its
  // feed's list.
  async function showEntry(id: number): Promise<void> {
    const here = rows.find((r) => !r.mail && r.id === id)
    if (here) {
      await openRow(here)
      return
    }
    try {
      const detail = await getFeedEntry(id)
      await showScope({ kind: 'feed', id: detail.feedId }, false)
      const row = $feedList.rows.find((r) => !r.mail && r.id === id) ?? entryRow(detail, side.feeds)
      await openRow(row)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  $: if ($feedFocus !== null) {
    const id = $feedFocus
    feedFocus.set(null)
    void showEntry(id)
  }

  // --- panes ----------------------------------------------------------------------------

  type Pane = 'sidebar' | 'list' | 'reader'

  function focusPane(pane: Pane): void {
    if (pane === 'sidebar') {
      if (foldSide) {
        sideOpen = true
      }
      void tick().then(() => document.querySelector<HTMLElement>('.feeds-screen .side .entry')?.focus())
    } else if (pane === 'list') {
      void listPane?.focusList()
    } else {
      document.querySelector<HTMLElement>('.feeds-screen [data-feed-reader]')?.focus()
    }
  }

  function paneOf(el: Element | null): Pane {
    if (el?.closest('.side')) {
      return 'sidebar'
    }
    return el?.closest('[data-feed-reader]') ? 'reader' : 'list'
  }

  function cyclePane(dir: 1 | -1): void {
    const order: Pane[] = showReader ? ['sidebar', 'list', 'reader'] : ['sidebar', 'list']
    const at = order.indexOf(paneOf(document.activeElement))
    focusPane(order[(at + dir + order.length) % order.length])
  }

  // --- keys -----------------------------------------------------------------------------

  function onKeydown(event: KeyboardEvent): void {
    if (event.metaKey || event.ctrlKey || event.altKey || popupIsOpen() || adding || settingsFor !== null) {
      return
    }
    if (event.key === 'Escape' && foldReader && readerOpen && !isEditableTarget(event.target)) {
      event.preventDefault()
      closeReader()
      return
    }
    if (isEditableTarget(event.target) || (event.target as HTMLElement).closest('.side')) {
      return
    }
    const row = current()
    const keys: Record<string, () => void> = {
      j: () => step(1),
      k: () => step(-1),
      ArrowDown: () => step(1),
      ArrowUp: () => step(-1),
      n: () => step(1, true),
      p: () => step(-1, true),
      v: () => row && openInBrowser(row),
      o: () => row && openInBrowser(row),
      A: () => void markAllRead(),
    }
    // the arrows scroll the reader while it has focus.
    if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && paneOf(event.target as Element) === 'reader') {
      return
    }
    const run = keys[event.key]
    if (run && (event.key !== 'A' || event.shiftKey)) {
      event.preventDefault()
      event.stopPropagation()
      run()
    }
  }

  // --- registration ---------------------------------------------------------------------

  let stopFeeds: (() => void) | undefined
  let unregister: (() => void) | undefined
  let stopSearch: (() => void) | undefined

  onMount(() => {
    stopFeeds = startFeeds()
    if (rows.length === 0) {
      void loadEntries()
    }
    unregister = registerSpaceActions(
      'feeds',
      {
        compose: () => {
          if (!popupIsOpen()) {
            adding = { folderId: scope.kind === 'folder' ? scope.id : 0 }
          }
        },
        search: () => {
          if (!focusSpaceSearch()) {
            listPane?.focusSearch()
          }
        },
        sync: () => void refreshAllFeeds(),
        flag: withRow((r) => void toggleStar(r)),
        'mark-read': withRow((r) => void setRowsRead([r], true)),
        'mark-unread': withRow((r) => void setRowsRead([r], !r.read)),
        snooze: withRow((r) => void toggleReadLater(r)),
        forward: withRow(share),
        'focus-sidebar': () => focusPane('sidebar'),
        'focus-list': () => focusPane('list'),
        'focus-reader': () => focusPane('reader'),
        'next-pane': () => cyclePane(1),
        'prev-pane': () => cyclePane(-1),
      },
      {
        compose: 'feeds.addFeed',
        sync: 'feeds.refresh',
        flag: 'feeds.star',
        'mark-unread': 'feeds.toggleRead',
        snooze: 'feeds.readLater.add',
        forward: 'feeds.share',
      },
    )
    stopSearch = registerSpaceSearch('feeds', {
      placeholderKey: 'feeds.searchPlaceholder',
      emptyKey: 'feeds.searchEmpty',
      search: async (query) => {
        const hits = await searchFeedEntries(query, 20)
        return hits.map((e) => ({
          id: String(e.id),
          title: e.title || $t('feeds.untitled'),
          detail: side.feeds.find((f) => f.id === e.feedId)?.title ?? '',
          aside: e.published ? formatListDate(e.published, timeFormat) : '',
        }))
      },
      open: (result) => void showEntry(Number(result.id)),
    })
  })

  onDestroy(() => {
    unregister?.()
    stopSearch?.()
    stopFeeds?.()
  })

  $: rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl'
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<section class="screen feeds-screen" aria-label={$t('space.feeds')} bind:clientWidth={width} on:keydown={onKeydown}>
  {#if !foldSide || sideOpen}
    <div class="side" class:drawer={foldSide} transition:fly={{ x: foldSide ? (rtl ? 24 : -24) : 0, duration: dur(foldSide ? motion.base : 0) }}>
      <FeedSidebar
        {scope}
        on:scope={(e) => void setScope(e.detail)}
        on:add={(e) => (adding = { folderId: e.detail })}
        on:settings={(e) => (settingsFor = e.detail)}
        on:markRead={(e) => void showScope(e.detail, false).then(markAllRead)}
      />
    </div>
  {/if}
  {#if foldSide && sideOpen}
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="scrim" transition:fade={{ duration: dur(motion.fast) }} on:click={() => (sideOpen = false)}></div>
  {/if}

  <div class="list-pane" class:full={foldReader}>
    <FeedEntryList
      bind:this={listPane}
      {rows}
      selectedKey={$feedSelected}
      {title}
      {canFilter}
      unreadOnly={list.unreadOnly && !scopeIsUnread(scope)}
      search={list.search}
      loading={list.loading}
      more={list.more}
      error={list.error}
      feeds={side.feeds}
      loadImages={$prefs.feedsLoadImages}
      singleFeed={scope.kind === 'feed'}
      noFeeds={side.feeds.length === 0}
      newsletters={scope.kind === 'newsletters'}
      progress={$feedProgress}
      markOnScroll={$prefs.feedsMarkReadOnScroll}
      sideToggle={foldSide}
      {sideOpen}
      {timeFormat}
      on:select={(e) => void openRow(e.detail, false)}
      on:open={(e) => void openRow(e.detail, false).then(() => focusPane('reader'))}
      on:star={(e) => void toggleStar(e.detail)}
      on:menu={(e) => rowMenu(e.detail.row, e.detail.x, e.detail.y)}
      on:unreadOnly={(e) => void showScope(scope, e.detail)}
      on:search={(e) => void searchEntries(e.detail)}
      on:markAll={() => void markAllRead()}
      on:refresh={() => void refreshAllFeeds()}
      on:loadMore={() => void loadMoreEntries()}
      on:passed={(e) => void setRowsRead(e.detail, true)}
      on:toggleSide={() => (sideOpen = !sideOpen)}
      on:add={() => (adding = { folderId: 0 })}
      on:importOpml={() => void runImport()}
    />
  </div>

  {#if !foldReader || (showReader && selected)}
    <div class="reader-pane" class:overlay={foldReader} transition:fly={{ x: foldReader ? (rtl ? -24 : 24) : 0, duration: dur(foldReader ? motion.base : 0) }}>
      <FeedReader
        row={selected}
        {entry}
        {mail}
        feed={readerFeed}
        loading={readerLoading}
        error={readerError}
        closable={foldReader}
        on:star={() => selected && void toggleStar(selected)}
        on:later={() => selected && void toggleReadLater(selected)}
        on:toggleRead={() => selected && void setRowsRead([selected], !selected.read)}
        on:openBrowser={() => selected && openInBrowser(selected)}
        on:copyLink={() => selected && void copyLink(selected)}
        on:share={() => selected && share(selected)}
        on:close={closeReader}
        on:showFeed={(e) => void setScope({ kind: 'feed', id: e.detail })}
      />
    </div>
  {/if}
</section>

{#if adding}
  <AddFeedDialog folders={side.folders} folderId={adding.folderId} on:close={() => (adding = null)} on:added={(e) => void onAdded(e.detail.id)} />
{/if}

{#if settingsFeed}
  <FeedSettingsDialog
    feed={settingsFeed}
    folders={side.folders}
    on:close={() => (settingsFor = null)}
    on:saved={() => {
      settingsFor = null
      void loadFeedSidebar()
    }}
    on:removed={(e) => void onRemoved(e.detail)}
  />
{/if}

<style>
  .screen {
    position: absolute;
    inset: 0;
    display: flex;
    min-width: 0;
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: var(--font-ui);
    overflow: hidden;
  }

  .side {
    flex-shrink: 0;
    height: 100%;
    background: var(--surface-base);
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .side.drawer {
    position: absolute;
    inset-block: 0;
    inset-inline-start: 0;
    z-index: 20;
    box-shadow: var(--shadow-overlay);
  }

  .scrim {
    position: absolute;
    inset: 0;
    z-index: 19;
    background: var(--scrim);
  }

  .list-pane {
    flex-shrink: 0;
    width: var(--feed-list-width);
    max-width: 50%;
    height: 100%;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .list-pane.full {
    flex: 1;
    width: auto;
    max-width: none;
    border-inline-end: none;
  }

  .reader-pane {
    display: flex;
    flex: 1;
    min-width: 0;
    height: 100%;
  }

  .reader-pane.overlay {
    position: absolute;
    inset-block: 0;
    inset-inline-end: 0;
    z-index: 18;
    width: min(100%, var(--reader-width-medium));
    box-shadow: var(--shadow-overlay);
    border-inline-start: var(--hairline) solid var(--border-default);
  }
</style>
