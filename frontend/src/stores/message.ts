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

function applyDetail(detail: MessageDetail): void {
  messageDetail.set(ready(detail))
  bodyLoading.set(detail.bodyComplete === false)
}

// loadMessage fetches the full message for the detail pane.
export async function loadMessage(id: number): Promise<void> {
  const prev = get(messageDetail)
  if (prev.data?.id !== id) {
    messageDetail.update((s) => loading(s))
    bodyLoading.set(false)
  }
  try {
    applyDetail(await getMessage(id))
  } catch (err) {
    bodyLoading.set(false)
    messageDetail.set(failed(errorMessage(err)))
  }
}

// refreshMessage reloads one open message after mail:updated without blanking
// the pane or putting it back into the full-pane loading state.
export async function refreshMessage(id: number): Promise<void> {
  const prev = get(messageDetail)
  if (prev.data?.id !== id) {
    return
  }
  bodyLoading.set(true)
  try {
    applyDetail(await getMessage(id))
  } catch {
    bodyLoading.set(false)
  }
}

// clearMessage empties the detail pane.
export function clearMessage(): void {
  bodyLoading.set(false)
  messageDetail.set(idle())
}

// setBodyHtml swaps the rendered body, used after loading remote images.
export function setBodyHtml(html: string): void {
  messageDetail.update((s) => {
    if (s.status !== 'ready' || !s.data) {
      return s
    }
    return ready({ ...s.data, bodyHtmlSafe: html })
  })
}
