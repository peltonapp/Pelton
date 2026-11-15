// triage.ts is "done": archive what is open or selected, conversations whole,
// and move the reader straight on to the next one, the way a run through an
// inbox should go. Where the reader goes next follows the afterMove
// preference through leaveList.

import { get } from 'svelte/store'
import { archiveMessage } from './api'
import type { MessageSummary, ThreadMessage, ThreadSummary } from './types'
import { nextAfterLeaving, reportArchiveExport } from './messageactions'
import { holdRegroup, messageList, removeFromList, restoreToList } from '../stores/messages'
import { loadThread, threadedOn } from '../stores/threads'
import { prefs } from '../stores/prefs'
import { openMessageId } from '../stores/selection'
import { clearSelection } from '../stores/listselect'
import { closeTab } from '../stores/tabs'
import { archivedEntry, recordArchivedBatch, type ArchivedMessage } from '../stores/undoarchive'
import { errorMessage, toastError } from '../stores/toast'
import { dropFollowUp, dueFollowUps } from '../stores/followups'

// folders whose mail a conversation's done leaves where it is: your own
// replies in Sent, drafts, and what is already in Archive, Trash or Junk.
const keptRoles = new Set(['sent', 'drafts', 'archive', 'trash', 'junk'])

/** archivable reports whether done moves a member of a conversation. */
export function archivable(member: Pick<ThreadMessage, 'folderRole'>): boolean {
  return !keptRoles.has(member.folderRole)
}

/**
 * conversationOf finds the loaded conversation a message belongs to, by its
 * thread, or null when the list is not grouped or the message has none.
 */
export function conversationOf(
  item: MessageSummary,
  threads: Record<number, ThreadSummary> | undefined,
): ThreadSummary | null {
  if (!threads || item.threadId === 0) {
    return null
  }
  return threads[item.id] ?? Object.values(threads).find((t) => t.threadId === item.threadId) ?? null
}

/**
 * doneTargets expands messages into everything done archives: a message on
 * its own, or every member of its conversation that sits in a folder done
 * moves mail out of. members reads a conversation's messages. The result has
 * no duplicates and keeps the order it was found in.
 */
export async function doneTargets(
  items: MessageSummary[],
  threads: Record<number, ThreadSummary> | undefined,
  members: (threadId: number) => Promise<ThreadMessage[]>,
): Promise<MessageSummary[]> {
  const out = new Map<number, MessageSummary>()
  for (const item of items) {
    const thread = conversationOf(item, threads)
    if (!thread || thread.count < 2) {
      out.set(item.id, item)
      continue
    }
    const moving = (await members(thread.threadId)).filter(archivable)
    if (moving.length === 0) {
      out.set(item.id, item)
    }
    for (const m of moving) {
      out.set(m.id, m)
    }
  }
  return [...out.values()]
}

/**
 * markDone archives the given messages, each with its whole conversation when
 * the list is grouped, as one undo step. The rows leave the list and the
 * reader moves on before the server hears about it, so the next conversation
 * is on screen at once; a move that fails puts its row back.
 */
export async function markDone(items: MessageSummary[]): Promise<void> {
  if (items.length === 0) {
    return
  }
  clearSelection()
  const data = get(messageList).data
  const threads = threadedOn(get(prefs)) ? data?.threads : undefined
  let targets: MessageSummary[]
  try {
    targets = await doneTargets(items, threads, loadThread)
  } catch (err) {
    toastError(errorMessage(err))
    return
  }
  const leaving = new Set([...items.map((m) => m.id), ...targets.map((m) => m.id)])
  // the reader may be on any message of a conversation that is going, a reply
  // of your own in Sent included; it moves on from that conversation's row.
  const open = get(openMessageId)
  const openRow =
    open === null
      ? undefined
      : (items.find((i) => i.id === open) ??
        items.find((i) => conversationOf(i, threads)?.messageIds.includes(open)) ??
        (leaving.has(open) ? items[0] : undefined))
  const next = openRow ? nextAfterLeaving(openRow.id, leaving, false) : null
  const release = holdRegroup()
  for (const id of leaving) {
    closeTab(id)
    removeFromList(id)
  }
  if (openRow) {
    openMessageId.set(next)
  }

  const undone: ArchivedMessage[] = []
  let failure = ''
  for (const target of targets) {
    try {
      const undo = await archiveMessage(target.id)
      reportArchiveExport(undo)
      const entry = archivedEntry(target, undo)
      if (entry) {
        undone.push(entry)
      }
    } catch (err) {
      failure = errorMessage(err)
      if (items.some((i) => i.id === target.id)) {
        restoreToList(target)
      }
    }
  }
  release()
  recordArchivedBatch(undone)
  // a conversation marked done is handled, so a reminder that came due on it
  // has done its job.
  const threadIds = new Set(items.map((i) => i.threadId).filter((id) => id !== 0))
  for (const f of get(dueFollowUps)) {
    if (threadIds.has(f.threadId)) {
      void dropFollowUp(f.id)
    }
  }
  if (failure !== '') {
    toastError(failure)
  }
}
