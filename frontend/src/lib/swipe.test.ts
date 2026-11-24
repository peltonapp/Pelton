import { describe, expect, it } from 'vitest'
import { anySwipe, idleSwipe, swipeOutcome, swipeTuning, swipeWheel, swiping, type SwipeState, type WheelSample } from './swipe'

const width = 400

// feed runs samples through the recogniser and reports the last state and
// which samples it claimed.
function feed(samples: WheelSample[], start: SwipeState = idleSwipe): { state: SwipeState; consumed: boolean[] } {
  let state = start
  const consumed: boolean[] = []
  for (const s of samples) {
    const step = swipeWheel(state, s, width)
    state = step.state
    consumed.push(step.consume)
  }
  return { state, consumed }
}

// burst makes n evenly spaced samples, 16ms apart from t0.
function burst(n: number, dx: number, dy: number, t0 = 1000): WheelSample[] {
  return Array.from({ length: n }, (_, i) => ({ dx, dy, t: t0 + i * 16 }))
}

const actions = { leftShort: 'archive', leftLong: 'delete', rightShort: 'read', rightLong: 'snooze' }

describe('swipeWheel axis lock', () => {
  it('does not claim the first few pixels before the axis is decided', () => {
    const { state, consumed } = feed(burst(2, 3, 0))
    expect(state.phase).toBe('deciding')
    expect(consumed).toEqual([false, false])
  })

  it('locks onto a clearly horizontal start and moves the row against the delta', () => {
    const { state, consumed } = feed(burst(6, 4, 0.5))
    expect(state.phase).toBe('horizontal')
    expect(state.offset).toBeLessThan(0)
    expect(consumed.at(-1)).toBe(true)
    expect(swiping(state)).toBe(true)
  })

  it('leaves a vertical scroll alone for the whole burst, even when it turns sideways', () => {
    const first = feed(burst(4, 0.5, 6))
    expect(first.state.phase).toBe('vertical')
    const later = feed(burst(10, 20, 0, 1000 + 4 * 16), first.state)
    expect(later.state.phase).toBe('vertical')
    expect(later.consumed.every((c) => !c)).toBe(true)
    expect(later.state.offset).toBe(0)
  })

  it('never treats a diagonal scroll as a swipe', () => {
    const { state, consumed } = feed(burst(10, 6, 5))
    expect(state.phase).toBe('vertical')
    expect(consumed.every((c) => !c)).toBe(true)
  })

  it('starts a new gesture after a pause', () => {
    const scroll = feed(burst(4, 0, 8))
    const resumed = feed(burst(6, -5, 0, 1000 + 4 * 16 + swipeTuning.gapMs + 50), scroll.state)
    expect(resumed.state.phase).toBe('horizontal')
    expect(resumed.state.offset).toBeGreaterThan(0)
  })
})

describe('swipeWheel momentum', () => {
  it('freezes the row once the deltas decay like a momentum tail', () => {
    const drag = feed(burst(10, 12, 0))
    const at = drag.state.offset
    let t = 1000 + 10 * 16
    const tail: WheelSample[] = []
    for (let d = 9; d > 0.5; d *= 0.8) {
      tail.push({ dx: d, dy: 0, t: (t += 16) })
    }
    const after = feed(tail, drag.state)
    expect(after.state.phase).toBe('coasting')
    expect(after.consumed.every(Boolean)).toBe(true)
    // the first shrinking deltas still move it a little before the tail is
    // recognised, never the whole tail.
    const tailTotal = tail.reduce((sum, s) => sum + s.dx, 0)
    expect(Math.abs(after.state.offset - at)).toBeLessThan(tailTotal * 0.75)
  })

  it('keeps following a steady drag', () => {
    const { state } = feed(burst(20, 6, 0))
    expect(state.phase).toBe('horizontal')
  })

  it('resists past the row width', () => {
    const { state } = feed(burst(60, 20, 0))
    expect(Math.abs(state.offset)).toBeGreaterThan(width)
    expect(Math.abs(state.offset)).toBeLessThan(width * 1.25)
  })
})

describe('swipeOutcome', () => {
  it('fires nothing short of the first mark', () => {
    expect(swipeOutcome(-width * 0.1, width, actions, false)).toEqual({ dir: 'left', stage: 'none', action: 'none' })
  })

  it('maps a short and a long swipe to their own actions per direction', () => {
    expect(swipeOutcome(-width * 0.4, width, actions, false).action).toBe('archive')
    expect(swipeOutcome(-width * 0.8, width, actions, false).action).toBe('delete')
    expect(swipeOutcome(width * 0.4, width, actions, false).action).toBe('read')
    expect(swipeOutcome(width * 0.8, width, actions, false)).toEqual({ dir: 'right', stage: 'long', action: 'snooze' })
  })

  it('mirrors the directions in a right to left interface', () => {
    expect(swipeOutcome(width * 0.4, width, actions, true)).toEqual({ dir: 'right', stage: 'short', action: 'archive' })
    expect(swipeOutcome(-width * 0.8, width, actions, true)).toEqual({ dir: 'left', stage: 'long', action: 'snooze' })
  })

  it('falls back to the short action when no long one is set', () => {
    const some = { ...actions, leftLong: 'none' }
    expect(swipeOutcome(-width * 0.9, width, some, false)).toEqual({ dir: 'left', stage: 'short', action: 'archive' })
  })

  it('does nothing for a short swipe set to off', () => {
    const some = { ...actions, rightShort: 'none' }
    expect(swipeOutcome(width * 0.4, width, some, false).action).toBe('none')
    expect(swipeOutcome(width * 0.8, width, some, false).action).toBe('snooze')
  })

  it('knows when every swipe is off', () => {
    expect(anySwipe(actions)).toBe(true)
    expect(anySwipe({ leftShort: 'none', leftLong: 'none', rightShort: 'none', rightLong: 'none' })).toBe(false)
  })
})
