// readingview.ts is whether the reading view is on: the open conversation on
// its own, over the rest of the window, set for reading. Its shortcut and the
// reader's button turn it on; Escape, the same shortcut or the message going
// away turn it off.

import { writable } from 'svelte/store'

/** Whether the reading view is showing. */
export const readingView = writable(false)

/** Shows the open conversation in the reading view. */
export function openReadingView(): void {
  readingView.set(true)
}

/** Leaves the reading view. */
export function closeReadingView(): void {
  readingView.set(false)
}

/** Turns the reading view on or off. */
export function toggleReadingView(): void {
  readingView.update((on) => !on)
}
