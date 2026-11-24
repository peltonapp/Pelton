// organize.ts holds pin, mute and private notes for every surface that offers
// them: the list's context menu, the reader's action bar, the command palette
// and the shortcuts. Each patches what is on screen first, then asks the
// backend, and offers an undo toast for pin and mute.

import { get, writable } from 'svelte/store'
import type { MessageSummary } from './types'
import { setConversationMuted, setPinned, snoozeMessage, unsnoozeMessage } from './api'
import { t } from './i18n'
import { messageList, patchInList, removeFromList, repinInList, restoreToList, rowPinned } from '../stores/messages'
import { messageDetail } from '../stores/message'
import { openMessage, openMessageId, selection } from '../stores/selection'
import { sidebar } from '../stores/accounts'
import { prefs } from '../stores/prefs'
import { threadedOn } from '../stores/threads'
import { isInboxSelection } from '../stores/splits'
import { errorMessage, pushAction, toastError } from '../stores/toast'
import { recordHistory, stateKey, subjectDetail, undoEntry } from '../stores/undohistory'

// undoTTL is how long an undo toast stays up.
const undoTTL = 5000

/** Whether an action on this message reaches its whole conversation. */
export function actsOnConversation(item: Pick<MessageSummary, 'threadId'>): boolean {
  return threadedOn(get(prefs)) && item.threadId !== 0
}

/** Whether a message, or its conversation in a grouped list, is pinned. */
export function isPinned(item: MessageSummary): boolean {
  const data = get(messageList).data
  const row = data?.items.find((m) => m.id === item.id)
  return rowPinned(row ?? item, data?.threads)
}

// patchDetail carries a change into the reading pane when it shows the message.
function patchDetail(id: number, patch: Partial<MessageSummary>): void {
  messageDetail.update((s) => (s.data?.id === id ? { ...s, data: { ...s.data, ...patch } } : s))
}

// applyPin pins or unpins on screen, then on the backend. It puts the screen
// back and throws when the backend refuses.
async function applyPin(item: MessageSummary, pinned: boolean, conversation: boolean): Promise<void> {
  repinInList(item.id, pinned)
  patchDetail(item.id, { pinned })
  try {
    await setPinned(item.id, pinned, conversation)
  } catch (err) {
    repinInList(item.id, !pinned)
    patchDetail(item.id, { pinned: !pinned })
    throw err
  }
}

/** Pins or unpins a message, or its conversation in a grouped list, with an undo toast. */
export async function togglePin(item: MessageSummary): Promise<void> {
  const conversation = actsOnConversation(item)
  const next = !isPinned(item)
  try {
    await applyPin(item, next, conversation)
  } catch (err) {
    toastError(errorMessage(err))
    return
  }
  const label = get(t)
  const entry = recordHistory({
    kind: 'pin',
    label: label(next ? 'history.kind.pinned' : 'history.kind.unpinned'),
    detail: subjectDetail([item.subject]),
    count: 1,
    touches: [stateKey('pin', item.id)],
    undo: () => applyPin(item, !next, conversation),
    redo: () => applyPin(item, next, conversation),
  })
  const message = conversation
    ? label(next ? 'pin.toast.conversationPinned' : 'pin.toast.conversationUnpinned')
    : label(next ? 'pin.toast.pinned' : 'pin.toast.unpinned')
  pushAction('info', message, { label: label('menu.undo'), run: () => void undoEntry(entry) }, undoTTL)
}

// mutedLeavesList reports whether the list on screen is an inbox, which a
// muted conversation leaves.
function mutedLeavesList(): boolean {
  const data = get(messageList).data
  return !!data && !data.searching && isInboxSelection(get(selection), get(sidebar).data)
}

// applyMute mutes or unmutes on screen, then on the backend. A conversation
// muted in an inbox leaves the list, and comes back on unmute.
async function applyMute(item: MessageSummary, muted: boolean): Promise<void> {
  const leaves = mutedLeavesList()
  const threadId = item.threadId
  const rows = (get(messageList).data?.items ?? []).filter((m) => m.id === item.id || (threadId !== 0 && m.threadId === threadId))
  if (muted && leaves) {
    for (const row of rows) {
      removeFromList(row.id)
    }
    if (get(openMessageId) === item.id) {
      openMessageId.set(null)
    }
  } else {
    for (const row of rows) {
      patchInList(row.id, { muted })
    }
  }
  patchDetail(item.id, { muted })
  await setConversationMuted(item.id, muted)
  if (!muted && leaves && rows.length === 0) {
    restoreToList({ ...item, muted: false })
  }
}

/** Mutes or unmutes the conversation a message belongs to, with an undo toast. */
export async function toggleMute(item: MessageSummary): Promise<void> {
  if (item.threadId === 0) {
    toastError(get(t)('mute.notThreaded'))
    return
  }
  const next = !item.muted
  try {
    await applyMute(item, next)
  } catch (err) {
    toastError(errorMessage(err))
    return
  }
  const label = get(t)
  const entry = recordHistory({
    kind: 'mute',
    label: label(next ? 'history.kind.muted' : 'history.kind.unmuted'),
    detail: subjectDetail([item.subject]),
    count: 1,
    touches: [stateKey('mute', item.id), stateKey('loc', item.id)],
    undo: () => applyMute(item, !next),
    redo: () => applyMute(item, next),
  })
  pushAction('info', label(next ? 'mute.toast.muted' : 'mute.toast.unmuted'), { label: label('menu.undo'), run: () => void undoEntry(entry) }, undoTTL)
}

/**
 * Records a snooze that was just applied to messages. Undo cancels it and
 * brings back a row the snooze hid; redo snoozes again. rows are the list's
 * rows for the ids, when it had them.
 */
export function recordSnooze(ids: number[], rows: ReadonlyMap<number, MessageSummary>, subject: string, until: string, hideNow: boolean): void {
  const show = (id: number): void => {
    const row = rows.get(id)
    if (hideNow && row) {
      restoreToList({ ...row, snoozeUntil: '' })
    } else {
      patchInList(id, { snoozeUntil: '' })
    }
  }
  const apply = async (step: (id: number) => Promise<void>): Promise<void> => {
    let failure: unknown = null
    for (const id of ids) {
      try {
        await step(id)
      } catch (err) {
        failure = failure ?? err
      }
    }
    if (failure !== null) {
      throw failure
    }
  }
  recordHistory({
    kind: 'snooze',
    label: get(t)('history.kind.snoozed'),
    detail: subjectDetail(ids.length === 1 ? [subject] : ids.map(() => '')),
    count: ids.length,
    touches: ids.flatMap((id) => [stateKey('snooze', id), stateKey('loc', id)]),
    undo: () =>
      apply(async (id) => {
        await unsnoozeMessage(id)
        show(id)
      }),
    redo: () =>
      apply(async (id) => {
        await snoozeMessage(id, until, hideNow)
        if (hideNow) {
          removeFromList(id)
        } else {
          patchInList(id, { snoozeUntil: until })
        }
      }),
  })
}

/**
 * The message whose note the reader should open for editing. The reader's
 * note card takes it and clears it.
 */
export const noteRequest = writable<number | null>(null)

/** Opens the private note of a message, or of its conversation, in the reader. */
export function editNote(item: MessageSummary): void {
  if (get(openMessageId) !== item.id) {
    openMessage(item.id)
  }
  noteRequest.set(item.id)
}

/** Marks the rows a note belongs to as having one, or not. */
export function markNote(messageId: number, threadId: number, hasNote: boolean): void {
  for (const row of get(messageList).data?.items ?? []) {
    if (row.id === messageId || (threadId !== 0 && row.threadId === threadId)) {
      patchInList(row.id, { hasNote })
    }
  }
  patchDetail(messageId, { hasNote })
}
