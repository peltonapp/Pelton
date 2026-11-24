// setuprun.ts carries a request to run the setup flow again from places that
// cannot reach the app shell directly, such as the command palette.

import { writable } from 'svelte/store'

/** Set when something asked for the setup flow; the shell opens it and clears this. */
export const setupRequested = writable(false)

/** Asks the shell to open the setup flow again. */
export function requestSetup(): void {
  setupRequested.set(true)
}
