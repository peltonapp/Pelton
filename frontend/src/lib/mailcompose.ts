// mailcompose.ts turns the editable compose session into the request the backend
// expects: it parses the raw address strings, and renders the body for the three
// editor modes. markdown is rendered to html with marked for the sent html part;
// the markdown source is kept as the plain text part.

import { renderMarkdown } from './markdown'
import { htmlToText } from './richtext'
import { writingDirection, markDirection } from './textdirection'
import type { Address, ComposeRequest, EditorMode } from './types'
import type { ComposeSession } from '../stores/compose'

/**
 * splitAddressList cuts a recipient string at every comma or semicolon that is
 * outside double quotes and outside <...>, returning trimmed non-empty tokens.
 * A display name like "Doe, John" therefore stays one recipient, and so does
 * the unquoted `Doe, John <j@x>` that older stored rows hold.
 */
export function splitAddressList(raw: string): string[] {
  const tokens: string[] = []
  // separators[i] is the character that ended tokens[i], kept so a name
  // fragment can be rejoined exactly as it was written.
  const separators: string[] = []
  let current = ''
  let quoted = false
  let angled = false
  for (let i = 0; i < raw.length; i++) {
    const ch = raw[i]
    if (quoted && ch === '\\' && i + 1 < raw.length) {
      current += ch + raw[++i]
      continue
    }
    if (ch === '"' && !angled) quoted = !quoted
    else if (ch === '<' && !quoted) angled = true
    else if (ch === '>' && !quoted) angled = false
    else if ((ch === ',' || ch === ';') && !quoted && !angled) {
      tokens.push(current)
      separators.push(ch)
      current = ''
      continue
    }
    current += ch
  }
  tokens.push(current)
  separators.push('')
  return rejoinNameFragments(tokens, separators)
    .map((t) => t.trim())
    .filter((t) => t.length > 0)
}

// rejoinNameFragments glues runs of tokens that are neither an address nor a
// named address onto a following `Name <addr>` token. Messages synced before
// names were quoted on storage hold `Doe, John <j@x>`, which would otherwise
// read as a bogus "Doe" recipient. A run not followed by a named address is
// left as separate tokens, so a stray word before a bare address stays apart.
function rejoinNameFragments(tokens: string[], separators: string[]): string[] {
  const out: string[] = []
  let run: number[] = []
  tokens.forEach((token, i) => {
    const trimmed = token.trim()
    if (trimmed !== '' && !trimmed.includes('@') && !trimmed.includes('<')) {
      run.push(i)
      return
    }
    if (run.length > 0 && trimmed.includes('<')) {
      out.push(run.map((j) => tokens[j] + separators[j]).join('') + token)
    } else {
      out.push(...run.map((j) => tokens[j]), token)
    }
    run = []
  })
  out.push(...run.map((j) => tokens[j]))
  return out
}

/** parseAddressList parses `"Name, X" <a@b>; c@d` into address objects. */
export function parseAddressList(raw: string): Address[] {
  return splitAddressList(raw).map(parseAddress)
}

function parseAddress(token: string): Address {
  const angle = token.match(/^(.*)<(.+?)>\s*$/)
  if (angle) {
    const name = angle[1].trim().replace(/^"(.*)"$/, (_, inner: string) => inner.replace(/\\(.)/g, '$1'))
    return { name, email: angle[2].trim() }
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
    return { text: body, html: markDirection(renderMarkdown(body), direction) }
  }
  if (mode === 'wysiwyg') {
    return { text: htmlToText(body), html: markDirection(body, direction) }
  }
  return { text: body, html: '' }
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
