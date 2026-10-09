// messages.ts owns the middle column: the list of summaries for the current
// selection (a folder or a unified view) or, when a query is active, the search
// results. it handles pagination and exposes optimistic helpers so flag toggles
// and deletes reflect immediately without a round trip.

import { writable, get, derived, type Readable } from 'svelte/store'
import type { MessageSummary, Selection, SearchSort, SearchSortPref, SearchKind } from '../lib/types'
import {
  listFolderMessages,
  listViewMessages,
  listSavedViewMessages,
  fetchOlderMessages,
  search,
  messageIds,
  searchMessageIds,
  type MessageIDs,
} from '../lib/api'
import { type AsyncState, idle, loading, ready, failed } from '../lib/async'
import { searchKind, resolveSort } from '../lib/searchsort'
import { errorMessage, push, toastError } from './toast'
import { prefs, searchSortPref } from './prefs'
import { refreshCountsSoon } from './sidebarcounts'

// how many rows we request per page.
export const PAGE_SIZE = 50

export interface ListData {
  items: MessageSummary[]
  total: number
  // searching marks a search result set. These page by ranked offset rather
  // than by row count, since rows can be dropped after ranking.
  searching: boolean
  // loadedHits is how many ranked hits have been requested so far, which is
  // what the next page offsets from. It outruns items.length whenever a page
  // loses rows to the attachment filter or to mail deleted since indexing.
  loadedHits?: number
  // exhausted marks a result set whose ranked list ran out, so the list stops
  // asking for more even if total still reads higher.
  exhausted?: boolean
  // hasOlder means the server still holds mail older than anything cached, so
  // the end of this list is not the end of the mailbox. total only counts what
  // is cached locally.
  hasOlder: boolean
  // backfilling is true while older mail is being fetched from the server, so
  // the list can show that it is waiting on the network rather than on disk.
  backfilling: boolean
}

// backfillFailed marks a 'load older' round trip that errored. The list falls
// back to a retry button instead of retrying on its own, so a server that is
// down does not get hammered once per scroll.
export const backfillFailed = writable(false)

export const messageList = writable<AsyncState<ListData>>(idle())

// the selection the current list belongs to, so loadMore can ask for the next
// page of the same thing.
let currentSelection: Selection | null = null
let currentOffset = 0
// bumped on every loadList call so a response from a superseded selection
// never overwrites a later one's result.
let loadGeneration = 0

// appendPage joins a freshly read page onto the loaded rows, dropping ids that
// are already on screen. Paging is by offset over a date-ordered query, so mail
// arriving between two page reads (a sync, or a backfill inserting older mail)
// can shift rows across the page boundary and repeat one. The list renders rows
// keyed by id, which a duplicate would break outright.
function appendPage(loaded: MessageSummary[], page: MessageSummary[]): MessageSummary[] {
  const seen = new Set(loaded.map((m) => m.id))
  return [...loaded, ...page.filter((m) => !seen.has(m.id))]
}

// Page is one page of cached summaries plus whether the server still holds
// older mail beyond them.
interface Page {
  items: MessageSummary[]
  total: number
  hasOlder: boolean
}

// fetchPage reads one page for a selection at the given offset.
async function fetchPage(sel: Selection, offset: number): Promise<Page> {
  if (sel.kind === 'view') {
    const page = await listViewMessages(sel.view, PAGE_SIZE, offset)
    return { items: page.messages ?? [], total: page.total, hasOlder: page.hasOlder }
  }
  if (sel.kind === 'savedView') {
    const page = await listSavedViewMessages(sel.viewId, PAGE_SIZE, offset)
    return { items: page.messages ?? [], total: page.total, hasOlder: page.hasOlder }
  }
  const page = await listFolderMessages(sel.folderId, PAGE_SIZE, offset)
  return { items: page.messages ?? [], total: page.total, hasOlder: page.hasOlder }
}

// listIsPaginated is true once the user has loaded beyond the first page.
export function listIsPaginated(): boolean {
  const state = get(messageList)
  if (state.status !== 'ready' || !state.data || state.data.searching) {
    return false
  }
  return currentOffset > 0
}

// refreshListHead re-reads the first page and prepends rows that are not
// already loaded. Body sync emits mail:new for ids already on screen; this
// keeps scroll and the loaded window intact while still picking up new stubs.
export async function refreshListHead(sel: Selection): Promise<void> {
  if (currentSearch) {
    return
  }
  if (!currentSelection || sel.kind !== currentSelection.kind) {
    return
  }
  if (
    sel.kind === 'view' &&
    currentSelection.kind === 'view' &&
    sel.view !== currentSelection.view
  ) {
    return
  }
  if (
    sel.kind === 'savedView' &&
    currentSelection.kind === 'savedView' &&
    sel.viewId !== currentSelection.viewId
  ) {
    return
  }
  if (
    sel.kind === 'folder' &&
    currentSelection.kind === 'folder' &&
    sel.folderId !== currentSelection.folderId
  ) {
    return
  }
  const state = get(messageList)
  if (state.status !== 'ready' || !state.data || state.data.searching) {
    return
  }
  const generation = loadGeneration
  try {
    const { items: head, total, hasOlder } = await fetchPage(sel, 0)
    if (generation !== loadGeneration) {
      return
    }
    messageList.update((s) => {
      if (!s.data || generation !== loadGeneration) {
        return s
      }
      const seen = new Set(s.data.items.map((m) => m.id))
      const fresh = head.filter((m) => !seen.has(m.id))
      if (fresh.length === 0 && total === s.data.total && hasOlder === s.data.hasOlder) {
        return s
      }
      const items = [...fresh, ...s.data.items].sort((a, b) =>
        a.date < b.date ? 1 : a.date > b.date ? -1 : 0,
      )
      return ready({ ...s.data, items, total, hasOlder })
    })
  } catch {
    // a failed head refresh must not disturb the list the user is scrolling.
  }
}

// loadList loads the first page for a selection, replacing the list.
export async function loadList(sel: Selection): Promise<void> {
  currentSelection = sel
  currentOffset = 0
  // leaving a result set: a late search page must not append onto a folder.
  currentSearch = null
  searchSortKind.set(null)
  backfillFailed.set(false)
  const generation = ++loadGeneration
  messageList.update((s) => loading(s))
  try {
    const { items, total, hasOlder } = await fetchPage(sel, 0)
    if (generation !== loadGeneration) {
      return
    }
    messageList.set(ready({ items, total, searching: false, hasOlder, backfilling: false }))
  } catch (err) {
    if (generation !== loadGeneration) {
      return
    }
    messageList.set(failed(errorMessage(err)))
  }
}

// loadMore appends the next page if there are more rows. it is a no-op while
// searching or when everything is already loaded.
//
// running out of cached rows is not the same as running out of mail: a first
// sync only caches a folder's newest messages (#175), so when the cache is
// exhausted and the server still has older mail, this hands off to the backfill
// unless the user turned automatic backfill off.
export async function loadMore(): Promise<void> {
  const state = get(messageList)
  if (state.status !== 'ready' || !state.data) {
    return
  }
  if (state.data.searching) {
    await loadMoreSearch()
    return
  }
  if (!currentSelection) {
    return
  }
  if (state.data.items.length >= state.data.total) {
    if (state.data.hasOlder && get(prefs).syncAutoBackfill) {
      await loadOlder()
    }
    return
  }
  const nextOffset = currentOffset + PAGE_SIZE
  const generation = loadGeneration
  messageList.update((s) => loading(s))
  try {
    const { items, total, hasOlder } = await fetchPage(currentSelection, nextOffset)
    if (generation !== loadGeneration) {
      return
    }
    currentOffset = nextOffset
    messageList.update((s) => {
      if (!s.data) {
        return s
      }
      return ready({
        ...s.data,
        items: appendPage(s.data.items, items),
        total,
        hasOlder,
        searching: false,
      })
    })
  } catch (err) {
    if (generation !== loadGeneration) {
      return
    }
    // leave currentOffset unchanged so a retry re-requests this same page
    // instead of skipping it; keep the existing list and surface the error
    // without discarding loaded rows.
    messageList.update((s) => (s.data ? ready(s.data) : failed(errorMessage(err))))
  }
}

// loadOlder fetches the next batch of older mail from the server and appends
// whatever it cached. Unlike loadMore this is a network round trip, so it is
// guarded against overlapping runs and reports failure through backfillFailed
// rather than replacing the list with an error.
export async function loadOlder(): Promise<void> {
  const state = get(messageList)
  if (!currentSelection || state.status !== 'ready' || !state.data) {
    return
  }
  if (state.data.backfilling || !state.data.hasOlder) {
    return
  }

  const sel = currentSelection
  const generation = loadGeneration
  backfillFailed.set(false)
  messageList.update((s) => (s.data ? ready({ ...s.data, backfilling: true }) : s))

  try {
    const result = await fetchOlderMessages(sel)
    if (generation !== loadGeneration) {
      return
    }
    if (result.fetched === 0) {
      messageList.update((s) =>
        s.data ? ready({ ...s.data, backfilling: false, hasOlder: result.hasOlder }) : s,
      )
      return
    }
    // the newly cached mail is older than everything on screen, so it lands at
    // the end of the list; re-read from the current offset rather than
    // reloading from the top, which would lose the user's scroll position.
    const nextOffset = currentOffset + PAGE_SIZE
    const { items, total } = await fetchPage(sel, nextOffset)
    if (generation !== loadGeneration) {
      return
    }
    currentOffset = nextOffset
    messageList.update((s) => {
      if (!s.data) {
        return s
      }
      return ready({
        ...s.data,
        items: appendPage(s.data.items, items),
        total,
        hasOlder: result.hasOlder,
        backfilling: false,
      })
    })
  } catch (err) {
    if (generation !== loadGeneration) {
      return
    }
    backfillFailed.set(true)
    messageList.update((s) => (s.data ? ready({ ...s.data, backfilling: false }) : s))
    push('error', errorMessage(err))
  }
}

// SearchFilter carries the structured constraints from the search chips: an
// optional date window (0 on a side leaves it open), field-scoped terms, and
// the attachment toggle. Free text is passed separately to runSearch.
export interface SearchFilter {
  afterUnix: number
  beforeUnix: number
  from: string
  to: string
  subject: string
  hasAttachment: boolean
  unreadOnly: boolean
}

export const emptyFilter: SearchFilter = {
  afterUnix: 0,
  beforeUnix: 0,
  from: '',
  to: '',
  subject: '',
  hasAttachment: false,
  unreadOnly: false,
}

// datesActive reports whether the filter carries a date window, which is what
// separates a 'dated' search from a 'filtered' one.
function datesActive(f: SearchFilter): boolean {
  return f.afterUnix > 0 || f.beforeUnix > 0
}

// filterActive reports whether any chip constraint is set (used to decide
// between the ranked search and the plain folder list).
export function filterActive(f: SearchFilter): boolean {
  return (
    f.afterUnix > 0 ||
    f.beforeUnix > 0 ||
    f.from !== '' ||
    f.to !== '' ||
    f.subject !== '' ||
    f.hasAttachment ||
    f.unreadOnly
  )
}

// searchPageSize is how many ranked hits one search page holds. It matches what
// the list used to request in one shot, so the first page is never worse than
// before; the difference is that a broad query now pages on to the rest instead
// of stopping here, where anything ranked below was indistinguishable from no
// match at all.
const searchPageSize = 200

// the search the current result set belongs to, so loadMore can ask for the
// next page of the same query. null whenever the list is not a result set.
//
// The resolved sort is part of it: every page of one result set has to be
// fetched in the same order, or paging by offset would interleave two orders
// and both duplicate and skip messages. Changing the sort re-runs the search
// from the first page rather than appending to what is there.
let currentSearch: { query: string; filter: SearchFilter; sort: SearchSort } | null = null

// searchSortKind is the kind of search the active result set is, or null when
// the list is not showing one. The sort menu needs it to know which preference
// a choice belongs to, and to show what 'automatic' currently resolves to.
export const searchSortKind = writable<SearchKind | null>(null)

/**
 * The sort preference in force for the search on screen, and 'auto' when there
 * is none. Reading it from prefs rather than storing a second copy means the
 * settings pane and the menu can never disagree.
 */
export const activeSortPref: Readable<SearchSortPref> = derived(
  [prefs, searchSortKind],
  ([$prefs, $kind]) => ($kind ? searchSortPref($prefs, $kind) : 'auto'),
)


// allMatchingIds returns every id the current list matches, ignoring paging,
// for select all. It follows whatever the list is showing: a result set asks
// the search index, everything else asks the message query.
export function allMatchingIds(): Promise<MessageIDs> {
  const active = currentSearch
  if (active) {
    return searchMessageIds({
      query: active.query,
      afterUnix: active.filter.afterUnix,
      beforeUnix: active.filter.beforeUnix,
      from: active.filter.from,
      to: active.filter.to,
      subject: active.filter.subject,
      hasAttachment: active.filter.hasAttachment,
      unreadOnly: active.filter.unreadOnly,
      limit: 0,
      offset: 0,
      // the same order the list is in: the backend caps how many ids it
      // returns, so a different order here would select a different set of
      // messages from the one on screen.
      sort: active.sort,
    })
  }
  if (!currentSelection) {
    return Promise.resolve({ ids: [], capped: false, matching: 0 })
  }
  return messageIds(currentSelection)
}

// searchPage fetches one page of ranked results.
function searchPage(
  query: string,
  filter: SearchFilter,
  offset: number,
  sort: SearchSort,
): Promise<{ messages: MessageSummary[]; total: number }> {
  return search({
    query,
    afterUnix: filter.afterUnix,
    beforeUnix: filter.beforeUnix,
    from: filter.from,
    to: filter.to,
    subject: filter.subject,
    hasAttachment: filter.hasAttachment,
    unreadOnly: filter.unreadOnly,
    limit: searchPageSize,
    offset,
    sort,
  })
}

// runSearch replaces the list with the first page of ranked search results for a
// query and the structured chip constraints.
export async function runSearch(query: string, filter: SearchFilter = emptyFilter): Promise<void> {
  // the order is settled once, here, and every page of this result set is then
  // fetched in it. Resolving it per page would let a preference change landing
  // mid-scroll page one order onto another.
  const kind = searchKind(query, datesActive(filter))
  const sort = resolveSort(searchSortPref(get(prefs), kind), kind)
  searchSortKind.set(kind)
  currentSearch = { query, filter, sort }
  // the same generation guard every other loader here uses. Without it a search
  // that resolves after the list has moved on writes its results over whatever
  // is showing now: clearing the search bar fires both a query change and a
  // filter change, so the old filtered search and the fresh folder load are in
  // flight together, and the search landing second put the list back into a
  // result set nobody had asked for.
  const generation = ++loadGeneration
  messageList.update((s) => loading(s))
  try {
    const { messages, total } = await searchPage(query, filter, 0, sort)
    if (generation !== loadGeneration) {
      return
    }
    // search runs over the local index, so backfilling older mail from the
    // server is not part of it: hasOlder stays false and the list shows no
    // "load older" affordance on a result set.
    messageList.set(
      ready({
        items: messages,
        total,
        searching: true,
        hasOlder: false,
        backfilling: false,
        // one page of ranked hits was consumed, whether or not every one of
        // them survived to become a row.
        loadedHits: searchPageSize,
      }),
    )
  } catch (err) {
    if (generation !== loadGeneration) {
      return
    }
    messageList.set(failed(errorMessage(err)))
  }
}

// loadMoreSearch appends the next page of the current result set. The backend
// total counts index matches, while the attachment filter and messages deleted
// since indexing are applied per page, so a page can come back short without
// meaning the results are exhausted; paging stops only on an empty page.
async function loadMoreSearch(): Promise<void> {
  const active = currentSearch
  if (!active) {
    return
  }
  const state = get(messageList)
  if (state.status !== 'ready' || !state.data) {
    return
  }
  const offset = state.data.loadedHits ?? state.data.items.length
  // the scroll handler fires repeatedly near the bottom; the loading status is
  // what stops it from launching the same page several times over.
  messageList.update((s) => loading(s))
  try {
    const { messages, total } = await searchPage(active.query, active.filter, offset, active.sort)
    messageList.update((s) => {
      // the status is 'loading' here (this function set it), so only the data
      // is checked: a newer query landing mid-flight is what must be discarded.
      if (!s.data || !s.data.searching || currentSearch !== active) {
        return s
      }
      const items = appendPage(s.data.items, messages)
      return ready({
        ...s.data,
        items,
        total,
        loadedHits: offset + searchPageSize,
        // an empty page means the ranked list ran out, whatever total says.
        exhausted: messages.length === 0,
      })
    })
  } catch (err) {
    // put the list back the way it was: a failed page must not leave it stuck
    // on 'loading', which would block every later page.
    messageList.update((s) => (s.data ? ready(s.data) : s))
    toastError(errorMessage(err))
  }
}

// removeFromList drops a message from the loaded list after a delete.
export function removeFromList(id: number): void {
  messageList.update((s) => {
    if (s.status !== 'ready' || !s.data) {
      return s
    }
    const items = s.data.items.filter((m) => m.id !== id)
    // only decrement total if the id was actually loaded (e.g. deleting from
    // the detail view for a message outside the currently loaded page must
    // not desync total from the real remaining count).
    const removed = items.length !== s.data.items.length
    return ready({
      ...s.data,
      items,
      total: removed ? Math.max(0, s.data.total - 1) : s.data.total,
    })
  })
  // the message left a folder (deleted, archived, moved or snoozed), so that
  // folder's badge and every unified view it feeds are now wrong. This fires
  // even when the row was not loaded: the count changed either way.
  refreshCountsSoon()
}

// neighbourInList returns the row that should take id's place when it leaves
// the list: the one below it, or the one above when it was the last. skip holds
// rows that are going too, so a bulk action does not land on one of its own
// casualties. null when the list holds nothing else, is not loaded, or does not
// have id at all (a message opened from outside the current page).
export function neighbourInList(id: number, skip: ReadonlySet<number> = new Set()): number | null {
  const state = get(messageList)
  if (state.status !== 'ready' || !state.data) {
    return null
  }
  const items = state.data.items
  const at = items.findIndex((m) => m.id === id)
  if (at < 0) {
    return null
  }
  const survives = (m: MessageSummary): boolean => m.id !== id && !skip.has(m.id)
  const below = items.slice(at + 1).find(survives)
  if (below) {
    return below.id
  }
  return items.slice(0, at).findLast(survives)?.id ?? null
}

// restoreToList re-inserts a previously removed row (used by undo-delete),
// keeping the newest-first order by date.
export function restoreToList(summary: MessageSummary): void {
  messageList.update((s) => {
    if (s.status !== 'ready' || !s.data) {
      return s
    }
    if (s.data.items.some((m) => m.id === summary.id)) {
      return s
    }
    const items = [...s.data.items, summary].sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : 0))
    return ready({ ...s.data, items, total: s.data.total + 1 })
  })
  // an undone delete puts the message back where it was, badge included.
  refreshCountsSoon()
}

// patchInList applies a partial update to one row, for optimistic flag changes.
export function patchInList(id: number, patch: Partial<MessageSummary>): void {
  messageList.update((s) => {
    if (s.status !== 'ready' || !s.data) {
      return s
    }
    return ready({
      ...s.data,
      items: s.data.items.map((m) => (m.id === id ? { ...m, ...patch } : m)),
    })
  })
  // only the two flags the sidebar counts. Read state drives every folder badge
  // and the unified views; flagged drives the Flagged view. A color label or an
  // offline copy changes nothing the sidebar shows, and refreshing on those
  // would re-read the whole tree for a row that did not move.
  if ('seen' in patch || 'flagged' in patch) {
    refreshCountsSoon()
  }
}
