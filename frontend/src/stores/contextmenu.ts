// contextmenu.ts drives the single custom right-click menu. components build a
// list of items and call openContextMenu with the pointer position; the
// ContextMenu component renders it and closes on outside-click, escape or scroll.
// this replaces the webview's default menu (which exposed inspect and reload).

import { writable } from 'svelte/store'
import type { ComponentType } from 'svelte'

// a menu entry, or the 'separator' marker for a divider.
export interface MenuItem {
  label: string
  action: () => void
  icon?: ComponentType
  danger?: boolean
  /**
   * The keyboard shortcut for this entry, already rendered for display. Build
   * it with menuHint so an unbound action leaves it empty and the row simply
   * has no hint.
   */
  hint?: string
}

// a color-swatch row for picking a message flag color inline. current is the
// selected color (0 = none); onPick fires with the chosen index (0 clears).
export interface MenuColors {
  kind: 'colors'
  current: number
  onPick: (color: number) => void
}

export type MenuEntry = MenuItem | 'separator' | MenuColors

export interface MenuState {
  x: number
  y: number
  entries: MenuEntry[]
}

export const contextMenu = writable<MenuState | null>(null)

/**
 * What a menu was opened on, for sections other modules add to every menu
 * of that kind: a place id, e.g. "mail.row", and the items as ids or fields.
 */
export interface MenuContext {
  place: string
  items: { id: string; fields?: Record<string, unknown> }[]
}

/** A section another module adds to menus, built from what they were opened on. */
export type MenuSection = (ctx: MenuContext) => MenuEntry[]

const sections: MenuSection[] = []

/** Adds a section to every menu opened with a context, e.g. the scripts placed there. */
export function registerMenuSection(section: MenuSection): void {
  sections.push(section)
}

// openContextMenu shows the menu at the given viewport coordinates. With a
// context, the registered sections are added below the entries, each after a
// separator; a menu with no entries of its own shows only them.
export function openContextMenu(x: number, y: number, entries: MenuEntry[], context?: MenuContext): void {
  const all = [...entries]
  for (const section of context ? sections : []) {
    const extra = section(context!)
    if (extra.length > 0) {
      all.push(...(all.length > 0 ? ['separator' as const] : []), ...extra)
    }
  }
  if (all.length === 0) {
    return
  }
  contextMenu.set({ x, y, entries: all })
}

export function closeContextMenu(): void {
  contextMenu.set(null)
}
