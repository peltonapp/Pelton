// datemarks.ts marks the dates a mail names inside its rendered document. The
// text is read off the text nodes of the already sanitized body, and each date
// found in it is wrapped in a span built with createElement: nothing from the
// mail is parsed again and not a character of it changes, so taking the
// marks out gives back the same text. Text inside links is left alone, since a
// click there already opens the link.

/** The class a date mark carries. */
export const dateClass = 'pelton-date'

/** The attribute holding a mark's number, shared by the pieces of one date. */
export const dateAttr = 'data-pelton-date'

const skipTags = new Set(['SCRIPT', 'STYLE', 'TEXTAREA', 'A', 'BUTTON', 'SELECT', 'OPTION', 'CODE', 'PRE', 'SVG', 'NOSCRIPT', 'TITLE'])

const blockTags = new Set([
  'ADDRESS', 'ARTICLE', 'ASIDE', 'BLOCKQUOTE', 'BR', 'DD', 'DIV', 'DL', 'DT', 'FIELDSET', 'FIGCAPTION', 'FIGURE', 'FOOTER', 'FORM',
  'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'HEADER', 'HR', 'LI', 'MAIN', 'NAV', 'OL', 'P', 'SECTION', 'TABLE', 'TBODY', 'TD', 'TFOOT',
  'TH', 'THEAD', 'TR', 'UL',
])

/** One text node and where its text starts in the joined text. */
export interface TextRun {
  node: Text
  from: number
}

/** The text of a body with block breaks as newlines, and the nodes it came from. */
export interface BodyText {
  text: string
  runs: TextRun[]
}

/** A range of the joined text to mark, with the number its spans get. */
export interface MarkRange {
  id: number
  index: number
  length: number
}

function skipped(node: Node, root: Node): boolean {
  for (let el = node.parentElement; el && el !== root; el = el.parentElement) {
    if (skipTags.has(el.tagName.toUpperCase())) {
      return true
    }
  }
  return false
}

/**
 * Joins the text of a body: every text node outside links, scripts and code,
 * with a newline wherever a block starts or ends, so a date never runs from
 * one paragraph into the next. Text the marks hold is read like any other.
 */
export function collectText(root: HTMLElement): BodyText {
  const doc = root.ownerDocument
  const walker = doc.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT)
  const runs: TextRun[] = []
  let text = ''
  const breakLine = (): void => {
    if (text !== '' && !text.endsWith('\n')) {
      text += '\n'
    }
  }
  let node: Node | null = walker.nextNode()
  while (node) {
    if (node.nodeType === Node.ELEMENT_NODE) {
      const tag = (node as Element).tagName.toUpperCase()
      if (blockTags.has(tag) || skipTags.has(tag)) {
        breakLine()
      }
    } else if (!skipped(node, root)) {
      const data = (node as Text).data
      if (data !== '') {
        runs.push({ node: node as Text, from: text.length })
        text += data
      }
    } else {
      breakLine()
    }
    node = walker.nextNode()
  }
  return { text, runs }
}

/**
 * Wraps each range of the joined text in date marks. A range that crosses
 * from one text node into another gets one span per node, all with the same
 * number; only the first takes keyboard focus. Ranges are applied from the
 * last to the first, so splitting a node never moves one still to come.
 * Returns how many ranges were marked.
 */
export function markRanges(body: BodyText, ranges: MarkRange[], label: (id: number) => string): number {
  const sorted = [...ranges].sort((a, b) => b.index - a.index)
  let count = 0
  for (const range of sorted) {
    const end = range.index + range.length
    const pieces = body.runs.filter((r) => r.from < end && r.from + r.node.data.length > range.index)
    if (pieces.length === 0) {
      continue
    }
    for (let i = pieces.length - 1; i >= 0; i--) {
      const run = pieces[i]
      const from = Math.max(0, range.index - run.from)
      const to = Math.min(run.node.data.length, end - run.from)
      if (to <= from || !run.node.parentNode) {
        continue
      }
      const doc = run.node.ownerDocument
      let target = run.node
      if (from > 0) {
        target = target.splitText(from)
      }
      if (to - from < target.data.length) {
        target.splitText(to - from)
      }
      const span = doc.createElement('span')
      span.className = dateClass
      span.setAttribute(dateAttr, String(range.id))
      if (i === 0) {
        span.tabIndex = 0
        span.setAttribute('role', 'button')
        span.setAttribute('aria-haspopup', 'dialog')
        span.setAttribute('aria-label', label(range.id))
      }
      target.parentNode?.replaceChild(span, target)
      span.appendChild(target)
    }
    count += 1
  }
  return count
}

/** Takes every date mark out of a document again, keeping what they held. */
export function unmarkDates(doc: Document): void {
  for (const span of Array.from(doc.querySelectorAll(`span.${dateClass}`))) {
    const parent = span.parentNode
    if (!parent) {
      continue
    }
    while (span.firstChild) {
      parent.insertBefore(span.firstChild, span)
    }
    parent.removeChild(span)
    parent.normalize()
  }
}
