// sendersections.ts is where another part of the app adds a section to the
// reader's sender panel without the panel importing it. The calendar registers
// its "upcoming events you share" section here; until something registers,
// the panel shows nothing extra.

import { writable } from 'svelte/store'
import type { ComponentType } from 'svelte'

/**
 * One extra section. The component is given `address` (the bare sender
 * address) and `name` (the display name, possibly empty) as props.
 */
export interface SenderSection {
  id: string
  component: ComponentType
}

/** The registered sections, in registration order. */
export const senderSections = writable<SenderSection[]>([])

/** Adds a section; registering an id again replaces it. */
export function registerSenderSection(section: SenderSection): void {
  senderSections.update((list) => [...list.filter((s) => s.id !== section.id), section])
}
