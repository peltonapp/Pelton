// swipe.ts recognises a two-finger horizontal swipe on a message row from
// the trackpad's wheel events, as pure functions so the rules can be tested
// without a trackpad.
//
// A wheel event carries no phase, so the recogniser infers one:
// - a gesture starts after a pause in the events and its axis is decided once
//   it has moved a few pixels. Only a clearly horizontal start becomes a
//   swipe; a vertical or diagonal one is a scroll for the rest of the burst,
//   so scrolling the list is never taken over.
// - once the fingers lift, the system keeps sending a decaying tail of
//   momentum events. A run of shrinking deltas well below the gesture's peak
//   reads as that tail, and from then on the row stays where the fingers left
//   it.
// - the gesture ends when the events stop for a moment; the caller runs that
//   timer and asks swipeOutcome what the swipe came to.

/** The knobs of the recogniser. */
export interface SwipeTuning {
  /** Movement in px before the axis is decided. */
  lockDistance: number
  /** How many times larger the horizontal movement must be than the vertical. */
  axisRatio: number
  /** Fraction of the row width a short swipe has to reach. */
  shortFraction: number
  /** Fraction of the row width a long swipe has to reach. */
  longFraction: number
  /** A pause in ms that ends a gesture; the next event starts a new one. */
  gapMs: number
  /** Consecutive shrinking deltas that read as the momentum tail. */
  decayEvents: number
}

/** The tuning the message list uses. */
export const swipeTuning: SwipeTuning = {
  lockDistance: 10,
  axisRatio: 2,
  shortFraction: 0.28,
  longFraction: 0.62,
  gapMs: 180,
  decayEvents: 5,
}

/** Where a gesture stands. */
export type SwipePhase = 'idle' | 'deciding' | 'horizontal' | 'vertical' | 'coasting'

/** The recogniser's state between events. */
export interface SwipeState {
  phase: SwipePhase
  /** Movement summed while the axis is undecided. */
  sumX: number
  sumY: number
  /** How far the row is moved, in px; negative is to the left. */
  offset: number
  /** Time of the last event, in ms. */
  lastAt: number
  /** Size of the last horizontal delta, and the largest one of the gesture. */
  lastAbs: number
  peakAbs: number
  /** How many deltas in a row were smaller than the one before. */
  shrinking: number
}

/** A recogniser that has seen nothing yet. */
export const idleSwipe: SwipeState = {
  phase: 'idle',
  sumX: 0,
  sumY: 0,
  offset: 0,
  lastAt: 0,
  lastAbs: 0,
  peakAbs: 0,
  shrinking: 0,
}

/** One wheel event: its deltas in css px and its time in ms. */
export interface WheelSample {
  dx: number
  dy: number
  t: number
}

/** The state after an event, and whether the event belongs to the swipe. */
export interface SwipeStep {
  state: SwipeState
  /** True when the caller should prevent the event's default scrolling. */
  consume: boolean
}

// rubberBand follows the fingers 1:1 across the row and resists past it, so
// the row never slides clean away.
function rubberBand(v: number, max: number): number {
  const abs = Math.abs(v)
  if (abs <= max) {
    return v
  }
  return Math.sign(v) * (max + (abs - max) * 0.2)
}

/**
 * Feeds one wheel event to the recogniser. width is the row's width in css
 * px; the offset is kept within a little more than it.
 */
export function swipeWheel(prev: SwipeState, sample: WheelSample, width: number, tuning: SwipeTuning = swipeTuning): SwipeStep {
  let state = prev
  if (state.phase !== 'idle' && sample.t - state.lastAt > tuning.gapMs) {
    state = idleSwipe
  }
  switch (state.phase) {
    case 'idle':
    case 'deciding': {
      const sumX = state.sumX + sample.dx
      const sumY = state.sumY + sample.dy
      if (Math.hypot(sumX, sumY) < tuning.lockDistance) {
        return { state: { ...state, phase: 'deciding', sumX, sumY, lastAt: sample.t }, consume: false }
      }
      if (Math.abs(sumX) < Math.abs(sumY) * tuning.axisRatio) {
        return { state: { ...idleSwipe, phase: 'vertical', lastAt: sample.t }, consume: false }
      }
      const abs = Math.abs(sample.dx)
      return {
        state: {
          ...idleSwipe,
          phase: 'horizontal',
          offset: rubberBand(-sumX, width),
          lastAt: sample.t,
          lastAbs: abs,
          peakAbs: abs,
        },
        consume: true,
      }
    }
    case 'vertical':
      return { state: { ...state, lastAt: sample.t }, consume: false }
    case 'coasting':
      return { state: { ...state, lastAt: sample.t }, consume: true }
    case 'horizontal': {
      const abs = Math.abs(sample.dx)
      const peakAbs = Math.max(state.peakAbs, abs)
      const shrinking = abs < state.lastAbs && abs < peakAbs * 0.85 ? state.shrinking + 1 : 0
      if (shrinking >= tuning.decayEvents) {
        return { state: { ...state, phase: 'coasting', lastAt: sample.t, shrinking }, consume: true }
      }
      return {
        state: {
          ...state,
          offset: rubberBand(state.offset - sample.dx, width),
          lastAt: sample.t,
          lastAbs: abs,
          peakAbs,
          shrinking,
        },
        consume: true,
      }
    }
  }
}

/** Whether the row is being swiped, so it should follow the offset. */
export function swiping(state: SwipeState): boolean {
  return state.phase === 'horizontal' || state.phase === 'coasting'
}

/** What each swipe does: a row action id or 'none'. */
export interface SwipeActions {
  leftShort: string
  leftLong: string
  rightShort: string
  rightLong: string
}

/** What a swipe comes to at a given offset. */
export interface SwipeOutcome {
  /** The way the row moves on screen. */
  dir: 'left' | 'right'
  /** How far it got: not far enough, past the short mark, or past the long one. */
  stage: 'none' | 'short' | 'long'
  /** The action that fires on release, or 'none'. */
  action: string
}

/**
 * Says what a swipe that moved the row by offset would do if released now.
 * The actions are named for a left to right interface; with rtl the
 * directions mirror, so a swipe toward the inline end does the same in both.
 * A long swipe with nothing set for it does the short one; a short swipe set
 * to nothing does nothing.
 */
export function swipeOutcome(offset: number, width: number, actions: SwipeActions, rtl: boolean, tuning: SwipeTuning = swipeTuning): SwipeOutcome {
  const dir = offset < 0 ? 'left' : 'right'
  const logical = rtl ? (dir === 'left' ? 'right' : 'left') : dir
  const short = logical === 'left' ? actions.leftShort : actions.rightShort
  const long = logical === 'left' ? actions.leftLong : actions.rightLong
  const reach = width > 0 ? Math.abs(offset) / width : 0
  if (reach >= tuning.longFraction && long !== 'none') {
    return { dir, stage: 'long', action: long }
  }
  if (reach >= tuning.shortFraction && short !== 'none') {
    return { dir, stage: 'short', action: short }
  }
  return { dir, stage: 'none', action: 'none' }
}

/** Whether any swipe is set to do something. */
export function anySwipe(actions: SwipeActions): boolean {
  return [actions.leftShort, actions.leftLong, actions.rightShort, actions.rightLong].some((a) => a !== 'none')
}
