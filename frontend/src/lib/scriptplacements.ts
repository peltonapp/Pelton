// scriptplacements.ts is how the app's surfaces offer scripts: the context
// menus, toolbars, the command palette, shortcuts and the row actions. Which
// scripts show where comes from the backend's placements, so a surface only
// names its placement id and hands over what it was used on; it never knows
// a script. Every context menu opened with a context gets a "scripts"
// section from here.

import { derived, get, type Readable } from 'svelte/store'
import { registerMenuSection, type MenuContext, type MenuEntry } from '../stores/contextmenu'
import { t } from './i18n'
import { scriptText } from './scriptmark'
import { selectedIds } from '../stores/listselect'
import { placedScripts, runPlaced } from '../stores/scripts'
import { visibleMessageId } from '../stores/tabs'
import type { PlacedItem, PlacedScript } from './scripting'

/** The scripts offered at a placement, kept current. */
export function placed(placement: string): Readable<PlacedScript[]> {
  return derived(placedScripts, ($p) => $p[placement] ?? [])
}

/** The scripts offered at a placement right now. */
export function placedNow(placement: string): PlacedScript[] {
  return get(placedScripts)[placement] ?? []
}

/** Messages by id as the items a placement hands over. */
export function messageItems(ids: readonly number[]): PlacedItem[] {
  return ids.map((id) => ({ id: String(id) }))
}

/** The messages the user is working on: the list's selection, else the open message, else none. */
export function currentMessageItems(): PlacedItem[] {
  const picked = [...get(selectedIds)]
  if (picked.length > 0) {
    return messageItems(picked)
  }
  const open = get(visibleMessageId)
  return open ? messageItems([open]) : []
}

/** Runs a placed script on the items, reporting the outcome as a toast. */
export function runAt(placement: string, scriptId: number, items: PlacedItem[]): void {
  void runPlaced(scriptId, placement, items)
}

/** The scripts placed in a menu as their own titled group, nothing when there are none. */
export function scriptMenuEntries(ctx: MenuContext): MenuEntry[] {
  const list = placedNow(ctx.place)
  if (list.length === 0) {
    return []
  }
  return [
    {
      kind: 'group',
      label: get(t)('scripts.menu.heading'),
      items: list.map((s) => ({
        label: scriptText(s.name),
        script: s.name,
        action: () => runAt(ctx.place, s.scriptId, ctx.items),
      })),
    },
  ]
}

registerMenuSection(scriptMenuEntries)
