// composecommands.ts carries composer actions from wherever they are asked
// for, a shortcut or the command palette, to the composer they act on: the
// one in this window that last had focus. Each Composer2 marks itself active
// on focus and answers the commands addressed to it.

import { get, writable } from 'svelte/store'
import type { ComposeAction } from '../lib/shortcuts'

/** A command on its way to a composer; seq tells two equal commands apart. */
export interface ComposeCommand {
  sessionId: number
  action: ComposeAction
  seq: number
}

/** The session of the composer in this window that last had focus. */
export const activeComposer = writable<number | null>(null)

/** The latest command, which only the addressed composer acts on. */
export const composeCommand = writable<ComposeCommand | null>(null)

let seq = 0

/**
 * runComposeCommand sends an action to the active composer and reports
 * whether there was one to send it to.
 */
export function runComposeCommand(action: ComposeAction): boolean {
  const sessionId = get(activeComposer)
  if (sessionId === null) {
    return false
  }
  composeCommand.set({ sessionId, action, seq: ++seq })
  return true
}

/** releaseComposer forgets a composer that went away, if it was the active one. */
export function releaseComposer(sessionId: number): void {
  if (get(activeComposer) === sessionId) {
    activeComposer.set(null)
  }
}
