// outboxactions.ts is what can be done with a message in the outbox, shared by
// the classic status bar panel and the new design's activity popover: read
// its state, cancel or send a scheduled one now, retry or discard a failed
// one, take either back into a composer, and cancel or move to the front a
// message that is going out now.

import { get } from 'svelte/store'
import {
  cancelOutboxSend,
  cancelSend,
  discardFailedSend,
  editOutboxMessage,
  prioritiseOutboxSend,
  retrySend,
  sendOutboxNow,
} from './api'
import { loadOutbox } from '../stores/outbox'
import { openRequestSession } from '../stores/compose'
import { errorMessage, toastError, toastInfo } from '../stores/toast'
import { t } from './i18n'
import type { ComposeRequest, OutboxRow } from './types'

// rows queued for longer than this out from now count as an explicitly
// scheduled send rather than sitting in the short undo-send window.
const scheduledThresholdMs = 60 * 1000

// sendFirstThreshold is how many messages have to be waiting before one can
// be moved to the front: with two or fewer the order hardly matters.
const sendFirstThreshold = 2

// openRequest opens a request taken back out of the outbox in a composer.
let openRequest: (req: ComposeRequest) => void | Promise<void> = (req) => {
  openRequestSession(req)
}

/**
 * setRequestOpener replaces how a request taken back out of the outbox is
 * opened, for a window that has no compose panes of its own.
 */
export function setRequestOpener(open: (req: ComposeRequest) => void | Promise<void>): void {
  openRequest = open
}

/**
 * sendingOrder sorts the rows going out now the way the worker takes them:
 * what is being transmitted first, then the waiting ones by priority and age.
 */
export function sendingOrder(rows: OutboxRow[]): OutboxRow[] {
  return [...rows].sort((a, b) => {
    const sendingFirst = Number(b.state === 'sending') - Number(a.state === 'sending')
    return sendingFirst || b.priority - a.priority || a.id - b.id
  })
}

/**
 * sendFirstIds is the waiting rows that can be moved to the front: every one
 * but the first, once more than two are waiting. rows is in sendingOrder.
 */
export function sendFirstIds(rows: OutboxRow[]): Set<number> {
  const waiting = rows.filter((row) => row.state === 'queued')
  if (waiting.length <= sendFirstThreshold) {
    return new Set()
  }
  return new Set(waiting.slice(1).map((row) => row.id))
}

/** isScheduled reports whether a queued row is a future "send later" send. */
export function isScheduled(row: OutboxRow, now = Date.now()): boolean {
  if (row.state !== 'queued' || !row.nextAttemptAt) {
    return false
  }
  return new Date(row.nextAttemptAt).getTime() - now > scheduledThresholdMs
}

/** recipientLabel is the first recipient, with a count of the others. */
export function recipientLabel(row: Pick<OutboxRow, 'recipients'>, tr: (key: string) => string): string {
  if (row.recipients.length === 0) {
    return tr('common.outboxPanel.noRecipients')
  }
  if (row.recipients.length === 1) {
    return row.recipients[0]
  }
  return `${row.recipients[0]} +${row.recipients.length - 1}`
}

// act runs one outbox action, reports a failure, and reloads the outbox
// whatever happened.
async function act(run: () => Promise<void>): Promise<void> {
  try {
    await run()
  } catch (err) {
    toastError(errorMessage(err))
  } finally {
    await loadOutbox()
  }
}

/** cancelScheduled pulls a still-queued scheduled send back out of the outbox. */
export function cancelScheduled(id: number): Promise<void> {
  return act(async () => {
    const tr = get(t)
    if (await cancelSend(id)) {
      toastInfo(tr('common.outboxPanel.scheduledCancelled'))
    } else {
      toastError(tr('common.outboxPanel.scheduledCancelTooLate'))
    }
  })
}

/** sendNow sends a scheduled message at once instead of at its time. */
export function sendNow(id: number): Promise<void> {
  return act(async () => {
    const tr = get(t)
    if (await sendOutboxNow(id)) {
      toastInfo(tr('outbox.sendingNow'))
    } else {
      toastError(tr('common.outboxPanel.scheduledCancelTooLate'))
    }
  })
}

/** retry puts a failed message back in the queue for another try. */
export function retry(id: number): Promise<void> {
  return act(async () => {
    if (await retrySend(id)) {
      toastInfo(get(t)('common.outboxPanel.retrying'))
    }
  })
}

/** discard removes a failed message for good; callers confirm first. */
export function discard(id: number): Promise<void> {
  return act(async () => {
    await discardFailedSend(id)
  })
}

/** edit takes a scheduled or failed message out of the outbox into a composer. */
export function edit(id: number): Promise<void> {
  return act(async () => {
    const taken = await editOutboxMessage(id)
    if (taken.taken) {
      await openRequest(taken.request)
    } else {
      toastError(get(t)('outbox.editTooLate'))
    }
  })
}

/**
 * cancelSending stops a message that is waiting or being transmitted. It goes
 * back to Drafts, or into a composer when it cannot, and a send the server
 * already took is reported as too late rather than cancelled.
 */
export function cancelSending(id: number): Promise<void> {
  return act(async () => {
    const tr = get(t)
    const result = await cancelOutboxSend(id)
    switch (result.outcome) {
      case 'draft':
        toastInfo(tr('outbox.cancel.toDrafts'))
        break
      case 'compose':
        await openRequest(result.request)
        toastInfo(tr('outbox.cancel.toComposer'))
        break
      case 'held':
        toastInfo(tr('outbox.cancel.held'))
        break
      case 'tooLate':
        toastError(tr('outbox.cancel.tooLate'))
        break
      default:
        toastError(tr('outbox.cancel.gone'))
    }
  })
}

/** sendFirst moves a waiting message to the front of the queue. */
export function sendFirst(id: number): Promise<void> {
  return act(async () => {
    if (await prioritiseOutboxSend(id)) {
      toastInfo(get(t)('outbox.sendFirst.done'))
    } else {
      toastError(get(t)('outbox.cancel.gone'))
    }
  })
}
