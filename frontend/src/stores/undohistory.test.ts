import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import {
  clearHistory,
  dropHistoryEntry,
  history,
  historyAvailable,
  historyCap,
  messageKeys,
  recordHistory,
  redoEntry,
  redoLatest,
  stateKey,
  undoEntry,
  undoLatest,
  type HistoryInput,
} from './undohistory'
import { toasts } from './toast'

function input(patch: Partial<HistoryInput> = {}): HistoryInput {
  return {
    kind: 'flag',
    label: 'Flagged',
    detail: 'Hello',
    count: 1,
    touches: [stateKey('flag', 1)],
    undo: vi.fn().mockResolvedValue(undefined),
    redo: vi.fn().mockResolvedValue(undefined),
    ...patch,
  }
}

const row = (id: number) => get(history).find((r) => r.id === id)!

beforeEach(() => {
  clearHistory()
  toasts.set([])
})

describe('recording', () => {
  it('lists the newest entry first', () => {
    const a = recordHistory(input({ label: 'A' }))
    const b = recordHistory(input({ label: 'B', touches: [stateKey('flag', 2)] }))
    expect(get(history).map((r) => r.id)).toEqual([b, a])
  })

  it('keeps the last fifty and drops the oldest', () => {
    const first = recordHistory(input({ touches: ['x:0'] }))
    for (let i = 1; i <= historyCap; i++) {
      recordHistory(input({ touches: [`x:${i}`] }))
    }
    const rows = get(history)
    expect(rows).toHaveLength(historyCap)
    expect(rows.some((r) => r.id === first)).toBe(false)
  })

  it('takes an entry out when it became permanent', () => {
    const id = recordHistory(input())
    dropHistoryEntry(id)
    expect(get(history)).toHaveLength(0)
  })
})

describe('undo', () => {
  it('runs the entry and marks it undone', async () => {
    const i = input()
    const id = recordHistory(i)
    expect(await undoEntry(id)).toBe(true)
    expect(i.undo).toHaveBeenCalledTimes(1)
    expect(row(id).state).toBe('undone')
    expect(get(toasts).at(-1)?.message).toBe('Undone: Flagged')
  })

  it('stays quiet for an action that reports itself', async () => {
    const id = recordHistory(input({ quiet: true }))
    await undoEntry(id)
    expect(get(toasts)).toHaveLength(0)
  })

  it('undoes entries on different messages in any order', async () => {
    const a = recordHistory(input({ touches: [stateKey('flag', 1)] }))
    recordHistory(input({ touches: [stateKey('flag', 2)] }))
    expect(row(a).canUndo).toBe(true)
    expect(await undoEntry(a)).toBe(true)
  })

  it('treats read and flag on the same message as independent', () => {
    const a = recordHistory(input({ kind: 'seen', touches: [stateKey('seen', 1)] }))
    recordHistory(input({ touches: [stateKey('flag', 1)] }))
    expect(row(a).canUndo).toBe(true)
  })

  it('disables an earlier entry while a later one touched the same state', async () => {
    const a = recordHistory(input())
    const b = recordHistory(input())
    expect(row(a)).toMatchObject({ canUndo: false, reason: 'later' })
    expect(await undoEntry(a)).toBe(false)
    expect(await undoEntry(b)).toBe(true)
    expect(row(a).canUndo).toBe(true)
  })

  it('disables an earlier flag while a later move holds the message', () => {
    const a = recordHistory(input())
    recordHistory(input({ kind: 'archive', touches: messageKeys(1), rekeys: true }))
    expect(row(a).reason).toBe('later')
  })

  it('marks older entries stale once an undo gave the messages new ids', async () => {
    const flag = recordHistory(input())
    const move = recordHistory(input({ kind: 'archive', touches: messageKeys(1), rekeys: true }))
    await undoEntry(move)
    expect(row(flag)).toMatchObject({ canUndo: false, reason: 'moved' })
  })

  it('does not stale entries on other messages', async () => {
    const other = recordHistory(input({ touches: [stateKey('flag', 9)] }))
    const move = recordHistory(input({ kind: 'archive', touches: messageKeys(1), rekeys: true }))
    await undoEntry(move)
    expect(row(other).canUndo).toBe(true)
  })

  it('reports a failed undo and does not offer it again', async () => {
    const id = recordHistory(input({ undo: vi.fn().mockRejectedValue(new Error('gone')) }))
    expect(await undoEntry(id)).toBe(false)
    expect(row(id)).toMatchObject({ state: 'failed', canUndo: false, reason: 'failed' })
    expect(get(toasts).at(-1)).toMatchObject({ kind: 'error', message: 'gone' })
  })

  it('ignores a second undo while the first is running', async () => {
    let release: () => void = () => {}
    const undo = vi.fn(() => new Promise<void>((resolve) => (release = resolve)))
    const id = recordHistory(input({ undo }))
    const first = undoEntry(id)
    expect(row(id).reason).toBe('busy')
    expect(await undoEntry(id)).toBe(false)
    release()
    await first
    expect(undo).toHaveBeenCalledTimes(1)
  })

  it('undoLatest takes the newest entry that can be undone', async () => {
    const a = recordHistory(input({ touches: [stateKey('flag', 1)] }))
    const b = recordHistory(input({ touches: [stateKey('flag', 2)], undo: vi.fn().mockRejectedValue(new Error('x')) }))
    await undoLatest()
    expect(row(b).state).toBe('failed')
    expect(await undoLatest()).toBe(true)
    expect(row(a).state).toBe('undone')
    expect(await undoLatest()).toBe(false)
  })
})

describe('redo', () => {
  it('replays an undone entry', async () => {
    const i = input()
    const id = recordHistory(i)
    await undoEntry(id)
    expect(row(id).canRedo).toBe(true)
    expect(await redoEntry(id)).toBe(true)
    expect(i.redo).toHaveBeenCalledTimes(1)
    expect(row(id).state).toBe('applied')
    expect(get(toasts).at(-1)?.message).toBe('Redone: Flagged')
  })

  it('cannot redo an entry that has no redo', async () => {
    const id = recordHistory(input({ redo: undefined }))
    await undoEntry(id)
    expect(row(id)).toMatchObject({ canRedo: false, reason: 'noRedo' })
  })

  it('loses the redo once a new action touches the same state', async () => {
    const a = recordHistory(input())
    await undoEntry(a)
    recordHistory(input())
    expect(row(a)).toMatchObject({ canRedo: false, reason: 'noRedo' })
  })

  it('keeps the redo when the new action is elsewhere', async () => {
    const a = recordHistory(input())
    await undoEntry(a)
    recordHistory(input({ touches: [stateKey('flag', 2)] }))
    expect(row(a).canRedo).toBe(true)
  })

  it('asks for the earlier entry to be redone first', async () => {
    const a = recordHistory(input())
    const b = recordHistory(input())
    await undoEntry(b)
    await undoEntry(a)
    expect(row(b)).toMatchObject({ canRedo: false, reason: 'earlier' })
    await redoEntry(a)
    expect(row(b).canRedo).toBe(true)
  })

  it('redoLatest takes the entry undone last', async () => {
    const i = input({ touches: [stateKey('flag', 1)] })
    const j = input({ touches: [stateKey('flag', 2)] })
    const a = recordHistory(i)
    const b = recordHistory(j)
    await undoEntry(b)
    await undoEntry(a)
    expect(await redoLatest()).toBe(true)
    expect(i.redo).toHaveBeenCalledTimes(1)
    expect(j.redo).not.toHaveBeenCalled()
    expect(row(b).state).toBe('undone')
  })

  it('restales older entries after a redo that moves messages', async () => {
    const move = recordHistory(input({ kind: 'archive', touches: messageKeys(1), rekeys: true }))
    await undoEntry(move)
    const flag = recordHistory(input({ touches: [stateKey('flag', 1)] }))
    expect(row(move).canRedo).toBe(false)
    expect(row(flag).canUndo).toBe(true)
  })
})

describe('availability', () => {
  it('follows what can be undone and redone', async () => {
    expect(get(historyAvailable)).toEqual({ undo: false, redo: false })
    const id = recordHistory(input())
    expect(get(historyAvailable)).toEqual({ undo: true, redo: false })
    await undoEntry(id)
    expect(get(historyAvailable)).toEqual({ undo: false, redo: true })
  })
})
