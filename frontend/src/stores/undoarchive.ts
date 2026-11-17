// undoarchive.ts records moves (archive, move to a folder, spam marks, done) in
// the undo history. a move happens on the server and drops the local row, so
// undo re-locates the message in the folder it went to by its rfc Message-ID
// and moves it back, and redo moves it forward again the same way. a move with
// no Message-ID cannot be found afterwards, so it is not recorded.
//
// One entry is one action, not one message, so archiving a selection of eleven
// is undone in one press.

import { get } from 'svelte/store'
import type { MessageSummary } from '../lib/types'
import { restoreMessage, type ArchiveUndo } from '../lib/api'
import { t } from '../lib/i18n'
import { messageKeys, recordHistory, subjectDetail, type HistoryKind } from './undohistory'

interface ArchivedMessage {
  summary: MessageSummary
  messageId: string
  originalFolderId: number
  destFolderId: number
}

/** The kinds of move the history records, and the words for one when the default does not fit. */
export interface MoveRecord {
  kind?: Extract<HistoryKind, 'archive' | 'move' | 'spam' | 'done'>
  label?: string
}

// recordArchived remembers a just-moved message so it can be moved back. A move
// with no Message-ID cannot be undone and is not recorded.
export function recordArchived(summary: MessageSummary, undo: ArchiveUndo, record?: MoveRecord): void {
  const entry = archivedEntry(summary, undo)
  if (entry) {
    recordArchivedBatch([entry], record)
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

// moveAll runs one move per message, in order, and reports the first failure
// after all of them were tried: a message that is already gone cannot hold back
// the ones that are not.
async function moveAll(moves: ArchivedMessage[], forward: boolean): Promise<void> {
  let failure: unknown = null
  for (const m of moves) {
    try {
      if (forward) {
        await restoreMessage(m.messageId, m.originalFolderId, m.destFolderId)
      } else {
        await restoreMessage(m.messageId, m.destFolderId, m.originalFolderId)
      }
    } catch (err) {
      failure = failure ?? err
    }
  }
  if (failure !== null) {
    throw failure
  }
}

// recordArchivedBatch remembers a whole bulk archive or move as one undo step,
// and returns the history entry's id, or null when there was nothing to record.
export function recordArchivedBatch(entries: ArchivedMessage[], record: MoveRecord = {}): number | null {
  if (entries.length === 0) {
    return null
  }
  const kind = record.kind ?? 'archive'
  return recordHistory({
    kind,
    label: record.label ?? get(t)(kind === 'done' ? 'history.kind.done' : 'history.kind.archive'),
    detail: subjectDetail(entries.map((e) => e.summary.subject)),
    count: entries.length,
    touches: entries.flatMap((e) => messageKeys(e.summary.id)),
    rekeys: true,
    undo: () => moveAll(entries, false),
    redo: () => moveAll(entries, true),
  })
}

export type { ArchivedMessage }
