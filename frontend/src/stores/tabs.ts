// tabs.ts owns the reading-pane tabs (#197): messages parked in the pane so you
// can go and look at something else without losing your place.
//
// The relationship with the plain reading pane is deliberately one-directional.
// Clicking or arrowing through the message list always lands in the untabbed
// pane, which is the first slot in the bar; a tab only ever changes when it is
// opened. That keeps the feature invisible to anyone who never opens one: with
// no tabs there is no bar and the pane behaves exactly as it did before.
//
// A tab holds a message id, not a snapshot, so flags and read state stay live
// and a message that moves folder is followed rather than broken. A message
// that is deleted leaves its tab behind marked stale, for you to close.

import { derived, get, writable } from 'svelte/store'
import { openMessageId } from './selection'
import { setSetting, getSetting, SettingKeys } from '../lib/api'
import { requestComposeClose } from './compose'

/** One parked message, or a message being written. */
export interface ReadingTab {
  /**
   * The message id. The tab shows whatever that message currently is. A
   * compose tab has the negated session id instead, so the two never meet.
   */
  id: number
  /** Subject at the time it was opened, for the tab label. */
  label: string
  /** The message is gone (deleted, or its folder removed). */
  stale: boolean
  /** The compose session the tab shows, for a compose tab. */
  compose?: number
}

/** composeTabId is the tab id of a compose session's tab. */
export function composeTabId(sessionId: number): number {
  return -sessionId
}

/** The open tabs, in the order they were opened. */
export const tabs = writable<ReadingTab[]>([])

/**
 * The tab currently showing in the pane, or null for the untabbed pane. Null is
 * the normal state and the first slot in the bar.
 */
export const activeTabId = writable<number | null>(null)

/**
 * The message the reading pane is showing: the active tab's, or the one picked
 * in the list when no tab is active.
 */
export const visibleMessageId = derived(
  [activeTabId, openMessageId],
  ([$activeTabId, $openMessageId]) => ($activeTabId !== null && $activeTabId > 0 ? $activeTabId : $openMessageId),
)

/** The compose session the active tab shows, or null when it shows a message. */
export const activeComposeId = derived([tabs, activeTabId], ([$tabs, $activeTabId]) =>
  $activeTabId === null ? null : ($tabs.find((tab) => tab.id === $activeTabId)?.compose ?? null),
)

/** Whether the tab bar should be on screen at all. */
export const hasTabs = derived(tabs, ($tabs) => $tabs.length > 0)

// picking a message in the list means the untabbed pane, always. Doing it here
// rather than at each call site means no future caller can forget: whatever
// changes the list selection, the pane comes forward.
openMessageId.subscribe(() => activeTabId.set(null))

/**
 * Opens a message in a tab and switches to it. A message already in a tab is
 * focused rather than opened twice.
 */
export function openInTab(id: number, label: string): void {
  const existing = get(tabs).find((tab) => tab.id === id)
  if (!existing) {
    tabs.update((open) => [...open, { id, label, stale: false }])
  }
  activeTabId.set(id)
  persist()
}

/**
 * Closes a tab. Closing the active one falls back to the tab on its left, or to
 * the untabbed pane when it was the first.
 */
export function closeTab(id: number): void {
  const compose = get(tabs).find((tab) => tab.id === id)?.compose
  if (compose !== undefined) {
    // the composer asks about unsaved text first; its tab goes with it.
    requestComposeClose(compose)
    return
  }
  removeTab(id)
}

// removeTab takes a tab out of the bar, handing the pane to its left
// neighbour when it was showing.
function removeTab(id: number): void {
  const open = get(tabs)
  const index = open.findIndex((tab) => tab.id === id)
  if (index < 0) {
    return
  }
  tabs.set(open.filter((tab) => tab.id !== id))

  if (get(activeTabId) === id) {
    const left = open[index - 1]
    activeTabId.set(left ? left.id : null)
  }
  persist()
}

/**
 * Closes every tab and returns to the pane. Switching profile does this: the
 * parked messages belong to accounts the new profile may not even show.
 */
export function clearAllTabs(): void {
  // a message being written stays: the text in it is not parked, it is work.
  tabs.update((open) => open.filter((tab) => tab.compose !== undefined))
  activeTabId.set(null)
  persist()
}

/** Opens a compose session in a tab of its own and switches to it. */
export function openComposeTab(sessionId: number): void {
  const id = composeTabId(sessionId)
  if (!get(tabs).some((tab) => tab.id === id)) {
    tabs.update((open) => [...open, { id, label: '', stale: false, compose: sessionId }])
  }
  activeTabId.set(id)
}

/** Takes the tab of a compose session that has closed out of the bar. */
export function dropComposeTab(sessionId: number): void {
  removeTab(composeTabId(sessionId))
}

/** Closes the tab currently showing, if the pane is showing one. */
export function closeActiveTab(): void {
  const active = get(activeTabId)
  if (active !== null) {
    closeTab(active)
  }
}

/** Brings a tab to the pane. */
export function focusTab(id: number): void {
  activeTabId.set(id)
}

/** Shows the untabbed pane again, leaving every tab open. */
export function focusPane(): void {
  activeTabId.set(null)
}

/**
 * Marks a tab's message as gone. The tab stays: something you parked should not
 * vanish from under you, and a stale tab is closed the same way as any other.
 */
export function markTabStale(id: number): void {
  tabs.update((open) => open.map((tab) => (tab.id === id && tab.compose === undefined ? { ...tab, stale: true } : tab)))
}

/**
 * Puts the tabs in the given order, by id. Ids that are not open are ignored
 * and open tabs missing from the list keep their place at the end, so a stale
 * order from a drag that raced a close cannot drop a tab.
 */
export function reorderTabs(ids: number[]): void {
  tabs.update((open) => {
    const byId = new Map(open.map((tab) => [tab.id, tab]))
    const ordered: ReadingTab[] = []
    for (const id of ids) {
      const tab = byId.get(id)
      if (tab) {
        ordered.push(tab)
        byId.delete(id)
      }
    }
    return [...ordered, ...byId.values()]
  })
  persist()
}

/** Renames a tab, for when the message loads and the subject is known. */
export function labelTab(id: number, label: string): void {
  if (label === '') {
    return
  }
  tabs.update((open) => open.map((tab) => (tab.id === id && tab.compose === undefined ? { ...tab, label } : tab)))
}

// persist writes the open tab ids so the restore setting has something to
// restore. Fire and forget: a failed write only costs the restore.
function persist(): void {
  const ids = get(tabs)
    .filter((tab) => !tab.stale && tab.compose === undefined)
    .map((tab) => tab.id)
    .join(',')
  void setSetting(SettingKeys.openTabs, ids)
}

/**
 * Reopens the tabs from the last session, when the user asked for that. Labels
 * come back empty and are filled in as each message loads, since the subject is
 * not worth storing separately from the message it belongs to.
 */
export async function restoreTabs(enabled: boolean): Promise<void> {
  if (!enabled) {
    // the stored list is left alone: turning the setting back on should bring
    // back what was open, not an empty list from the session in between.
    return
  }
  try {
    const { value, found } = await getSetting(SettingKeys.openTabs)
    if (!found || value === '') {
      return
    }
    const restored = value
      .split(',')
      .map((raw) => Number(raw))
      .filter((id) => Number.isInteger(id) && id > 0)
      .map((id) => ({ id, label: '', stale: false }))
    tabs.set(restored)
  } catch {
    // no tabs is the safe outcome; the pane works either way.
  }
}
