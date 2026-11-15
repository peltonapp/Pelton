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
  markSpam,
  markNotSpam,
  exportMessageEml,
  exportMessagesEml,
  moveMessage,
} from './api'
import type { ArchiveUndo } from './api'
import type { MessageSummary, Folder } from './types'
import { patchInList, removeFromList, neighbourInList, newestFirst } from '../stores/messages'
import { prefs } from '../stores/prefs'
import { openMessageId } from '../stores/selection'
import { clearSelection } from '../stores/listselect'
import { recordDeleted, recordDeletedBatch } from '../stores/undodelete'
import { recordArchived, archivedEntry, recordArchivedBatch, type ArchivedMessage } from '../stores/undoarchive'
import { askConfirm } from '../stores/confirm'
import { isVIPAddress, addVIP, removeVIP } from '../stores/vip'
import { errorMessage, toastError, toastInfo, toastSuccess } from '../stores/toast'
import { sidebar } from '../stores/accounts'
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
  patchInList(item.id, { offline })
  try {
    if (offline) {
      await downloadMessageOffline(item.id)
      toastSuccess(get(t)('messageList.toast.savedOffline'))
    } else {
      await removeOffline(item.id)
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/**
 * nextAfterLeaving picks the message the reader opens once the open message
 * leaves the list. The new design follows the afterMove preference: the older
 * or newer neighbour, falling back to the other side at either end, or null
 * for back to the list. Classic keeps its own behaviour: a delete moves on to
 * the neighbour below and anything else closes the pane.
 */
export function nextAfterLeaving(openId: number, leaving: ReadonlySet<number>, classicAdvance: boolean): number | null {
  const p = get(prefs)
  if (p.design === 'classic') {
    return classicAdvance ? neighbourInList(openId, leaving) : null
  }
  if (p.afterMove === 'list') {
    return null
  }
  const older = newestFirst() ? 'below' : 'above'
  const newer = older === 'below' ? 'above' : 'below'
  return neighbourInList(openId, leaving, p.afterMove === 'newer' ? newer : older)
}

/**
 * Takes rows out of the list after they were archived, deleted or moved, and
 * moves the reader on when it was showing one of them, so working down a
 * mailbox never goes back to an empty pane in between. alsoLeaving names the
 * rest of a batch, so the reader does not land on a row about to go as well.
 * classicAdvance keeps the classic design's delete behaviour (see
 * nextAfterLeaving).
 *
 * It removes the rows itself because the neighbour has to be read while the
 * open row is still there to have one.
 */
export function leaveList(ids: readonly number[], alsoLeaving?: ReadonlySet<number>, classicAdvance = false): void {
  const leaving = new Set([...ids, ...(alsoLeaving ?? [])])
  const open = get(openMessageId)
  const wasOpen = open !== null && ids.includes(open)
  const next = wasOpen ? nextAfterLeaving(open, leaving, classicAdvance) : null
  for (const id of ids) {
    removeFromList(id)
  }
  if (wasOpen) {
    openMessageId.set(next)
  }
}

/**
 * Drops a deleted message from the list and moves the detail pane on.
 * alsoDeleting names the rest of a batch, so a bulk delete does not land on a
 * row that is about to go as well.
 */
export function dropDeleted(id: number, alsoDeleting?: ReadonlySet<number>): void {
  leaveList([id], alsoDeleting, true)
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

/**
 * Moves messages to a folder of their account as one undo step. Messages
 * already in that folder are left alone. The rows leave the list at once and
 * a failure is reported as a toast.
 */
export async function moveToFolder(items: MessageSummary[], folder: Folder): Promise<void> {
  const targets = items.filter((m) => m.folderId !== folder.id)
  if (targets.length === 0) {
    return
  }
  leaveList(targets.map((m) => m.id))
  const undone: ArchivedMessage[] = []
  let failure = ''
  for (const target of targets) {
    try {
      const entry = archivedEntry(target, await moveMessage(target.id, folder.id))
      if (entry) {
        undone.push(entry)
      }
    } catch (err) {
      failure = errorMessage(err)
    }
  }
  recordArchivedBatch(undone)
  if (failure !== '') {
    toastError(failure)
    return
  }
  const say = get(t)
  toastInfo(
    targets.length === 1
      ? say('detail.moveDialog.moved').replace('{folder}', folder.name)
      : say('detail.moveDialog.movedCount').replace('{n}', String(targets.length)).replace('{folder}', folder.name),
  )
}

/** Archives one message, recording it for undo and moving the reader on if it was open. */
export async function archive(item: MessageSummary): Promise<void> {
  try {
    const undo = await archiveMessage(item.id)
    reportArchiveExport(undo)
    recordArchived(item, undo)
    leaveList([item.id])
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/**
 * Where a message stands for the spam marks: 'spam' when it sits in its
 * account's spam folder, 'markable' when that account has one to move it to,
 * and 'none' when it has no spam folder at all, so there is nothing to offer.
 */
export function spamState(item: MessageSummary): 'spam' | 'markable' | 'none' {
  const folders = get(sidebar).data?.foldersByAccount[item.accountId] ?? []
  if (folders.find((f) => f.id === item.folderId)?.role === 'junk') {
    return 'spam'
  }
  return folders.some((f) => f.role === 'junk') ? 'markable' : 'none'
}

/**
 * Marks a message as spam, or rescues it when it already is, recording the move
 * for undo and closing it if it was open. Rescue sends it back to the folder it
 * was marked from, or to the inbox for mail the server filtered. Resolves to
 * whether the message moved.
 */
export async function toggleSpam(item: MessageSummary): Promise<boolean> {
  const state = spamState(item)
  if (state === 'none') {
    toastError(get(t)('spam.noFolder'))
    return false
  }
  try {
    const undo = state === 'spam' ? await markNotSpam(item.id) : await markSpam(item.id)
    recordArchived(item, undo)
    leaveList([item.id])
    toastSuccess(get(t)(state === 'spam' ? 'spam.toast.notSpam' : 'spam.toast.marked'))
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
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

/**
 * Exports messages as .eml files: one asks for a file name, several ask for a
 * folder. The dialogs and the work run in the backend; a cancelled dialog does
 * nothing and any failure is reported as a toast.
 */
export async function exportEml(ids: number[]): Promise<void> {
  if (ids.length === 0) {
    return
  }
  try {
    if (ids.length === 1) {
      const path = await exportMessageEml(ids[0])
      if (path) {
        toastSuccess(get(t)('export.eml.done'))
      }
      return
    }
    const result = await exportMessagesEml(ids)
    if (!result.dir) {
      return
    }
    if (result.failed > 0) {
      const text = get(t)('export.eml.partial')
        .replace('{done}', String(result.exported))
        .replace('{failed}', String(result.failed))
      toastError(`${text} ${result.firstError}`)
      return
    }
    toastSuccess(get(t)('export.eml.doneCount').replace('{n}', String(result.exported)))
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Archives every given message, sequentially, as one undo step. */
export async function bulkArchive(items: MessageSummary[]): Promise<void> {
  clearSelection()
  const batch = new Set(items.map((m) => m.id))
  const undone: ArchivedMessage[] = []
  for (const item of items) {
    try {
      const undo = await archiveMessage(item.id)
      reportArchiveExport(undo)
      const entry = archivedEntry(item, undo)
      if (entry) {
        undone.push(entry)
      }
      leaveList([item.id], batch)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
  recordArchivedBatch(undone)
}
