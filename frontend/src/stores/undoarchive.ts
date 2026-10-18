// undoarchive.ts keeps a small stack of recently moved messages (archive, move
// to a folder, spam marks) so a global cmd+z can move the last one back to where
// it came from. a move happens on the server and drops the local row, so undo
// re-locates the message in the folder it went to by its rfc Message-ID and
// moves it back. undo is impossible for a message with no Message-ID (rare),
// which we surface rather than silently fail.
//
// One entry is one action, not one message, so archiving a selection of eleven
// is undone in one press.

import { get, writable } from 'svelte/store'
import type { MessageSummary } from '../lib/types'
import { restoreMessage, type ArchiveUndo } from '../lib/api'
import { t } from '../lib/i18n'
import { toastInfo, toastError, errorMessage } from './toast'

interface ArchivedMessage {
  summary: MessageSummary
  messageId: string
  originalFolderId: number
  destFolderId: number
}

const archived = writable<ArchivedMessage[][]>([])

// recordArchived remembers a just-moved message so it can be moved back. A move
// with no Message-ID cannot be undone and is not recorded.
export function recordArchived(summary: MessageSummary, undo: ArchiveUndo): void {
  const entry = archivedEntry(summary, undo)
  if (entry) {
    recordArchivedBatch([entry])
  }
}

// archivedEntry is the undo record for one move, or null when the message had
// no Message-ID to find it by afterwards.
export function archivedEntry(summary: MessageSummary, undo: ArchiveUndo): ArchivedMessage | null {
  if (!undo.messageId) {
    return null
  }
  return { summary, messageId: undo.messageId, originalFolderId: undo.originalFolderId, destFolderId: undo.destFolderId }
}

// recordArchivedBatch remembers a whole bulk archive or move as one undo step.
export function recordArchivedBatch(entries: ArchivedMessage[]): void {
  if (entries.length === 0) {
    return
  }
  archived.update((s) => [...s, entries])
}

// triggerUndoArchive restores the most recent archive, however many messages it
// covered. It returns true when it had something to undo (and kicks off the
// async move), false otherwise.
export function triggerUndoArchive(): boolean {
  const stack = get(archived)
  if (stack.length === 0) {
    return false
  }
  const last = stack[stack.length - 1]
  archived.set(stack.slice(0, -1))
  void (async () => {
    let failure = ''
    for (const entry of last) {
      try {
        await restoreMessage(entry.messageId, entry.destFolderId, entry.originalFolderId)
      } catch (err) {
        failure = errorMessage(err)
      }
    }
    if (failure !== '') {
      toastError(failure)
      return
    }
    const label = get(t)
    toastInfo(last.length === 1 ? label('undo.archiveDone') : label('undo.archiveDoneCount').replace('{n}', String(last.length)))
  })()
  return true
}

export type { ArchivedMessage }
