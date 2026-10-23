// messageactions.ts holds the message operations that more than one surface
// runs: the message list's context menu, and the command palette. Each function
// patches the in-memory list first so the row reacts immediately, then reports
// failures as a toast rather than throwing, because every caller is a fire and
// forget menu click.
//
// Single-message actions that only ever come from a keyboard shortcut still
// live in App.svelte's dispatcher; this module is the shared subset.

import {
  setSeen,
  setFlagged,
  setFlagColor,
  deleteMessage,
  archiveMessage,
  downloadMessageOffline,
  removeOffline,
} from './api'
import type { ArchiveUndo } from './api'
import type { MessageSummary } from './types'
import { patchInList, removeFromList, neighbourInList } from '../stores/messages'
import { openMessageId } from '../stores/selection'
import { clearSelection } from '../stores/listselect'
import { recordDeleted, recordDeletedBatch } from '../stores/undodelete'
import { recordArchived, recordArchivedBatch, type ArchivedMessage } from '../stores/undoarchive'
import { askConfirm } from '../stores/confirm'
import { isVIPAddress, addVIP, removeVIP } from '../stores/vip'
import { errorMessage, toastError, toastSuccess } from '../stores/toast'
import { t } from './i18n'
import { get } from 'svelte/store'

/** Marks one message read or unread. */
export async function markSeen(item: MessageSummary, seen: boolean): Promise<void> {
  patchInList(item.id, { seen })
  try {
    await setSeen(item.id, seen)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Flags or unflags one message. */
export async function markFlagged(item: MessageSummary, flagged: boolean): Promise<void> {
  patchInList(item.id, { flagged })
  try {
    await setFlagged(item.id, flagged)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Sets a message's color label; 0 clears it. */
export async function markColor(item: MessageSummary, color: number): Promise<void> {
  patchInList(item.id, { flagColor: color })
  try {
    await setFlagColor(item.id, color)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Stars or unstars the message's sender. */
export async function toggleSenderVIP(item: MessageSummary): Promise<void> {
  try {
    if (isVIPAddress(item.fromAddress)) {
      await removeVIP(item.fromAddress)
    } else {
      await addVIP(item.fromAddress)
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Downloads a message for offline reading, or drops the offline copy. */
export async function setOffline(item: MessageSummary, offline: boolean): Promise<void> {
  const previous = item.offline
  patchInList(item.id, { offline })
  try {
    if (offline) {
      await downloadMessageOffline(item.id)
      toastSuccess(get(t)('messageList.toast.savedOffline'))
    } else {
      await removeOffline(item.id)
    }
  } catch (err) {
    // the optimistic flag would otherwise keep claiming a download that failed
    patchInList(item.id, { offline: previous })
    toastError(errorMessage(err))
  }
}

/**
 * Drops a deleted message from the list and moves the detail pane on.
 *
 * Emptying the pane instead means a run of deletes has to go back to the list
 * between each one, which is most of the work of triaging a mailbox. alsoDeleting
 * names the rest of a batch, so a bulk delete does not land on a row that is
 * about to go as well.
 *
 * It removes the row itself because the neighbour has to be read while the row
 * is still there to have one.
 */
export function dropDeleted(id: number, alsoDeleting?: ReadonlySet<number>): void {
  const wasOpen = get(openMessageId) === id
  const next = wasOpen ? neighbourInList(id, alsoDeleting) : null
  removeFromList(id)
  if (wasOpen) {
    openMessageId.set(next)
  }
}

/** Deletes one message, recording it for undo and moving the pane on if it was open. */
export async function trashMessage(item: MessageSummary): Promise<void> {
  try {
    await deleteMessage(item.id)
    recordDeleted(item)
    dropDeleted(item.id)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/**
 * Reports a failed export-on-archive copy. The archive itself succeeded, so
 * this is a warning about the copy and never a failed action: staying quiet
 * would leave the user believing a local copy exists when it does not.
 */
export function reportArchiveExport(undo: ArchiveUndo): void {
  if (undo.exportError) {
    toastError(get(t)('mailboxes.export.failed').replace('{error}', undo.exportError))
  }
}

/** Archives one message, recording it for undo and closing it if it was open. */
export async function archive(item: MessageSummary): Promise<void> {
  try {
    const undo = await archiveMessage(item.id)
    reportArchiveExport(undo)
    recordArchived(item, undo)
    removeFromList(item.id)
    if (get(openMessageId) === item.id) {
      openMessageId.set(null)
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
}

// --- bulk variants ---
//
// These clear the multi-selection up front: the rows are about to change or
// disappear, and leaving them selected would let a second action run against a
// set the user can no longer see.

/** Marks every given message read or unread. */
export async function bulkMarkSeen(items: MessageSummary[], seen: boolean): Promise<void> {
  clearSelection()
  await Promise.all(items.map((item) => markSeen(item, seen)))
}

/** Flags or unflags every given message. */
export async function bulkMarkFlagged(items: MessageSummary[], flagged: boolean): Promise<void> {
  clearSelection()
  await Promise.all(items.map((item) => markFlagged(item, flagged)))
}

/** Sets the same color label on every given message. */
export async function bulkMarkColor(items: MessageSummary[], color: number): Promise<void> {
  clearSelection()
  await Promise.all(items.map((item) => markColor(item, color)))
}

/** Downloads every given message for offline reading, or drops the copies. */
export async function bulkSetOffline(items: MessageSummary[], offline: boolean): Promise<void> {
  clearSelection()
  await Promise.all(items.map((item) => setOffline(item, offline)))
}

/**
 * Deletes every given message, after asking when it is more than one. The
 * question lives here rather than at each button, so the menu, the toolbar, a
 * shortcut and the command palette all ask it.
 *
 * Sequential rather than parallel: each delete mutates the list, and the server
 * is happier with a queue than with fifty concurrent stores. The whole batch is
 * one undo step, so one press brings all of them back.
 */
export async function bulkTrash(items: MessageSummary[]): Promise<void> {
  if (items.length > 1 && !(await confirmBulkDelete(items.length))) {
    return
  }
  clearSelection()
  const batch = new Set(items.map((m) => m.id))
  const deleted: MessageSummary[] = []
  for (const item of items) {
    try {
      await deleteMessage(item.id)
      deleted.push(item)
      dropDeleted(item.id, batch)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
  recordDeletedBatch(deleted)
}

// confirmBulkDelete asks before a delete that covers more than one message.
// Deleting one stays instant: it is a single row, visibly gone, and undo is a
// keypress away.
function confirmBulkDelete(count: number): Promise<boolean> {
  const label = get(t)
  return askConfirm({
    title: label('messageList.bulk.deleteConfirmTitle').replace('{n}', String(count)),
    body: label('messageList.bulk.deleteConfirmBody'),
    confirmLabel: label('messageList.bulk.deleteConfirmAction').replace('{n}', String(count)),
    danger: true,
  })
}

/** Archives every given message, sequentially, as one undo step. */
export async function bulkArchive(items: MessageSummary[]): Promise<void> {
  clearSelection()
  const undone: ArchivedMessage[] = []
  for (const item of items) {
    try {
      const undo = await archiveMessage(item.id)
      reportArchiveExport(undo)
      if (undo.messageId) {
        undone.push({
          summary: item,
          messageId: undo.messageId,
          fromFolderId: undo.destFolderId,
          originalFolderId: undo.originalFolderId,
        })
      }
      removeFromList(item.id)
      if (get(openMessageId) === item.id) {
        openMessageId.set(null)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
  recordArchivedBatch(undone)
}
