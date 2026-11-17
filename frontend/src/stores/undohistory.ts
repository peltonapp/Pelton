// undohistory.ts is the one list of recent reversible actions: archive, move,
// delete, read and flag changes, done, mute, pin, snooze, spam, a send still
// inside its undo window, mail filed under a project, and tasks and events
// made from mail. It lives for the session only and keeps the last
// fifty. Permanent actions (empty trash, permanent delete, a send past its
// window) are never recorded.
//
// Each entry can be undone on its own. It is safe when no later, still applied
// entry touched the same messages; otherwise the entry stays listed but
// disabled with a reason. An undone entry can be redone when it carries a redo
// and nothing has touched its messages since.
//
// This module only holds the list and the rules. The closures that reverse an
// action come from the callers, so it imports nothing from the action modules.

import { derived, get, writable, type Readable } from 'svelte/store'
import { t } from '../lib/i18n'
import { errorMessage, pushAction, toastError, toastInfo } from './toast'

/** What kind of action an entry records. */
export type HistoryKind =
  | 'archive'
  | 'move'
  | 'delete'
  | 'spam'
  | 'done'
  | 'seen'
  | 'flag'
  | 'color'
  | 'pin'
  | 'mute'
  | 'snooze'
  | 'send'
  | 'project'
  | 'task'
  | 'event'
  | 'script'

/** The most entries kept; the oldest fall off first. */
export const historyCap = 50

/** What a caller hands over to record one action. */
export interface HistoryInput {
  kind: HistoryKind
  /** The action in words, such as "Archived". */
  label: string
  /** What it acted on: a subject, or a count of messages. */
  detail: string
  count: number
  /**
   * The state it changed, as `domain:id` keys. Two entries conflict when they
   * share a key; see messageKeys for the keys of a message.
   */
  touches: string[]
  undo: () => Promise<void>
  redo?: () => Promise<void>
  /** Undo and redo give the messages new ids, so older entries on them go stale. */
  rekeys?: boolean
  /** The action reports its own outcome, so the history stays quiet. */
  quiet?: boolean
}

/** Why an entry cannot be undone or redone right now. */
export type HistoryReason = 'later' | 'earlier' | 'moved' | 'failed' | 'busy' | 'noRedo'

/** One row of the history as the UI shows it. */
export interface HistoryRow {
  id: number
  kind: HistoryKind
  label: string
  detail: string
  count: number
  at: number
  state: 'applied' | 'undone' | 'failed'
  busy: boolean
  canUndo: boolean
  canRedo: boolean
  /** Set when the entry's current action (undo while applied, redo while undone) is disabled. */
  reason: HistoryReason | null
}

interface Entry extends HistoryInput {
  id: number
  at: number
  state: 'applied' | 'undone' | 'failed'
  busy: boolean
  stale: boolean
  /** Order of the undos, so redo takes the one undone last. */
  undoneAt: number
}

const entries = writable<Entry[]>([])
let nextId = 1
let undoSeq = 0

const domains = ['seen', 'flag', 'color', 'pin', 'mute', 'snooze', 'loc'] as const

/** Every state key of one message, for an action that moves or removes it. */
export function messageKeys(id: number): string[] {
  return domains.map((d) => `${d}:${id}`)
}

/** The key of one kind of state on one message. */
export function stateKey(domain: (typeof domains)[number], id: number): string {
  return `${domain}:${id}`
}

/** What an action acted on, in words: the subject of one message, or a count of several. */
export function subjectDetail(subjects: readonly string[]): string {
  const say = get(t)
  if (subjects.length === 1) {
    return subjects[0] || say('messageList.noSubject')
  }
  return say('history.count').replace('{n}', String(subjects.length))
}

function overlaps(a: readonly string[], b: readonly string[]): boolean {
  return a.some((k) => b.includes(k))
}

// reasonFor says why the entry at index i cannot take its next step, or null.
function reasonFor(list: Entry[], i: number): HistoryReason | null {
  const e = list[i]
  if (e.state === 'failed') {
    return 'failed'
  }
  if (e.busy) {
    return 'busy'
  }
  if (e.stale) {
    return 'moved'
  }
  if (e.state === 'applied') {
    const blocked = list.slice(i + 1).some((l) => l.state === 'applied' && overlaps(e.touches, l.touches))
    return blocked ? 'later' : null
  }
  if (!e.redo) {
    return 'noRedo'
  }
  const waiting = list.slice(0, i).some((p) => p.state === 'undone' && overlaps(e.touches, p.touches))
  return waiting ? 'earlier' : null
}

function toRow(list: Entry[], i: number): HistoryRow {
  const e = list[i]
  const reason = reasonFor(list, i)
  return {
    id: e.id,
    kind: e.kind,
    label: e.label,
    detail: e.detail,
    count: e.count,
    at: e.at,
    state: e.state,
    busy: e.busy,
    canUndo: e.state === 'applied' && reason === null,
    canRedo: e.state === 'undone' && reason === null,
    reason,
  }
}

/** The history, newest first. */
export const history: Readable<HistoryRow[]> = derived(entries, (list) =>
  list.map((_, i) => toRow(list, i)).reverse(),
)

/** Whether anything can be undone, and whether anything can be redone. */
export const historyAvailable: Readable<{ undo: boolean; redo: boolean }> = derived(history, (rows) => ({
  undo: rows.some((r) => r.canUndo),
  redo: rows.some((r) => r.canRedo),
}))

/** Whether the history popover is open. */
export const historyOpen = writable(false)

/** Records an action that just happened, and returns its id. */
export function recordHistory(input: HistoryInput): number {
  const id = nextId++
  entries.update((list) => {
    // an undone entry whose messages this action touched can no longer be
    // replayed as it was.
    const next = list.map((e) => (e.state === 'undone' && overlaps(e.touches, input.touches) ? { ...e, redo: undefined } : e))
    next.push({ ...input, id, at: Date.now(), state: 'applied', busy: false, stale: false, undoneAt: 0 })
    return next.length > historyCap ? next.slice(next.length - historyCap) : next
  })
  return id
}

/** The id of the newest entry, or 0 while the history is empty. */
export function newestHistoryId(): number {
  return get(entries).at(-1)?.id ?? 0
}

// actions that already put up their own undo toast.
const ownToast = new Set<HistoryKind>(['pin', 'mute', 'send', 'project', 'task', 'event'])

/** How long an offered undo stays up. */
export const undoToastTTL = 6000

/**
 * Puts up an undo toast for the newest entry recorded after since, the id
 * newestHistoryId gave before the action ran. Nothing shows when the action
 * recorded nothing (it failed, or opened a dialog instead) or already offered
 * its own undo.
 */
export function offerUndo(since: number): void {
  const entry = get(entries).at(-1)
  if (!entry || entry.id <= since || entry.state !== 'applied' || ownToast.has(entry.kind)) {
    return
  }
  const say = get(t)
  const message = say('undoToast.message').replace('{action}', entry.label).replace('{what}', entry.detail)
  pushAction('info', message, { label: say('menu.undo'), run: () => void undoEntry(entry.id) }, undoToastTTL)
}

/** Takes an entry out of the history, for an action that became permanent. */
export function dropHistoryEntry(id: number): void {
  entries.update((list) => list.filter((e) => e.id !== id))
}

/** Empties the history. */
export function clearHistory(): void {
  entries.set([])
}

function patch(id: number, change: Partial<Entry>): void {
  entries.update((list) => list.map((e) => (e.id === id ? { ...e, ...change } : e)))
}

// staleOthers marks every other entry on the same messages as out of date,
// after an undo or redo gave those messages new ids.
function staleOthers(id: number, touches: readonly string[]): void {
  entries.update((list) => list.map((e) => (e.id !== id && overlaps(e.touches, touches) ? { ...e, stale: true } : e)))
}

async function step(id: number, direction: 'undo' | 'redo'): Promise<boolean> {
  const row = get(history).find((r) => r.id === id)
  if (!row || row.busy || (direction === 'undo' ? !row.canUndo : !row.canRedo)) {
    return false
  }
  const entry = get(entries).find((e) => e.id === id)
  const run = direction === 'undo' ? entry?.undo : entry?.redo
  if (!entry || !run) {
    return false
  }
  patch(id, { busy: true })
  try {
    await run()
  } catch (err) {
    patch(id, { busy: false, state: 'failed' })
    toastError(errorMessage(err))
    return false
  }
  if (direction === 'undo') {
    undoSeq += 1
    patch(id, { busy: false, state: 'undone', undoneAt: undoSeq })
  } else {
    patch(id, { busy: false, state: 'applied' })
  }
  if (entry.rekeys) {
    staleOthers(id, entry.touches)
  }
  if (!entry.quiet) {
    const key = direction === 'undo' ? 'history.toast.undone' : 'history.toast.redone'
    toastInfo(get(t)(key).replace('{action}', entry.label))
  }
  return true
}

/** Undoes one entry. Resolves to whether it ran and succeeded. */
export function undoEntry(id: number): Promise<boolean> {
  return step(id, 'undo')
}

/** Redoes one undone entry. Resolves to whether it ran and succeeded. */
export function redoEntry(id: number): Promise<boolean> {
  return step(id, 'redo')
}

/** Undoes the newest entry that can be undone. False when there is none. */
export function undoLatest(): Promise<boolean> {
  const row = get(history).find((r) => r.canUndo)
  return row ? undoEntry(row.id) : Promise.resolve(false)
}

/** Undoes the newest entry that can be, and says so when there is none. */
export async function undoOrSay(): Promise<void> {
  if (!(await undoLatest())) {
    toastInfo(get(t)('app.toast.nothingToUndo'))
  }
}

/** Redoes the last undone entry that can be, and says so when there is none. */
export async function redoOrSay(): Promise<void> {
  if (!(await redoLatest())) {
    toastInfo(get(t)('history.toast.nothingToRedo'))
  }
}

/** Redoes the entry that was undone last and can be redone. False when there is none. */
export function redoLatest(): Promise<boolean> {
  const list = get(entries)
  const ready = new Set(get(history).filter((r) => r.canRedo).map((r) => r.id))
  const next = list.filter((e) => ready.has(e.id)).sort((a, b) => b.undoneAt - a.undoneAt)[0]
  return next ? redoEntry(next.id) : Promise.resolve(false)
}
