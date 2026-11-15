// compose.ts manages open compose sessions. each session is a self-contained
// draft the user is editing in a compose pane. reply and forward prefills build
// the quoting and threading references from the original message; the backend
// turns the references into the actual threading headers.

import { writable, get } from 'svelte/store'
import type { Address, ComposeRequest, Draft, EditorMode, MessageDetail, ComposeAttachment } from '../lib/types'
import { getSetting, setSetting } from '../lib/api'
import { formatAddress, formatAddressList, parseAddressList, renderBody } from '../lib/mailcompose'
import { formatFullDate } from '../lib/format'
import { escapeHtml, textToHtml } from '../lib/richtext'

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
  // draftMessageId is the Message-ID of the copy uploaded to the server Drafts
  // folder, '' until the first upload. A later save replaces that copy.
  draftMessageId: string
  // saved marks a session that opens as a saved draft (opened from Drafts, or
  // brought back after its upload failed): closing it unchanged asks nothing,
  // and the account's default protection does not override what it was saved with.
  saved: boolean
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
  // quote is the quoted original a reply opened with, so a footer can go above
  // it (#512). Unset for anything that is not a reply.
  quote?: string
  // origin is the message a reply or forward was opened from, so the reply
  // type can be switched in place and the original found again.
  origin?: ComposeOrigin
  // fold is the quoted or forwarded original at the end of the body, which
  // the new composer shows folded until it is opened.
  fold?: string
  // placement is where the new design shows this session in the main window,
  // unset until it has been placed.
  placement?: ComposePlacement
  // followUpAt is when to be reminded if nobody has replied, as an ISO time,
  // unset for no reminder.
  followUpAt?: string
}

/** The three reply types a reply can be switched between. */
export type ReplyKind = 'reply' | 'replyAll' | 'forward'

/** Where the new design shows a compose session in the main window. */
export type ComposePlacement = 'docked' | 'tab' | 'leaving'

/** What a reply or forward keeps from the message it was opened from. */
export interface ComposeOrigin {
  /** The reply type the session currently is. */
  kind: ReplyKind
  /** The original's local message id, folder and account. */
  id: number
  accountId: number
  folderId: number
  /** The address of the account the original arrived in. */
  accountEmail: string
  fromName: string
  fromAddress: string
  toAddresses: string
  ccAddresses: string
  subject: string
  date: string
  /** The original's text as quoted in a reply. */
  bodyQuote: string
  /** The original's Message-ID and the references a reply threads with. */
  messageId: string
  references: string[]
}

export const composeSessions = writable<ComposeSession[]>([])

// composeInWindow is set in a compose window (#573), where the one compose
// fills the window and the pane's own size controls have nothing to do.
export const composeInWindow = writable(false)

let nextId = 1

// adoptSession takes over a pane moved here from another window, with every
// field as it was, and returns its new id.
export function adoptSession(session: ComposeSession): number {
  const adopted: ComposeSession = { ...session, references: [...session.references], id: nextId++, minimized: false, fullscreen: false, placement: undefined }
  composeSessions.update((list) => [...list, adopted])
  return adopted.id
}

// forcedMode overrides the editor mode callers ask for, set while the new
// design is on: its composer has one editor and no mode switch.
let forcedMode: EditorMode | null = null

/**
 * setForcedComposeMode makes every new session start in one editor mode, or
 * restores the mode each caller asks for when given null.
 */
export function setForcedComposeMode(mode: EditorMode | null): void {
  forcedMode = mode
}

// blankSession builds an empty session for a given account and editor mode.
function blankSession(accountId: number, mode: EditorMode): ComposeSession {
  return {
    id: nextId++,
    accountId,
    mode: forcedMode ?? mode,
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
    draftMessageId: '',
    saved: false,
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

/** The recipients a reply or reply all is addressed to. */
export interface ReplyRecipients {
  to: string
  cc: string
}

/**
 * replyRecipients addresses a reply to the sender. Reply all adds the original
 * To and Cc recipients, leaving out the account's own address and anyone
 * already listed.
 */
export function replyRecipients(source: Pick<MessageDetail, 'fromName' | 'fromAddress' | 'toAddresses' | 'ccAddresses' | 'accountEmail'>, replyAll: boolean): ReplyRecipients {
  const sender: Address = { name: source.fromName, email: source.fromAddress }
  if (!replyAll) {
    return { to: formatAddress(sender), cc: '' }
  }
  const seen = new Set<string>()
  const own = (source.accountEmail ?? '').trim().toLowerCase()
  const keep = (a: Address): boolean => {
    const key = a.email.trim().toLowerCase()
    if (key === '' || seen.has(key) || (key === own && own !== '')) {
      return false
    }
    seen.add(key)
    return true
  }
  const to = [sender, ...parseAddressList(source.toAddresses ?? '')].filter(keep)
  // replying to a message you sent yourself goes back to its recipients.
  if (to.length === 0 && sender.email) {
    to.push(sender)
  }
  const cc = parseAddressList(source.ccAddresses ?? '').filter(keep)
  return { to: formatAddressList(to), cc: formatAddressList(cc) }
}

// openReply prefills a reply. replyAll also carries the other To and the Cc
// recipients. the quoted body and threading references come from the original
// message.
export function openReply(detail: MessageDetail, mode: EditorMode, replyAll: boolean): number {
  return openReplyWith(detail, mode, replyAll ? 'replyAll' : 'reply', '')
}

// openForward prefills a forward with the original quoted and no recipients.
export function openForward(detail: MessageDetail, mode: EditorMode): number {
  return openReplyWith(detail, mode, 'forward', '')
}

// typedLead turns text typed outside the composer into what goes above the
// original in a body of the given mode.
function typedLead(text: string, mode: EditorMode): string {
  if (text.trim() === '') {
    return ''
  }
  return mode === 'wysiwyg' ? `${textToHtml(text)}<p></p>` : `${text}\n\n`
}

/**
 * replySession builds a reply, reply all or forward of a message without
 * opening a composer for it, with typed (plain text) already written above
 * the original.
 */
export function replySession(detail: MessageDetail, mode: EditorMode, kind: ReplyKind, typed: string): ComposeSession {
  const session = blankSession(detail.accountId, mode)
  Object.assign(session, replyFields(originOf(detail, kind), session.mode, typedLead(typed, session.mode)))
  return session
}

/**
 * openReplyWith opens a reply, reply all or forward in the composer, with
 * typed (plain text) already written above the original, and returns its id.
 */
export function openReplyWith(detail: MessageDetail, mode: EditorMode, kind: ReplyKind, typed: string): number {
  const session = replySession(detail, mode, kind, typed)
  composeSessions.update((list) => [...list, session])
  return session.id
}

// originOf keeps what a reply or forward needs from the original message.
function originOf(detail: MessageDetail, kind: ReplyKind): ComposeOrigin {
  return {
    kind,
    id: detail.id,
    accountId: detail.accountId,
    folderId: detail.folderId,
    accountEmail: detail.accountEmail ?? '',
    fromName: detail.fromName,
    fromAddress: detail.fromAddress,
    toAddresses: detail.toAddresses ?? '',
    ccAddresses: detail.ccAddresses ?? '',
    subject: detail.subject,
    date: detail.date,
    bodyQuote: detail.bodyQuote,
    messageId: messageIdRef(detail),
    references: buildReferences(detail),
  }
}

// blankLead is the empty line a reply opens with above the quote.
function blankLead(mode: EditorMode): string {
  return mode === 'wysiwyg' ? '<p></p>' : '\n\n'
}

// replyFields builds the fields a reply type sets on a session: recipients,
// subject, threading and a body of typed above the quoted original. A forward
// has no recipients and does not thread.
function replyFields(origin: ComposeOrigin, mode: EditorMode, typed: string): Partial<ComposeSession> {
  const lead = typed === '' ? blankLead(mode) : typed
  if (origin.kind === 'forward') {
    const body = forwardBody(origin, mode)
    return {
      origin,
      to: '',
      cc: '',
      showCc: false,
      subject: withPrefix(origin.subject, 'Fwd:'),
      body: typed === '' ? body : lead + body.replace(/^(\n|<p><\/p>)+/, ''),
      fold: body.replace(/^(\n|<p><\/p>)+/, ''),
      quote: undefined,
      inReplyTo: '',
      references: [],
    }
  }
  const recipients = replyRecipients(origin, origin.kind === 'replyAll')
  const full = quoteBody(origin, mode)
  const quote = full.replace(/^(\n|<p><\/p>)+/, '')
  return {
    origin,
    to: recipients.to,
    cc: recipients.cc,
    showCc: recipients.cc !== '',
    subject: withPrefix(origin.subject, 'Re:'),
    body: typed === '' ? full : lead + quote,
    quote,
    fold: quote,
    inReplyTo: origin.messageId,
    references: origin.references,
  }
}

/**
 * typedPart is the body without the folded original at its end: what the user
 * wrote above it. A body whose original was edited away is returned whole.
 */
export function typedPart(session: Pick<ComposeSession, 'body' | 'fold'>): string {
  if (!session.fold) {
    return session.body
  }
  const at = session.body.lastIndexOf(session.fold)
  return at < 0 ? session.body : session.body.slice(0, at)
}

/**
 * switchReplyKind turns a reply into a reply all or a forward (and back) in
 * place. What was written above the original stays; recipients, subject,
 * threading and the quoted original follow the new type. A subject the user
 * changed is kept.
 */
export function switchReplyKind(id: number, kind: ReplyKind): void {
  const session = getSession(id)
  if (!session?.origin || session.origin.kind === kind) {
    return
  }
  const before = replyFields(session.origin, session.mode, '')
  const typed = typedPart(session)
  const after = replyFields({ ...session.origin, kind }, session.mode, typed.trim() === '' ? '' : typed)
  if (session.subject !== before.subject) {
    delete after.subject
  }
  updateCompose(id, after)
}

// openDraftSession opens a saved local draft for editing. The editor mode is not
// stored with a draft, so it is read back from the parts: no html is plaintext,
// html that the text renders to is markdown, anything else is rich text. A
// draft already open is brought back rather than opened in a second pane.
export function openDraftSession(draft: Draft): number {
  const open = bringBack(draft.id)
  if (open) {
    return open
  }
  const session = requestSession(draft.accountId, draft.request)
  session.draftId = draft.id
  session.saved = true
  composeSessions.update((list) => [...list, session])
  return session.id
}

/**
 * openRequestSession opens a compose request taken back out of the outbox for
 * editing, as a new unsaved message.
 */
export function openRequestSession(req: ComposeRequest): number {
  const session = requestSession(req.accountId, req)
  session.draftMessageId = ''
  composeSessions.update((list) => [...list, session])
  return session.id
}

// requestSession builds a session from a stored compose request. The editor
// mode is read back from the parts: no html is plaintext, html that the text
// renders to is markdown, anything else is rich text.
function requestSession(accountId: number, req: ComposeRequest): ComposeSession {
  let mode: EditorMode = 'wysiwyg'
  if (!req.html) {
    mode = 'plaintext'
  } else if (renderBody('markdown', req.text).html === req.html) {
    mode = 'markdown'
  }
  const session = blankSession(accountId, mode)
  Object.assign(session, {
    to: formatAddressList(req.to),
    cc: formatAddressList(req.cc),
    bcc: formatAddressList(req.bcc),
    showCc: (req.cc ?? []).length > 0,
    showBcc: (req.bcc ?? []).length > 0,
    subject: req.subject,
    body: session.mode === 'wysiwyg' ? req.html || textToHtml(req.text) : req.text,
    attachments: req.attachments ?? [],
    inReplyTo: req.inReplyTo ?? '',
    references: [...(req.references ?? [])],
    protection: req.protection,
    draftMessageId: req.draftMessageId ?? '',
    signaturesApplied: true,
    followUpAt: req.followUpAt || undefined,
  })
  return session
}

// restoreSession brings back the pane of a saved draft that closed before its
// upload failed, still saved and linked to its local draft and server copy. A
// pane already editing that draft is brought back instead of opening a second.
export function restoreSession(session: ComposeSession): number {
  const open = bringBack(session.draftId)
  if (open) {
    return open
  }
  const restored: ComposeSession = { ...session, references: [...session.references], id: nextId++, saved: true, minimized: false, placement: undefined }
  composeSessions.update((list) => [...list, restored])
  return restored.id
}

// bringBack un-minimises the pane editing a draft and returns its id, or 0 when
// none is open.
function bringBack(draftId: number): number {
  const open = get(composeSessions).find((s) => s.draftId === draftId)
  if (!open) {
    return 0
  }
  updateCompose(open.id, { minimized: false })
  return open.id
}

// reopenSession brings a sent-but-undone message back into a fresh compose pane,
// preserving every edited field. draftId and draftMessageId are reset because
// the send already removed the local draft and its server copy, so a save here
// starts a new one.
export function reopenSession(session: ComposeSession): number {
  // a reopened message already has whatever signature it was sent with, so do not
  // auto-insert defaults again.
  const restored: ComposeSession = { ...session, references: [...session.references], id: nextId++, draftId: 0, draftMessageId: '', saved: false, signaturesApplied: true, placement: undefined }
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

// quoteBody builds a quoted reply body. plaintext and markdown quote with "> ";
// the rich editor reads html, so it gets the quote as a blockquote, nested
// where the original quoted further.
//
// bodyQuote, not bodyPlain: an html-only message has no text part, so quoting
// bodyPlain produced an empty reply (#239). The backend renders the html down
// to text for this field, and falls back to it only when there is no text part
// to prefer.
function quoteBody(detail: ComposeOrigin, mode: EditorMode): string {
  const date = formatFullDate(detail.date) || detail.date
  const attribution = `On ${date}, ${detail.fromName || detail.fromAddress} wrote:`
  if (mode === 'wysiwyg') {
    return `<p></p><p>${escapeHtml(attribution)}</p><blockquote>${textToHtml(detail.bodyQuote)}</blockquote>`
  }
  const quoted = detail.bodyQuote
    .split('\n')
    .map((line) => `> ${line}`)
    .join('\n')
  return `\n\n${attribution}\n${quoted}\n`
}

// forwardBody builds a forwarded message body with a header block, as html in
// the rich editor.
function forwardBody(detail: ComposeOrigin, mode: EditorMode): string {
  const header = [
    '---------- Forwarded message ----------',
    `From: ${detail.fromName || ''} <${detail.fromAddress}>`,
    `Date: ${formatFullDate(detail.date) || detail.date}`,
    `Subject: ${detail.subject}`,
    `To: ${detail.toAddresses}`,
  ].join('\n')
  if (mode === 'wysiwyg') {
    return `<p></p>${textToHtml(header)}${textToHtml(detail.bodyQuote)}`
  }
  return `\n\n${header}\n\n${detail.bodyQuote}\n`
}

function messageIdRef(detail: MessageDetail): string {
  return detail.messageId ?? ''
}

function buildReferences(detail: MessageDetail): string[] {
  const parent = messageIdRef(detail)
  if (!parent) return []
  const references = detail.references ?? []
  const inReplyTo = detail.inReplyTo ?? []
  const ancestors = references.length ? references : inReplyTo.length === 1 ? inReplyTo : []
  return [...ancestors, parent]
}
