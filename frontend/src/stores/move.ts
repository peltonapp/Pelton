// move.ts drives the "Move to folder" dialog. a message's context menu calls
// openMove with the message, and a multi-selection calls openMoveMany with all
// of them; the dialog lists the account's folders and moves the batch on the
// server (reusing the archive/undo machinery), so one folder choice covers the
// whole selection rather than asking once per message.

import { writable } from 'svelte/store'
import type { MessageSummary } from '../lib/types'
import { noteAction } from '../lib/coach'

// the messages waiting for a destination, empty when the dialog is closed.
export const moveTargets = writable<MessageSummary[]>([])

export function openMove(message: MessageSummary): void {
  noteAction('move-to')
  moveTargets.set([message])
}

export function openMoveMany(messages: MessageSummary[]): void {
  if (messages.length === 0) {
    return
  }
  noteAction('move-to')
  moveTargets.set([...messages])
}

export function closeMove(): void {
  moveTargets.set([])
}
