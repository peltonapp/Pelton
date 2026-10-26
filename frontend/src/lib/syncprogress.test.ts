import { afterEach, describe, expect, it, vi } from 'vitest'
import { accountPhase, PROGRESS_SWEEP_MS, PROGRESS_TTL_MS, VERIFY_TTL_MS, startProgressSweep, applyProgress, applyProgressEvent, applySyncState, currentProgress, currentVerify, isSyncing, type ProgressState, type SyncView } from './syncprogress'
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

const empty = (running = false): SyncView => ({ progress: new Map(), verify: new Map(), running })

describe('syncprogress', () => {
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

  it('does not hand the line to an account that only repeats itself', () => {
    let s: ProgressState = new Map()
    s = applyProgress(s, ev(1, 'INBOX'), 1)
    s = applyProgress(s, ev(2, 'Sent'), 2)
    // the backend heartbeat re-sends account 1's last event unchanged.
    s = applyProgress(s, ev(1, 'INBOX'), 3)
    expect(currentProgress(s)?.accountId).toBe(2)
  })

  // a heartbeat refreshes the TTL (at) but not the line (changed): the
  // account stays alive past its first event without taking the line back.
  it('keeps an account that only repeats itself alive, without the line', () => {
    let s: ProgressState = new Map()
    s = applyProgress(s, ev(1, 'INBOX'), 0)
    s = applyProgress(s, ev(2, 'Sent'), 10)
    s = applyProgress(s, ev(1, 'INBOX'), PROGRESS_TTL_MS)
    expect(currentProgress(s)?.accountId).toBe(2)
    s = applyProgress(s, ev(2, 'Sent'), PROGRESS_TTL_MS + 10)
    expect(s.has(1)).toBe(true)
    expect(currentProgress(s)?.accountId).toBe(2)
  })

  it('does not mutate its input', () => {
    const s: ProgressState = new Map()
    const next = applyProgress(s, ev(1, 'INBOX'), 1)
    expect(s.size).toBe(0)
    applyProgress(next, ev(1, ''), 2)
    expect(next.size).toBe(1)
  })

  it('keeps the line and spinner for B when A finishes first', () => {
    // also covers falling back to the other account when one closes.
    let v = empty()
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
    let v = empty(true)
    v = { ...v, progress: applyProgress(v.progress, ev(1, 'INBOX'), 1) }
    v = { ...v, progress: applyProgress(v.progress, ev(1, ''), 2) }
    v = applySyncState(v, false, 5)
    expect(isSyncing(v)).toBe(false)
  })

  it.each([
    { name: 'keeps an account that is still within the TTL', after: PROGRESS_TTL_MS, kept: true },
    { name: 'drops an account that went silent past the TTL', after: PROGRESS_TTL_MS + 1, kept: false },
  ])('$name', ({ after, kept }) => {
    let v = applyProgressEvent(empty(), ev(1, 'INBOX'), 1000)
    v = applySyncState(v, false, 1000 + after)
    expect(isSyncing(v)).toBe(kept)
    expect(currentProgress(v.progress) !== null).toBe(kept)
  })

  it('expires the silent account when another one reports', () => {
    let s: ProgressState = new Map()
    s = applyProgress(s, ev(1, 'INBOX'), 0)
    s = applyProgress(s, ev(2, 'Sent'), PROGRESS_TTL_MS + 1)
    expect(s.has(1)).toBe(false)
    expect(s.has(2)).toBe(true)
  })

  describe('sweep', () => {
    afterEach(() => vi.useRealTimers())

    it('clears a stale entry with no further events', () => {
      vi.useFakeTimers()
      vi.setSystemTime(1_000_000)
      let view: SyncView = { ...empty(), progress: applyProgress(new Map(), ev(1, 'INBOX'), Date.now()) }
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

    it('drops a silent check line past its TTL', () => {
      let v = applyProgressEvent(empty(), verifyEv(1, 'Archive'), 1000)
      v = applySyncState(v, false, 1000 + VERIFY_TTL_MS + 1)
      expect(currentVerify(v)).toBeNull()
    })

    // the backend re-sends the check line while a long folder is checked,
    // so its line lives exactly as long as a sync line does.
    it('drops a check line silent past the common TTL', () => {
      let v = applyProgressEvent(empty(), verifyEv(1, 'Archive'), 1000)
      v = applySyncState(v, false, 1000 + PROGRESS_TTL_MS + 1)
      expect(currentVerify(v)).toBeNull()
    })
  })
})

describe('accountPhase', () => {
  // mail:new from one account must not be judged by another account's phase.
  it('reads the phase of the given account, not of the last event', () => {
    let v = empty()
    v = applyProgressEvent(v, { ...ev(1, 'INBOX'), phase: 'bodies' }, 1)
    v = applyProgressEvent(v, { ...ev(2, 'INBOX'), phase: 'stubs' }, 2)
    expect(accountPhase(v, 1)).toBe('bodies')
    expect(accountPhase(v, 2)).toBe('stubs')
    expect(accountPhase(v, 3)).toBe('')
  })
})
