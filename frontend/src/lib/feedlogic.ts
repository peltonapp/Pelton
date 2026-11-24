// feedlogic.ts holds the store-free rules of the Feeds space: naming and
// comparing list scopes, the rows the entry list shows, moving through them,
// where a dragged feed lands, and what an enclosure is.

import type { EditorMode, Feed, FeedEnclosure, FeedEntry, FeedScope, FeedSidebar, FeedSmartView, MessageSummary } from './types'

/** The smart views in sidebar order. */
export const smartViews: FeedSmartView[] = ['unread', 'today', 'starred', 'later', 'newsletters']

/** A stable string for a scope, for keys and comparisons. */
export function scopeKey(scope: FeedScope): string {
  return scope.kind === 'feed' || scope.kind === 'folder' ? `${scope.kind}:${scope.id}` : scope.kind
}

/** Whether two scopes show the same thing. */
export function sameScope(a: FeedScope, b: FeedScope): boolean {
  return scopeKey(a) === scopeKey(b)
}

/** Reads a scope back from its key; an unknown key is the unread view. */
export function parseScopeKey(key: string): FeedScope {
  const [kind, rawId] = key.split(':')
  const id = Number(rawId)
  if ((kind === 'feed' || kind === 'folder') && Number.isInteger(id) && id >= 0) {
    return { kind, id }
  }
  return smartViews.includes(kind as FeedSmartView) ? { kind: kind as FeedSmartView } : { kind: 'unread' }
}

/** Whether the scope lists only unread entries by its nature. */
export function scopeIsUnread(scope: FeedScope): boolean {
  return scope.kind === 'unread' || scope.kind === 'today'
}

/** The feeds of a folder (0 for none) in the user's order. */
export function feedsIn(feeds: Feed[], folderId: number): Feed[] {
  return feeds.filter((f) => f.folderId === folderId).sort((a, b) => a.position - b.position || a.id - b.id)
}

/** The unread entries of a folder's feeds. */
export function folderUnread(feeds: Feed[], folderId: number): number {
  return feeds.reduce((n, f) => (f.folderId === folderId ? n + f.unread : n), 0)
}

/** The name of what a scope shows; `t` resolves localization keys. */
export function scopeTitle(scope: FeedScope, sidebar: FeedSidebar, t: (key: string) => string): string {
  switch (scope.kind) {
    case 'feed':
      return sidebar.feeds.find((f) => f.id === scope.id)?.title ?? t('feeds.title')
    case 'folder':
      return scope.id === 0 ? t('feeds.noFolder') : (sidebar.folders.find((f) => f.id === scope.id)?.name ?? t('feeds.title'))
    default:
      return t(`feeds.smart.${scope.kind}`)
  }
}

/**
 * FeedRow is one line of the entry list: a feed entry, or a newsletter mail in
 * the Newsletters view. key is unique across both kinds.
 */
export interface FeedRow {
  key: string
  /** The entry id, or the message id of a mail. */
  id: number
  mail: boolean
  /** 0 for a mail. */
  feedId: number
  title: string
  /** The feed's name or the sender's. */
  source: string
  url: string
  author: string
  published: string
  snippet: string
  image: string
  read: boolean
  starred: boolean
  readLater: boolean
  media: FeedEnclosure[]
}

/** The list row of a feed entry; feeds name its source. */
export function entryRow(e: FeedEntry, feeds: Feed[]): FeedRow {
  return {
    key: `e${e.id}`,
    id: e.id,
    mail: false,
    feedId: e.feedId,
    title: e.title,
    source: feeds.find((f) => f.id === e.feedId)?.title ?? '',
    url: e.url,
    author: e.author,
    published: e.published,
    snippet: e.snippet,
    image: e.image,
    read: e.read,
    starred: e.starred,
    readLater: e.readLater,
    media: e.media ?? [],
  }
}

/** The list row of a newsletter mail. */
export function mailRow(m: MessageSummary): FeedRow {
  return {
    key: `m${m.id}`,
    id: m.id,
    mail: true,
    feedId: 0,
    title: m.subject,
    source: m.fromName || m.fromAddress,
    url: '',
    author: m.fromName,
    published: m.date,
    snippet: m.snippet,
    image: '',
    read: m.seen,
    starred: m.flagged,
    readLater: false,
    media: [],
  }
}

/**
 * The row next to `key` in direction dir (1 down, -1 up). With unreadOnly the
 * nearest unread row in that direction. null when there is none; with no
 * current row, the first (or last) that qualifies.
 */
export function neighbour(rows: FeedRow[], key: string | null, dir: 1 | -1, unreadOnly = false): FeedRow | null {
  const start = key === null ? -1 : rows.findIndex((r) => r.key === key)
  let i = start === -1 ? (dir === 1 ? 0 : rows.length - 1) : start + dir
  for (; i >= 0 && i < rows.length; i += dir) {
    if (!unreadOnly || !rows[i].read) {
      return rows[i]
    }
  }
  return null
}

/**
 * The feed ids of a folder after a feed was dropped into it, before the feed
 * with id `before` or at the end when that is null.
 */
export function dropOrder(feeds: Feed[], feedId: number, folderId: number, before: number | null): number[] {
  const ids = feedsIn(feeds, folderId)
    .map((f) => f.id)
    .filter((id) => id !== feedId)
  const at = before === null ? -1 : ids.indexOf(before)
  ids.splice(at === -1 ? ids.length : at, 0, feedId)
  return ids
}

/** The folder ids after one was dropped before another (or at the end). */
export function folderOrder(ids: number[], moved: number, before: number | null): number[] {
  const out = ids.filter((id) => id !== moved)
  const at = before === null ? -1 : out.indexOf(before)
  out.splice(at === -1 ? out.length : at, 0, moved)
  return out
}

/** What an enclosure is, for how the reader shows it. */
export type MediaKind = 'audio' | 'video' | 'image' | 'file'

const audioExt = /\.(mp3|m4a|aac|ogg|oga|opus|wav|flac)(\?|$)/i
const videoExt = /\.(mp4|m4v|webm|mov|ogv)(\?|$)/i
const imageExt = /\.(jpe?g|png|gif|webp|avif|svg)(\?|$)/i

/** The kind of an enclosure, from its type or, failing that, its file name. */
export function mediaKind(e: FeedEnclosure): MediaKind {
  const type = e.type.toLowerCase()
  if (type.startsWith('audio') || (!type && audioExt.test(e.url))) {
    return 'audio'
  }
  if (type.startsWith('video') || (!type && videoExt.test(e.url))) {
    return 'video'
  }
  if (type.startsWith('image') || (!type && imageExt.test(e.url))) {
    return 'image'
  }
  return 'file'
}

/** The file name an enclosure url ends in, for its download link. */
export function fileName(url: string): string {
  try {
    const path = new URL(url).pathname
    const last = decodeURIComponent(path.split('/').filter(Boolean).pop() ?? '')
    return last || new URL(url).hostname
  } catch {
    return url
  }
}

/** The host of a url without a leading www., or ''. */
export function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return ''
  }
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

/** The subject and body of a mail sharing an entry, in the composer's format. */
export function shareMail(title: string, url: string, mode: EditorMode): { subject: string; body: string } {
  const subject = title.trim() || url
  if (mode === 'wysiwyg') {
    const link = url ? `<p><a href="${escapeHtml(url)}">${escapeHtml(url)}</a></p>` : ''
    return { subject, body: `<p>${escapeHtml(subject)}</p>${link}` }
  }
  if (mode === 'markdown' && url) {
    return { subject, body: `[${subject.replace(/[\\[\]]/g, '\\$&')}](${url})\n` }
  }
  return { subject, body: url ? `${subject}\n\n${url}\n` : `${subject}\n` }
}

/**
 * The unread rows that have scrolled past the top of the list: every row
 * whose bottom edge is above top. bottoms maps a row key to that edge, in the
 * list's scroll coordinates.
 */
export function scrolledPast(rows: FeedRow[], bottoms: Map<string, number>, top: number): FeedRow[] {
  return rows.filter((r) => !r.read && (bottoms.get(r.key) ?? Infinity) <= top)
}
