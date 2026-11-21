// message.ts owns the detail pane: the full message for the open id. it loads on
// demand and exposes a way to swap in the remote-allowed body when the user opts
// to load remote images.

import { writable } from 'svelte/store'
import type { MessageDetail } from '../lib/types'
import { getMessage } from '../lib/api'
import { type AsyncState, idle, loading, ready, failed } from '../lib/async'
import { errorMessage } from './toast'

export const messageDetail = writable<AsyncState<MessageDetail>>(idle())

// latest numbers the loads, so only the newest one may fill the pane. Loads take
// different times, and without this a slow one for a message clicked earlier
// could land after the one for the message clicked last, leaving the pane on
// the wrong message while the list showed the right one selected.
let latest = 0

// loadMessage fetches the full message for the detail pane.
export async function loadMessage(id: number): Promise<void> {
  const request = ++latest
  messageDetail.update((s) => loading(s))
  try {
    const detail = await getMessage(id)
    if (request === latest) {
      messageDetail.set(ready(detail))
    }
  } catch (err) {
    if (request === latest) {
      messageDetail.set(failed(errorMessage(err)))
    }
  }
}

// clearMessage empties the detail pane, and drops any load still on its way.
export function clearMessage(): void {
  latest++
  messageDetail.set(idle())
}

// setBodyHtml swaps the rendered body of message id, used after loading remote
// images. It does nothing if another message is open by then.
export function setBodyHtml(id: number, html: string): void {
  messageDetail.update((s) => {
    if (s.status !== 'ready' || !s.data || s.data.id !== id) {
      return s
    }
    return ready({ ...s.data, bodyHtmlSafe: html })
  })
}
