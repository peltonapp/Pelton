// mailcompose.ts turns the editable compose session into the request the backend
// expects: it parses the raw address strings, and renders the body for the three
// editor modes. markdown is rendered to html with marked for the sent html part;
// the markdown source is kept as the plain text part.

import { marked } from 'marked'
import { writingDirection, markDirection } from './textdirection'
import type { Address, ComposeRequest, EditorMode } from './types'
import type { ComposeSession } from '../stores/compose'

// parseAddressList parses "Name <a@b>, c@d" into address objects. it splits on
// commas, then pulls an <email> if present, treating the rest as the name.
export function parseAddressList(raw: string): Address[] {
  return raw
    .split(',')
    .map((part) => part.trim())
    .filter((part) => part.length > 0)
    .map(parseAddress)
}

function parseAddress(token: string): Address {
  const angle = token.match(/^(.*)<(.+?)>\s*$/)
  if (angle) {
    return { name: angle[1].trim().replace(/^"|"$/g, ''), email: angle[2].trim() }
  }
  return { name: '', email: token }
}

// renderedBody is the text and html parts produced from one editor mode.
export interface RenderedBody {
  text: string
  html: string
}

// renderBody produces the parts to send for a given editor mode:
//  - plaintext: text only, no html part.
//  - markdown: markdown source as text, rendered html as the html part.
//  - wysiwyg: the contenteditable html as the html part, with a plain-text
//    fallback derived from it. wysiwyg is the stubbed editor (basic
//    contenteditable); a richer editor can replace it without changing this
//    contract.
// The html part carries the direction the message was written in (#356), so it
// reads the same way in the recipient's client as it did in the composer. A
// plaintext-only message cannot say anything about direction, since plain text
// has no markup to say it in; that one is left to the reader's own client.
export function renderBody(mode: EditorMode, body: string): RenderedBody {
  const direction = writingDirection(body)
  if (mode === 'markdown') {
    return { text: body, html: markDirection(marked.parse(body) as string, direction) }
  }
  if (mode === 'wysiwyg') {
    return { text: htmlToText(body), html: markDirection(body, direction) }
  }
  return { text: body, html: '' }
}

// htmlToText extracts readable text from html for the plain-text alternative.
function htmlToText(html: string): string {
  const el = document.createElement('div')
  el.innerHTML = html
  return el.textContent ?? ''
}

// buildRequest assembles the full compose request from a session. the result is
// a plain object matching the generated ComposeRequest shape, which the bindings
// serialize as-is.
export function buildRequest(session: ComposeSession): ComposeRequest {
  const { text, html } = renderBody(session.mode, session.body)
  return {
    accountId: session.accountId,
    to: parseAddressList(session.to),
    cc: parseAddressList(session.cc),
    bcc: parseAddressList(session.bcc),
    subject: session.subject,
    text,
    html,
    inReplyTo: session.inReplyTo,
    references: session.references,
    attachments: session.attachments,
    sendAt: '',
    protection: session.protection,
    draftMessageId: session.draftMessageId,
  } as ComposeRequest
}

// hasRecipients reports whether a session has at least one address, used to gate
// sending so we do not enqueue a message with no recipients.
export function hasRecipients(session: ComposeSession): boolean {
  return (
    parseAddressList(session.to).length > 0 ||
    parseAddressList(session.cc).length > 0 ||
    parseAddressList(session.bcc).length > 0
  )
}
