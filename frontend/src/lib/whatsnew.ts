// whatsnew.ts holds the notable additions of each release and decides when
// they count as unseen. the entries are keyed by release line (year.quarter),
// so patch releases share their line's list. what is stored as seen is the
// line plus a hash of the entry ids: a nightly or a build with a commit suffix
// shows the newest list, and only a change to that list brings the dot back,
// not every new build.

import type { Space } from './types'

/** What the "Show me" button of an entry opens. */
export type WhatsNewTarget =
  | { kind: 'space'; space: Space }
  | { kind: 'settings'; anchor: string }
  | { kind: 'history' }
  | { kind: 'calendar-view'; view: 'year' }

/** One addition worth a line. */
export interface WhatsNewEntry {
  /** Stable id; part of the content hash. */
  id: string
  titleKey: string
  bodyKey: string
  target: WhatsNewTarget
}

/** The additions of one release line. */
export interface WhatsNewRelease {
  /** Release line, year.quarter, for example 2027.1. */
  line: string
  entries: WhatsNewEntry[]
}

/** Every release with notes, newest first. */
export const releases: WhatsNewRelease[] = [
  {
    line: '2027.1',
    entries: [
      { id: 'projects', titleKey: 'whatsnew.projects.title', bodyKey: 'whatsnew.projects.body', target: { kind: 'space', space: 'projects' } },
      { id: 'notes', titleKey: 'whatsnew.notes.title', bodyKey: 'whatsnew.notes.body', target: { kind: 'space', space: 'notes' } },
      { id: 'feeds', titleKey: 'whatsnew.feeds.title', bodyKey: 'whatsnew.feeds.body', target: { kind: 'space', space: 'feeds' } },
      { id: 'chat', titleKey: 'whatsnew.chat.title', bodyKey: 'whatsnew.chat.body', target: { kind: 'settings', anchor: 'chat#chat-enabled' } },
      { id: 'undo-history', titleKey: 'whatsnew.history.title', bodyKey: 'whatsnew.history.body', target: { kind: 'history' } },
      { id: 'row-actions', titleKey: 'whatsnew.rowActions.title', bodyKey: 'whatsnew.rowActions.body', target: { kind: 'settings', anchor: 'list#row-actions' } },
      { id: 'reading', titleKey: 'whatsnew.reading.title', bodyKey: 'whatsnew.reading.body', target: { kind: 'settings', anchor: 'reading#html-colors' } },
      { id: 'nav-keys', titleKey: 'whatsnew.navKeys.title', bodyKey: 'whatsnew.navKeys.body', target: { kind: 'settings', anchor: 'shortcuts#bindings' } },
      { id: 'spaces', titleKey: 'whatsnew.spaces.title', bodyKey: 'whatsnew.spaces.body', target: { kind: 'settings', anchor: 'layout#spaces' } },
      { id: 'meeting-prep', titleKey: 'whatsnew.meetingPrep.title', bodyKey: 'whatsnew.meetingPrep.body', target: { kind: 'space', space: 'calendar' } },
      { id: 'merge-contacts', titleKey: 'whatsnew.merge.title', bodyKey: 'whatsnew.merge.body', target: { kind: 'space', space: 'contacts' } },
      { id: 'year-planner', titleKey: 'whatsnew.year.title', bodyKey: 'whatsnew.year.body', target: { kind: 'calendar-view', view: 'year' } },
      { id: 'quick-compose', titleKey: 'whatsnew.quickCompose.title', bodyKey: 'whatsnew.quickCompose.body', target: { kind: 'settings', anchor: 'composing#quick-compose' } },
      { id: 'app-lock', titleKey: 'whatsnew.appLock.title', bodyKey: 'whatsnew.appLock.body', target: { kind: 'settings', anchor: 'privacy#app-lock' } },
      { id: 'clean-links', titleKey: 'whatsnew.cleanLinks.title', bodyKey: 'whatsnew.cleanLinks.body', target: { kind: 'settings', anchor: 'privacy#clean-links' } },
      { id: 'notify-actions', titleKey: 'whatsnew.notifyActions.title', bodyKey: 'whatsnew.notifyActions.body', target: { kind: 'settings', anchor: 'notifications#notify-action-archive' } },
      { id: 'scheduling', titleKey: 'whatsnew.scheduling.title', bodyKey: 'whatsnew.scheduling.body', target: { kind: 'space', space: 'calendar' } },
      { id: 'send-variants', titleKey: 'whatsnew.sendVariants.title', bodyKey: 'whatsnew.sendVariants.body', target: { kind: 'settings', anchor: 'composing#after-reply-send' } },
      { id: 'auto-encrypt', titleKey: 'whatsnew.autoEncrypt.title', bodyKey: 'whatsnew.autoEncrypt.body', target: { kind: 'settings', anchor: 'encryption#auto-encrypt' } },
    ],
  },
]

/**
 * The release line of a version string, or null for anything that is not a
 * plain release: nightlies (nightly-...), builds with a commit suffix
 * (v2026.4.8-12-gabc), dev builds.
 */
export function releaseLine(version: string): string | null {
  const m = /^v?(\d{4})\.(\d+)(?:\.\d+)?$/.exec(version.trim())
  return m ? `${m[1]}.${Number(m[2])}` : null
}

function compareLines(a: string, b: string): number {
  const [ay, aq] = a.split('.').map(Number)
  const [by, bq] = b.split('.').map(Number)
  return ay !== by ? ay - by : aq - bq
}

/**
 * The notes that belong to a running version: the newest release not newer
 * than its line, or the newest of all for a version without a line. Null when
 * the version predates every release with notes.
 */
export function releaseFor(version: string, list: readonly WhatsNewRelease[] = releases): WhatsNewRelease | null {
  const sorted = [...list].sort((a, b) => compareLines(b.line, a.line))
  const line = releaseLine(version)
  if (line === null) {
    return sorted[0] ?? null
  }
  return sorted.find((r) => compareLines(r.line, line) <= 0) ?? null
}

// fnv-1a over the ids, short and stable across runs; it only has to tell one
// list from another, not resist anyone.
function hashIds(ids: readonly string[]): string {
  let h = 0x811c9dc5
  for (const ch of ids.join('\n')) {
    h ^= ch.codePointAt(0) ?? 0
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return h.toString(16).padStart(8, '0')
}

/** What is stored once a release's notes have been seen. */
export function seenKey(release: WhatsNewRelease): string {
  return `${release.line}:${hashIds(release.entries.map((e) => e.id))}`
}

/** Whether a running version has notes the stored key has not seen. */
export function hasUnseen(stored: string, version: string, list: readonly WhatsNewRelease[] = releases): boolean {
  const release = releaseFor(version, list)
  return release !== null && release.entries.length > 0 && seenKey(release) !== stored
}
