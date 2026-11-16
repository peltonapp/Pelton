// undodelete.ts records deletes in the undo history. a delete only marks the row
// pending on the backend (it is expunged on the next sync), so undo is possible
// until then; if the row is already gone the undo call fails and the history
// reports it. redo marks the same rows pending again.
//
// One entry is one action, not one message: deleting eleven selected messages
// is one thing the user did and one press has to put all eleven back.

import { get } from 'svelte/store'
import type { MessageSummary } from '../lib/types'
import { deleteMessage, undoDelete } from '../lib/api'
import { removeFromList, restoreToList } from './messages'
import { t } from '../lib/i18n'
import { messageKeys, recordHistory, subjectDetail } from './undohistory'

// recordDeleted remembers a just-deleted message so it can be undone.
export function recordDeleted(summary: MessageSummary): void {
  recordDeletedBatch([summary])
}

// each runs the step on every message and reports the first failure after all
// were tried: a row that is already expunged cannot hold back the ones that
// are not.
async function each(list: MessageSummary[], step: (m: MessageSummary) => Promise<void>): Promise<void> {
  let failure: unknown = null
  for (const m of list) {
    try {
      await step(m)
    } catch (err) {
      failure = failure ?? err
    }
  }
  if (failure !== null) {
    throw failure
  }
}

// recordDeletedBatch remembers a whole bulk delete as one undo step.
export function recordDeletedBatch(summaries: MessageSummary[]): void {
  if (summaries.length === 0) {
    return
  }
  recordHistory({
    kind: 'delete',
    label: get(t)('history.kind.delete'),
    detail: subjectDetail(summaries.map((m) => m.subject)),
    count: summaries.length,
    touches: summaries.flatMap((m) => messageKeys(m.id)),
    undo: () =>
      each(summaries, async (m) => {
        await undoDelete(m.id)
        restoreToList(m)
      }),
    redo: () =>
      each(summaries, async (m) => {
        await deleteMessage(m.id)
        removeFromList(m.id)
      }),
  })
}
