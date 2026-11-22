// message.ts owns the detail pane: the full message for the open id. it loads on
// demand and exposes a way to swap in the remote-allowed body when the user opts
// to load remote images.

import { writable, get } from 'svelte/store'
import type { MessageDetail } from '../lib/types'
import { getMessage } from '../lib/api'
import { type AsyncState, idle, loading, ready, failed } from '../lib/async'
import { errorMessage } from './toast'

export const messageDetail = writable<AsyncState<MessageDetail>>(idle())

// bodyLoading is true while a stub's preview is on screen and the full body is
// still being fetched in the background.
export const bodyLoading = writable(false)

// bodyFetchFailed is the id of the message whose body the backend gave up on, or
// null. it lives next to bodyLoading so the two cannot disagree: a detail reply
// that lands after the failure event must not turn the spinner back on.
export const bodyFetchFailed = writable<number | null>(null)

// generation bumps whenever the pane switches message or closes. a getMessage
// that resolves after a bump is stale and must not touch the pane.
let generation = 0
let currentMessageId: number | null = null
let initialLoadGeneration: number | null = null
let pendingRefresh = false
let detailSequence = 0

function applyDetail(detail: MessageDetail): void {
  messageDetail.set(ready(detail))
  if (detail.bodyComplete !== false) {
    bodyFetchFailed.update((id) => (id === detail.id ? null : id))
    bodyLoading.set(false)
    return
  }
  // an incomplete reply waits for a body, unless that body already failed.
  bodyLoading.set(get(bodyFetchFailed) !== detail.id)
}

/** Records that the body of a message will not arrive and stops its spinner. */
export function noteBodyFailed(id: number): void {
  bodyFetchFailed.set(id)
  if (currentMessageId === id) bodyLoading.set(false)
}

/** Loads the selected detail and rereads once for updates received while opening. */
export async function loadMessage(id: number): Promise<void> {
  const gen = ++generation
  currentMessageId = id
  initialLoadGeneration = gen
  pendingRefresh = false
  // this read starts a fresh body fetch, so an older failure no longer holds.
  bodyFetchFailed.set(null)
  const request = ++detailSequence
  const prev = get(messageDetail)
  if (prev.data?.id !== id) {
    messageDetail.update((s) => loading(s))
    bodyLoading.set(false)
  }
  let succeeded = false
  try {
    const detail = await getMessage(id)
    if (gen !== generation || request !== detailSequence) return
    applyDetail(detail)
    succeeded = true
  } catch (err) {
    if (gen === generation && request === detailSequence) {
      bodyLoading.set(false)
      messageDetail.set(failed(errorMessage(err)))
    }
  } finally {
    if (gen === generation) initialLoadGeneration = null
  }
  if (gen !== generation) return
  const reread = succeeded && pendingRefresh
  pendingRefresh = false
  if (reread) await refreshMessage(id)
}

/** Refreshes the selected message after mail:updated without blanking the pane. */
export async function refreshMessage(id: number): Promise<void> {
  if (id !== currentMessageId) return
  if (initialLoadGeneration === generation) {
    pendingRefresh = true
    return
  }
  if (get(messageDetail).data?.id !== id) return
  const gen = generation
  const request = ++detailSequence
  bodyLoading.set(true)
  try {
    const detail = await getMessage(id)
    if (gen === generation && request === detailSequence) applyDetail(detail)
  } catch {
    if (gen === generation && request === detailSequence) bodyLoading.set(false)
  }
}

/** Empties the pane and cancels any queued reread. */
export function clearMessage(): void {
  generation++
  currentMessageId = null
  initialLoadGeneration = null
  pendingRefresh = false
  bodyLoading.set(false)
  bodyFetchFailed.set(null)
  messageDetail.set(idle())
}

// setBodyHtml swaps the rendered body, used after loading remote images. It
// names the message the html belongs to: the load is a round trip, and if
// another message opened meanwhile this one's body must not land under it.
export function setBodyHtml(id: number, html: string): void {
  messageDetail.update((s) => {
    if (s.status !== 'ready' || !s.data || s.data.id !== id) {
      return s
    }
    return ready({ ...s.data, bodyHtmlSafe: html })
  })
}
