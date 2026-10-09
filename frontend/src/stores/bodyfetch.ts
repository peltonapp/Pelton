// bodyfetch.ts remembers the open message whose body the backend gave up
// fetching, so the reading pane can swap its spinner for a notice with a retry
// instead of waiting for a body that is not coming.

import { writable } from 'svelte/store'
import { bodyLoading, refreshMessage } from './message'

/** Id of the message whose body fetch failed after every retry, or null. */
export const bodyFetchFailed = writable<number | null>(null)

/** Records a failed body fetch for the open message and stops its spinner. */
export function markBodyFetchFailed(id: number): void {
  bodyFetchFailed.set(id)
  bodyLoading.set(false)
}

/** Forgets a recorded failure once that message's body has arrived. */
export function clearBodyFetchFailed(id: number): void {
  bodyFetchFailed.update((failed) => (failed === id ? null : failed))
}

/** Reloads the message, which starts a new background fetch of its body. */
export async function retryBodyFetch(id: number): Promise<void> {
  bodyFetchFailed.set(null)
  await refreshMessage(id)
}
