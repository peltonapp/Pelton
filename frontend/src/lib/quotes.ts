// quotes.ts finds the quoted original at the end of a reply, so the reader can
// fold it away behind a toggle. Only a quote that ends the message is folded:
// a reply written between quoted lines would lose its answers otherwise.

/** The part of a message before its quoted original, and whether one was found. */
export interface QuoteSplit {
  head: string
  hasQuote: boolean
}

// where a mail client starts the quoted original, most specific first.
const quoteSelectors = [
  '.gmail_quote',
  '.moz-cite-prefix',
  '#divRplyFwdMsg',
  '#appendonsend',
  '.yahoo_quoted',
  'blockquote[type="cite"]',
  'blockquote',
]

// an attribution line ("On Monday, Ada wrote:") in the languages Pelton ships.
const attribution =
  /(wrote|schrieb|a écrit|escribió|ha scritto|schreef|napisał|escreveu|написал|yazdı|كتب|写道)\s*:?\s*$/i

// text that may still follow a quote without the message counting as a reply
// written between quoted lines: a signature or a short sign-off.
const trailingAllowance = 200

function textLength(node: Node | null): number {
  return (node?.textContent ?? '').replace(/\s+/g, '').length
}

// removeFrom drops node and everything after it in document order.
function removeFrom(node: Node, body: HTMLElement): void {
  let cur: Node = node
  while (cur !== body && cur.parentNode) {
    const parent: Node = cur.parentNode
    while (cur.nextSibling) {
      parent.removeChild(cur.nextSibling)
    }
    if (cur === node) {
      parent.removeChild(cur)
    }
    cur = parent
  }
}

// precedes reports whether a comes before b in the document.
function precedes(a: Node, b: Node): boolean {
  return (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
}

/**
 * splitHtmlQuote returns html with the quoted original at its end taken off.
 * The html is the sanitized body; nothing in it runs while it is parsed here.
 */
export function splitHtmlQuote(html: string): QuoteSplit {
  const none = { head: html, hasQuote: false }
  if (!html) {
    return none
  }
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const body = doc.body
  let start: Element | null = null
  for (const selector of quoteSelectors) {
    const found = body.querySelector(selector)
    if (found && (!start || precedes(found, start))) {
      start = found
    }
  }
  if (!start) {
    return none
  }
  // a bare blockquote holds the whole original, so text after it is the
  // sender's own; the markers of other clients start an original that runs on
  // in the elements after them.
  const quote = start.tagName === 'BLOCKQUOTE' ? textLength(start) : -1
  // the attribution line sits just before the quote in most clients.
  const before = start.previousElementSibling
  let lead = 0
  if (before && textLength(before) < 300 && attribution.test((before.textContent ?? '').trim())) {
    lead = textLength(before)
    start = before
  }
  const total = textLength(body)
  removeFrom(start, body)
  const head = textLength(body)
  if (head === 0) {
    return none
  }
  if (quote >= 0 && total - head - lead - quote > trailingAllowance) {
    return none
  }
  return { head: body.innerHTML, hasQuote: true }
}

/** splitPlainQuote returns plain text with the quoted original at its end taken off. */
export function splitPlainQuote(text: string): QuoteSplit {
  const none = { head: text, hasQuote: false }
  const lines = text.split('\n')
  let start = -1
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim()
    if (line === '' || line.startsWith('>')) {
      if (line.startsWith('>')) {
        start = i
      }
      continue
    }
    break
  }
  const marker = lines.findIndex((l) => /^-{2,}\s*(original message|ursprüngliche nachricht)\s*-{2,}$/i.test(l.trim()))
  if (marker >= 0 && (start < 0 || marker < start)) {
    start = marker
  }
  if (start < 0) {
    return none
  }
  let prev = start - 1
  while (prev >= 0 && lines[prev].trim() === '') {
    prev--
  }
  if (prev >= 0 && attribution.test(lines[prev].trim())) {
    start = prev
  }
  const head = lines.slice(0, start).join('\n').replace(/\s+$/, '')
  if (head.trim() === '') {
    return none
  }
  return { head, hasQuote: true }
}
