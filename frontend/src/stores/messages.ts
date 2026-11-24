// messages.ts owns the middle column: the list of summaries for the current
// selection (a folder or a unified view) or, when a query is active, the search
// results. it handles pagination and exposes optimistic helpers so flag toggles
// and deletes reflect immediately without a round trip.

import { writable, get, derived, type Readable } from 'svelte/store'
import type { MessageList, MessageSummary, Selection, SearchSort, SearchSortPref, SearchKind, ThreadSummary } from '../lib/types'
import {
  listFolderMessages,
  listViewMessages,
  listSavedViewMessages,
  listFolderThreads,
  listViewThreads,
  listSavedViewThreads,
  fetchOlderMessages,
  search,
  messageIds,
  searchMessageIds,
  type MessageIDs,
  type SearchRequest,
} from '../lib/api'
import { emptyFilter, type SearchFilter } from '../lib/searchquery'
import { type AsyncState, idle, loading, ready, failed } from '../lib/async'
import { searchKind, resolveSort } from '../lib/searchsort'
import { errorMessage, push, toastError } from './toast'
import { prefs, searchSortPref } from './prefs'
import { refreshCountsSoon } from './sidebarcounts'
import { dropThreadMessage, invalidateThreads, patchThreadMessage, threadedOn, threadSeenOf } from './threads'
import { splitFor } from './splits'

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
  // threads is set when the list is grouped into conversations: items are
  // then each conversation's newest message, and this maps such a message's
  // id to its conversation. total counts conversations.
  threads?: Record<number, ThreadSummary>
}

// backfillFailed marks a 'load older' round trip that errored. The list falls
// back to a retry button instead of retrying on its own, so a server that is
// down does not get hammered once per scroll.
export const backfillFailed = writable(false)

export const messageList = writable<AsyncState<ListData>>(idle())

// the selection the current list belongs to, so loadMore can ask for the next
// page of the same thing.
//
// The next page of a folder or view starts at the number of rows loaded, not at
// a running offset. Every local change to the list (a head merge, a delete, an
// undo) changes the server's order the same way it changes the loaded rows, so
// items.length stays the server position of the first row not loaded yet. When
// the server moved first (mail saved before a page read), the page overlaps
// rows already loaded and appendPage drops them: a repeat, never a gap.
let currentSelection: Selection | null = null
// bumped on every loadList call so a response from a superseded selection
// never overwrites a later one's result.
let loadGeneration = 0
// the loadMore whose page is being read, or 0. While one is in flight the user
// is already past the first page, even before that page lands.
let moreInFlight = 0
let moreSeq = 0

// appendPage joins a freshly read page onto the loaded rows, dropping ids that
// are already on screen. Paging is by offset over a date-ordered query, so mail
// arriving between two page reads (a sync, or a backfill inserting older mail)
// can shift rows across the page boundary and repeat one. The list renders rows
// keyed by id, which a duplicate would break outright.
// A conversation can move across a page boundary the same way, with a new
// newest message, so a grouped list also drops a conversation already shown.
function appendPage(loaded: MessageSummary[], page: MessageSummary[], grouped = false): MessageSummary[] {
  const seen = new Set(loaded.map((m) => m.id))
  const threads = new Set(grouped ? loaded.filter((m) => m.threadId !== 0).map((m) => m.threadId) : [])
  return [...loaded, ...page.filter((m) => !seen.has(m.id) && !threads.has(m.threadId))]
}

// mergeThreads joins the conversations of a freshly read page onto the loaded
// ones. undefined means the list is not grouped.
function mergeThreads(
  loaded: Record<number, ThreadSummary> | undefined,
  page: Record<number, ThreadSummary> | undefined,
): Record<number, ThreadSummary> | undefined {
  if (!loaded && !page) {
    return undefined
  }
  return { ...page, ...loaded }
}

// Page is one page of cached summaries plus whether the server still holds
// older mail beyond them.
interface Page {
  items: MessageSummary[]
  total: number
  hasOlder: boolean
  threads?: Record<number, ThreadSummary>
}

// fetchThreadPage reads one page of conversations for a selection. The first
// page puts the Pinned group in front.
async function fetchThreadPage(sel: Selection, offset: number, limit: number): Promise<Page> {
  const split = splitFor(sel)
  const page =
    sel.kind === 'view'
      ? await listViewThreads(sel.view, limit, offset, split)
      : sel.kind === 'savedView'
        ? await listSavedViewThreads(sel.viewId, limit, offset)
        : await listFolderThreads(sel.folderId, limit, offset, split)
  const pinned = (page.pinned ?? []).map((thread) => ({ ...thread, head: { ...thread.head, pinned: true } }))
  const all = [...pinned, ...(page.threads ?? [])]
  const threads: Record<number, ThreadSummary> = {}
  for (const thread of all) {
    threads[thread.head.id] = thread
  }
  return {
    items: all.map((thread) => thread.head),
    total: page.total + pinned.length,
    hasOlder: page.hasOlder,
    threads,
  }
}

// flatPage joins a page of messages onto its Pinned group.
function flatPage(page: MessageList): Page {
  const pinned = page.pinned ?? []
  return { items: [...pinned, ...(page.messages ?? [])], total: page.total + pinned.length, hasOlder: page.hasOlder }
}

// fetchPage reads one page for a selection at the given offset, as
// conversations when the list is grouped.
async function fetchPage(sel: Selection, offset: number): Promise<Page> {
  if (threadedOn(get(prefs))) {
    return fetchThreadPage(sel, offset, PAGE_SIZE)
  }
  if (sel.kind === 'view') {
    return flatPage(await listViewMessages(sel.view, PAGE_SIZE, offset, splitFor(sel)))
  }
  if (sel.kind === 'savedView') {
    return flatPage(await listSavedViewMessages(sel.viewId, PAGE_SIZE, offset))
  }
  return flatPage(await listFolderMessages(sel.folderId, PAGE_SIZE, offset, splitFor(sel)))
}

// pagedCount is the server position of the first row not loaded yet: the
// loaded rows less the Pinned group, which comes with the first page and is
// not paged.
function pagedCount(data: ListData | null | undefined): number {
  return data ? data.items.filter((m) => !rowPinned(m, data.threads)).length : 0
}

/** Whether a row belongs to the Pinned group: the message or its conversation is pinned. */
export function rowPinned(m: MessageSummary, threads?: Record<number, ThreadSummary>): boolean {
  return !!m.pinned || !!threads?.[m.id]?.pinned
}

/**
 * Puts the pinned rows in front, each side keeping its order. A list that is
 * not a result set keeps its rest newest first, which is where an unpinned
 * row has to go back to.
 */
export function pinnedFirst(items: MessageSummary[], threads?: Record<number, ThreadSummary>, byDate = false): MessageSummary[] {
  const pinned = items.filter((m) => rowPinned(m, threads))
  const rest = items.filter((m) => !rowPinned(m, threads))
  if (byDate) {
    rest.sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : 0))
  }
  return [...pinned, ...rest]
}

/** Pins or unpins a loaded row, conversation included, and moves it into place. */
export function repinInList(id: number, pinned: boolean): void {
  messageList.update((s) => {
    if (s.status !== 'ready' || !s.data) {
      return s
    }
    const threads = s.data.threads
      ? Object.fromEntries(
          Object.entries(s.data.threads).map(([head, thread]) =>
            Number(head) === id ? [head, { ...thread, pinned, head: { ...thread.head, pinned } }] : [head, thread],
          ),
        )
      : undefined
    const items = s.data.items.map((m) => (m.id === id ? { ...m, pinned } : m))
    return ready({ ...s.data, threads, items: pinnedFirst(items, threads, !s.data.searching) })
  })
  patchThreadMessage(id, { pinned })
}

// listIsPaginated is true once the user has loaded, or is loading, beyond the
// first page. mail:new reads it to merge the head instead of reloading the list
// back to one page, so a page or a backfill still in flight counts: reloading
// then would also throw that page away.
export function listIsPaginated(): boolean {
  const state = get(messageList)
  if (!state.data || state.data.searching) {
    return false
  }
  if (moreInFlight !== 0 || state.data.backfilling) {
    return true
  }
  // a reload would cut the list back to one page.
  return state.status === 'ready' && state.data.items.length > PAGE_SIZE
}

// a mail:new that came while the list was loading. One refresh covers any
// number of them, so only the latest selection is kept.
let queuedHead: Selection | null = null

function sameSelection(a: Selection, b: Selection): boolean {
  if (a.kind === 'view' && b.kind === 'view') return a.view === b.view
  if (a.kind === 'savedView' && b.kind === 'savedView') return a.viewId === b.viewId
  if (a.kind === 'folder' && b.kind === 'folder') return a.folderId === b.folderId
  return false
}

// flushQueuedHead runs the head refresh queued while the list was loading.
function flushQueuedHead(): void {
  const sel = queuedHead
  queuedHead = null
  if (sel) {
    void refreshListHead(sel)
  }
}

// refreshListHead re-reads the first page, prepends rows that are not loaded
// yet, replaces loaded rows with their fresh copy (body sync fills in the
// snippet, attachment and security flags of rows already on screen) and drops
// loaded rows within the head's date range that are gone from it. Scroll and
// the rest of the loaded window stay intact. While the list is loading it queues one
// refresh for when the load ends.
export async function refreshListHead(sel: Selection): Promise<void> {
  if (currentSearch || !currentSelection || !sameSelection(sel, currentSelection)) {
    return
  }
  const state = get(messageList)
  if (state.status === 'loading') {
    queuedHead = sel
    return
  }
  if (state.status !== 'ready' || !state.data || state.data.searching) {
    return
  }
  const generation = loadGeneration
  try {
    const { items: head, total, hasOlder, threads: headThreads } = await fetchPage(sel, 0)
    if (generation !== loadGeneration) {
      return
    }
    if (get(messageList).status === 'loading') {
      // a loadMore started meanwhile; its result may predate this head.
      queuedHead = sel
      return
    }
    messageList.update((s) => {
      if (s.status !== 'ready' || !s.data || s.data.searching) {
        return s
      }
      const byId = new Map(head.map((m) => [m.id, m]))
      // the head is the whole selection down to its oldest row, so a loaded
      // row newer than that row that the head lacks has left (deleted, moved,
      // read under an unread view, or no longer the newest of its
      // conversation). A row on the same date may just sort below the head
      // (the server breaks date ties by uid, which the list does not have), so
      // it stays. A short head is the entire selection. The Pinned group comes
      // whole with the head and is not paged, so it does not count here, and a
      // loaded pinned row the head lacks has left the group.
      const paged = head.filter((m) => !rowPinned(m, headThreads))
      const whole = paged.length < PAGE_SIZE
      const oldestHead = whole ? '' : paged.reduce((min, m) => (m.date < min ? m.date : min), paged[0].date)
      let changed = total !== s.data.total || hasOlder !== s.data.hasOlder
      const kept: MessageSummary[] = []
      for (const m of s.data.items) {
        const next = byId.get(m.id)
        byId.delete(m.id)
        if (!next) {
          if (whole || m.date > oldestHead || rowPinned(m, s.data.threads)) {
            changed = true
          } else {
            kept.push(m)
          }
          continue
        }
        if (
          JSON.stringify(next) !== JSON.stringify(m) ||
          JSON.stringify(headThreads?.[m.id]) !== JSON.stringify(s.data.threads?.[m.id])
        ) {
          changed = true
          kept.push(next)
        } else {
          kept.push(m)
        }
      }
      const fresh = [...byId.values()]
      if (fresh.length === 0 && !changed) {
        return s
      }
      const sorted = [...fresh, ...kept].sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : 0))
      // a conversation keeps the head's summary of it; one that left keeps
      // nothing.
      let threads = s.data.threads
      if (threads || headThreads) {
        const merged = mergeThreads(s.data.threads, undefined) ?? {}
        Object.assign(merged, headThreads)
        threads = Object.fromEntries(sorted.filter((m) => merged[m.id]).map((m) => [m.id, merged[m.id]]))
      }
      const items = pinnedFirst(sorted, threads, true)
      return ready({ ...s.data, items, total, hasOlder, threads })
    })
  } catch {
    // a failed head refresh must not disturb the list the user is scrolling.
  }
}

// loadList loads the first page for a selection, replacing the list.
export async function loadList(sel: Selection): Promise<void> {
  currentSelection = sel
  moreInFlight = 0
  // leaving a result set: a late search page must not append onto a folder.
  currentSearch = null
  searchSortKind.set(null)
  backfillFailed.set(false)
  const generation = ++loadGeneration
  invalidateThreads()
  messageList.update((s) => loading(s))
  try {
    const { items, total, hasOlder, threads } = await fetchPage(sel, 0)
    if (generation !== loadGeneration) {
      return
    }
    messageList.set(ready({ items, total, searching: false, hasOlder, backfilling: false, threads }))
  } catch (err) {
    if (generation !== loadGeneration) {
      return
    }
    messageList.set(failed(errorMessage(err)))
  }
  flushQueuedHead()
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
  const nextOffset = pagedCount(state.data)
  const generation = loadGeneration
  const token = ++moreSeq
  moreInFlight = token
  messageList.update((s) => loading(s))
  try {
    const { items, total, hasOlder, threads } = await fetchPage(currentSelection, nextOffset)
    if (moreInFlight === token) {
      moreInFlight = 0
    }
    if (generation !== loadGeneration) {
      return
    }
    messageList.update((s) => {
      if (!s.data) {
        return s
      }
      return ready({
        ...s.data,
        items: appendPage(s.data.items, items, threads !== undefined),
        total,
        hasOlder,
        searching: false,
        threads: mergeThreads(s.data.threads, threads),
      })
    })
  } catch (err) {
    if (moreInFlight === token) {
      moreInFlight = 0
    }
    if (generation !== loadGeneration) {
      return
    }
    // nothing was appended, so a retry re-requests this same page; keep the existing list and surface the error
    // without discarding loaded rows.
    messageList.update((s) => (s.data ? ready(s.data) : failed(errorMessage(err))))
  }
  flushQueuedHead()
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
    // the end of the list; read on from the loaded rows rather than reloading
    // from the top, which would lose the user's scroll position. The count is
    // read now, not before the round trip: a head merge may have run meanwhile.
    const loaded = pagedCount(get(messageList).data)
    const { items, total, threads } = await fetchPage(sel, loaded)
    if (generation !== loadGeneration) {
      return
    }
    messageList.update((s) => {
      if (!s.data) {
        return s
      }
      return ready({
        ...s.data,
        items: appendPage(s.data.items, items, threads !== undefined),
        threads: mergeThreads(s.data.threads, threads),
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

// datesActive reports whether the filter carries a date window, which is what
// separates a 'dated' search from a 'filtered' one.
function datesActive(f: SearchFilter): boolean {
  return f.afterUnix > 0 || f.beforeUnix > 0
}

// requestFilter is the part of a search request the filter supplies.
function requestFilter(f: SearchFilter): Omit<SearchRequest, 'query' | 'limit' | 'offset' | 'sort'> {
  return {
    afterUnix: f.afterUnix,
    beforeUnix: f.beforeUnix,
    from: f.from,
    to: f.to,
    subject: f.subject,
    hasAttachment: f.hasAttachment,
    unreadOnly: f.unreadOnly,
    flaggedOnly: f.flaggedOnly,
    accountId: f.accountId,
    folderId: f.folderId,
    note: f.note,
    hasNote: f.hasNote,
  }
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
      ...requestFilter(active.filter),
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
  return messageIds(currentSelection, splitFor(currentSelection))
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
    ...requestFilter(filter),
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
        items: pinnedFirst(messages),
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
      const items = pinnedFirst(appendPage(s.data.items, messages))
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

// withoutMember takes a message out of the conversation that lists it as a
// member, so its count and unread count stay true. The conversation whose
// newest message it was is left alone: that row is going.
function withoutMember(
  threads: Record<number, ThreadSummary> | undefined,
  id: number,
): Record<number, ThreadSummary> | undefined {
  if (!threads) {
    return threads
  }
  const seen = threadSeenOf(id)
  const out: Record<number, ThreadSummary> = {}
  for (const [head, thread] of Object.entries(threads)) {
    if (Number(head) !== id && thread.messageIds.includes(id)) {
      out[Number(head)] = {
        ...thread,
        messageIds: thread.messageIds.filter((m) => m !== id),
        count: Math.max(1, thread.count - 1),
        unreadCount: seen === false ? Math.max(0, thread.unreadCount - 1) : thread.unreadCount,
      }
    } else {
      out[Number(head)] = thread
    }
  }
  return out
}

// a conversation whose newest message left the list still has the rest of its
// messages, so the loaded pages are read again shortly after to bring it back
// under its next newest message.
let regroupTimer: ReturnType<typeof setTimeout> | undefined
// while a hold is out, a regroup waits for its release: a conversation whose
// members are still being moved away one by one would otherwise come back
// under a member that is about to go too.
let regroupHolds = 0
let regroupPending = false

function regroupSoon(): void {
  if (regroupHolds > 0) {
    regroupPending = true
    return
  }
  clearTimeout(regroupTimer)
  regroupTimer = setTimeout(() => void regroup(), 400)
}

/**
 * holdRegroup keeps conversations that lost their row from being read back
 * until the returned release is called, which then regroups if one was due.
 */
export function holdRegroup(): () => void {
  regroupHolds++
  let released = false
  return () => {
    if (released) {
      return
    }
    released = true
    regroupHolds--
    if (regroupHolds === 0 && regroupPending) {
      regroupPending = false
      regroupSoon()
    }
  }
}

// regroup reads the conversations loaded so far again in one request, without
// showing the list as loading or moving its scroll.
async function regroup(): Promise<void> {
  const sel = currentSelection
  const state = get(messageList)
  if (!sel || state.status !== 'ready' || !state.data?.threads || currentSearch) {
    return
  }
  const generation = loadGeneration
  try {
    const page = await fetchThreadPage(sel, 0, Math.max(pagedCount(state.data), PAGE_SIZE))
    if (generation !== loadGeneration) {
      return
    }
    messageList.update((s) =>
      s.status === 'ready' && s.data?.threads
        ? ready({ ...s.data, items: page.items, threads: page.threads, total: page.total, hasOlder: page.hasOlder })
        : s,
    )
  } catch {
    // the rows already on screen are still right; the next reload catches up.
  }
}

// removeFromList drops a message from the loaded list after a delete.
export function removeFromList(id: number): void {
  messageList.update((s) => {
    if (s.status !== 'ready' || !s.data) {
      return s
    }
    const thread = s.data.threads?.[id]
    if (thread && thread.count > 1) {
      regroupSoon()
    }
    const threads = withoutMember(s.data.threads, id)
    if (threads && thread) {
      delete threads[id]
    }
    const items = s.data.items.filter((m) => m.id !== id)
    // only decrement total if the id was actually loaded (e.g. deleting from
    // the detail view for a message outside the currently loaded page must
    // not desync total from the real remaining count).
    const removed = items.length !== s.data.items.length
    return ready({
      ...s.data,
      items,
      threads,
      total: removed ? Math.max(0, s.data.total - 1) : s.data.total,
    })
  })
  dropThreadMessage(id)
  // the message left a folder (deleted, archived, moved or snoozed), so that
  // folder's badge and every unified view it feeds are now wrong. This fires
  // even when the row was not loaded: the count changed either way.
  refreshCountsSoon()
}

// neighbourInList returns the row that should take id's place when it leaves
// the list: the one below it, or the one above when it was the last. toward
// 'above' prefers the row above and falls back to the one below. skip holds
// rows that are going too, so a bulk action does not land on one of its own
// casualties. null when the list holds nothing else, is not loaded, or does not
// have id at all (a message opened from outside the current page).
export function neighbourInList(
  id: number,
  skip: ReadonlySet<number> = new Set(),
  toward: 'below' | 'above' = 'below',
): number | null {
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
  const below = items.slice(at + 1).find(survives)?.id ?? null
  const above = items.slice(0, at).findLast(survives)?.id ?? null
  return toward === 'below' ? (below ?? above) : (above ?? below)
}

/**
 * newestFirst reports whether the loaded list runs from newest to oldest,
 * which is what every folder does and what a search sorted by date may not.
 */
export function newestFirst(): boolean {
  const items = get(messageList).data?.items ?? []
  if (items.length < 2) {
    return true
  }
  return Date.parse(items[0].date) >= Date.parse(items[items.length - 1].date)
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
    // a grouped list reads its conversations again rather than guessing which
    // one the message rejoins.
    if (s.data.threads) {
      regroupSoon()
      return s
    }
    const items = pinnedFirst([...s.data.items, summary], undefined, true)
    return ready({ ...s.data, items, total: s.data.total + 1 })
  })
  // an undone delete puts the message back where it was, badge included.
  refreshCountsSoon()
}

// patchThreads carries a change to a message into the conversation it belongs
// to: the newest message's copy, the unread count when its read state moved,
// and the conversation's flag when it is flagged.
function patchThreads(
  threads: Record<number, ThreadSummary> | undefined,
  items: MessageSummary[],
  id: number,
  patch: Partial<MessageSummary>,
): Record<number, ThreadSummary> | undefined {
  if (!threads) {
    return threads
  }
  const before = items.find((m) => m.id === id)?.seen ?? threadSeenOf(id)
  const out: Record<number, ThreadSummary> = {}
  for (const [key, thread] of Object.entries(threads)) {
    const head = Number(key)
    if (head !== id && !thread.messageIds.includes(id)) {
      out[head] = thread
      continue
    }
    let unreadCount = thread.unreadCount
    if (patch.seen !== undefined && before !== undefined && patch.seen !== before) {
      unreadCount = Math.max(0, unreadCount + (patch.seen ? -1 : 1))
    }
    out[head] = {
      ...thread,
      head: head === id ? { ...thread.head, ...patch } : thread.head,
      unreadCount,
      flagged: patch.flagged === true ? true : thread.flagged,
    }
  }
  return out
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
      threads: patchThreads(s.data.threads, s.data.items, id, patch),
    })
  })
  patchThreadMessage(id, patch)
  // only the two flags the sidebar counts. Read state drives every folder badge
  // and the unified views; flagged drives the Flagged view. A color label or an
  // offline copy changes nothing the sidebar shows, and refreshing on those
  // would re-read the whole tree for a row that did not move.
  if ('seen' in patch || 'flagged' in patch) {
    refreshCountsSoon()
  }
}
