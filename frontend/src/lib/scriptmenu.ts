// scriptmenu.ts opens the right-click menu of a surface that has no menu of
// its own, so the only entries are the scripts placed there.

import { openContextMenu } from '../stores/contextmenu'
import type { PlacedItem, PlacedScript } from './scripting'
import { placedNow } from './scriptplacements'

/** The text of a placed script's button: its label option, else the script's name. */
export function placedLabel(script: PlacedScript): string {
  const label = script.options.label
  return typeof label === 'string' && label.trim() !== '' ? label.trim() : script.name
}

/**
 * Opens the menu of the scripts placed at a place for the item under the
 * pointer, and reports whether it did. Nothing opens, and the event is left
 * to whatever else handles it, when no script is placed there.
 */
export function openScriptMenu(event: MouseEvent, place: string, item: PlacedItem): boolean {
  if (placedNow(place).length === 0) {
    return false
  }
  event.preventDefault()
  event.stopPropagation()
  openContextMenu(event.clientX, event.clientY, [], { place, items: [item] })
  return true
}
