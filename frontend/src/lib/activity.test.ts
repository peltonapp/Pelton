import { describe, expect, it, vi } from 'vitest'
import { watchActivity } from './activity'

describe('watchActivity', () => {
  it('reports input at most once per interval and stops when asked', () => {
    let now = 1_000
    const report = vi.fn()
    const stop = watchActivity(report, 20_000, () => now)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    window.dispatchEvent(new Event('pointermove'))
    expect(report).toHaveBeenCalledTimes(1)

    now += 19_999
    window.dispatchEvent(new Event('pointerdown'))
    expect(report).toHaveBeenCalledTimes(1)

    now += 1
    window.dispatchEvent(new Event('wheel'))
    expect(report).toHaveBeenCalledTimes(2)

    stop()
    now += 60_000
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    expect(report).toHaveBeenCalledTimes(2)
  })
})
