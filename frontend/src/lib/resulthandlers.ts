// resulthandlers.ts carries out the result panel's buttons that only the
// window can do, by the client name a result action registers with in the
// backend: copying, and putting the result into a reply. Buttons without a
// client run in the backend.

import { get } from 'svelte/store'
import { getMessage } from './api'
import type { ScriptResult } from './scripting'
import type { EditorMode } from './types'
import { sidebar } from '../stores/accounts'
import { composeSessions, insertRequest, openComposeWith, openReplyWith } from '../stores/compose'
import { activeComposer } from '../stores/composecommands'
import { prefs } from '../stores/prefs'
import { visibleMessageId } from '../stores/tabs'

/** Carries out a button on a result and returns a locale key to confirm it with, or ''. */
export type ResultHandler = (result: ScriptResult) => Promise<string>

const handlers = new Map<string, ResultHandler>()

/** Adds the handler a result action names as its client. */
export function registerResultHandler(client: string, handler: ResultHandler): void {
  handlers.set(client, handler)
}

/** The handler of a client name, if the window has one. */
export function resultHandler(client: string): ResultHandler | undefined {
  return handlers.get(client)
}

/**
 * Puts text into the composer the user was last in, when it is still open;
 * otherwise opens a reply to the message on screen with the text above the
 * quote, or a new message when none is open.
 */
export async function insertIntoReply(text: string): Promise<void> {
  const mode = get(prefs).defaultEditorMode as EditorMode
  const active = get(activeComposer)
  if (active !== null && get(composeSessions).some((s) => s.id === active)) {
    insertRequest.set({ session: active, text })
    return
  }
  const open = get(visibleMessageId)
  if (open) {
    openReplyWith(await getMessage(open), mode, 'reply', text)
    return
  }
  const account = get(sidebar).data?.accounts[0]?.id ?? 0
  openComposeWith(account, mode, { to: '', cc: '', bcc: '', subject: '', body: text })
}

registerResultHandler('copy', async (r) => {
  await navigator.clipboard.writeText(r.text)
  return 'scripts.result.copied'
})

registerResultHandler('reply', async (r) => {
  await insertIntoReply(r.text)
  return ''
})
