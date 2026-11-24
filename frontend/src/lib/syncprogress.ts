import type { SyncProgressEvent } from './events'
import type { SyncPhase } from './syncstatus'

/** The latest progress event of one running account and when it arrived. */
export interface ProgressEntry {
  accountId: number
  event: SyncProgressEvent
  // when an event last arrived, heartbeat repeats included; the TTL reads this.
  at: number
  // when the event last said something new. A heartbeat re-sends the same
  // event, so it keeps the entry alive without making it the newest.
  changed: number
}

/** Running accounts by id. */
export type ProgressState = Map<number, ProgressEntry>

/** How long an account may go without a progress event before it counts as gone. */
export const PROGRESS_TTL_MS = 2 * 60 * 1000

/**
 * The same for the folder check line. The backend re-sends its latest event
 * while checks run (every 45 s), as it does for the sync line, so a long
 * silent folder keeps it alive; this bounds how long a lost closing event
 * could pin the line.
 */
export const VERIFY_TTL_MS = 2 * 60 * 1000

/** Copy of state without entries not updated within ttl. */
function prune(state: ProgressState, now: number, ttl = PROGRESS_TTL_MS): ProgressState {
  const next = new Map(state)
  for (const [id, entry] of next) {
    if (now - entry.at > ttl) next.delete(id)
  }
  return next
}

function pruneVerify(state: ProgressState, now: number): ProgressState {
  return prune(state, now, VERIFY_TTL_MS)
}

/**
 * Returns a new state with the event applied: a running event upserts its
 * account, a closing event (empty folder) removes it. Accounts sync
 * concurrently, so one account closing must not clear another's line. Entries
 * silent for longer than the TTL are dropped, so a closing event that never
 * arrived cannot pin the line.
 */
export function applyProgress(state: ProgressState, e: SyncProgressEvent, now: number, ttl = PROGRESS_TTL_MS): ProgressState {
  const next = prune(state, now, ttl)
  if (e.folder === '') {
    next.delete(e.accountId)
  } else {
    const prev = next.get(e.accountId)
    const repeat = prev !== undefined && JSON.stringify(prev.event) === JSON.stringify(e)
    next.set(e.accountId, { accountId: e.accountId, event: e, at: now, changed: repeat ? prev.changed : now })
  }
  return next
}

/**
 * The event of the running account that last made progress, or null when none
 * runs. Heartbeats do not count, or two syncing accounts would swap the line
 * every time one of them repeats itself.
 */
export function currentProgress(state: ProgressState): SyncProgressEvent | null {
  let best: ProgressEntry | null = null
  for (const entry of state.values()) {
    if (!best || entry.changed >= best.changed) best = entry
  }
  return best ? best.event : null
}

/** The sync phase of one account, or '' when it is not syncing. */
export function accountPhase(view: SyncView, accountId: number): SyncPhase {
  return view.progress.get(accountId)?.event.phase ?? ''
}

/**
 * Progress plus the last sync:state flag. That event carries no account id and
 * fires once per account run, so on its own it cannot say whether everything
 * is idle. The background folder check (phase verify) is kept apart in verify:
 * it is a separate, calm line that does not count as syncing, and an account
 * can run a sync and a check at once.
 */
export interface SyncView {
  progress: ProgressState
  verify: ProgressState
  running: boolean
}

/** Records a sync:progress event: a verify event goes to the check line, any other to the sync line. */
export function applyProgressEvent(view: SyncView, e: SyncProgressEvent, now: number): SyncView {
  if (e.phase === 'verify') {
    return { ...view, progress: prune(view.progress, now), verify: applyProgress(view.verify, e, now, VERIFY_TTL_MS) }
  }
  return { ...view, progress: applyProgress(view.progress, e, now), verify: pruneVerify(view.verify, now) }
}

/** The most recently updated folder check, or null when none runs. */
export function currentVerify(view: SyncView): SyncProgressEvent | null {
  return currentProgress(view.verify)
}

/** Records a sync:state event; it only expires stale progress, never clears live accounts. */
export function applySyncState(view: SyncView, running: boolean, now: number): SyncView {
  return { progress: prune(view.progress, now), verify: pruneVerify(view.verify, now), running }
}

/** The view without accounts that have been silent past the TTL. */
function pruneView(view: SyncView, now: number): SyncView {
  return { ...view, progress: prune(view.progress, now), verify: pruneVerify(view.verify, now) }
}

/** How often the sweep looks for silent accounts. */
export const PROGRESS_SWEEP_MS = 30 * 1000

/**
 * Prunes the view on a timer, so a lost closing event cannot pin the line when
 * no other event arrives. publish runs after every sweep; returns the stop function.
 */
export function startProgressSweep(
  get: () => SyncView,
  set: (view: SyncView) => void,
  publish: (view: SyncView) => void,
): () => void {
  const id = setInterval(() => {
    const next = pruneView(get(), Date.now())
    set(next)
    publish(next)
  }, PROGRESS_SWEEP_MS)
  return () => clearInterval(id)
}

/** True while the last sync:state said running or any account still reports sync progress. The folder check does not count, so the Sync button stops after the delta. */
export function isSyncing(view: SyncView): boolean {
  return view.running || view.progress.size > 0
}
