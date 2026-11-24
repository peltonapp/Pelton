// activity.ts reports that a window is in use, for the app lock's idle time.
// Input reaches the backend at most once per interval, so moving the pointer
// around costs one call every so often rather than one per event.

/** The input events that count as using the app. */
const activityEvents = ['keydown', 'pointerdown', 'pointermove', 'wheel'] as const

/** How often, at most, activity is reported. */
export const activityInterval = 20_000

/**
 * watchActivity calls report on input in the window, at most once per
 * interval, and returns the function that stops watching. now is the clock,
 * replaceable for tests.
 */
export function watchActivity(report: () => void, interval = activityInterval, now: () => number = Date.now): () => void {
  let last = -Infinity
  const onInput = (): void => {
    const at = now()
    if (at - last < interval) {
      return
    }
    last = at
    report()
  }
  for (const name of activityEvents) {
    window.addEventListener(name, onInput, { capture: true, passive: true })
  }
  return () => {
    for (const name of activityEvents) {
      window.removeEventListener(name, onInput, { capture: true })
    }
  }
}
