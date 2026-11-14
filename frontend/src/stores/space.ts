// space.ts holds which top-level area of the app is showing in the new design:
// mail, contacts or the calendar. the rail, the sidebar footer switcher and the
// top bar tabs all read and write this one store.

import { writable } from 'svelte/store'
import type { Space } from '../lib/types'

/** The area currently on screen. */
export const space = writable<Space>('mail')

/** Shows the given area. */
export function openSpace(next: Space): void {
  space.set(next)
}
