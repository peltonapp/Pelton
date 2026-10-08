// compose.ts manages open compose sessions. each session is a self-contained
// draft the user is editing in a compose pane. reply and forward prefills build
// the quoting and threading references from the original message; the backend
// turns the references into the actual threading headers.

import { writable, get } from 'svelte/store'
import type { EditorMode, MessageDetail, ComposeAttachment } from '../lib/types'
import { getSetting, setSetting } from '../lib/api'

// the compose pane remembers whether the user last worked fullscreen or in the
// small floating size, so new panes open at that size. persisted in the backend
// settings table (not localStorage).
const FULLSCREEN_KEY = 'compose_fullscreen'
let defaultFullscreen = false

// initComposePrefs loads the remembered size once at startup.
export async function initComposePrefs(): Promise<void> {
  try {
    const { value, found } = await getSetting(FULLSCREEN_KEY)
    if (found) {
      defaultFullscreen = value === 'true'
    }
  } catch {
    // ignore: default to the small floating size.
  }
}

// setComposeFullscreenDefault records the user's latest size choice so the next
// compose opens the same way.
export function setComposeFullscreenDefault(fullscreen: boolean): void {
  defaultFullscreen = fullscreen
  void setSetting(FULLSCREEN_KEY, String(fullscreen))
}

// ComposeSession is the editable state of one compose pane. address fields are
// kept as raw comma-separated strings for the inputs and parsed on send.
export interface ComposeSession {
  id: number
  accountId: number
  mode: EditorMode
  to: string
  cc: string
  bcc: string
  showCc: boolean
  showBcc: boolean
  subject: string
  // body holds plain text for plaintext mode, markdown source for markdown mode,
  // and html for the (stubbed) wysiwyg mode.
  body: string
  attachments: ComposeAttachment[]
  inReplyTo: string
  references: string[]
  // draftId is non-zero when this session is editing a saved local draft.
  draftId: number
  // window state for the floating pane: fullscreen expands it to fill the
  // window; minimized collapses it to its title bar (gmail-style).
  fullscreen: boolean
  minimized: boolean
  // signaturesApplied guards the one-time insertion of the account's default
  // header/footer when a fresh compose opens, so editing never re-inserts them.
  signaturesApplied: boolean
  // protection is the pgp treatment for this message: 'none', 'sign',
  // 'encrypt' or 'signencrypt'. It starts from the account default resolved
  // against the keys actually available, and the user can change it per
  // message.
  protection: string
}

export const composeSessions = writable<ComposeSession[]>([])

let nextId = 1

// blankSession builds an empty session for a given account and editor mode.
function blankSession(accountId: number, mode: EditorMode): ComposeSession {
  return {
    id: nextId++,
    accountId,
    mode,
    to: '',
    cc: '',
    bcc: '',
    showCc: false,
    showBcc: false,
    subject: '',
    body: '',
    attachments: [],
    inReplyTo: '',
    references: [],
    draftId: 0,
    fullscreen: defaultFullscreen,
    minimized: false,
    signaturesApplied: false,
    protection: 'none',
  }
}

// openCompose starts a new empty compose session and returns its id.
export function openCompose(accountId: number, mode: EditorMode): number {
  const session = blankSession(accountId, mode)
  composeSessions.update((list) => [...list, session])
  return session.id
}

// MailtoPrefill is the compose seed parsed from a mailto: link (see events.ts).
export interface MailtoPrefill {
  to: string
  cc: string
  bcc: string
  subject: string
  body: string
}

// openComposeWith starts a compose session seeded from a mailto: link. Empty
// fields fall back to the blank session, and cc/bcc reveal their rows only when
// they carry a value.
export function openComposeWith(accountId: number, mode: EditorMode, prefill: MailtoPrefill): number {
  const session = blankSession(accountId, mode)
  session.to = prefill.to
  session.subject = prefill.subject
  session.body = prefill.body
  if (prefill.cc) {
    session.cc = prefill.cc
    session.showCc = true
  }
  if (prefill.bcc) {
    session.bcc = prefill.bcc
    session.showBcc = true
  }
  composeSessions.update((list) => [...list, session])
  return session.id
}

// openReply prefills a reply. replyAll also carries the cc recipients. the quoted
// body and threading references come from the original message.
export function openReply(detail: MessageDetail, mode: EditorMode, replyAll: boolean): number {
  const session = blankSession(detail.accountId, mode)
  session.to = detail.fromAddress
  if (replyAll && detail.ccAddresses) {
    session.cc = detail.ccAddresses
    session.showCc = true
  }
  session.subject = withPrefix(detail.subject, 'Re:')
  session.body = quoteBody(detail, mode)
  session.inReplyTo = messageIdRef(detail)
  session.references = buildReferences(detail)
  composeSessions.update((list) => [...list, session])
  return session.id
}

// openForward prefills a forward with the original quoted and no recipients.
export function openForward(detail: MessageDetail, mode: EditorMode): number {
  const session = blankSession(detail.accountId, mode)
  session.subject = withPrefix(detail.subject, 'Fwd:')
  session.body = forwardBody(detail, mode)
  composeSessions.update((list) => [...list, session])
  return session.id
}

// reopenSession brings a sent-but-undone message back into a fresh compose pane,
// preserving every edited field. draftId is reset because the original local
// draft was already removed on send, so a save here starts a new one.
export function reopenSession(session: ComposeSession): number {
  // a reopened message already has whatever signature it was sent with, so do not
  // auto-insert defaults again.
  const restored: ComposeSession = { ...session, id: nextId++, draftId: 0, signaturesApplied: true }
  composeSessions.update((list) => [...list, restored])
  return restored.id
}

// updateCompose merges a partial change into a session.
export function updateCompose(id: number, patch: Partial<ComposeSession>): void {
  composeSessions.update((list) => list.map((s) => (s.id === id ? { ...s, ...patch } : s)))
}

// closeCompose removes a session. It drops whatever is in the pane, so callers
// outside the pane itself should go through requestComposeClose instead.
export function closeCompose(id: number): void {
  composeSessions.update((list) => list.filter((s) => s.id !== id))
}

// closeRequest names a session the app asked to close from outside the pane,
// which the pane watches so the close still runs its save-or-discard prompt.
export const closeRequest = writable<number | null>(null)

// requestComposeClose asks a compose pane to close itself the way its own close
// button does, prompting first when the draft has content.
export function requestComposeClose(id: number): void {
  closeRequest.set(id)
}

// getSession reads the current state of one session.
export function getSession(id: number): ComposeSession | undefined {
  return get(composeSessions).find((s) => s.id === id)
}

// withPrefix adds a reply/forward prefix unless it is already present.
function withPrefix(subject: string, prefix: string): string {
  const trimmed = subject.trim()
  if (trimmed.toLowerCase().startsWith(prefix.toLowerCase())) {
    return trimmed
  }
  return `${prefix} ${trimmed}`
}

// quoteBody builds a quoted reply body. plaintext and markdown both quote with
// "> "; html mode is the stubbed editor so it also gets the plain quote.
//
// bodyQuote, not bodyPlain: an html-only message has no text part, so quoting
// bodyPlain produced an empty reply (#239). The backend renders the html down
// to text for this field, and falls back to it only when there is no text part
// to prefer.
function quoteBody(detail: MessageDetail, _mode: EditorMode): string {
  const attribution = `On ${detail.date}, ${detail.fromName || detail.fromAddress} wrote:`
  const quoted = detail.bodyQuote
    .split('\n')
    .map((line) => `> ${line}`)
    .join('\n')
  return `\n\n${attribution}\n${quoted}\n`
}

// forwardBody builds a forwarded message body with a header block.
function forwardBody(detail: MessageDetail, _mode: EditorMode): string {
  const header = [
    '---------- Forwarded message ----------',
    `From: ${detail.fromName || ''} <${detail.fromAddress}>`,
    `Date: ${detail.date}`,
    `Subject: ${detail.subject}`,
    `To: ${detail.toAddresses}`,
  ].join('\n')
  return `\n\n${header}\n\n${detail.bodyQuote}\n`
}

// messageIdRef would be the original Message-ID for threading. the detail dto
// does not expose it directly today, so we leave it empty and rely on the
// backend, which has the stored Message-ID, to fill threading on send.
// TODO(backend): expose the original Message-ID in MessageDetailDTO so replies
// thread precisely from the frontend reference too.
function messageIdRef(_detail: MessageDetail): string {
  return ''
}

// buildReferences returns the reference chain for threading. empty for the same
// reason as messageIdRef above.
function buildReferences(_detail: MessageDetail): string[] {
  return []
}
