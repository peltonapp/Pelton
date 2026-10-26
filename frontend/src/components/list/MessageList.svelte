<script lang="ts">
  // column 2: the message list. it loads the page for the current selection,
  // runs fts5 search, and owns keyboard navigation (up/down to move, enter to
  // open). opening a message marks it seen optimistically. when multi-select is
  // enabled, cmd/ctrl-click toggles rows and shift-click selects a range, with a
  // selection toolbar for bulk mark/flag/delete. loading, empty and error states
  // are all explicit so the pane is never blank.
  import { tick, onMount } from 'svelte'
  import MessageRow from './MessageRow.svelte'
  import SearchBar from './SearchBar.svelte'
  import Spinner from '../common/Spinner.svelte'
  import MessageSkeleton from './MessageSkeleton.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ErrorState from '../common/ErrorState.svelte'
  import {
    IconMail,
    IconArrowBackUp,
    IconArrowForwardUp,
    IconMailOpened,
    IconMailFilled,
    IconFlag,
    IconFlagFilled,
    IconTrash,
    IconX,
    IconSquare,
    IconSquareCheck,
    IconSquareMinus,
    IconClockPause,
    IconDownload,
    IconDownloadOff,
    IconFolderSymlink,
    IconStar,
    IconStarFilled,
    IconLayoutColumns,
    IconArchive,
  } from '@tabler/icons-svelte'
  import { selection, searchQuery, openMessageId, openMessage } from '../../stores/selection'
  import {
    messageList,
    loadList,
    loadMore,
    loadOlder,
    backfillFailed,
    runSearch,
    patchInList,
    removeFromList,
    restoreToList,
    emptyFilter,
    filterActive,
    searchSortKind,
    type SearchFilter,
  } from '../../stores/messages'
  import {
    selectedIds,
    clearSelection,
    anchorAt,
    selectOnly,
    toggleSelect,
    selectRange,
  } from '../../stores/listselect'
  import {
    expandOffer,
    expanding,
    selectAllInList,
    expandSelection,
    clearExpandOffer,
    clearAll,
  } from '../../stores/selectall'
  import { prefs, setSearchSort } from '../../stores/prefs'
  import {
    setSeen,
    getMessage,
    removeOffline,
    archiveMessage,
  } from '../../lib/api'
  import { openReply, openForward } from '../../stores/compose'
  import { openSnooze, openSnoozeMany } from '../../stores/snooze'
  import { openMove, openMoveMany } from '../../stores/move'
  import { recordDeleted } from '../../stores/undodelete'
  import { recordArchived } from '../../stores/undoarchive'
  import { openContextMenu, type MenuEntry } from '../../stores/contextmenu'
  import { menuHint, shortcutTitle } from '../../stores/shortcuts'
  import { openInTab } from '../../stores/tabs'
  import { errorMessage, toastError } from '../../stores/toast'
  import { isVIPAddress } from '../../stores/vip'
  import type { Selection, MessageSummary, SwipeAction, EditorMode, SearchSortPref } from '../../lib/types'
  import { t } from '../../lib/i18n'
  import {
    markSeen,
    markFlagged,
    markColor,
    toggleSenderVIP,
    setOffline,
    trashMessage,
    bulkMarkSeen,
    bulkMarkFlagged,
    bulkMarkColor,
    bulkSetOffline,
    bulkArchive,
    bulkTrash,
    reportArchiveExport,
  } from '../../lib/messageactions'

  // the meta-bar title. built-in unified views are localized by key (matching the
  // sidebar), so it stays correct after a language switch; folders and saved
  // views keep their own stored name.
  function viewTitle(sel: Selection): string {
    if (sel.kind !== 'view') {
      return sel.label
    }
    const key = sel.view === 'inbox' ? 'sidebar.unifiedInbox' : `sidebar.view.${sel.view}`
    const translated = $t(key)
    return translated === key ? sel.label : translated
  }

  let listEl: HTMLDivElement
  let activeIndex = -1

  // virtualization. the list can hold thousands of rows, so we only render the
  // window around the viewport plus a little overscan, padding the rest with
  // spacers so the scrollbar stays accurate. all rows share a template, so a
  // single measured row height is enough; we fall back to a per-template estimate
  // until the first real row is measured.
  const OVERSCAN = 8
  const ROW_ESTIMATES: Record<string, number> = {
    relaxed: 76,
    comfortable: 62,
    compact: 50,
    single: 34,
  }
  let scrollTop = 0
  let viewportHeight = 600
  let rowHeight = 0
  $: estRowHeight = rowHeight || ROW_ESTIMATES[$prefs.rowTemplate] || 64

  // re-measure when anything that changes a row's height changes.
  $: rowMetricKey = `${$prefs.rowTemplate}|${$prefs.density}|${$prefs.previewLines}|${$prefs.rowShowSnippet}|${$prefs.rowShowAvatar}|${$prefs.showDateTime}`
  let lastMetricKey = ''
  $: if (rowMetricKey !== lastMetricKey) {
    lastMetricKey = rowMetricKey
    rowHeight = 0
    void measureRow()
  }

  // measureRow reads the first rendered row's height so the window math matches
  // the real layout.
  async function measureRow(): Promise<void> {
    await tick()
    const node = listEl?.querySelector('[role="option"]')
    if (node instanceof HTMLElement && node.offsetHeight > 0) {
      rowHeight = node.offsetHeight
    }
  }

  onMount(() => {
    if (listEl) {
      viewportHeight = listEl.clientHeight || viewportHeight
    }
  })

  // selectionKey identifies a selection so we reload only when it actually
  // changes, not on unrelated store updates.
  function selectionKey(sel: Selection): string {
    if (sel.kind === 'view') {
      return `view:${sel.view}`
    }
    if (sel.kind === 'savedView') {
      return `savedView:${sel.viewId}`
    }
    return `folder:${sel.folderId}`
  }

  // resetScroll puts the list back at the top. the container outlives the rows
  // it holds, so without this a change of content keeps the old offset and a
  // short result set renders far above the viewport, looking like no results.
  function resetScroll(): void {
    scrollTop = 0
    if (listEl) {
      listEl.scrollTop = 0
    }
  }

  let lastKey = ''
  $: if ($selection && selectionKey($selection) !== lastKey) {
    lastKey = selectionKey($selection)
    activeIndex = -1
    clearSelection()
    resetScroll()
    void loadList($selection)
  }

  $: items = $messageList.data?.items ?? []
  // a result set pages too: a broad query matches far more than one page, and
  // stopping at the first page is what made a match ranked below it look like
  // no match at all.
  $: searching = $messageList.data?.searching ?? false
  $: hasMore = searching
    ? !($messageList.data?.exhausted ?? false) &&
      ($messageList.data?.loadedHits ?? items.length) < ($messageList.data?.total ?? 0)
    : items.length < ($messageList.data?.total ?? 0)
  // the cache is exhausted but the server still has older mail. hasMore covers
  // paging what is cached; this covers going back to the server for the rest.
  $: backfilling = $messageList.data?.backfilling ?? false
  $: canLoadOlder = !hasMore && ($messageList.data?.hasOlder ?? false) && !($messageList.data?.searching ?? false)
  // the button shows when auto-backfill is off, or when an automatic attempt
  // failed and retrying is the user's call.
  $: showLoadOlder = canLoadOlder && !backfilling && (!$prefs.syncAutoBackfill || $backfillFailed)

  // measure once rows first appear and the height is still unknown.
  $: if (items.length > 0 && rowHeight === 0) {
    void measureRow()
  }

  // the rendered window: a contiguous slice around the viewport, with spacer
  // heights above and below so the scroll range stays correct.
  $: startIndex = Math.max(0, Math.floor(scrollTop / estRowHeight) - OVERSCAN)
  $: visibleCount = Math.ceil(viewportHeight / estRowHeight) + OVERSCAN * 2
  $: endIndex = Math.min(items.length, startIndex + visibleCount)
  $: windowItems = items.slice(startIndex, endIndex)
  $: topPad = startIndex * estRowHeight
  $: bottomPad = Math.max(0, (items.length - endIndex) * estRowHeight)

  // the live multi-selection, intersected with what is still loaded so bulk
  // actions never act on rows that have scrolled out of the data.
  $: selectedItems = items.filter((m) => $selectedIds.has(m.id))

  // select-all is not offered in the unified views unless it is asked for: one
  // of those lists spans every account, so "everything in this list" is a much
  // bigger claim there than in a single mailbox. A search result set counts as
  // a search wherever it was run from, and a saved view is a stored search.
  $: selectAllAvailable =
    $prefs.multiSelectEnabled &&
    (($messageList.data?.searching ?? false) ||
      $selection.kind !== 'view' ||
      $prefs.selectAllUnified)

  // the header checkbox is ticked only when every loaded row is in the
  // selection, and dashed for anything in between.
  $: allLoadedSelected = items.length > 0 && items.every((m) => $selectedIds.has(m.id))

  // clicking it clears a full selection and otherwise selects, which is what a
  // checkbox in that state looks like it will do.
  async function onSelectAllClick(): Promise<void> {
    if (allLoadedSelected && selectionCount > 0) {
      clearAll()
      return
    }
    await selectAllInList(items.map((m) => m.id))
  }
  $: selectionCount = selectedItems.length

  // keep the keyboard-nav highlight in sync with whichever message is open,
  // however it got opened (click, Enter, or the in-app vim motions), so a
  // stale arrow-key position never leaves two rows looking selected at once.
  $: if ($openMessageId !== null) {
    // -1 is a real result: changing from search results back to the mailbox can
    // leave the open message outside the new list, so its old numeric position
    // must not highlight whichever row replaced it.
    activeIndex = items.findIndex((m) => m.id === $openMessageId)
  }

  // search handling. the list shows ranked results when there is a query or an
  // active date filter, and the selection's normal list otherwise.
  let searchFilter: SearchFilter = emptyFilter

  function applySearch(query: string, filter: SearchFilter): void {
    activeIndex = -1
    clearSelection()
    resetScroll()
    if (query === '' && !filterActive(filter)) {
      void loadList($selection)
    } else {
      void runSearch(query, filter)
    }
  }

  function onSearch(event: CustomEvent<string>): void {
    searchQuery.set(event.detail)
    applySearch(event.detail.trim(), searchFilter)
  }

  function onFilter(event: CustomEvent<SearchFilter>): void {
    searchFilter = event.detail
    applySearch($searchQuery.trim(), searchFilter)
  }

  // a sort picked from the search bar is remembered for this kind of search and
  // the results are read again in the new order (#404). It goes through
  // applySearch so the rows reordering also resets the scroll and drops a
  // selection that would otherwise point at whatever moved into those rows.
  function onSort(event: CustomEvent<SearchSortPref>): void {
    if (!$searchSortKind) {
      return
    }
    setSearchSort($searchSortKind, event.detail)
    applySearch($searchQuery.trim(), searchFilter)
  }

  // open marks the message seen if needed and shows it in the detail pane. a
  // plain open also clears any multi-selection.
  async function open(index: number): Promise<void> {
    const item = items[index]
    if (!item) {
      return
    }
    activeIndex = index
    clearSelection()
    // the row that was opened is where a following shift-click measures from.
    anchorAt(item.id)
    openMessage(item.id)
    if (!item.seen) {
      patchInList(item.id, { seen: true })
      try {
        await setSeen(item.id, true)
      } catch (err) {
        toastError(errorMessage(err))
      }
    }
  }

  // onRowClick routes a click to either multi-selection (cmd/ctrl or shift, when
  // enabled) or opening the message.
  function onRowClick(event: MouseEvent, index: number): void {
    const item = items[index]
    if (!item) {
      return
    }
    if ($prefs.multiSelectEnabled && (event.metaKey || event.ctrlKey)) {
      toggleSelect(item.id)
      activeIndex = index
      return
    }
    if ($prefs.multiSelectEnabled && event.shiftKey) {
      selectRange(items.map((m) => m.id), item.id)
      activeIndex = index
      return
    }
    void open(index)
  }

  // keyboard navigation over the rows.
  async function onKeydown(event: KeyboardEvent): Promise<void> {
    if (event.key === 'Escape' && selectionCount > 0) {
      clearAll()
      return
    }
    if (items.length === 0) {
      return
    }
    // cmd/ctrl+a while the list has focus. What it reaches is a preference:
    // the loaded rows with the rest offered, the whole list, or only what is
    // loaded (#320).
    if (selectAllAvailable && (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'a') {
      event.preventDefault()
      await selectAllInList(items.map((m) => m.id))
      return
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      activeIndex = Math.min(activeIndex + 1, items.length - 1)
      await scrollActiveIntoView()
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      activeIndex = Math.max(activeIndex - 1, 0)
      await scrollActiveIntoView()
    } else if (event.key === 'Enter' && activeIndex >= 0) {
      event.preventDefault()
      await open(activeIndex)
    }
  }

  // scrollActiveIntoView keeps the highlighted row visible during arrow nav.
  // because rows are virtualized, it scrolls by computed offset rather than
  // asking a (possibly unrendered) node to scroll itself into view.
  async function scrollActiveIntoView(): Promise<void> {
    if (!listEl || activeIndex < 0) {
      return
    }
    const top = activeIndex * estRowHeight
    const bottom = top + estRowHeight
    if (top < listEl.scrollTop) {
      listEl.scrollTop = top
    } else if (bottom > listEl.scrollTop + listEl.clientHeight) {
      listEl.scrollTop = bottom - listEl.clientHeight
    }
    scrollTop = listEl.scrollTop
    await tick()
  }

  // toggleSeen / toggleFlag / remove act on a single row from the context menu,
  // updating the list optimistically and surfacing any backend error.
  async function toggleSeen(item: MessageSummary): Promise<void> {
    await markSeen(item, !item.seen)
  }

  async function toggleFlag(item: MessageSummary): Promise<void> {
    await markFlagged(item, !item.flagged)
  }

  // toggleVIP stars or unstars a row's sender from the context menu; the vip
  // store drives the live star on every row from that sender.
  async function toggleVIP(item: MessageSummary): Promise<void> {
    await toggleSenderVIP(item)
  }

  async function remove(item: MessageSummary): Promise<void> {
    await trashMessage(item)
  }

  // bulk actions operate on the whole multi-selection, then clear it.
  async function bulkSetSeen(seen: boolean): Promise<void> {
    await bulkMarkSeen(selectedItems, seen)
  }

  async function bulkSetFlagged(flagged: boolean): Promise<void> {
    await bulkMarkFlagged(selectedItems, flagged)
  }

  async function bulkDelete(): Promise<void> {
    await bulkTrash(selectedItems)
  }

  async function bulkSetColor(color: number): Promise<void> {
    await bulkMarkColor(selectedItems, color)
  }

  async function bulkSetOfflineCopies(offline: boolean): Promise<void> {
    await bulkSetOffline(selectedItems, offline)
  }

  async function bulkArchiveSelection(): Promise<void> {
    await bulkArchive(selectedItems)
  }

  // the move and snooze dialogs take the whole selection, so the folder or the
  // time is chosen once rather than once per message. Both clear the selection
  // here: the rows are on their way out of this list either way.
  function bulkMove(): void {
    const items = selectedItems
    clearSelection()
    openMoveMany(items)
  }

  function bulkSnooze(): void {
    const ids = selectedItems.map((m) => m.id)
    clearSelection()
    openSnoozeMany(ids)
  }

  // reply/forward need the full message (for quoting), so load it first.
  $: editorMode = $prefs.defaultEditorMode as EditorMode

  async function replyTo(item: MessageSummary, all: boolean): Promise<void> {
    try {
      const detail = await getMessage(item.id)
      openReply(detail, editorMode, all)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function forward(item: MessageSummary): Promise<void> {
    try {
      const detail = await getMessage(item.id)
      openForward(detail, editorMode)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // setColor applies a flag color to a row (0 clears), optimistically.
  async function setColor(item: MessageSummary, color: number): Promise<void> {
    await markColor(item, color)
  }

  // archive moves a row to the account's Archive folder, removing it from the
  // current view optimistically.
  async function archive(item: MessageSummary): Promise<void> {
    removeFromList(item.id)
    if ($openMessageId === item.id) {
      openMessageId.set(null)
    }
    try {
      const undo = await archiveMessage(item.id)
      reportArchiveExport(undo)
      recordArchived(item, undo)
    } catch (err) {
      toastError(errorMessage(err))
      // the move failed; bring the row back so nothing is silently lost.
      restoreToList(item)
    }
  }

  // toggleOffline pins or unpins a row for offline availability.
  async function toggleOffline(item: MessageSummary): Promise<void> {
    await setOffline(item, !item.offline)
  }

  // performSwipe runs the configured action for a swipe direction on a row.
  function performSwipe(item: MessageSummary, dir: 'left' | 'right'): void {
    const action = (dir === 'left' ? $prefs.swipeLeftAction : $prefs.swipeRightAction) as SwipeAction
    switch (action) {
      case 'delete':
        void remove(item)
        break
      case 'read':
      case 'unread':
        void toggleSeen(item)
        break
      case 'flag':
        void toggleFlag(item)
        break
      case 'archive':
        void archive(item)
        break
      case 'snooze':
        openSnooze(item.id, item.subject)
        break
      case 'none':
      default:
        break
    }
  }

  // onContext builds and opens the right-click menu. when the row is part of a
  // multi-selection of more than one, the menu offers bulk actions instead.
  function onContext(event: MouseEvent, item: MessageSummary): void {
    event.preventDefault()
    if (selectionCount > 1 && $selectedIds.has(item.id)) {
      const n = String(selectionCount)
      const anyUnread = selectedItems.some((m) => !m.seen)
      const anyUnflagged = selectedItems.some((m) => !m.flagged)
      const anyOnline = selectedItems.some((m) => !m.offline)
      const entries: MenuEntry[] = [
        anyUnread
          ? { label: $t('messageList.bulk.markReadCount').replace('{n}', n), icon: IconMailOpened, action: () => void bulkSetSeen(true) }
          : { label: $t('messageList.bulk.markUnreadCount').replace('{n}', n), icon: IconMailFilled, action: () => void bulkSetSeen(false) },
        anyUnflagged
          ? { label: $t('messageList.bulk.flagCount').replace('{n}', n), icon: IconFlagFilled, action: () => void bulkSetFlagged(true) }
          : { label: $t('messageList.bulk.unflagCount').replace('{n}', n), icon: IconFlag, action: () => void bulkSetFlagged(false) },
        // the colour row has no count of its own; it sits under the labels above
        // and applies to the same selection they name.
        { kind: 'colors', current: 0, onPick: (color) => void bulkSetColor(color) },
        'separator',
        { label: $t('messageList.bulk.snoozeCount').replace('{n}', n), icon: IconClockPause, hint: menuHint('snooze'), action: bulkSnooze },
        { label: $t('messageList.bulk.moveCount').replace('{n}', n), icon: IconFolderSymlink, hint: menuHint('move-to'), action: bulkMove },
        { label: $t('messageList.bulk.archiveCount').replace('{n}', n), icon: IconArchive, hint: menuHint('archive'), action: () => void bulkArchiveSelection() },
        anyOnline
          ? { label: $t('messageList.bulk.downloadCount').replace('{n}', n), icon: IconDownload, action: () => void bulkSetOfflineCopies(true) }
          : { label: $t('messageList.bulk.removeOfflineCount').replace('{n}', n), icon: IconDownloadOff, action: () => void bulkSetOfflineCopies(false) },
        'separator',
        { label: $t('messageList.bulk.deleteCount').replace('{n}', n), icon: IconTrash, danger: true, hint: menuHint('delete-message'), action: () => void bulkDelete() },
      ]
      openContextMenu(event.clientX, event.clientY, entries)
      return
    }
    // right clicking outside the selection is a fresh start: the row under the
    // pointer becomes the selection and the menu is about that message, which is
    // what every other app does and what stops an action landing on a set the
    // user had forgotten about.
    if (selectionCount > 0 && !$selectedIds.has(item.id)) {
      selectOnly(item.id)
    }
    const entries: MenuEntry[] = [
      { label: $t('messageList.menu.open'), icon: IconMail, action: () => open(items.indexOf(item)) },
      { label: $t('messageList.menu.openInTab'), icon: IconLayoutColumns, action: () => openInTab(item.id, item.subject) },
      { label: $t('action.reply'), icon: IconArrowBackUp, hint: menuHint('reply'), action: () => void replyTo(item, false) },
      { label: $t('shortcut.replyAll'), icon: IconArrowBackUp, hint: menuHint('reply-all'), action: () => void replyTo(item, true) },
      { label: $t('action.forward'), icon: IconArrowForwardUp, hint: menuHint('forward'), action: () => void forward(item) },
      'separator',
      item.seen
        ? { label: $t('shortcut.markUnread'), icon: IconMailFilled, hint: menuHint('mark-unread'), action: () => void toggleSeen(item) }
        : { label: $t('shortcut.markRead'), icon: IconMailOpened, hint: menuHint('mark-read'), action: () => void toggleSeen(item) },
      item.flagged
        ? { label: $t('messageList.unflag'), icon: IconFlag, hint: menuHint('flag'), action: () => void toggleFlag(item) }
        : { label: $t('messageList.flag'), icon: IconFlagFilled, hint: menuHint('flag'), action: () => void toggleFlag(item) },
      { kind: 'colors', current: item.flagColor, onPick: (color) => void setColor(item, color) },
      isVIPAddress(item.fromAddress)
        ? { label: $t('vip.unmark'), icon: IconStar, action: () => void toggleVIP(item) }
        : { label: $t('vip.mark'), icon: IconStarFilled, action: () => void toggleVIP(item) },
      'separator',
      { label: $t('messageList.menu.snooze'), icon: IconClockPause, hint: menuHint('snooze'), action: () => openSnooze(item.id, item.subject) },
      { label: $t('messageList.menu.moveTo'), icon: IconFolderSymlink, hint: menuHint('move-to'), action: () => openMove(item) },
      item.offline
        ? { label: $t('messageList.menu.removeOffline'), icon: IconDownloadOff, hint: menuHint('remove-offline'), action: () => void toggleOffline(item) }
        : { label: $t('shortcut.downloadOffline'), icon: IconDownload, hint: menuHint('download-offline'), action: () => void toggleOffline(item) },
      'separator',
      { label: $t('action.delete'), icon: IconTrash, danger: true, hint: menuHint('delete-message'), action: () => void remove(item) },
    ]
    openContextMenu(event.clientX, event.clientY, entries)
  }

  // middle-click opens a message in a tab, the way it opens a link in a browser.
  // auxclick rather than mousedown, so the platform's autoscroll never starts.
  function onRowAux(event: MouseEvent, item: MessageSummary): void {
    if (event.button !== 1) {
      return
    }
    event.preventDefault()
    openInTab(item.id, item.subject)
  }

  // onScroll updates the virtualization window and pages in more rows near the
  // bottom.
  function onScroll(): void {
    scrollTop = listEl.scrollTop
    viewportHeight = listEl.clientHeight
    if ($messageList.status === 'loading' || backfilling) {
      return
    }
    // once the cache is paged out, loadMore hands off to the server backfill,
    // so this still fires when hasMore is false and older mail remains. a
    // failed backfill stops the automatic retry and waits for the button.
    if (!hasMore && !(canLoadOlder && $prefs.syncAutoBackfill && !$backfillFailed)) {
      return
    }
    const nearBottom = listEl.scrollTop + listEl.clientHeight >= listEl.scrollHeight - 200
    if (nearBottom) {
      void loadMore()
    }
  }

  // keep the viewport height current when the window resizes.
  function onResize(): void {
    if (listEl) {
      viewportHeight = listEl.clientHeight
    }
  }
</script>

<svelte:window on:resize={onResize} />

<section class="list-col">
  <div class="header">
    <SearchBar value={$searchQuery} on:search={onSearch} on:filter={onFilter} on:sort={onSort} />
  </div>

  {#if selectionCount > 0}
    <div class="select-bar">
      {#if selectAllAvailable}
        <button
          type="button"
          class="select-all"
          role="checkbox"
          aria-checked={allLoadedSelected ? 'true' : 'mixed'}
          aria-label={$t('list.selectAll.label')}
          title={$t('list.selectAll.label')}
          on:click={onSelectAllClick}
        >
          {#if allLoadedSelected}
            <IconSquareCheck size={16} stroke={1.7} />
          {:else}
            <IconSquareMinus size={16} stroke={1.7} />
          {/if}
        </button>
      {/if}
      <button type="button" class="clear" aria-label={$t('messageList.clearSelection')} on:click={clearAll}>
        <IconX size={15} stroke={1.9} />
      </button>
      {#if $prefs.showSelectedCount}
        <span class="sel-count">{selectionCount} {$t('messageList.selectedSuffix')}</span>
      {/if}
      <span class="sel-spacer"></span>
      {#if selectedItems.some((m) => !m.seen)}
        <button type="button" class="act" title={$shortcutTitle($t('shortcut.markRead'), 'mark-read')} on:click={() => bulkSetSeen(true)}>
          <IconMailOpened size={16} stroke={1.7} />
        </button>
      {:else}
        <button type="button" class="act" title={$shortcutTitle($t('shortcut.markUnread'), 'mark-unread')} on:click={() => bulkSetSeen(false)}>
          <IconMailFilled size={16} stroke={1.7} />
        </button>
      {/if}
      {#if selectedItems.some((m) => !m.flagged)}
        <button type="button" class="act" title={$shortcutTitle($t('messageList.flag'), 'flag')} on:click={() => bulkSetFlagged(true)}>
          <IconFlagFilled size={16} stroke={1.7} />
        </button>
      {:else}
        <button type="button" class="act" title={$shortcutTitle($t('messageList.unflag'), 'flag')} on:click={() => bulkSetFlagged(false)}>
          <IconFlag size={16} stroke={1.7} />
        </button>
      {/if}
      <button type="button" class="act" title={$shortcutTitle($t('action.archive'), 'archive')} on:click={bulkArchiveSelection}>
        <IconArchive size={16} stroke={1.7} />
      </button>
      <button type="button" class="act" title={$shortcutTitle($t('messageList.menu.moveTo'), 'move-to')} on:click={bulkMove}>
        <IconFolderSymlink size={16} stroke={1.7} />
      </button>
      <button type="button" class="act danger" title={$shortcutTitle($t('action.delete'), 'delete-message')} on:click={bulkDelete}>
        <IconTrash size={16} stroke={1.7} />
      </button>
    </div>
  {:else}
    <div class="meta-bar">
      {#if selectAllAvailable && items.length > 0}
        <button
          type="button"
          class="select-all"
          role="checkbox"
          aria-checked="false"
          aria-label={$t('list.selectAll.label')}
          title={$t('list.selectAll.label')}
          on:click={onSelectAllClick}
        >
          <IconSquare size={16} stroke={1.7} />
        </button>
      {/if}
      <span class="title">{viewTitle($selection)}</span>
      {#if $messageList.data}
        <span class="count">
          {#if $messageList.data.searching}
            <!-- showing the match count, not just the loaded count, is what
                 tells "that is everything" apart from "there is more below". -->
            {#if items.length < $messageList.data.total}
              {$t('messageList.resultsOf')
                .replace('{loaded}', String(items.length))
                .replace('{total}', String($messageList.data.total))}
            {:else}
              {items.length} {items.length === 1 ? $t('messageList.result') : $t('messageList.results')}
            {/if}
          {:else}
            {$messageList.data.total} {$messageList.data.total === 1 ? $t('messageList.message') : $t('messageList.messages')}
          {/if}
        </span>
      {/if}
    </div>
  {/if}

  <!-- selecting the loaded rows is not the same as selecting the mailbox, so
       the difference is offered rather than assumed (#320). -->
  {#if $expandOffer}
    <div class="expand-offer">
      <span>
        {$t('list.selectAll.loadedSelected').replace('{n}', $expandOffer.loaded.toLocaleString())}
      </span>
      <button type="button" class="expand-btn" disabled={$expanding} on:click={() => void expandSelection()}>
        {$expanding
          ? $t('list.selectAll.expanding')
          : $t('list.selectAll.expand').replace('{n}', $expandOffer.matching.toLocaleString())}
      </button>
    </div>
  {/if}

  <div
    class="rows"
    role="listbox"
    tabindex="0"
    aria-label={$t('messageList.ariaLabel')}
    aria-multiselectable={$prefs.multiSelectEnabled}
    aria-activedescendant={activeIndex >= 0 ? `msg-${items[activeIndex]?.id}` : undefined}
    bind:this={listEl}
    on:keydown={onKeydown}
    on:scroll={onScroll}
  >
    {#if $messageList.status === 'loading' && items.length === 0}
      <!-- placeholders rather than a spinner, and only once the load has taken
           long enough to be felt. -->
      <MessageSkeleton active rows={10} />
    {:else if $messageList.status === 'error'}
      <ErrorState message={$messageList.error} onRetry={() => loadList($selection)} />
    {:else if items.length === 0}
      <EmptyState
        title={$searchQuery ? $t('messageList.empty.noMatch') : $t('messageList.empty.noMessages')}
        detail={$searchQuery ? $t('messageList.empty.tryDifferentSearch') : $t('messageList.empty.viewEmpty')}
      >
        <IconMail size={28} stroke={1.4} />
      </EmptyState>
    {:else}
      {#if topPad > 0}
        <div class="spacer" style={`height:${topPad}px`} aria-hidden="true"></div>
      {/if}
      {#each windowItems as item, i (item.id)}
        {@const index = startIndex + i}
        <div id={`msg-${item.id}`}>
          <MessageRow
            message={item}
            selected={item.id === $openMessageId || index === activeIndex}
            checked={$selectedIds.has(item.id)}
            on:click={(e) => onRowClick(e, index)}
            on:auxclick={(e) => onRowAux(e, item)}
            on:contextmenu={(e) => onContext(e, item)}
            on:swipe={(e) => performSwipe(item, e.detail)}
          />
        </div>
      {/each}
      {#if bottomPad > 0}
        <div class="spacer" style={`height:${bottomPad}px`} aria-hidden="true"></div>
      {/if}
      {#if $messageList.status === 'loading'}
        <MessageSkeleton active rows={3} inline />
      {:else if backfilling}
        <Spinner label={$t('messageList.fetchingOlder')} inline />
      {:else if showLoadOlder}
        <div class="load-older">
          <p class="load-older-note">
            {$backfillFailed ? $t('messageList.olderFailed') : $t('messageList.olderOnServer')}
          </p>
          <button type="button" class="load-older-btn" on:click={() => void loadOlder()}>
            {$t('messageList.loadOlder')}
          </button>
        </div>
      {/if}
    {/if}
  </div>
</section>

<style>
  .list-col {
    display: grid;
    grid-template-rows: auto auto 1fr;
    height: 100%;
    background: var(--surface-raised);
    border-inline-end: var(--hairline) solid var(--border-default);
    min-width: 0;
  }

  .header {
    /* a grid item's automatic minimum is its min-content, so without this the
       search bar's contents can widen the row past the column. */
    min-width: 0;
    padding: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .meta-bar {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) var(--row-pad-x);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    flex-shrink: 0;
  }

  /* the end of the cache when the mailbox itself is not over: older mail is
     still on the server and one press fetches the next batch. */
  .load-older {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-6) var(--row-pad-x);
  }

  .load-older-note {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    text-align: center;
  }

  .load-older-btn {
    padding: var(--space-2) var(--space-4);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-secondary);
    background: var(--surface-sunken);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .load-older-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  /* the selection toolbar replaces the meta bar while rows are selected. */
  .select-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--selection-bg);
  }

  .sel-count {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .sel-spacer {
    flex: 1;
  }

  .select-all,
  .clear,
  .act {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    padding: var(--space-2);
    border-radius: var(--radius-control);
  }

  .select-all:hover,
  .clear:hover,
  .act:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  /* in the quiet bar the checkbox sits with the title, so it takes the tertiary
     colour rather than announcing itself above an untouched list. */
  .meta-bar .select-all {
    padding: 0 var(--space-2) 0 0;
    color: var(--text-tertiary);
  }

  .expand-offer {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--row-pad-x);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    background: var(--selection-bg);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .expand-btn {
    padding: 0;
    font: inherit;
    color: var(--accent);
    background: transparent;
    border: none;
    cursor: var(--cursor-action);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .expand-btn:disabled {
    color: var(--text-tertiary);
    text-decoration: none;
    cursor: default;
  }

  .act.danger:hover {
    color: var(--danger);
  }

  .rows {
    /* min-height:0 lets this 1fr grid track shrink below its content so it
       actually scrolls instead of growing the column past the viewport. */
    min-height: 0;
    overflow-y: auto;
    outline: none;
  }

  /* virtualization spacers stand in for the rows outside the rendered window. */
  .spacer {
    flex-shrink: 0;
  }
</style>
