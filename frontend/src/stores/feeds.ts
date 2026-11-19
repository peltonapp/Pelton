// feeds.ts holds the state of the Feeds space: the sidebar of folders and
// feeds with their counts, the entry list of the chosen scope and which row is
// open, and a running refresh. Changes go to the backend and are shown at once;
// the list keeps rows that stopped matching (an entry just read in Unread)
// until the scope changes, the way the mail list does.

import { derived, get, writable } from 'svelte/store'
import {
  countNewsletterMail,
  listFeedEntries,
  listFeedSidebar,
  listNewsletterMail,
  markFeedScopeRead,
  refreshFeeds,
  setFeedEntriesRead,
  setFeedEntryReadLater,
  setFeedEntryStarred,
  setFlagged,
  setSeen,
} from '../lib/api'
import { onFeedsChanged, onFeedsProgress, onMailNew, onMailRepaired, type FeedsProgressEvent, type Unsubscribe } from '../lib/events'
import { entryRow, mailRow, sameScope, scopeIsUnread, type FeedRow } from '../lib/feedlogic'
import { t } from '../lib/i18n'
import type { FeedScope, FeedSidebar } from '../lib/types'
import { patchInList } from './messages'
import { errorMessage, pushAction, toastError } from './toast'

const emptySidebar: FeedSidebar = { folders: [], feeds: [], counts: { unread: 0, today: 0, starred: 0, readLater: 0 } }

/** The folders, feeds and smart view counts of the sidebar. */
export const feedSidebar = writable<FeedSidebar>(emptySidebar)

/** Unread newsletter mail, for the Newsletters view. */
export const newsletterUnread = writable(0)

/** The refresh running now, or null. */
export const feedProgress = writable<FeedsProgressEvent | null>(null)

/** Unread entries across every feed, for the rail. */
export const feedsUnread = derived(feedSidebar, (s) => s.counts.unread)

/** An entry to show when the Feeds screen is up, set by a notification click. */
export const feedFocus = writable<number | null>(null)

/** What the entry list shows and holds. */
export interface FeedListState {
  scope: FeedScope
  /** Narrows a feed, folder, starred or read-later list to unread rows. */
  unreadOnly: boolean
  search: string
  rows: FeedRow[]
  loading: boolean
  /** More rows can be loaded past the last. */
  more: boolean
  error: string
}

/** The entry list. */
export const feedList = writable<FeedListState>({ scope: { kind: 'unread' }, unreadOnly: false, search: '', rows: [], loading: false, more: false, error: '' })

/** The key of the row open in the reader, or null. */
export const feedSelected = writable<string | null>(null)

/** How many rows one page of the list loads. */
export const feedPageSize = 100

/** Loads the sidebar again. */
export async function loadFeedSidebar(): Promise<void> {
  try {
    feedSidebar.set(await listFeedSidebar())
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Counts the unread newsletter mail again. */
export async function loadNewsletterCount(): Promise<void> {
  try {
    newsletterUnread.set(await countNewsletterMail())
  } catch {
    // the count is a nicety; the list itself reports errors.
  }
}

// each load bumps this, so a reply to an older load is dropped.
let listRun = 0

async function fetchRows(scope: FeedScope, unreadOnly: boolean, search: string, offset: number): Promise<FeedRow[]> {
  if (scope.kind === 'newsletters') {
    const mail = await listNewsletterMail(feedPageSize, offset, unreadOnly)
    const q = search.trim().toLocaleLowerCase()
    const rows = mail.map(mailRow)
    return q ? rows.filter((r) => `${r.title}\n${r.source}\n${r.snippet}`.toLocaleLowerCase().includes(q)) : rows
  }
  const entries = await listFeedEntries(scope, unreadOnly, search, feedPageSize, offset)
  const feeds = get(feedSidebar).feeds
  return entries.map((e) => entryRow(e, feeds))
}

/** Shows a scope, optionally narrowed to unread rows, from its first page. */
export async function showScope(scope: FeedScope, unreadOnly = get(feedList).unreadOnly): Promise<void> {
  const prev = get(feedList)
  const changed = !sameScope(prev.scope, scope)
  feedList.set({ ...prev, scope, unreadOnly, search: changed ? '' : prev.search, rows: changed ? [] : prev.rows })
  if (changed) {
    feedSelected.set(null)
  }
  await loadEntries()
}

/** Narrows the list to rows matching text. */
export async function searchEntries(search: string): Promise<void> {
  feedList.update((s) => ({ ...s, search }))
  await loadEntries()
}

/** Loads the first page of the current scope again. */
export async function loadEntries(): Promise<void> {
  const run = ++listRun
  const { scope, unreadOnly, search } = get(feedList)
  feedList.update((s) => ({ ...s, loading: true, error: '' }))
  try {
    const rows = await fetchRows(scope, unreadOnly && !scopeIsUnread(scope), search, 0)
    if (run === listRun) {
      feedList.update((s) => ({ ...s, rows, loading: false, more: rows.length >= feedPageSize }))
    }
  } catch (err) {
    if (run === listRun) {
      feedList.update((s) => ({ ...s, loading: false, error: errorMessage(err) }))
    }
  }
}

/** Loads the next page and appends it. */
export async function loadMoreEntries(): Promise<void> {
  const state = get(feedList)
  if (state.loading || !state.more) {
    return
  }
  const run = ++listRun
  feedList.update((s) => ({ ...s, loading: true }))
  try {
    const rows = await fetchRows(state.scope, state.unreadOnly && !scopeIsUnread(state.scope), state.search, state.rows.length)
    if (run === listRun) {
      feedList.update((s) => {
        const known = new Set(s.rows.map((r) => r.key))
        return { ...s, rows: [...s.rows, ...rows.filter((r) => !known.has(r.key))], loading: false, more: rows.length >= feedPageSize }
      })
    }
  } catch (err) {
    if (run === listRun) {
      feedList.update((s) => ({ ...s, loading: false, error: errorMessage(err) }))
    }
  }
}

/**
 * Puts new rows of the first page in front of the list without dropping the
 * ones the user is looking at, after a refresh brought entries in.
 */
export async function mergeNewEntries(): Promise<void> {
  const { scope, unreadOnly, search } = get(feedList)
  try {
    const fresh = await fetchRows(scope, unreadOnly && !scopeIsUnread(scope), search, 0)
    feedList.update((s) => {
      if (!sameScope(s.scope, scope)) {
        return s
      }
      const known = new Map(s.rows.map((r) => [r.key, r]))
      const added = fresh.filter((r) => !known.has(r.key))
      const updated = s.rows.map((r) => {
        const next = fresh.find((f) => f.key === r.key)
        return next ? { ...next, read: r.read, starred: r.starred, readLater: r.readLater } : r
      })
      return { ...s, rows: [...added, ...updated] }
    })
  } catch {
    // the next scope change loads the list again.
  }
}

function patchRows(keys: Set<string>, patch: Partial<FeedRow>): void {
  feedList.update((s) => ({ ...s, rows: s.rows.map((r) => (keys.has(r.key) ? { ...r, ...patch } : r)) }))
}

function adjustUnread(rows: FeedRow[], read: boolean): void {
  const entries = rows.filter((r) => !r.mail)
  const mail = rows.filter((r) => r.mail)
  if (mail.length) {
    newsletterUnread.update((n) => Math.max(0, n + (read ? -mail.length : mail.length)))
  }
  if (!entries.length) {
    return
  }
  const delta = read ? -1 : 1
  feedSidebar.update((s) => ({
    ...s,
    counts: { ...s.counts, unread: Math.max(0, s.counts.unread + delta * entries.length) },
    feeds: s.feeds.map((f) => {
      const n = entries.filter((r) => r.feedId === f.id).length
      return n ? { ...f, unread: Math.max(0, f.unread + delta * n) } : f
    }),
  }))
}

/** Marks rows read or unread, entries and newsletter mail alike. */
export async function setRowsRead(rows: FeedRow[], read: boolean): Promise<void> {
  const target = rows.filter((r) => r.read !== read)
  if (!target.length) {
    return
  }
  patchRows(new Set(target.map((r) => r.key)), { read })
  adjustUnread(target, read)
  try {
    const entries = target.filter((r) => !r.mail).map((r) => r.id)
    if (entries.length) {
      await setFeedEntriesRead(entries, read)
    }
    for (const r of target.filter((x) => x.mail)) {
      await setSeen(r.id, read)
      patchInList(r.id, { seen: read })
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
  void loadFeedSidebar()
}

/** Stars or unstars a row; a newsletter mail is flagged. */
export async function toggleStar(row: FeedRow): Promise<void> {
  const starred = !row.starred
  patchRows(new Set([row.key]), { starred })
  try {
    if (row.mail) {
      await setFlagged(row.id, starred)
      patchInList(row.id, { flagged: starred })
    } else {
      await setFeedEntryStarred(row.id, starred)
    }
  } catch (err) {
    patchRows(new Set([row.key]), { starred: row.starred })
    toastError(errorMessage(err))
  }
  void loadFeedSidebar()
}

/** Puts an entry on the read-later list or takes it off. Mail has no such list. */
export async function toggleReadLater(row: FeedRow): Promise<void> {
  if (row.mail) {
    return
  }
  const readLater = !row.readLater
  patchRows(new Set([row.key]), { readLater })
  try {
    await setFeedEntryReadLater(row.id, readLater)
  } catch (err) {
    patchRows(new Set([row.key]), { readLater: row.readLater })
    toastError(errorMessage(err))
  }
  void loadFeedSidebar()
}

/** How long the undo of mark all read stays offered. */
const undoMs = 8000

/**
 * Marks everything the list's scope holds read, past the loaded pages too,
 * with an undo in the toast that says how many it was.
 */
export async function markAllRead(): Promise<void> {
  const { scope, search, rows } = get(feedList)
  const tr = get(t)
  try {
    let entryIds: number[] = []
    let mail: FeedRow[] = []
    if (scope.kind === 'newsletters') {
      mail = rows.filter((r) => !r.read)
      for (const r of mail) {
        await setSeen(r.id, true)
        patchInList(r.id, { seen: true })
      }
      newsletterUnread.update((n) => Math.max(0, n - mail.length))
    } else {
      entryIds = await markFeedScopeRead(scope, search)
    }
    const changed = new Set([...entryIds.map((id) => `e${id}`), ...mail.map((r) => r.key)])
    if (!changed.size) {
      return
    }
    patchRows(changed, { read: true })
    void loadFeedSidebar()
    pushAction(
      'info',
      tr('feeds.markedRead').replace('{n}', String(changed.size)),
      {
        label: tr('feeds.undo'),
        run: () => void undoMarkAll(entryIds, mail),
      },
      undoMs,
    )
  } catch (err) {
    toastError(errorMessage(err))
  }
}

async function undoMarkAll(entryIds: number[], mail: FeedRow[]): Promise<void> {
  try {
    if (entryIds.length) {
      await setFeedEntriesRead(entryIds, false)
    }
    for (const r of mail) {
      await setSeen(r.id, false)
      patchInList(r.id, { seen: false })
    }
    newsletterUnread.update((n) => n + mail.length)
    patchRows(new Set([...entryIds.map((id) => `e${id}`), ...mail.map((r) => r.key)]), { read: false })
  } catch (err) {
    toastError(errorMessage(err))
  }
  void loadFeedSidebar()
}

/** Fetches every feed now; progress arrives through feedProgress. */
export async function refreshAllFeeds(): Promise<void> {
  try {
    await refreshFeeds()
  } catch (err) {
    toastError(errorMessage(err))
  }
}

let started = 0
let stopEvents: Unsubscribe[] = []

/**
 * Keeps the Feeds state current: loads the sidebar and the newsletter count
 * and follows refreshes. Returns the function that stops it; calls nest, so
 * the app and the screen can both hold it.
 */
export function startFeeds(): Unsubscribe {
  started += 1
  if (started === 1) {
    void loadFeedSidebar()
    void loadNewsletterCount()
    stopEvents = [
      onFeedsProgress((e) => feedProgress.set(e.running ? e : null)),
      onFeedsChanged((e) => {
        void loadFeedSidebar().then(() => {
          if (e.added > 0) {
            void mergeNewEntries()
          }
        })
      }),
      onMailNew(() => void loadNewsletterCount()),
      onMailRepaired(() => void loadNewsletterCount()),
    ]
  }
  let stopped = false
  return () => {
    if (stopped) {
      return
    }
    stopped = true
    started -= 1
    if (started === 0) {
      stopEvents.forEach((off) => off())
      stopEvents = []
    }
  }
}
