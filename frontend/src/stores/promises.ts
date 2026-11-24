// promises.ts holds the promises found in mail: the suggestions the reader
// shows on a mail, and the accepted ones the Tasks space lists as "I owe" and
// "Owed to me". The backend finds and settles them (bind_promises.go); this
// store mirrors the list and reloads whenever the backend says it changed.
// It runs in the new design only.

import { derived, get, writable } from 'svelte/store'
import {
  acceptPromise,
  dismissPromise,
  findPromises,
  getMessage,
  getPromiseSettings,
  listPromises,
  quietPromiseSender,
  reopenPromise,
  restorePromise,
  setPromiseSettings,
  unquietPromiseSender,
} from '../lib/api'
import { onPromisesChanged } from '../lib/events'
import { t } from '../lib/i18n'
import { errorMessage, pushActions, toastError, toastInfo } from './toast'
import type { EditorMode, MailPromise, PromiseDirection, PromiseEdit, PromiseSettings } from '../lib/types'

/** The promises that are suggested, accepted or closed. */
export const promises = writable<MailPromise[]>([])

/** The settings the app works with until the stored ones have loaded. */
export const defaultPromiseSettings: PromiseSettings = {
  enabled: true,
  autoAcceptMine: false,
  nudge: true,
  taskList: 'local',
  accounts: [],
  aiMaxChars: 4000,
  aiPerDay: 50,
}

/** The promise preferences. */
export const promiseSettings = writable<PromiseSettings>(defaultPromiseSettings)

/** The sentence of a suggestion being pointed at, which the reader marks in the body. */
export const promiseHighlight = writable<{ messageId: number; quote: string } | null>(null)

/** Who owes each accepted promise's task, by task UID. */
export const owedByTask = derived(promises, (list) => {
  const out = new Map<string, PromiseDirection>()
  for (const p of list) {
    if (p.taskUid && p.state !== 'suggested') {
      out.set(p.taskUid, p.direction)
    }
  }
  return out
})

/** The accepted or closed promise behind each task, by task UID. */
export const promiseByTask = derived(promises, (list) => new Map(list.filter((p) => p.taskUid).map((p) => [p.taskUid, p])))

/** A Message-ID without its angle brackets, the form promises are keyed by. */
export function bareMailId(id: string): string {
  return id.trim().replace(/^<|>$/g, '')
}

/** The suggestions waiting on one mail. */
export function suggestionsFor(list: MailPromise[], accountId: number, messageId: string): MailPromise[] {
  const id = bareMailId(messageId)
  return id ? list.filter((p) => p.state === 'suggested' && p.accountId === accountId && p.mailId === id) : []
}

/** The parts of a sentence that can be found in a body: its lines, since a body wraps them. */
export function highlightTerms(quote: string): string[] {
  return quote
    .split(/\s*\n\s*/)
    .map((s) => s.trim())
    .filter((s) => s.length >= 4)
}

let generation = 0

/** Reads the promises again. A failure keeps what is shown. */
export async function loadPromises(): Promise<void> {
  const mine = ++generation
  try {
    const list = await listPromises()
    if (mine === generation) {
      promises.set(list)
    }
  } catch {
    // the next change event retries.
  }
}

/** Reads the promise preferences. */
export async function loadPromiseSettings(): Promise<void> {
  try {
    promiseSettings.set({ ...defaultPromiseSettings, ...(await getPromiseSettings()) })
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Changes some promise preferences and saves them. A refused save puts the old ones back. */
export async function savePromiseSettings(patch: Partial<PromiseSettings>): Promise<void> {
  const before = get(promiseSettings)
  const next = { ...before, ...patch }
  promiseSettings.set(next)
  try {
    await setPromiseSettings(next)
  } catch (err) {
    promiseSettings.set(before)
    toastError(errorMessage(err))
  }
}

/** Loads the promises and keeps them current; returns the function that stops. */
export function startPromises(): () => void {
  void loadPromises()
  void loadPromiseSettings()
  return onPromisesChanged(() => {
    void loadPromises()
    void loadPromiseSettings()
  })
}

/** The editable fields of a suggestion, to accept it unchanged or edit them first. */
export function editOf(p: MailPromise): PromiseEdit {
  return { id: p.id, title: p.title, due: p.due, direction: p.direction, personName: p.personName, personEmail: p.personEmail }
}

/** Makes a task of a suggestion. Returns whether it worked. */
export async function acceptSuggestion(edit: PromiseEdit): Promise<boolean> {
  try {
    const saved = await acceptPromise(edit)
    promises.update((list) => list.map((p) => (p.id === saved.id ? saved : p)))
    toastInfo(get(t)(saved.direction === 'mine' ? 'promises.accepted.mine' : 'promises.accepted.theirs'))
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** Sets a suggestion aside, with an undo. */
export async function dismissSuggestion(p: MailPromise): Promise<void> {
  const tr = get(t)
  const before = get(promises)
  promises.set(before.filter((x) => x.id !== p.id))
  try {
    await dismissPromise(p.id)
    pushActions('info', tr('promises.dismissed'), [{ label: tr('promises.undo'), run: () => void restorePromise(p.id).then(loadPromises) }], 6000)
  } catch (err) {
    promises.set(before)
    toastError(errorMessage(err))
  }
}

/** Stops reading a sender's mail for promises, with an undo. */
export async function quietSender(p: MailPromise): Promise<void> {
  const tr = get(t)
  try {
    const ids = await quietPromiseSender(p.personEmail)
    await loadPromises()
    pushActions(
      'info',
      tr('promises.quieted').replace('{name}', p.personName || p.personEmail),
      [{ label: tr('promises.undo'), run: () => void unquietPromiseSender(p.personEmail, ids).then(loadPromises) }],
      8000,
    )
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Opens a promise again that a reply closed, or turns down the offer to complete it. */
export async function reopen(p: MailPromise): Promise<void> {
  try {
    await reopenPromise(p.id)
    await loadPromises()
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Reads a mail for promises now and says what it found. */
export async function findPromisesIn(messageId: number): Promise<void> {
  const tr = get(t)
  try {
    const found = await findPromises(messageId)
    await loadPromises()
    const waiting = found.filter((p) => p.state === 'suggested').length
    toastInfo(waiting === 0 ? tr('promises.find.none') : tr('promises.find.some').replace('{n}', String(waiting)))
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Opens a reply draft to the mail a promise came from, to remind its person. Nothing is sent. */
export async function nudge(p: MailPromise, mode: EditorMode): Promise<void> {
  if (!p.messageId) {
    return
  }
  try {
    const [detail, compose] = await Promise.all([getMessage(p.messageId), import('./compose')])
    compose.openReply(detail, mode, false)
  } catch (err) {
    toastError(errorMessage(err))
  }
}
