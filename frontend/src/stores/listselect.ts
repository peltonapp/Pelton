// listselect.ts holds the message-list multi-selection: the set of selected
// message ids and the anchor row for shift-range selection. this is ephemeral ui
// state, cleared whenever the list changes (folder switch, search, delete), so an
// in-memory store is correct here. the "open" message (detail pane) is tracked
// separately in selection.ts; this is only the bulk-action set.

import { writable, get } from 'svelte/store'

// the currently selected message ids. empty means no multi-selection is active.
export const selectedIds = writable<Set<number>>(new Set())

// the anchor row id for shift-click range selection, or null when there is none.
let anchorId: number | null = null

// what was selected when the anchor was set. A shift-click adds its range to
// this rather than to whatever the previous shift-click produced, so moving the
// shift target shrinks the range as well as growing it, and a range started
// after a cmd-click keeps what the cmd-clicks had picked.
let anchored: Set<number> = new Set()

// what the "select the rest too" banner is offering, or null when there is
// nothing to offer. It lives here rather than with the rest of the select-all
// logic so that clearing the selection clears it too, whoever does the
// clearing: an offer left standing over a list nobody selected is a lie.
export const expandOffer = writable<{ loaded: number; matching: number } | null>(null)

/**
 * Whether a vim-mode visual selection is running. While it is, moving the
 * cursor extends the range from the anchor instead of only moving. It ends with
 * the selection, so a bulk action or a folder switch never leaves the next
 * motion quietly selecting rows.
 */
export const visualMode = writable(false)

/**
 * The row a keyboard range ends on in app vim mode, or null while the cursor is
 * simply the open message. Extending a range moves this instead of opening
 * every message on the way, which would mark each of them read.
 */
export const rangeCursor = writable<number | null>(null)

// clearSelection drops every selection and the anchor. called on folder/search
// changes so a stale selection never lingers across lists.
export function clearSelection(): void {
  anchorId = null
  anchored = new Set()
  visualMode.set(false)
  rangeCursor.set(null)
  selectedIds.set(new Set())
  expandOffer.set(null)
}

// anchorAt points the next shift-click at a row without selecting it, for a
// plain click that opened a message. Without it, click a row then shift-click
// another and there is nothing to measure the range from, so only the second
// row ends up selected.
export function anchorAt(id: number): void {
  anchorId = id
  anchored = new Set(get(selectedIds))
}

/**
 * Anchors at a row unless an anchor is already set. Clearing the selection and
 * select all both drop the anchor, and a keyboard range extended after either
 * has to start at the highlighted row rather than one past it.
 */
export function ensureAnchor(id: number): void {
  if (anchorId === null) {
    anchorAt(id)
  }
}

// toggleSelect flips one id (cmd/ctrl-click) and makes it the new anchor.
export function toggleSelect(id: number): void {
  const next = new Set(get(selectedIds))
  if (next.has(id)) {
    next.delete(id)
  } else {
    next.add(id)
  }
  anchorId = id
  anchored = new Set(next)
  selectedIds.set(next)
}

// selectOnly replaces the selection with a single id and sets it as the anchor.
export function selectOnly(id: number): void {
  anchorId = id
  anchored = new Set([id])
  selectedIds.set(new Set([id]))
}

// selectRange selects every id from the anchor through targetId inclusive, using
// the given ordered id list, on top of what was selected when the anchor was
// set. when there is no anchor yet it falls back to a single selection.
export function selectRange(orderedIds: number[], targetId: number): void {
  if (anchorId === null) {
    selectOnly(targetId)
    return
  }
  const a = orderedIds.indexOf(anchorId)
  const b = orderedIds.indexOf(targetId)
  if (a < 0 || b < 0) {
    selectOnly(targetId)
    return
  }
  const [lo, hi] = a <= b ? [a, b] : [b, a]
  const next = new Set(anchored)
  for (const id of orderedIds.slice(lo, hi + 1)) {
    next.add(id)
  }
  selectedIds.set(next)
}

// selectAll replaces the selection with the given ids and drops the anchor,
// since a range measured from one row of an everything-selection means nothing.
export function selectAll(ids: number[]): void {
  anchorId = null
  anchored = new Set(ids)
  selectedIds.set(new Set(ids))
}

// deselect removes one id without touching the anchor, used after a bulk action
// consumes part of the selection.
export function deselect(id: number): void {
  const next = new Set(get(selectedIds))
  if (next.delete(id)) {
    selectedIds.set(next)
  }
}

// startVisual begins a visual selection at the cursor row, on top of whatever is
// already selected, so ending one range, moving past some rows and starting
// another skips the rows in between.
export function startVisual(orderedIds: number[], id: number): void {
  anchorAt(id)
  visualMode.set(true)
  selectRange(orderedIds, id)
}

// endVisual stops extending the selection and keeps what it picked.
export function endVisual(): void {
  visualMode.set(false)
}

/** Reports whether a vim-mode visual selection is running. */
export function inVisual(): boolean {
  return get(visualMode)
}
