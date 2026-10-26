import { describe, expect, it } from 'vitest'
import { SYNC_PARALLEL_DEFAULT, clampSyncParallel, syncParallelOptions } from './syncparallel'

describe('sync parallel limits', () => {
  it('offers 2 to 10, without 1', () => {
    expect(syncParallelOptions.map((o) => o.key)).toEqual(['2', '3', '4', '5', '6', '7', '8', '9', '10'])
    expect(syncParallelOptions.every((o) => o.label === o.key)).toBe(true)
  })

  it('defaults to 3, one of the options', () => {
    expect(SYNC_PARALLEL_DEFAULT).toBe(3)
    expect(syncParallelOptions.map((o) => o.key)).toContain(String(SYNC_PARALLEL_DEFAULT))
  })

  it.each([
    [1, 2],
    [0, 2],
    [-3, 2],
    [2, 2],
    [3, 3],
    [10, 10],
    [11, 10],
    [99, 10],
    [Number.NaN, 3],
  ])('clamps %d to %d', (input, want) => {
    expect(clampSyncParallel(input)).toBe(want)
  })
})
