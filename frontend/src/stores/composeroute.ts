// composeroute.ts decides where a compose session opens in the new design. A
// session starts in the store of the window that made it, wherever that was
// (the toolbar, the list, a shortcut, a mailto link, undo send); this places
// it once, by the composeOpenIn pref: into a window of its own through the
// pop-out handoff, into a tab, or docked in the corner. The classic design
// keeps every session docked, the way it always has.

import { get } from 'svelte/store'
import { closeCompose, composeSessions, updateCompose, type ComposeSession } from './compose'
import { dropComposeTab, openComposeTab, tabs } from './tabs'
import { prefs } from './prefs'
import { openComposeWindow } from '../lib/api'
import { isDemoActive } from '../lib/demo'
import { errorMessage, toastError } from './toast'
import type { ComposeOpenIn } from '../lib/types'

/** What the window running the router can show a session in. */
export interface RouteOptions {
  /** The window has a tab bar to put a compose tab in. */
  tabs: boolean
}

/**
 * placementFor is where a new session goes, given the pref and what is
 * available: windows are not in demo mode, and tabs only where a tab bar is.
 * Anything unavailable falls back to docked.
 */
export function placementFor(pref: ComposeOpenIn, windows: boolean, tabBar: boolean): ComposeOpenIn {
  if (pref === 'window' && windows) {
    return 'window'
  }
  if (pref === 'tab' && tabBar) {
    return 'tab'
  }
  return 'docked'
}

// place moves one unplaced session to where it belongs.
function place(session: ComposeSession, options: RouteOptions): void {
  const where = placementFor(get(prefs).composeOpenIn, !isDemoActive(), options.tabs)
  if (where === 'docked') {
    updateCompose(session.id, { placement: 'docked' })
    return
  }
  if (where === 'tab') {
    updateCompose(session.id, { placement: 'tab' })
    openComposeTab(session.id)
    return
  }
  // hidden here while it moves, so it never flashes up docked first.
  updateCompose(session.id, { placement: 'leaving' })
  openComposeWindow(session)
    .then(() => closeCompose(session.id))
    .catch((err) => {
      toastError(errorMessage(err))
      updateCompose(session.id, { placement: 'docked' })
    })
}

/**
 * routeComposeSessions places every session that appears in this window while
 * the new design is on, and closes the tab of a compose session that is gone.
 * It returns the unsubscribe.
 */
export function routeComposeSessions(options: RouteOptions): () => void {
  const run = (): void => {
    const list = get(composeSessions)
    const open = new Set(list.map((s) => s.id))
    for (const tab of get(tabs)) {
      if (tab.compose !== undefined && !open.has(tab.compose)) {
        dropComposeTab(tab.compose)
      }
    }
    if (get(prefs).design === 'classic') {
      return
    }
    for (const session of list) {
      if (!session.placement) {
        place(session, options)
      }
    }
  }
  // the design can be switched with sessions open, so a pref change places
  // them too.
  const stopSessions = composeSessions.subscribe(run)
  const stopPrefs = prefs.subscribe(run)
  return () => {
    stopSessions()
    stopPrefs()
  }
}
