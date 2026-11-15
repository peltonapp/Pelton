// undosend.ts orchestrates the delayed-send window. when the send delay is on, a
// message is queued but held by the backend; this schedules a countdown toast
// with an undo button and exposes the pending undo so a global cmd+z can trigger
// it too. undo cancels the still-queued message and reopens it as a draft.

import { writable, get } from 'svelte/store'
import { cancelSend } from '../lib/api'
import { reopenSession, type ComposeSession } from './compose'
import { pushAction, updateToast, dismiss, toastInfo, toastError, errorMessage } from './toast'
import { loadOutbox } from './outbox'

// pendingUndo holds the active undo callback (or null). App.svelte calls it on
// cmd+z. only one send can be pending its window at a time, which matches the
// short delays offered.
export const pendingUndo = writable<(() => void) | null>(null)

// scheduleUndo shows the countdown toast for a held message and wires undo. The
// session is the exact compose state that was sent, used to reopen on undo.
// committed runs once the message is past taking back: the window ran out, or
// an undo came too late.
export function scheduleUndo(messageId: number, session: ComposeSession, delaySeconds: number, committed?: () => void): void {
  let remaining = delaySeconds
  let done = false

  const finish = (): void => {
    done = true
    clearInterval(timer)
    if (get(pendingUndo) === run) {
      pendingUndo.set(null)
    }
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

  const toastId = pushAction(
    'info',
    `Sending in ${remaining}s…`,
    { label: 'Undo', run },
    // keep the toast up for the whole window plus a moment of slack.
    (delaySeconds + 1) * 1000,
  )
  pendingUndo.set(run)

  const timer = setInterval(() => {
    remaining -= 1
    if (remaining <= 0) {
      finish()
      committed?.()
      return
    }
    updateToast(toastId, `Sending in ${remaining}s…`)
  }, 1000)
}

// triggerUndo runs the pending undo if there is one, returning whether it acted.
// the cmd+z handler uses the return to decide whether to swallow the keystroke.
export function triggerUndo(): boolean {
  const run = get(pendingUndo)
  if (run) {
    run()
    return true
  }
  return false
}
