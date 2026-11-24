// snooze.ts drives the snooze dialog. a row's context menu (or a shortcut) calls
// openSnooze with the message, and a multi-selection calls openSnoozeMany; the
// dialog reads snoozeTarget, collects one time and the "hide now" choice, and
// applies it to every message in the target. keeping the target in a store lets
// the dialog live once at the app root instead of per row.

import { writable } from 'svelte/store'
import { noteAction } from '../lib/coach'

export interface SnoozeTarget {
  ids: number[]
  // the subject of the one message, or empty for a batch, which the dialog
  // names by count instead.
  subject: string
}

export const snoozeTarget = writable<SnoozeTarget | null>(null)

export function openSnooze(id: number, subject: string): void {
  noteAction('snooze')
  snoozeTarget.set({ ids: [id], subject })
}

export function openSnoozeMany(ids: number[]): void {
  if (ids.length === 0) {
    return
  }
  noteAction('snooze')
  snoozeTarget.set({ ids: [...ids], subject: '' })
}

export function closeSnooze(): void {
  snoozeTarget.set(null)
}
