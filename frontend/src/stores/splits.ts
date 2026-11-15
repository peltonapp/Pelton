// splits.ts holds the split inbox: which tabs the inbox shows, which one is
// active, and their counts. The backend does the sorting; this module only
// decides which tab a list asks for and keeps the counts the tab strip shows.

import { get, writable } from 'svelte/store'
import type { Selection, SplitTab, UIPrefs, View } from '../lib/types'
import { splitCounts } from '../lib/api'
import { prefs, setSplitActive } from './prefs'
import { sidebar, type SidebarData } from './accounts'
import { views } from './views'

/** The built-in tabs a split inbox can show, besides the catch-all. */
export const builtinSplits = ['important', 'newsletters', 'notifications'] as const

/** The catch-all tab, always last. */
export const otherSplit = 'other'

const viewPrefix = 'view:'

/** The saved view a view:<id> key names, or null for any other key. */
export function splitViewId(key: string): number | null {
  if (!key.startsWith(viewPrefix)) {
    return null
  }
  const id = Number(key.slice(viewPrefix.length))
  return Number.isInteger(id) && id > 0 ? id : null
}

/** The key a saved view takes as a tab. */
export function viewSplitKey(id: number): string {
  return `${viewPrefix}${id}`
}

/**
 * The tabs in order, the catch-all last. Unknown keys, repeats and saved views
 * that no longer exist are dropped.
 */
export function splitKeys(stored: string, savedViews: View[]): string[] {
  const out: string[] = []
  for (const raw of stored.split(',')) {
    const key = raw.trim()
    if (key === '' || out.includes(key)) {
      continue
    }
    const viewId = splitViewId(key)
    const known = (builtinSplits as readonly string[]).includes(key) || (viewId !== null && savedViews.some((v) => v.id === viewId))
    if (known) {
      out.push(key)
    }
  }
  out.push(otherSplit)
  return out
}

/** The tab to show: the remembered one when it still exists, else the first. */
export function activeSplit(remembered: string, keys: string[]): string {
  return keys.includes(remembered) ? remembered : keys[0]
}

/** Whether a selection is an inbox: the unified inbox or an account's inbox folder. */
export function isInboxSelection(sel: Selection, data: SidebarData | null | undefined): boolean {
  if (sel.kind === 'view') {
    return sel.view === 'inbox'
  }
  if (sel.kind === 'folder') {
    return data?.foldersByAccount[sel.accountId]?.find((f) => f.id === sel.folderId)?.role === 'inbox'
  }
  return false
}

/** Whether the split inbox applies to a selection under these preferences. */
export function splitApplies(sel: Selection, p: Pick<UIPrefs, 'design' | 'splitInbox'>, data: SidebarData | null | undefined): boolean {
  return p.design !== 'classic' && p.splitInbox && isInboxSelection(sel, data)
}

/** The tab key a list of sel asks the backend for, '' when it is not split. */
export function splitFor(sel: Selection): string {
  const p = get(prefs)
  if (!splitApplies(sel, p, get(sidebar).data)) {
    return ''
  }
  return activeSplit(p.splitActive, splitKeys(p.splitTabs, get(views)))
}

/** The tabs' counts for the selection on screen. */
export const splitTabCounts = writable<SplitTab[]>([])

let countsGeneration = 0

/** Reads the tab counts again for a selection. */
export async function refreshSplitCounts(sel: Selection): Promise<void> {
  const generation = ++countsGeneration
  try {
    const tabs = await splitCounts(sel)
    if (generation === countsGeneration) {
      splitTabCounts.set(tabs)
    }
  } catch {
    // the strip keeps the counts it had; the next refresh catches up.
  }
}

/** Moves to the next tab (1) or the previous one (-1), wrapping around. */
export function stepSplit(direction: 1 | -1): string {
  const p = get(prefs)
  const keys = splitKeys(p.splitTabs, get(views))
  const at = keys.indexOf(activeSplit(p.splitActive, keys))
  const next = keys[(at + direction + keys.length) % keys.length]
  setSplitActive(next)
  return next
}
