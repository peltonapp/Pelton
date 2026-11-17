<script lang="ts">
  // column 2: the message list. it loads the page for the current selection,
  // runs fts5 search, and owns keyboard navigation (up/down to move, enter to
  // open). opening a message marks it seen optimistically. when multi-select is
  // enabled, cmd/ctrl-click or space toggles rows and shift-click or shift+up/down
  // selects a range, with a selection toolbar for bulk mark/flag/delete. loading, empty and error states
  // are all explicit so the pane is never blank.
  import { tick, onMount } from 'svelte'
  import MessageRow from './MessageRow.svelte'
  import SearchBar from './SearchBar.svelte'
  import Spinner from '../common/Spinner.svelte'
  import MessageSkeleton from './MessageSkeleton.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ErrorState from '../common/ErrorState.svelte'
  import funnelIcon from '@phosphor-icons/core/assets/duotone/funnel-duotone.svg?raw'
  import trayIcon from '@phosphor-icons/core/assets/duotone/tray-duotone.svg?raw'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
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
    IconChecklist,
    IconLayoutKanban,
    IconFilterPlus,
    IconDownload,
    IconDownloadOff,
    IconFolderSymlink,
    IconStar,
    IconStarFilled,
    IconLayoutColumns,
    IconAppWindow,
    IconArchive,
    IconFileExport,
    IconAlertTriangle,
    IconMailCheck,
    IconCircleCheck,
    IconPin,
    IconPinnedOff,
    IconBell,
    IconBellOff,
    IconNote,
  } from '@tabler/icons-svelte'
  import InboxZero from './InboxZero.svelte'
  import FollowUpBand from './FollowUpBand.svelte'
  import SplitTabs from './SplitTabs.svelte'
  import { dueFollowUps, followUpsIn } from '../../stores/followups'
  import { markDone } from '../../lib/triage'
  import { activeSplit, splitApplies, splitKeys, stepSplit } from '../../stores/splits'
  import { actsOnConversation, editNote, toggleMute, togglePin } from '../../lib/organize'
  import { selection, searchQuery, openMessageId, openMessage } from '../../stores/selection'
  import {
    messageList,
    loadList,
    loadMore,
    loadOlder,
    backfillFailed,
    runSearch,
    patchInList,
    restoreToList,
    searchSortKind,
    rowPinned,
  } from '../../stores/messages'
  import { emptyFilter, filterActive, buildFilter, type SearchFilter } from '../../lib/searchquery'
  import { applyQuickFilter, quickFilterActive } from '../../lib/quickfilter'
  import ListHeader from './ListHeader.svelte'
  import ResultsFilters from './ResultsFilters.svelte'
  import QuickFilterBar from './QuickFilterBar.svelte'
  import { activeSearch, clearSearch, searchOpenedId, type SearchState } from '../../stores/search'
  import { quickFilter, contactAddresses, resetQuickFilter } from '../../stores/quickfilter'
  import { views as savedViews } from '../../stores/views'
  import {
    selectedIds,
    clearSelection,
    anchorAt,
    ensureAnchor,
    rangeCursor,
    selectOnly,
    toggleSelect,
    selectRange,
    selectAll,
  } from '../../stores/listselect'
  import {
    expandOffer,
    expanding,
    selectAllInList,
    expandSelection,
    clearExpandOffer,
    clearAll,
  } from '../../stores/selectall'
  import { prefs, setSearchSort, setSplitActive } from '../../stores/prefs'
  import { createTasksFromMail } from '../../lib/taskmail'
  import { pickProjectFor } from '../../lib/projectpicker'
  import { createRuleFromMail } from '../automations/blocks/rulefrommail'
  import { maildragSource } from '../../stores/maildrag'
  import { threadedOn, threadMessages, loadThread } from '../../stores/threads'
  import { listRows, threadAware, type ListRow } from '../../lib/threadrows'
  import { motion } from '../../lib/motion'
  import { parseRowActions, rowActionView, type RowActionId } from '../../lib/rowactions'
  import { runRowAction } from '../../lib/rowactionrun'
  import { messageItems } from '../../lib/scriptplacements'
  import { newestHistoryId, offerUndo } from '../../stores/undohistory'
  import { noteAction } from '../../lib/coach'
  import {
    setSeen,
    getMessage,
    archiveMessage,
    openDraft,
    openMessageWindow,
  } from '../../lib/api'
  import { openReply, openForward, openDraftSession } from '../../stores/compose'
  import { sidebar } from '../../stores/accounts'
  import { openSnooze, openSnoozeMany } from '../../stores/snooze'
  import { openMove, openMoveMany } from '../../stores/move'
  import { recordArchived } from '../../stores/undoarchive'
  import { openContextMenu, type MenuEntry } from '../../stores/contextmenu'
  import { activeBindings, bindings, menuHint, shortcutTitle } from '../../stores/shortcuts'
  import { matchShortcut } from '../../lib/shortcuts'
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
    exportEml,
    reportArchiveExport,
    spamState,
    toggleSpam,
    leaveList,
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

  // isInbox reports whether the list is an inbox: the unified one, or an
  // account's own. An empty inbox says so in its own quiet way.
  function isInbox(sel: Selection): boolean {
    if (sel.kind === 'view') {
      return sel.view === 'inbox'
    }
    if (sel.kind !== 'folder') {
      return false
    }
    return ($sidebar.data?.foldersByAccount[sel.accountId] ?? []).some((f) => f.id === sel.folderId && f.role === 'inbox')
  }

  // an inbox of the new design, outside a search, carries the follow-ups that
  // came due on top and says so when it is empty.
  $: inboxShown = !classic && !inSearch && !$searchQuery && isInbox($selection)
  $: inboxFollowUps = inboxShown
    ? followUpsIn($dueFollowUps, $selection.kind === 'folder' ? $selection.accountId : 0)
    : []

  let listEl: HTMLDivElement
  // highlightId is the row the keyboard highlight sits on; activeIndex is its
  // position in the current list. Keeping the id rather than the index means a
  // sync or a flag change that rewrites the list leaves the highlight where
  // the user put it.
  let highlightId: number | null = null
  let activeIndex = -1
  $: activeIndex = highlightId === null ? -1 : items.findIndex((m) => m.id === highlightId)

  // in the new design the selection is one band that slides from row to row,
  // sitting on the highlighted row or else on the open message.
  $: bandOn = $prefs.design !== 'classic'
  $: bandIndex = activeIndex >= 0 ? activeIndex : items.findIndex((m) => m.id === $openMessageId)
  $: bandId = bandIndex >= 0 ? items[bandIndex].id : null

  let bandTop = 0
  let bandHeight = 0
  let hoverTop = 0
  let hoverHeight = 0
  let hoverShown = false

  $: if (bandOn) {
    void placeBand(bandId, bandIndex, estRowHeight, labelsAbove(bandIndex, groupLabels) * labelHeight, windowRows)
  }

  // the band takes the place of the row it marks. a rendered row is measured;
  // one outside the rendered window gets the place its index and the group
  // labels above it give it.
  async function placeBand(id: number | null, index: number, height: number, labels: number, _rendered: unknown): Promise<void> {
    if (id === null) {
      return
    }
    await tick()
    const el = listEl?.querySelector<HTMLElement>(`[id="msg-${id}"]`)
    if (el) {
      bandTop = el.offsetTop
      bandHeight = el.offsetHeight
    } else {
      bandTop = index * height + labels
      bandHeight = height
    }
  }

  // a second, fainter band follows the pointer from row to row.
  function onRowsPointerMove(event: PointerEvent): void {
    if (!bandOn || event.pointerType !== 'mouse') {
      return
    }
    const el = (event.target as HTMLElement).closest<HTMLElement>('[id^="msg-"]')
    if (!el) {
      hoverShown = false
      return
    }
    hoverTop = el.offsetTop
    hoverHeight = el.offsetHeight
    hoverShown = true
  }

  function hideHover(): void {
    hoverShown = false
  }

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
    const label = listEl?.querySelector('.group-label')
    if (label instanceof HTMLElement && label.offsetHeight > 0) {
      groupLabelHeight = label.offsetHeight
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

  // the new design searches from the top bar; classic keeps its own bar here.
  $: classic = $prefs.design === 'classic'
  $: inSearch = !classic && $activeSearch !== null

  let searchFilter: SearchFilter = emptyFilter
  let lastKey = ''
  let lastActive: SearchState | null = null
  $: if ($selection) {
    syncList($selection, $activeSearch, classic)
  }

  // syncList keeps the list on whatever it should show: the selection's list,
  // or in the new design the top bar's search while one is running. Moving to
  // another folder ends a running search.
  function syncList(sel: Selection, active: SearchState | null, isClassic: boolean): void {
    const key = selectionKey(sel)
    if (key !== lastKey) {
      const first = lastKey === ''
      lastKey = key
      // the search bar drops its chips with the folder, so the filter they
      // stood for must go too, or the next search still carries it.
      searchFilter = emptyFilter
      expanded = new Set()
      clearSelection()
      resetScroll()
      resetQuickFilter()
      if (!isClassic && active && first) {
        lastActive = active
        applySearch(active.text, buildFilter(active.chips))
        return
      }
      lastActive = null
      if (!isClassic && active) {
        clearSearch()
      }
      void loadList(sel)
      return
    }
    if (isClassic || active === lastActive) {
      return
    }
    lastActive = active
    if (active) {
      searchQuery.set(active.text)
      applySearch(active.text, buildFilter(active.chips))
    } else {
      searchQuery.set('')
      applySearch('', emptyFilter)
    }
  }

  // the split inbox: tabs over an inbox in the new design, while no search
  // runs. A change of tab on the same inbox reads the list again; a change of
  // selection is syncList's to load.
  $: splitOn = !classic && !inSearch && splitApplies($selection, $prefs, $sidebar.data)
  $: splitKey = splitOn ? activeSplit($prefs.splitActive, splitKeys($prefs.splitTabs, $savedViews)) : ''
  let lastSplit = ''
  let lastSplitSelection = ''
  $: onSplitChange(splitKey, selectionKey($selection))

  function onSplitChange(split: string, selKey: string): void {
    const sameSelection = selKey === lastSplitSelection
    lastSplitSelection = selKey
    if (split === lastSplit) {
      return
    }
    lastSplit = split
    if (sameSelection && lastKey !== '' && !inSearch) {
      clearSelection()
      resetScroll()
      void loadList($selection)
    }
  }

  function selectSplit(key: string): void {
    if (key !== splitKey) {
      setSplitActive(key)
    }
    listEl?.focus()
  }

  $: loadedItems = $messageList.data?.items ?? []
  $: threads = $messageList.data?.threads
  // the quick filter narrows what is loaded; paging still follows the
  // unfiltered list underneath. A conversation passes on what any of its
  // messages has, not only its newest.
  $: quickOn = !classic && $prefs.quickFilterBar && quickFilterActive($quickFilter)
  $: heads = quickOn ? filterHeads(loadedItems, threads) : loadedItems

  function filterHeads(list: MessageSummary[], groups: typeof threads): MessageSummary[] {
    const kept = new Set(applyQuickFilter(threadAware(list, groups), $quickFilter, $contactAddresses).map((m) => m.id))
    return list.filter((m) => kept.has(m.id))
  }

  // the conversations shown open, by thread id. They stay open while the list
  // reloads under them and close when the list moves elsewhere.
  let expanded = new Set<number>()
  // the conversation just opened, whose replies slide in; only that one, so a
  // reply scrolled back into view does not animate again.
  let entering = 0
  let enteringTimer: ReturnType<typeof setTimeout> | undefined

  // a grouped list is one row per conversation with the open ones' replies
  // beneath. items is what is on screen in order, so selection, keyboard and
  // the virtual window work on replies the way they do on any row.
  $: rows = listRows(heads, threads, expanded, $threadMessages)
  $: items = rows.map((r) => r.message)

  // the Pinned group is the leading run of rows whose message or conversation
  // is pinned. A label sits above it and another above the rest.
  $: pinnedRows = classic ? 0 : leadingPinned(rows, threads)
  $: groupLabels = pinnedRows === 0 ? [] : pinnedRows < rows.length ? [0, pinnedRows] : [0]
  let groupLabelHeight = 0
  $: labelHeight = groupLabelHeight || estRowHeight / 3

  function leadingPinned(list: ListRow[], groups: typeof threads): number {
    let n = 0
    let headPinned = false
    for (const row of list) {
      if (row.parentId === 0) {
        headPinned = rowPinned(row.message, groups)
      }
      if (!headPinned) {
        break
      }
      n++
    }
    return n
  }

  // labelsAbove counts the group labels drawn above row index.
  function labelsAbove(index: number, labels: number[]): number {
    return labels.filter((p) => p <= index).length
  }

  // an open conversation whose replies were dropped by a reload reads them
  // again.
  $: for (const threadId of expanded) {
    if (!$threadMessages.has(threadId)) {
      loadThread(threadId).catch((err) => toastError(errorMessage(err)))
    }
  }

  // toggleThread opens or closes a conversation's replies. Closing one with
  // the highlight on a reply moves the highlight up to the conversation.
  function toggleThread(row: ListRow): void {
    const thread = row.thread
    if (!thread || thread.count < 2) {
      return
    }
    const next = new Set(expanded)
    if (next.has(thread.threadId)) {
      next.delete(thread.threadId)
      const lit = rows.find((r) => r.message.id === highlightId)
      if (lit?.parentId === row.message.id) {
        highlightId = row.message.id
      }
    } else {
      next.add(thread.threadId)
      hideHover()
      entering = thread.threadId
      clearTimeout(enteringTimer)
      enteringTimer = setTimeout(() => (entering = 0), motion.slow)
    }
    expanded = next
  }

  // the grouping follows the preference: turning it on or off reads the
  // list again in the other shape.
  $: threaded = threadedOn($prefs)
  let lastThreaded: boolean | null = null
  $: if (threaded !== lastThreaded) {
    const changed = lastThreaded !== null
    lastThreaded = threaded
    expanded = new Set()
    if (changed && !inSearch && lastKey !== '') {
      void loadList($selection)
    }
  }
  // each account's colour (#486), shown only while the list mixes accounts.
  $: accountColors = Object.fromEntries(($sidebar.data?.accounts ?? []).map((a) => [a.id, a.color ?? '']))
  $: mixedAccounts = items.some((m) => m.accountId !== items[0].accountId)
  // a result set pages too: a broad query matches far more than one page, and
  // stopping at the first page is what made a match ranked below it look like
  // no match at all.
  $: searching = $messageList.data?.searching ?? false
  $: hasMore = searching
    ? !($messageList.data?.exhausted ?? false) &&
      ($messageList.data?.loadedHits ?? loadedItems.length) < ($messageList.data?.total ?? 0)
    : loadedItems.length < ($messageList.data?.total ?? 0)
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
  $: windowRows = rows.slice(startIndex, endIndex)
  $: topPad = startIndex * estRowHeight + labelsAbove(startIndex - 1, groupLabels) * labelHeight
  $: bottomPad =
    Math.max(0, (items.length - endIndex) * estRowHeight) + groupLabels.filter((p) => p >= endIndex).length * labelHeight

  // the live multi-selection, intersected with what is still loaded so bulk
  // actions never act on rows that have scrolled out of the data.
  $: selectedItems = items.filter((m) => $selectedIds.has(m.id))

  // what a drag that starts on a row carries: the whole selection when the row
  // is part of it, otherwise that row alone. the classic design has no drag.
  function dragItems(item: MessageSummary): MessageSummary[] {
    if ($prefs.design === 'classic') {
      return []
    }
    return $selectedIds.has(item.id) ? selectedItems : [item]
  }

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
    await selectEveryRow()
  }

  // a quick-filtered list selects only the rows it shows: widening to the
  // whole mailbox would take in the messages the filter hides.
  async function selectEveryRow(): Promise<void> {
    if (quickOn) {
      clearExpandOffer()
      selectAll(items.map((m) => m.id))
      return
    }
    await selectAllInList(items.map((m) => m.id))
  }
  $: selectionCount = selectedItems.length

  // keep the keyboard-nav highlight in sync with whichever message is open,
  // however it got opened (click, Enter, or the in-app vim motions), so a
  // stale arrow-key position never leaves two rows looking selected at once.
  // While app vim mode extends a range its cursor leaves the open message
  // behind, and the highlight goes with the cursor.
  $: cursorId = $rangeCursor ?? $openMessageId
  let lastCursorId: number | null = null
  $: if (cursorId !== lastCursorId) {
    lastCursorId = cursorId
    if (cursorId !== null) {
      highlightId = cursorId
    }
  }

  // a range cursor is moved from App.svelte, which cannot scroll this list.
  $: void followRangeCursor($rangeCursor)
  async function followRangeCursor(id: number | null): Promise<void> {
    if (id === null) {
      return
    }
    await tick()
    await scrollActiveIntoView()
  }

  // search handling. the list shows ranked results when there is a query or an
  // active date filter, and the selection's normal list otherwise.
  function applySearch(query: string, filter: SearchFilter): void {
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

  // the search bar sends the filter right before the query, so the query
  // event runs the search for both.
  function onFilter(event: CustomEvent<SearchFilter>): void {
    searchFilter = event.detail
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
    if (!classic && $activeSearch) {
      applySearch($activeSearch.text, buildFilter($activeSearch.chips))
      return
    }
    applySearch($searchQuery.trim(), searchFilter)
  }

  // listTitle names what the new design's header shows.
  function listTitle(sel: Selection, isSearching: boolean): string {
    return isSearching ? $t('search.results.title') : viewTitle(sel)
  }

  // unreadFor reads the selection's unread count from the sidebar, which keeps
  // it current as mail is read.
  function unreadFor(sel: Selection): number {
    if (sel.kind === 'view') {
      return $sidebar.data?.views.find((v) => v.key === sel.view)?.unreadCount ?? 0
    }
    if (sel.kind === 'savedView') {
      return $savedViews.find((v) => v.id === sel.viewId)?.unreadCount ?? 0
    }
    return $sidebar.data?.foldersByAccount[sel.accountId]?.find((f) => f.id === sel.folderId)?.unreadCount ?? 0
  }

  // countText words the count beside the title.
  function countText(data: typeof $messageList.data, shown: number, quick: boolean): string {
    if (!data) {
      return ''
    }
    if (quick) {
      return $t('list.header.shown').replace('{shown}', String(shown)).replace('{total}', String(data.items.length))
    }
    if (data.searching) {
      if (data.items.length < data.total) {
        return $t('messageList.resultsOf').replace('{loaded}', String(data.items.length)).replace('{total}', String(data.total))
      }
      return `${data.items.length} ${data.items.length === 1 ? $t('messageList.result') : $t('messageList.results')}`
    }
    return `${data.total.toLocaleString()} ${data.total === 1 ? $t('messageList.message') : $t('messageList.messages')}`
  }

  // open marks the message seen if needed (and wanted) and shows it in the detail pane. a
  // plain open also clears any multi-selection. A draft Pelton saved on this
  // device opens in the composer instead; any other draft is only read.
  async function open(index: number): Promise<void> {
    const item = items[index]
    if (!item) {
      return
    }
    const folder = $sidebar.data?.foldersByAccount[item.accountId]?.find((f) => f.id === item.folderId)
    if (folder?.role === 'drafts') {
      try {
        const draft = await openDraft(item.id)
        if (draft.id !== 0) {
          openDraftSession(draft)
          return
        }
      } catch (err) {
        toastError(errorMessage(err))
      }
    }
    highlightId = item.id
    clearSelection()
    // the row that was opened is where a following shift-click measures from.
    anchorAt(item.id)
    searchOpenedId.set(!classic && searching ? item.id : null)
    openMessage(item.id)
    if (!item.seen && $prefs.markReadOnOpen) {
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
  // the row is found by message id rather than its position when it was drawn,
  // so a list that changed under the pointer (a sync landing) cannot turn the
  // click into a different message.
  function onRowClick(event: MouseEvent, id: number): void {
    const index = items.findIndex((m) => m.id === id)
    const item = items[index]
    if (!item) {
      return
    }
    if ($prefs.multiSelectEnabled && (event.metaKey || event.ctrlKey)) {
      toggleSelect(item.id)
      highlightId = item.id
      return
    }
    if ($prefs.multiSelectEnabled && event.shiftKey) {
      selectRange(items.map((m) => m.id), item.id)
      highlightId = item.id
      return
    }
    void open(index)
  }

  // keyboard navigation over the rows. In app vim mode, escape and
  // shift+up/down belong to the vim motions in App.svelte, which keep their own
  // cursor. Space and shift+up/down also stand aside for a binding the user put
  // on them, which the global handler then runs alone.
  async function onKeydown(event: KeyboardEvent): Promise<void> {
    const vim = $prefs.appVimMode
    const bound = matchShortcut(event, $activeBindings)
    if (splitOn && (bound === 'next-split' || bound === 'prev-split')) {
      event.preventDefault()
      stepSplit(bound === 'next-split' ? 1 : -1)
      return
    }
    if (event.key === 'Escape' && selectionCount > 0 && !vim) {
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
      await selectEveryRow()
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      const extend = event.shiftKey && $prefs.multiSelectEnabled
      if (extend && (vim || matchShortcut(event, $activeBindings) !== null)) {
        return
      }
      event.preventDefault()
      if (extend && activeIndex >= 0) {
        ensureAnchor(items[activeIndex].id)
      }
      const next = event.key === 'ArrowDown' ? Math.min(activeIndex + 1, items.length - 1) : Math.max(activeIndex - 1, 0)
      const id = items[next].id
      highlightId = id
      await tick()
      // a plain move makes the row the anchor, so a range started from it
      // includes it, the way a click does. With the setting on it also opens
      // the row, the way a click does.
      if (extend) {
        selectRange(items.map((m) => m.id), id)
      } else if ($prefs.openOnArrow) {
        await open(activeIndex)
      } else {
        anchorAt(id)
      }
      await scrollActiveIntoView()
    } else if ((event.key === 'ArrowRight' || event.key === 'ArrowLeft') && activeIndex >= 0 && threads) {
      // right opens a conversation and left closes it, or steps from a reply
      // up to its conversation; mirrored for a right to left interface.
      const row = rows[activeIndex]
      const rtl = getComputedStyle(listEl).direction === 'rtl'
      const opening = (event.key === 'ArrowRight') !== rtl
      if (row.parentId !== 0) {
        if (!opening) {
          event.preventDefault()
          highlightId = row.parentId
          anchorAt(row.parentId)
          await tick()
          await scrollActiveIntoView()
        }
      } else if (row.thread && row.thread.count > 1 && opening !== expanded.has(row.thread.threadId)) {
        event.preventDefault()
        toggleThread(row)
      }
    } else if (event.key === 'Enter' && activeIndex >= 0) {
      event.preventDefault()
      await open(activeIndex)
    } else if (
      event.key === ' ' &&
      $prefs.multiSelectEnabled &&
      activeIndex >= 0 &&
      matchShortcut(event, $activeBindings) === null
    ) {
      event.preventDefault()
      toggleSelect(items[activeIndex].id)
    }
  }

  // scrollActiveIntoView keeps the highlighted row visible during arrow nav.
  // because rows are virtualized, it scrolls by computed offset rather than
  // asking a (possibly unrendered) node to scroll itself into view.
  async function scrollActiveIntoView(): Promise<void> {
    if (!listEl || activeIndex < 0) {
      return
    }
    const top = activeIndex * estRowHeight + labelsAbove(activeIndex, groupLabels) * labelHeight
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
    leaveList([item.id])
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

  // the quick buttons over a hovered row, new design only, and out of the
  // way while a selection is being built.
  $: rowActionList = !classic && $prefs.rowActions && selectionCount === 0 ? parseRowActions($prefs.rowActionList) : []

  // onRowAction runs a quick button or a swipe of the new design on its row
  // alone, through the same functions as the action's shortcut. A swipe also
  // offers its undo, since nothing on screen says what the gesture did. A
  // button that took its row away hands the focus back to the list.
  async function onRowAction(item: MessageSummary, id: RowActionId, via: 'button' | 'swipe'): Promise<void> {
    if (via === 'button') {
      noteAction(rowActionView(id, { seen: item.seen, flagged: item.flagged, pinned: false, muted: false, inSpam: false }).shortcut)
    }
    const hadFocus = listEl?.contains(document.activeElement) ?? false
    const since = newestHistoryId()
    await runRowAction(id, item, via)
    if (via === 'swipe') {
      offerUndo(since)
    }
    await tick()
    if (hadFocus && listEl && !listEl.contains(document.activeElement)) {
      listEl.focus()
    }
  }

  // performSwipe runs the configured action for a swipe direction on a row.
  function performSwipe(item: MessageSummary, dir: 'left' | 'right'): void {
    const action = (dir === 'left' ? $prefs.swipeLeftAction : $prefs.swipeRightAction) as SwipeAction
    switch (action) {
      case 'delete':
        void remove(item)
        break
      case 'read':
        void markSeen(item, true)
        break
      case 'unread':
        void markSeen(item, false)
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
        ...(classic
          ? []
          : [
              { label: $t('tasks.createFromMailCount').replace('{n}', n), icon: IconChecklist, hint: menuHint('create-task'), action: () => void createTasksFromMail(selectedItems.map((m) => m.id)) },
              { label: $t('projects.addToCount').replace('{n}', n), icon: IconLayoutKanban, hint: menuHint('add-to-project'), action: () => pickProjectFor(selectedItems.map((m) => m.id)) },
            ]),
        { label: $t('messageList.bulk.moveCount').replace('{n}', n), icon: IconFolderSymlink, hint: menuHint('move-to'), action: bulkMove },
        { label: $t('messageList.bulk.archiveCount').replace('{n}', n), icon: IconArchive, hint: menuHint('archive'), action: () => void bulkArchiveSelection() },
        ...(classic ? [] : [{ label: $t('palette.bulk.doneCount').replace('{n}', n), icon: IconCircleCheck, hint: menuHint('done'), action: () => void markDone(selectedItems) }]),
        anyOnline
          ? { label: $t('messageList.bulk.downloadCount').replace('{n}', n), icon: IconDownload, action: () => void bulkSetOfflineCopies(true) }
          : { label: $t('messageList.bulk.removeOfflineCount').replace('{n}', n), icon: IconDownloadOff, action: () => void bulkSetOfflineCopies(false) },
        { label: $t('messageList.bulk.exportEmlCount').replace('{n}', n), icon: IconFileExport, action: () => void exportEml(selectedItems.map((m) => m.id)) },
        'separator',
        { label: $t('messageList.bulk.deleteCount').replace('{n}', n), icon: IconTrash, danger: true, hint: menuHint('delete-message'), action: () => void bulkDelete() },
      ]
      openContextMenu(event.clientX, event.clientY, entries, { place: 'mail.row', items: messageItems(selectedItems.map((m) => m.id)) })
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
      { label: $t('messageList.menu.openInWindow'), icon: IconAppWindow, action: () => void openMessageWindow(item.id, item.subject) },
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
      ...organizeEntries(item),
      'separator',
      { label: $t('messageList.menu.snooze'), icon: IconClockPause, hint: menuHint('snooze'), action: () => openSnooze(item.id, item.subject) },
      ...(classic
        ? []
        : [
            { label: $t('shortcut.createTask'), icon: IconChecklist, hint: menuHint('create-task'), action: () => void createTasksFromMail([item.id]) },
            { label: $t('projects.addTo'), icon: IconLayoutKanban, hint: menuHint('add-to-project'), action: () => pickProjectFor([item.id], item.threadId) },
            { label: $t('scripts.blocks.ruleFromMail'), icon: IconFilterPlus, action: () => void createRuleFromMail(item.id) },
          ]),
      { label: $t('messageList.menu.moveTo'), icon: IconFolderSymlink, hint: menuHint('move-to'), action: () => openMove(item) },
      ...(classic ? [] : [{ label: $t('shortcut.done'), icon: IconCircleCheck, hint: menuHint('done'), action: () => void markDone([item]) }]),
      ...spamEntry(item),
      item.offline
        ? { label: $t('messageList.menu.removeOffline'), icon: IconDownloadOff, hint: menuHint('remove-offline'), action: () => void toggleOffline(item) }
        : { label: $t('shortcut.downloadOffline'), icon: IconDownload, hint: menuHint('download-offline'), action: () => void toggleOffline(item) },
      { label: $t('menu.exportEml'), icon: IconFileExport, action: () => void exportEml([item.id]) },
      'separator',
      { label: $t('action.delete'), icon: IconTrash, danger: true, hint: menuHint('delete-message'), action: () => void remove(item) },
    ]
    openContextMenu(event.clientX, event.clientY, entries, { place: 'mail.row', items: messageItems([item.id]) })
  }

  // organizeEntries are pin, mute and the private note, in the new design only.
  function organizeEntries(item: MessageSummary): MenuEntry[] {
    if (classic) {
      return []
    }
    const conversation = actsOnConversation(item)
    const pinned = rowPinned(item, threads)
    const pinLabel = conversation
      ? $t(pinned ? 'pin.unpinConversation' : 'pin.pinConversation')
      : $t(pinned ? 'pin.unpin' : 'pin.pin')
    const entries: MenuEntry[] = [
      'separator',
      { label: pinLabel, icon: pinned ? IconPinnedOff : IconPin, hint: menuHint('toggle-pin'), action: () => void togglePin(item) },
    ]
    if (item.threadId !== 0) {
      entries.push(
        item.muted
          ? { label: $t('mute.unmute'), icon: IconBell, hint: menuHint('toggle-mute'), action: () => void toggleMute(item) }
          : { label: $t('mute.mute'), icon: IconBellOff, hint: menuHint('toggle-mute'), action: () => void toggleMute(item) },
      )
    }
    entries.push({ label: $t(item.hasNote ? 'note.edit' : 'note.add'), icon: IconNote, hint: menuHint('edit-note'), action: () => editNote(item) })
    return entries
  }

  // spamEntry is the context menu's spam item: "not spam" inside the spam folder,
  // "mark as spam" elsewhere, and nothing for an account without a spam folder.
  function spamEntry(item: MessageSummary): MenuEntry[] {
    switch (spamState(item)) {
      case 'spam':
        return [{ label: $t('spam.notSpam'), icon: IconMailCheck, hint: menuHint('toggle-spam'), action: () => void toggleSpam(item) }]
      case 'markable':
        return [{ label: $t('spam.mark'), icon: IconAlertTriangle, hint: menuHint('toggle-spam'), action: () => void toggleSpam(item) }]
      default:
        return []
    }
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

<section class="list-col" class:new={!classic}>
  {#if classic}
    <div class="header">
      <SearchBar value={$searchQuery} on:search={onSearch} on:filter={onFilter} on:sort={onSort} />
    </div>
  {/if}

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
      {#if !classic}
        <button
          type="button"
          class="act"
          title={$shortcutTitle($t('shortcut.done'), 'done')}
          aria-label={$t('shortcut.done')}
          on:click={() => void markDone(selectedItems)}
        >
          <IconCircleCheck size={16} stroke={1.7} />
        </button>
      {/if}
      <button type="button" class="act" title={$shortcutTitle($t('messageList.menu.moveTo'), 'move-to')} on:click={bulkMove}>
        <IconFolderSymlink size={16} stroke={1.7} />
      </button>
      <button type="button" class="act" title={$t('messageList.bulk.exportEml')} aria-label={$t('messageList.bulk.exportEml')} on:click={() => exportEml(selectedItems.map((m) => m.id))}>
        <IconFileExport size={16} stroke={1.7} />
      </button>
      <button type="button" class="act danger" title={$shortcutTitle($t('action.delete'), 'delete-message')} on:click={bulkDelete}>
        <IconTrash size={16} stroke={1.7} />
      </button>
    </div>
  {:else if !classic}
    <ListHeader
      title={listTitle($selection, inSearch)}
      count={countText($messageList.data, heads.length, quickOn)}
      unread={unreadFor($selection)}
      searching={inSearch}
      backLabel={viewTitle($selection)}
      on:sort={onSort}
    >
      <svelte:fragment slot="lead">
        {#if selectAllAvailable && items.length > 0}
          <button
            type="button"
            class="select-all quiet"
            role="checkbox"
            aria-checked="false"
            aria-label={$t('list.selectAll.label')}
            title={$t('list.selectAll.label')}
            on:click={onSelectAllClick}
          >
            <IconSquare size={16} stroke={1.7} />
          </button>
        {/if}
      </svelte:fragment>
    </ListHeader>
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

  {#if inSearch}
    <ResultsFilters on:sort={onSort} />
  {/if}
  {#if !classic && $prefs.quickFilterBar}
    <QuickFilterBar />
  {/if}
  {#if splitOn}
    <SplitTabs selection={$selection} on:select={(e) => selectSplit(e.detail)} />
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

  {#if inboxShown}
    <FollowUpBand items={inboxFollowUps} />
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
    on:scroll={hideHover}
    on:pointermove={onRowsPointerMove}
    on:pointerleave={hideHover}
  >
    {#if $messageList.status === 'loading' && items.length === 0}
      <!-- placeholders rather than a spinner, and only once the load has taken
           long enough to be felt. -->
      <MessageSkeleton active rows={10} />
    {:else if $messageList.status === 'error'}
      <ErrorState message={$messageList.error} onRetry={() => loadList($selection)} />
    {:else if items.length === 0 && quickOn && loadedItems.length > 0}
      <EmptyState
        kind="filtered"
        icon={funnelIcon}
        title={$t('quickFilter.empty')}
        detail={$t('quickFilter.emptyDetail')}
        actionLabel={$t('quickFilter.clear')}
        onAction={resetQuickFilter}
      />
    {:else if items.length === 0 && inboxShown && splitKey !== ''}
      <EmptyState icon={trayIcon} title={$t('split.empty')} detail={$t('split.emptyDetail')} />
    {:else if items.length === 0 && inboxShown}
      {#if inboxFollowUps.length === 0}
        <InboxZero />
      {/if}
    {:else if items.length === 0 && classic}
      <EmptyState
        title={$searchQuery ? $t('messageList.empty.noMatch') : $t('messageList.empty.noMessages')}
        detail={$searchQuery ? $t('messageList.empty.tryDifferentSearch') : $t('messageList.empty.viewEmpty')}
      >
        <IconMail size={28} stroke={1.4} />
      </EmptyState>
    {:else if items.length === 0 && (inSearch || $searchQuery)}
      <EmptyState
        kind="filtered"
        icon={searchIcon}
        title={$t('messageList.empty.noMatch')}
        detail={$t('messageList.empty.searchDetail')}
        actionLabel={$t('common.clearSearch')}
        onAction={clearSearch}
      />
    {:else if items.length === 0}
      <EmptyState icon={trayIcon} title={$t('messageList.empty.noMessages')} detail={$t('messageList.empty.folderDetail')} />
    {:else}
      {#if topPad > 0}
        <div class="spacer" style={`height:${topPad}px`} aria-hidden="true"></div>
      {/if}
      {#if bandOn}
        <div
          class="hover-band"
          class:shown={hoverShown}
          style={`transform: translateY(${hoverTop}px); height: ${hoverHeight}px`}
          aria-hidden="true"
        ></div>
        <div
          class="select-band"
          class:shown={bandIndex >= 0}
          style={`transform: translateY(${bandTop}px); height: ${bandHeight}px`}
          aria-hidden="true"
        ></div>
      {/if}
      {#each windowRows as row, i (row.key)}
        {@const index = startIndex + i}
        {@const item = row.message}
        {#if groupLabels.includes(index)}
          <div class="group-label" aria-hidden="true">{index === 0 ? $t('pin.group') : $t('pin.groupRest')}</div>
        {/if}
        <div id={`msg-${item.id}`} use:maildragSource={() => dragItems(item)}>
          <MessageRow
            message={item}
            thread={row.thread}
            expanded={row.thread !== null && expanded.has(row.thread.threadId)}
            child={row.parentId !== 0}
            fromMe={row.fromMe}
            entering={row.threadId !== 0 && row.threadId === entering}
            banded={bandOn && index === bandIndex}
            accountColor={mixedAccounts ? (accountColors[item.accountId] ?? '') : ''}
            selected={item.id === $openMessageId || index === activeIndex}
            checked={$selectedIds.has(item.id)}
            actions={rowActionList}
            active={index === activeIndex}
            on:rowaction={(e) => void onRowAction(item, e.detail.id, e.detail.via)}
            on:click={(e) => onRowClick(e, item.id)}
            on:auxclick={(e) => onRowAux(e, item)}
            on:dblclick={() => void openMessageWindow(item.id, item.subject)}
            on:contextmenu={(e) => onContext(e, item)}
            on:swipe={(e) => performSwipe(item, e.detail)}
            on:toggle={() => toggleThread(row)}
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

  .list-col.new {
    display: flex;
    flex-direction: column;
  }

  .list-col.new .rows {
    flex: 1;
  }

  .select-all.quiet {
    padding: var(--space-1);
    color: var(--text-tertiary);
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
    position: relative;
  }

  /* lies over the rows as a tint, so it can travel between them. */
  .select-band {
    position: absolute;
    top: 0;
    inset-inline: 0;
    z-index: 1;
    background: var(--selection-bg);
    opacity: 0;
    pointer-events: none;
    transition:
      transform var(--motion-base) var(--ease-out),
      height var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out);
  }

  .select-band.shown,
  .hover-band.shown {
    opacity: 1;
  }

  .hover-band {
    position: absolute;
    top: 0;
    inset-inline: 0;
    z-index: 1;
    background: var(--hover-tint);
    opacity: 0;
    pointer-events: none;
    transition:
      transform var(--motion-fast) var(--ease-out),
      height var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out);
  }

  @media (prefers-reduced-motion: reduce) {
    .hover-band,
    .select-band {
      transition: none;
    }
  }

  /* the Pinned group's labels. */
  .group-label {
    padding: var(--space-3) var(--row-pad-x) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
    background: var(--surface-raised);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  /* virtualization spacers stand in for the rows outside the rendered window. */
  .spacer {
    flex-shrink: 0;
  }
</style>
