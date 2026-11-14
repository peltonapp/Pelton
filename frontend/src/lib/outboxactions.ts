// outboxactions.ts is what can be done with a message in the outbox, shared by
// the classic status bar panel and the new design's activity popover: read
// its state, cancel or send a scheduled one now, retry or discard a failed
// one, and take either back into a composer.

import { get } from 'svelte/store'
import { cancelSend, discardFailedSend, editOutboxMessage, retrySend, sendOutboxNow } from './api'
import { loadOutbox } from '../stores/outbox'
import { openRequestSession } from '../stores/compose'
import { errorMessage, toastError, toastInfo } from '../stores/toast'
import { t } from './i18n'
import type { OutboxRow } from './types'

// rows queued for longer than this out from now count as an explicitly
// scheduled send rather than sitting in the short undo-send window.
const scheduledThresholdMs = 60 * 1000

/** isScheduled reports whether a queued row is a future "send later" send. */
export function isScheduled(row: OutboxRow, now = Date.now()): boolean {
  if (row.state !== 'queued' || !row.nextAttemptAt) {
    return false
  }
  return new Date(row.nextAttemptAt).getTime() - now > scheduledThresholdMs
}

/** recipientLabel is the first recipient, with a count of the others. */
export function recipientLabel(row: OutboxRow, tr: (key: string) => string): string {
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
      openRequestSession(taken.request)
    } else {
      toastError(get(t)('outbox.editTooLate'))
    }
  })
}
