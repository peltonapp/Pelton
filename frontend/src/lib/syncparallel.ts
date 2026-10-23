// How many connections one mailbox may keep open to its server at once. The
// number counts every connection, the one that listens for new mail
// included. The ui offers 2..10 (default 3); the backend accepts 1..10, and
// the throttle may lower the running number to 1 when the server refuses.
// A stored 1 shows as 2 and is saved only when the user changes it.
export const SYNC_PARALLEL_MIN = 2
export const SYNC_PARALLEL_MAX = 10
export const SYNC_PARALLEL_DEFAULT = 3

// clampSyncParallel keeps a connection count inside the ui's 2..10. A stored
// 1, or anything that is not a number, lands on a step the slider offers.
export function clampSyncParallel(value: number): number {
  if (!Number.isFinite(value)) return SYNC_PARALLEL_DEFAULT
  return Math.min(SYNC_PARALLEL_MAX, Math.max(SYNC_PARALLEL_MIN, Math.round(value)))
}

// syncParallelOptions are the steps the global and per-mailbox sliders offer.
export const syncParallelOptions: { key: string; label: string }[] = Array.from(
  { length: SYNC_PARALLEL_MAX - SYNC_PARALLEL_MIN + 1 },
  (_, i) => {
    const n = String(SYNC_PARALLEL_MIN + i)
    return { key: n, label: n }
  },
)
