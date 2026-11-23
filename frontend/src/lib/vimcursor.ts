// vimcursor.ts is where an app-vim motion lands in the message list, kept out
// of App.svelte so it can be tested without mounting the whole app.

import { openMessageId } from '../stores/selection'
import { anchorAt, rangeCursor, selectRange } from '../stores/listselect'
import { autoMarkRead } from './messageactions'
import type { MessageSummary } from './types'

/**
 * Moves the cursor to a row. Extending carries the selection along from the
 * anchor and leaves the reading pane alone, so a range does not open and mark
 * read every message it passes. A plain move opens the row and makes it the
 * anchor, so a selection started from it includes it. The row is marked read
 * only if the user set a delay and the cursor stays on it that long.
 */
export function vimCursorTo(items: MessageSummary[], index: number, extend: boolean): void {
  const item = items[index]
  if (extend) {
    rangeCursor.set(item.id)
    selectRange(items.map((m) => m.id), item.id)
  } else {
    rangeCursor.set(null)
    openMessageId.set(item.id)
    anchorAt(item.id)
    autoMarkRead(item)
  }
}
