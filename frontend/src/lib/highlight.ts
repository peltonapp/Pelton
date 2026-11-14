// highlight.ts finds the words of a search inside text, so list rows and the
// reading pane can mark them. Matching is case-insensitive and by substring,
// since the search itself matches prefixes and close spellings.

/** One run of text, marked when it matched a term. */
export interface HighlightPart {
  text: string
  hit: boolean
}

/** Builds one pattern over every term, longest first, or null for none. */
export function termPattern(terms: readonly string[]): RegExp | null {
  const usable = [...new Set(terms.map((t) => t.trim()).filter((t) => t.length >= 2))]
  if (usable.length === 0) {
    return null
  }
  usable.sort((a, b) => b.length - a.length)
  const escaped = usable.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  return new RegExp(escaped.join('|'), 'giu')
}

/** Splits text into marked and unmarked runs. */
export function highlightParts(text: string, terms: readonly string[]): HighlightPart[] {
  const pattern = termPattern(terms)
  if (!pattern || text === '') {
    return [{ text, hit: false }]
  }
  const parts: HighlightPart[] = []
  let last = 0
  for (const match of text.matchAll(pattern)) {
    const at = match.index ?? 0
    if (at > last) {
      parts.push({ text: text.slice(last, at), hit: false })
    }
    parts.push({ text: match[0], hit: true })
    last = at + match[0].length
  }
  if (last < text.length) {
    parts.push({ text: text.slice(last), hit: false })
  }
  return parts
}

/** The class marks inside a mail document carry, so they can be found again. */
export const markClass = 'pelton-hl'

/**
 * Wraps every match inside a document's body in a mark element, after taking
 * out any marks a previous pass left. Only text nodes are touched and the
 * marks are built with createElement, so nothing from the mail is parsed.
 * Returns how many marks were placed.
 */
export function markDocument(doc: Document, terms: readonly string[]): number {
  for (const old of Array.from(doc.querySelectorAll(`mark.${markClass}`))) {
    const parent = old.parentNode
    if (!parent) {
      continue
    }
    parent.replaceChild(doc.createTextNode(old.textContent ?? ''), old)
    parent.normalize()
  }
  const pattern = termPattern(terms)
  if (!pattern || !doc.body) {
    return 0
  }
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const tag = node.parentElement?.tagName
      if (tag === 'SCRIPT' || tag === 'STYLE' || tag === 'TEXTAREA') {
        return NodeFilter.FILTER_REJECT
      }
      return NodeFilter.FILTER_ACCEPT
    },
  })
  const nodes: Text[] = []
  while (walker.nextNode()) {
    nodes.push(walker.currentNode as Text)
  }
  let count = 0
  for (const node of nodes) {
    const parts = highlightParts(node.data, terms)
    if (parts.length === 1 && !parts[0].hit) {
      continue
    }
    const frag = doc.createDocumentFragment()
    for (const part of parts) {
      if (part.hit) {
        const mark = doc.createElement('mark')
        mark.className = markClass
        mark.textContent = part.text
        frag.appendChild(mark)
        count += 1
      } else {
        frag.appendChild(doc.createTextNode(part.text))
      }
    }
    node.parentNode?.replaceChild(frag, node)
  }
  return count
}
