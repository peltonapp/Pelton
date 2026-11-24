// spaceorder.ts works out the order of the spaces in the navigation and which
// of them are left out, from the two comma separated prefs. it is pure so the
// rail, the switchers and settings agree on it and it can be tested alone.

import type { Space } from './types'

/** The space that can be moved but never hidden. */
export const pinnedSpace: Space = 'mail'

/** Splits a stored comma list into its trimmed, non-empty keys. */
export function parseKeys(raw: string): string[] {
  return raw
    .split(',')
    .map((k) => k.trim())
    .filter((k) => k !== '')
}

/**
 * Every space of `all` in the stored order. Unknown or repeated keys are
 * ignored, and spaces the stored order does not name follow in their default
 * order, so a space added later shows up at the end.
 */
export function orderSpaces(all: readonly Space[], raw: string): Space[] {
  const known = new Set<string>(all)
  const out: Space[] = []
  for (const key of parseKeys(raw)) {
    if (known.has(key) && !out.includes(key as Space)) {
      out.push(key as Space)
    }
  }
  for (const key of all) {
    if (!out.includes(key)) {
      out.push(key)
    }
  }
  return out
}

/** The spaces left out of the navigation. Mail and unknown keys never are. */
export function hiddenSpaces(all: readonly Space[], raw: string): Set<Space> {
  const known = new Set<string>(all)
  return new Set(parseKeys(raw).filter((k): k is Space => known.has(k) && k !== pinnedSpace))
}

/** Whether a space may be left out of the navigation. */
export function canHide(key: Space): boolean {
  return key !== pinnedSpace
}

/** The order as stored: empty when it is the default one. */
export function serializeOrder(all: readonly Space[], order: readonly Space[]): string {
  const same = order.length === all.length && order.every((k, i) => k === all[i])
  return same ? '' : order.join(',')
}

/** The hidden set as stored, in the given order so it reads stable. */
export function serializeHidden(order: readonly Space[], hidden: ReadonlySet<Space>): string {
  return order.filter((k) => hidden.has(k) && canHide(k)).join(',')
}

/** Moves a space by delta places in the list; out of range leaves it alone. */
export function moveInList(order: readonly Space[], key: Space, delta: number): Space[] {
  const from = order.indexOf(key)
  const to = from + delta
  if (from < 0 || delta === 0 || to < 0 || to >= order.length) {
    return [...order]
  }
  const next = [...order]
  next.splice(from, 1)
  next.splice(to, 0, key)
  return next
}

/**
 * Puts the shown spaces in a new order while every hidden space keeps its
 * slot: the slots the shown ones held are filled with them in their new
 * order. Keys in shown that are not in order are ignored.
 */
export function applyShownOrder(order: readonly Space[], hidden: ReadonlySet<Space>, shown: readonly Space[]): Space[] {
  const wanted = shown.filter((k) => order.includes(k) && !hidden.has(k))
  const rest = order.filter((k) => !hidden.has(k) && !wanted.includes(k))
  const queue = [...wanted, ...rest]
  return order.map((k) => (hidden.has(k) ? k : (queue.shift() as Space)))
}

/**
 * Moves a space by delta places among the shown spaces only, so "move up" on
 * the rail always passes the entry above it there.
 */
export function moveShown(order: readonly Space[], hidden: ReadonlySet<Space>, key: Space, delta: number): Space[] {
  const shown = order.filter((k) => !hidden.has(k))
  return applyShownOrder(order, hidden, moveInList(shown, key, delta))
}
