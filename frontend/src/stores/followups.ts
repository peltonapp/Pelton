// followups.ts holds the "remind me if nobody replies" reminders. The backend
// decides when one is answered or due (bind_followups.go); this store mirrors
// the list, reloading whenever the backend says it changed, so the inbox band,
// the reader and the activity popover all show the same reminders.

import { derived, get, writable } from 'svelte/store'
import type { FollowUp } from '../lib/types'
import { cancelFollowUp, listFollowUps } from '../lib/api'
import { onFollowUpsChanged } from '../lib/events'
import { errorMessage, toastError } from './toast'

/** Every reminder, the soonest due first. */
export const followUps = writable<FollowUp[]>([])

/** Reminders still waiting for their time or a reply. */
export const pendingFollowUps = derived(followUps, (list) => list.filter((f) => f.firedAt === ''))

/** Reminders that came due with no reply, the most recent first. */
export const dueFollowUps = derived(followUps, (list) =>
  list.filter((f) => f.firedAt !== '').sort((a, b) => Date.parse(b.firedAt) - Date.parse(a.firedAt)),
)

// a reload asked for while one is out supersedes it.
let generation = 0

/** loadFollowUps reads the reminders again. A failure keeps what is shown. */
export async function loadFollowUps(): Promise<void> {
  const mine = ++generation
  try {
    const list = await listFollowUps()
    if (mine === generation) {
      followUps.set(list)
    }
  } catch {
    // the list on screen is still the last good one; the next event retries.
  }
}

/**
 * watchFollowUps loads the reminders and keeps them current, returning the
 * function that stops watching.
 */
export function watchFollowUps(): () => void {
  void loadFollowUps()
  return onFollowUpsChanged(() => void loadFollowUps())
}

/**
 * dropFollowUp cancels a waiting reminder or dismisses a fired one. It leaves
 * the list at once and comes back if the backend refuses.
 */
export async function dropFollowUp(id: number): Promise<void> {
  const before = get(followUps)
  followUps.set(before.filter((f) => f.id !== id))
  try {
    await cancelFollowUp(id)
  } catch (err) {
    followUps.set(before)
    toastError(errorMessage(err))
  }
}

/** bareId strips the angle brackets and space a Message-ID may carry. */
export function bareId(messageId: string): string {
  return messageId.trim().replace(/^</, '').replace(/>$/, '').trim()
}

/** followUpFor finds the reminder on the message with this Message-ID. */
export function followUpFor(list: FollowUp[], messageId: string): FollowUp | null {
  const key = bareId(messageId)
  return key === '' ? null : (list.find((f) => f.messageId === key) ?? null)
}

/**
 * followUpsIn narrows reminders to what a list shows: every account for the
 * unified inbox (accountId 0), or one account's own.
 */
export function followUpsIn(list: FollowUp[], accountId: number): FollowUp[] {
  return accountId === 0 ? list : list.filter((f) => f.accountId === accountId)
}
