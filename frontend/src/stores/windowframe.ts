// windowframe.ts knows whether the main window draws its own title bar. the
// backend decides that from the design and the title bar setting, and on
// Windows and Linux can change it while running, so the answer is asked for
// again after either setting changes.

import { writable } from 'svelte/store'
import { windowFrameIntegrated } from '../lib/api'

/** True while the interface, not the operating system, is the title bar. */
export const frameIntegrated = writable(true)

/** Asks the backend which frame the window has now. */
export async function refreshWindowFrame(): Promise<void> {
  try {
    frameIntegrated.set(await windowFrameIntegrated())
  } catch {
    // the demo has no window to ask and keeps the integrated bar.
  }
}
