// rowactionrun.ts runs a row action (lib/rowactions.ts) on one message, with
// the same functions its keyboard shortcut reaches, so the undo history, the
// reader moving on and the toasts behave the way they do for the key.

import type { MessageSummary } from './types'
import type { RowActionId } from './rowactions'
import { archive, markFlagged, markSeen, toggleSpam, trashMessage } from './messageactions'
import { markDone } from './triage'
import { togglePin, toggleMute } from './organize'
import { pickProjectFor } from './projectpicker'
import { createTasksFromMail } from './taskmail'
import { openSnooze } from '../stores/snooze'
import { openMove } from '../stores/move'
import { closeTab } from '../stores/tabs'
import { messageList } from '../stores/messages'
import { get } from 'svelte/store'

// a tab holding a message that left the list goes with it, as it does when
// the shortcut archives or deletes the message on screen.
function closeIfGone(id: number): void {
  if (!get(messageList).data?.items.some((m) => m.id === id)) {
    closeTab(id)
  }
}

/** Runs one row action on one message. Failures are reported as toasts. */
export async function runRowAction(id: RowActionId, item: MessageSummary): Promise<void> {
  switch (id) {
    case 'archive':
      await archive(item)
      closeIfGone(item.id)
      return
    case 'delete':
      await trashMessage(item)
      closeIfGone(item.id)
      return
    case 'done':
      await markDone([item])
      return
    case 'snooze':
      openSnooze(item.id, item.subject)
      return
    case 'pin':
      await togglePin(item)
      return
    case 'read':
      await markSeen(item, !item.seen)
      return
    case 'flag':
      await markFlagged(item, !item.flagged)
      return
    case 'mute':
      await toggleMute(item)
      return
    case 'move':
      openMove(item)
      return
    case 'spam':
      if (await toggleSpam(item)) {
        closeTab(item.id)
      }
      return
    case 'project':
      pickProjectFor([item.id], item.threadId)
      return
    case 'task':
      await createTasksFromMail([item.id])
      return
  }
}
