// composeactions.ts is what a composer does with its session, shared by the
// classic pane and the new composer: send (now, later, or held for undo),
// save a draft locally and to the server Drafts folder, and the checks the
// close prompt and the send-later menu need.

import { get } from 'svelte/store'
import { closeCompose, getSession, restoreSession, updateCompose, type ComposeSession } from '../stores/compose'
import { composeProtectionStatus, deleteDraft, removeServerDraft, saveDraft, sendMessage, uploadDraft } from './api'
import { buildRequest, hasRecipients, parseAddressList } from './mailcompose'
import type { FollowUpInterval, ProtectionStatus } from './types'
import { loadOutbox } from '../stores/outbox'
import { scheduleUndo } from '../stores/undosend'
import { prefs } from '../stores/prefs'
import { errorMessage, toastError, toastInfo, toastSuccess } from '../stores/toast'
import { formatWeekdayTime, type TimeFormat } from './format'
import { addDays, atTime, nextWeekday } from './datepresets'
import { t } from './i18n'

// pending is each session's draft save (local save, then server upload) still
// in flight. It outlives the pane (save and close returns before the upload
// does), and the next save, send or discard waits on it so it reads the draft
// id and Message-ID that save stored rather than making a second copy.
const pending = new Map<number, Promise<void>>()

/**
 * settled waits for a session's save in flight and returns the session as it
 * stands afterwards, carrying the draft id and Message-ID it stored. fallback
 * is returned when the session is gone.
 */
export async function settled(id: number, fallback: ComposeSession): Promise<ComposeSession> {
  await pending.get(id)
  return getSession(id) ?? fallback
}

/** draftState is what a save would store, minus the server link it gains after. */
export function draftState(s: ComposeSession): string {
  return JSON.stringify({ ...buildRequest(s), draftMessageId: '' })
}

/**
 * hasContent decides whether closing needs a save or discard prompt: any
 * recipient, subject, body text or attachment counts.
 */
export function hasContent(s: ComposeSession): boolean {
  return (
    s.to.trim().length > 0 ||
    s.cc.trim().length > 0 ||
    s.bcc.trim().length > 0 ||
    s.subject.trim().length > 0 ||
    s.body.trim().length > 0 ||
    s.attachments.length > 0
  )
}

/**
 * saveSession stores the draft locally, runs stored with the session as saved
 * (the pane closing, for save and close), and puts a copy in the server
 * Drafts folder. It is queued behind the session's save already in flight
 * before anything awaits.
 */
export function saveSession(id: number, fallback: ComposeSession, stored?: (current: ComposeSession) => void): Promise<void> {
  const run = (pending.get(id) ?? Promise.resolve()).then(async () => {
    const current = getSession(id) ?? fallback
    const draftId = await saveDraft(current.draftId, buildRequest(current))
    updateCompose(id, { draftId })
    toastSuccess(get(t)('compose.toast.draftSaved'))
    stored?.(current)
    await upload(current, draftId)
  })
  const done = run.catch((err) => toastError(errorMessage(err)))
  pending.set(id, done)
  void done.then(() => {
    if (pending.get(id) === done) {
      pending.delete(id)
    }
  })
  return done
}

// upload replaces the draft's server copy. An encrypted draft is not uploaded,
// and a plaintext copy an earlier save left there is removed.
async function upload(current: ComposeSession, draftId: number): Promise<void> {
  if (current.protection === 'encrypt' || current.protection === 'signencrypt') {
    toastInfo(get(t)('compose.toast.draftLocalOnly'))
    if (current.draftMessageId) {
      await removeServerDraft(current.accountId, current.draftMessageId)
      updateCompose(current.id, { draftMessageId: '' })
    }
    return
  }
  try {
    updateCompose(current.id, { draftMessageId: await uploadDraft(draftId) })
  } catch (err) {
    toastError(get(t)('compose.toast.draftUploadFailed').replace('{error}', errorMessage(err)))
    // a pane that save and close already shut comes back, since nothing
    // could open a draft kept only on this device.
    if (!getSession(current.id)) {
      restoreSession({ ...current, draftId })
    }
  }
}

/** formatSendWhen renders a send time the way the send-later menu shows it. */
export function formatSendWhen(d: Date): string {
  return formatWeekdayTime(d, get(prefs).timeFormat as TimeFormat)
}

/** Options for sendSession. */
export interface SendOptions {
  /** Holds the message until this time instead of sending it now. */
  scheduledAt?: Date
  /** Runs once the message is queued, before the composer closes. */
  beforeClose?: () => Promise<void>
  /**
   * Runs once the message can no longer be taken back: straight after it is
   * queued, or when the undo-send window closes without an undo.
   */
  afterSent?: () => void
}

/**
 * sendSession enqueues a session's message and closes its composer, and
 * reports whether it was queued. With no scheduledAt it goes through the
 * undo-send delay when one is set; with scheduledAt it is held until that time
 * and skips the undo window (see resolveNotBefore in bind_compose.go).
 */
export async function sendSession(id: number, fallback: ComposeSession, options: SendOptions = {}): Promise<boolean> {
  const tr = get(t)
  if (!hasRecipients(getSession(id) ?? fallback)) {
    toastError(tr('compose.error.noRecipients'))
    return false
  }
  const { scheduledAt, beforeClose, afterSent } = options
  const followUp = (getSession(id) ?? fallback).followUpAt
  if (followUp && Date.parse(followUp) <= (scheduledAt ?? new Date()).getTime()) {
    toastError(tr('followUp.tooEarly'))
    return false
  }
  try {
    const current = await settled(id, fallback)
    const snapshot: ComposeSession = { ...current }
    const req = buildRequest(current)
    if (scheduledAt) {
      req.sendAt = scheduledAt.toISOString()
    }
    const queued = await sendMessage(req)
    if (current.draftId) {
      await deleteDraft(current.draftId)
    }
    await loadOutbox()
    await beforeClose?.()
    closeCompose(id)

    if (scheduledAt) {
      toastSuccess(tr('compose.sendLater.scheduledToast').replace('{when}', formatSendWhen(scheduledAt)))
      afterSent?.()
    } else {
      const delay = get(prefs).sendDelaySeconds
      if (delay > 0) {
        scheduleUndo(queued, snapshot, delay, afterSent)
      } else {
        toastSuccess(tr('compose.toast.queued'))
        afterSent?.()
      }
    }
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** recipientsOf is every address typed into a session, lowercased. */
export function recipientsOf(s: Pick<ComposeSession, 'to' | 'cc' | 'bcc'>): string[] {
  return [s.to, s.cc, s.bcc]
    .flatMap(parseAddressList)
    .map((a) => a.email.toLowerCase())
    .filter((email) => email.includes('@'))
}

/**
 * ProtectionWatch keeps what signing and encryption are possible for one
 * session's account and recipients. A control that offers encryption to
 * somebody with no key is a control that fails at send time, so it is asked
 * again as the recipients change.
 */
export class ProtectionWatch {
  /** The last known answer, null until the first one arrives. */
  status: ProtectionStatus | null = null
  // the account and recipients the status was fetched for, so an unchanged
  // list does not refetch on every keystroke elsewhere in the form.
  private statusFor = ''

  /**
   * suggestedApplied keeps the account default from overriding a choice the
   * user made: a saved draft keeps the protection it was saved with.
   */
  constructor(
    private readonly id: number,
    private suggestedApplied: boolean,
  ) {}

  /**
   * refresh asks the backend what is possible and reports whether the status
   * changed. The account default is applied once; encryption that is no
   * longer possible is dropped. A failure leaves the last answer in place,
   * since the send still refuses on its own when the keys are not there.
   */
  async refresh(accountId: number, recipients: string[]): Promise<boolean> {
    const key = JSON.stringify([accountId, recipients])
    if (key === this.statusFor && this.status !== null) {
      return false
    }
    this.statusFor = key
    try {
      const status = (await composeProtectionStatus(accountId, recipients)) as unknown as ProtectionStatus
      if (key !== this.statusFor) {
        return false
      }
      this.status = status
      if (!this.suggestedApplied) {
        this.suggestedApplied = true
        if (status.suggested !== 'none') {
          updateCompose(this.id, { protection: status.suggested })
        }
      }
      const protection = getSession(this.id)?.protection
      if (!status.canEncrypt && (protection === 'encrypt' || protection === 'signencrypt')) {
        updateCompose(this.id, { protection: protection === 'signencrypt' ? 'sign' : 'none' })
      }
      return true
    } catch {
      return false
    }
  }
}

/** isSigning reports whether a protection value signs. */
export function isSigning(protection: string): boolean {
  return protection === 'sign' || protection === 'signencrypt'
}

/** isEncrypting reports whether a protection value encrypts. */
export function isEncrypting(protection: string): boolean {
  return protection === 'encrypt' || protection === 'signencrypt'
}

/** combineProtection turns the sign and encrypt switches into one value. */
export function combineProtection(sign: boolean, encrypt: boolean): string {
  if (sign && encrypt) return 'signencrypt'
  if (sign) return 'sign'
  if (encrypt) return 'encrypt'
  return 'none'
}

/** One entry of the follow-up reminder menu. */
export interface FollowUpPreset {
  key: FollowUpInterval
  label: string
  when: Date
}

/**
 * buildFollowUpPresets is the reminder menu's choices: tomorrow, in three days
 * and in a week, each at nine in the morning so the reminder lands at the
 * start of a working day rather than at the minute the mail went out.
 */
export function buildFollowUpPresets(now: Date, tr: (key: string) => string): FollowUpPreset[] {
  return [
    { key: 'tomorrow', label: tr('followUp.tomorrow'), when: atTime(addDays(now, 1), 9, 0) },
    { key: 'threeDays', label: tr('followUp.threeDays'), when: atTime(addDays(now, 3), 9, 0) },
    { key: 'week', label: tr('followUp.week'), when: atTime(addDays(now, 7), 9, 0) },
  ]
}

/** One entry of the send-later menu. */
export interface SendPreset {
  /** Which preset this is, for the icon the menu draws beside it. */
  key: 'laterToday' | 'thisEvening' | 'tomorrowMorning' | 'mondayMorning'
  label: string
  when: Date
}

/**
 * buildSendPresets is a short list of friendly relative send times, leaving
 * out ones already past and "next Monday" when it is tomorrow.
 */
export function buildSendPresets(now: Date, tr: (key: string) => string): SendPreset[] {
  const out: SendPreset[] = []
  out.push({ key: 'laterToday', label: tr('compose.sendLater.laterToday'), when: new Date(now.getTime() + 3 * 60 * 60 * 1000) })

  const evening = atTime(now, 18, 0)
  if (evening.getTime() > now.getTime() + 60 * 1000) {
    out.push({ key: 'thisEvening', label: tr('compose.sendLater.thisEvening'), when: evening })
  }

  const tomorrow = atTime(addDays(now, 1), 8, 0)
  out.push({ key: 'tomorrowMorning', label: tr('compose.sendLater.tomorrowMorning'), when: tomorrow })

  const monday = atTime(nextWeekday(now, 1), 8, 0)
  if (monday.toDateString() !== tomorrow.toDateString()) {
    out.push({ key: 'mondayMorning', label: tr('compose.sendLater.mondayMorning'), when: monday })
  }
  return out
}
