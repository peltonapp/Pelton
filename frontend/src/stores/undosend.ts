// undosend.ts orchestrates the delayed-send window. when the send delay is on, a
// message is queued but held by the backend; this schedules a countdown toast
// with an undo button and lists the send in the undo history while the window
// is open, so a global cmd+z can take it back too. undo cancels the still-queued
// message and reopens it as a draft. once the window closes the send is
// permanent and leaves the history.

import { get } from 'svelte/store'
import { cancelSend } from '../lib/api'
import { t } from '../lib/i18n'
import { reopenSession, type ComposeSession } from './compose'
import { pushAction, updateToast, dismiss, toastInfo, toastError, errorMessage } from './toast'
import { loadOutbox } from './outbox'
import { dropHistoryEntry, recordHistory, subjectDetail, undoEntry } from './undohistory'

// scheduleUndo shows the countdown toast for a held message and wires undo. The
// session is the exact compose state that was sent, used to reopen on undo.
// committed runs once the message is past taking back: the window ran out, or
// an undo came too late.
export function scheduleUndo(messageId: number, session: ComposeSession, delaySeconds: number, committed?: () => void): void {
  let remaining = delaySeconds
  let done = false
  let restored = false

  const finish = (): void => {
    done = true
    clearInterval(timer)
  }

  const run = async (): Promise<void> => {
    if (done) {
      return
    }
    finish()
    dismiss(toastId)
    try {
      const cancelled = await cancelSend(messageId)
      if (cancelled) {
        restored = true
        reopenSession(session)
        toastInfo('Send undone. Your message is back as a draft.')
      } else {
        toastError('Too late to undo, the message is already on its way.')
        committed?.()
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      await loadOutbox()
    }
  }

  const entry = recordHistory({
    kind: 'send',
    label: get(t)('history.kind.send'),
    detail: subjectDetail([session.subject]),
    count: 1,
    touches: [`send:${messageId}`],
    quiet: true,
    undo: async () => {
      await run()
      if (!restored) {
        dropHistoryEntry(entry)
      }
    },
  })

  const toastId = pushAction(
    'info',
    `Sending in ${remaining}s…`,
    { label: 'Undo', run: () => void undoEntry(entry) },
    // keep the toast up for the whole window plus a moment of slack.
    (delaySeconds + 1) * 1000,
  )

  const timer = setInterval(() => {
    remaining -= 1
    if (remaining <= 0) {
      finish()
      dropHistoryEntry(entry)
      committed?.()
      return
    }
    updateToast(toastId, `Sending in ${remaining}s…`)
  }, 1000)
}
