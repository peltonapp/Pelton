// sendvariants.ts decides what a send does besides sending: Send and archive
// archives the conversation a reply or forward answered, Send and remind sets
// a follow-up reminder on the message, and plain Send archives too when the
// afterReplySend pref asks for it.

import type { ComposeSession } from '../stores/compose'
import type { AfterReplySend, FollowUpInterval } from './types'
import { buildFollowUpPresets } from './composeactions'

/** Which send the user asked for. */
export type SendVariant = 'send' | 'archive' | 'remind'

/** What a send does on top of queueing the message. */
export interface SendPlan {
  /** The answered message whose conversation is archived after the send, or null. */
  archiveId: number | null
  /** The follow-up reminder to send the message with, or null to keep the session's own. */
  followUpAt: string | null
}

/** The prefs a send plan reads. */
export interface SendPlanPrefs {
  afterReplySend: AfterReplySend
  sendRemindAfter: FollowUpInterval
}

/** canArchiveAfter reports whether a session answers a message, so sending can archive it. */
export function canArchiveAfter(session: Pick<ComposeSession, 'origin'>): boolean {
  return session.origin !== undefined
}

/** remindAt is when a reminder of the given interval comes, counted from now. */
export function remindAt(interval: FollowUpInterval, now: Date): Date {
  const presets = buildFollowUpPresets(now, (k) => k)
  return (presets.find((p) => p.key === interval) ?? presets[0]).when
}

/**
 * sendPlan works out what a send of the given variant does. Send and remind
 * uses the interval it was picked with, or the default one; every send of a
 * reply archives when it is Send and archive or the pref says so.
 */
export function sendPlan(
  session: Pick<ComposeSession, 'origin'>,
  variant: SendVariant,
  prefs: SendPlanPrefs,
  now: Date,
  interval?: FollowUpInterval,
): SendPlan {
  const archive = canArchiveAfter(session) && (variant === 'archive' || prefs.afterReplySend === 'archive')
  return {
    archiveId: archive ? (session.origin?.id ?? null) : null,
    followUpAt: variant === 'remind' ? remindAt(interval ?? prefs.sendRemindAfter, now).toISOString() : null,
  }
}
