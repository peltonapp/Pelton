// confirm.ts is the app's one "are you sure" prompt. An action that needs it
// awaits askConfirm and gets a boolean back, so the rule lives with the action
// rather than with each button that can start it: a bulk delete asks the same
// question whether it came from the menu, the toolbar, a shortcut or the
// command palette.

import { writable, get } from 'svelte/store'

export interface ConfirmRequest {
  title: string
  /** The line under the heading. Empty leaves it out. */
  body: string
  /** Label of the button that goes ahead; the other one always cancels. */
  confirmLabel: string
  /** Label of the other button. Empty shows Cancel. */
  cancelLabel: string
  /** Paints the confirm button as destructive. */
  danger: boolean
  resolve: (ok: boolean) => void
}

// the open request, or null when nothing is being asked.
export const confirmRequest = writable<ConfirmRequest | null>(null)

interface AskOptions {
  title: string
  body?: string
  confirmLabel: string
  cancelLabel?: string
  danger?: boolean
}

// askConfirm opens the dialog and resolves with what the user chose. A second
// question while one is open answers the first with false rather than stacking
// prompts, which only ever happens if an action fires twice.
export function askConfirm(options: AskOptions): Promise<boolean> {
  get(confirmRequest)?.resolve(false)
  return new Promise<boolean>((resolve) => {
    confirmRequest.set({
      title: options.title,
      body: options.body ?? '',
      confirmLabel: options.confirmLabel,
      cancelLabel: options.cancelLabel ?? '',
      danger: options.danger ?? false,
      resolve: (ok) => {
        confirmRequest.set(null)
        resolve(ok)
      },
    })
  })
}
