import { afterEach, describe, expect, it, vi } from 'vitest'
import { PROGRESS_SWEEP_MS, PROGRESS_TTL_MS, startProgressSweep, applyProgress, applyProgressEvent, applySyncState, currentProgress, currentVerify, isSyncing, type ProgressState, type SyncView } from './syncprogress'
import type { SyncProgressEvent } from './events'

function verifyEv(accountId: number, folder: string): SyncProgressEvent {
  return { ...ev(accountId, folder), phase: 'verify', foldersDone: 1, foldersTotal: 3 }
}

function ev(accountId: number, folder: string): SyncProgressEvent {
  return {
    accountId,
    accountEmail: `a${accountId}@example.com`,
    server: 'srv',
    folder,
    done: 0,
    total: 0,
    folderDone: 0,
    folderTotal: 0,
    foldersDone: 0,
    foldersTotal: 0,
  }
}

describe('syncprogress', () => {
  it('falls back to the other account when one closes', () => {
    let s: ProgressState = new Map()
    s = applyProgress(s, ev(1, 'INBOX'), 1)
    s = applyProgress(s, ev(2, 'Sent'), 2)
    s = applyProgress(s, ev(1, ''), 3)
    expect(currentProgress(s)?.accountId).toBe(2)
  })

  it('is null when only closing events arrived', () => {
    let s: ProgressState = new Map()
    s = applyProgress(s, ev(1, ''), 1)
    expect(currentProgress(s)).toBeNull()
  })

  it('picks the most recently updated running account', () => {
    let s: ProgressState = new Map()
    s = applyProgress(s, ev(1, 'INBOX'), 1)
    s = applyProgress(s, ev(2, 'Sent'), 2)
    expect(currentProgress(s)?.accountId).toBe(2)
    s = applyProgress(s, ev(1, 'Drafts'), 3)
    expect(currentProgress(s)?.folder).toBe('Drafts')
  })

  it('does not mutate its input', () => {
    const s: ProgressState = new Map()
    const next = applyProgress(s, ev(1, 'INBOX'), 1)
    expect(s.size).toBe(0)
    applyProgress(next, ev(1, ''), 2)
    expect(next.size).toBe(1)
  })

  it('keeps the line and spinner for B when A finishes first', () => {
    let v: SyncView = { progress: new Map(), verify: new Map(), running: false }
    v = { ...v, progress: applyProgress(v.progress, ev(1, 'INBOX'), 1) }
    v = { ...v, progress: applyProgress(v.progress, ev(2, 'Sent'), 2) }
    v = applySyncState(v, false, 5)
    v = { ...v, progress: applyProgress(v.progress, ev(1, ''), 3) }
    expect(currentProgress(v.progress)?.accountId).toBe(2)
    expect(isSyncing(v)).toBe(true)
    v = { ...v, progress: applyProgress(v.progress, ev(2, ''), 4) }
    expect(currentProgress(v.progress)).toBeNull()
    v = applySyncState(v, false, 5)
    expect(isSyncing(v)).toBe(false)
  })

  it('stops syncing when the last run ends before its closing event', () => {
    let v: SyncView = { progress: new Map(), verify: new Map(), running: true }
    v = { ...v, progress: applyProgress(v.progress, ev(1, 'INBOX'), 1) }
    v = { ...v, progress: applyProgress(v.progress, ev(1, ''), 2) }
    v = applySyncState(v, false, 5)
    expect(isSyncing(v)).toBe(false)
  })

  it('drops an account that went silent past the TTL', () => {
    let v: SyncView = { progress: new Map(), verify: new Map(), running: false }
    v = { ...v, progress: applyProgress(v.progress, ev(1, 'INBOX'), 1000) }
    v = applySyncState(v, false, 1000 + PROGRESS_TTL_MS + 1)
    expect(isSyncing(v)).toBe(false)
    expect(currentProgress(v.progress)).toBeNull()
  })

  it('expires the silent account when another one reports', () => {
    let s: ProgressState = new Map()
    s = applyProgress(s, ev(1, 'INBOX'), 0)
    s = applyProgress(s, ev(2, 'Sent'), PROGRESS_TTL_MS + 1)
    expect(s.has(1)).toBe(false)
    expect(s.has(2)).toBe(true)
  })

  it('keeps an account that is still within the TTL', () => {
    let v: SyncView = { progress: new Map(), verify: new Map(), running: false }
    v = { ...v, progress: applyProgress(v.progress, ev(1, 'INBOX'), 1000) }
    v = applySyncState(v, false, 1000 + PROGRESS_TTL_MS)
    expect(isSyncing(v)).toBe(true)
  })

  describe('sweep', () => {
    afterEach(() => vi.useRealTimers())

    it('clears a stale entry with no further events', () => {
      vi.useFakeTimers()
      vi.setSystemTime(1_000_000)
      let view: SyncView = { progress: applyProgress(new Map(), ev(1, 'INBOX'), Date.now()), verify: new Map(), running: false }
      const seen: boolean[] = []
      const stop = startProgressSweep(
        () => view,
        (v) => (view = v),
        (v) => seen.push(isSyncing(v)),
      )
      vi.advanceTimersByTime(PROGRESS_TTL_MS)
      expect(isSyncing(view)).toBe(true)
      vi.advanceTimersByTime(PROGRESS_SWEEP_MS)
      expect(isSyncing(view)).toBe(false)
      expect(currentProgress(view.progress)).toBeNull()
      expect(seen[seen.length - 1]).toBe(false)
      stop()
      const n = seen.length
      vi.advanceTimersByTime(PROGRESS_SWEEP_MS * 3)
      expect(seen.length).toBe(n)
    })
  })

  describe('folder check (verify)', () => {
    const empty = (): SyncView => ({ progress: new Map(), verify: new Map(), running: false })

    it('is not syncing while only the folder check runs', () => {
      const v = applyProgressEvent(empty(), verifyEv(1, 'Archive'), 1)
      expect(isSyncing(v)).toBe(false)
      expect(currentProgress(v.progress)).toBeNull()
      expect(currentVerify(v)?.folder).toBe('Archive')
    })

    it('keeps the sync line and the check line of one account apart', () => {
      let v = applyProgressEvent(empty(), ev(1, 'INBOX'), 1)
      v = applyProgressEvent(v, verifyEv(1, 'Archive'), 2)
      expect(currentProgress(v.progress)?.folder).toBe('INBOX')
      expect(isSyncing(v)).toBe(true)
      v = applyProgressEvent(v, verifyEv(1, ''), 3)
      expect(currentVerify(v)).toBeNull()
      expect(currentProgress(v.progress)?.folder).toBe('INBOX')
      v = applyProgressEvent(v, verifyEv(1, 'Sent'), 4)
      v = applyProgressEvent(v, ev(1, ''), 5)
      expect(isSyncing(v)).toBe(false)
      expect(currentVerify(v)?.folder).toBe('Sent')
    })

    it('drops a silent check line past the TTL', () => {
      let v = applyProgressEvent(empty(), verifyEv(1, 'Archive'), 1000)
      v = applySyncState(v, false, 1000 + PROGRESS_TTL_MS + 1)
      expect(currentVerify(v)).toBeNull()
    })
  })
})
