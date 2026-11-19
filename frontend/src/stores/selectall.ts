// selectall.ts is select-all for the message list (#320). The list holds only
// the pages that were scrolled to, so "select everything in this mailbox" is a
// different act from "select what is loaded", and this is where the difference
// lives.
//
// What the header checkbox and cmd+a do is a preference: offer the rest in a
// banner (the default, and what Gmail does), reach for the whole list straight
// away, or stop at the loaded rows.

import { get, writable } from 'svelte/store'
import { allMatchingIds, messageList } from './messages'
import { selectAll, clearSelection, expandOffer } from './listselect'
import { askConfirm } from './confirm'
import { errorMessage, toastError, toastInfo } from './toast'
import { prefs } from './prefs'
import { t } from '../lib/i18n'

// selecting more than this at once asks first. Expanding is not destructive,
// but it is the step that makes a later delete expensive, and it is one
// keystroke away.
const confirmAbove = 500

// the banner state lives in listselect so that clearing the selection clears
// the offer with it, wherever the clearing comes from.
export { expandOffer }

// true while the ids are being fetched, so the banner can say so rather than
// looking dead on a large mailbox.
export const expanding = writable<boolean>(false)

/** Drops the banner. Called whenever the list or the selection changes under it. */
export function clearExpandOffer(): void {
  expandOffer.set(null)
}

/**
 * Selects every loaded row and, depending on the preference, either offers the
 * rest or goes and gets it.
 */
export async function selectAllInList(loadedIds: number[]): Promise<void> {
  const scope = get(prefs).selectAllScope
  if (scope === 'all') {
    await expandSelection()
    return
  }
  selectAll(loadedIds)
  const state = get(messageList)
  const total = state.data?.total ?? loadedIds.length
  if (scope === 'offer' && total > loadedIds.length) {
    expandOffer.set({ loaded: loadedIds.length, matching: total })
    return
  }
  clearExpandOffer()
}

/**
 * Widens the selection to every message the list matches, asking first when
 * that is a lot. Resolves whether anything was selected.
 */
export async function expandSelection(): Promise<boolean> {
  if (get(expanding)) {
    return false
  }
  const total = get(messageList).data?.total ?? 0
  if (total > confirmAbove) {
    const ok = await askConfirm({
      title: get(t)('list.selectAll.confirmTitle'),
      body: get(t)('list.selectAll.confirmBody').replace('{n}', total.toLocaleString()),
      confirmLabel: get(t)('list.selectAll.confirmAction'),
    })
    if (!ok) {
      return false
    }
  }

  expanding.set(true)
  try {
    const result = await allMatchingIds()
    selectAll(result.ids)
    clearExpandOffer()
    if (result.capped) {
      // saying so beats a selection that quietly holds less than it claims.
      toastInfo(get(t)('list.selectAll.capped').replace('{n}', result.ids.length.toLocaleString()))
    }
    return result.ids.length > 0
  } catch (err) {
    toastError(errorMessage(err))
    return false
  } finally {
    expanding.set(false)
  }
}

/** Clears the selection and the banner together. */
export function clearAll(): void {
  clearSelection()
  clearExpandOffer()
}
