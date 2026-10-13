// Conversions between compose editor formats preserve readable quote structure.
import { renderMarkdown } from './markdown'
import TurndownService from 'turndown'
import type { EditorMode } from './types'

/** Escape characters that would start markup in a text node. */
export function escapeHtml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// quotePrefix matches the "> " quote markers that open a line, "> > " and ">>"
// alike.
const quotePrefix = /^(?:>[ \t]?)+/

/** Render plain text as paragraphs and line breaks, preserving nested quote markers. */
export function textToHtml(text: string): string {
  let out = ''
  let open = 0
  let depth = 0
  let lines: string[] = []

  const flush = (): void => {
    if (lines.length === 0) {
      return
    }
    for (; open > depth; open--) out += '</blockquote>'
    for (; open < depth; open++) out += '<blockquote>'
    out += `<p>${lines.join('<br>')}</p>`
    lines = []
  }

  for (const raw of text.replace(/\r\n?/g, '\n').split('\n')) {
    const marker = raw.match(quotePrefix)?.[0] ?? ''
    const level = (marker.match(/>/g) ?? []).length
    const line = raw.slice(marker.length).trimEnd()
    if (level !== depth) {
      flush()
      depth = level
    }
    if (line === '') {
      flush()
      continue
    }
    lines.push(escapeHtml(line))
  }
  flush()
  for (; open > 0; open--) out += '</blockquote>'
  return out
}

// Both serializers work on inert fragments so conversion never mounts quoted
// mail in the app document or loads its remote resources.
function inertContent(html: string): DocumentFragment {
  const template = document.createElement('template')
  template.innerHTML = html
  template.content.querySelectorAll('script, style, iframe, object, embed, template').forEach((node) => node.remove())
  return template.content
}

const markdown = new TurndownService({ headingStyle: 'atx', bulletListMarker: '-', codeBlockStyle: 'fenced' })
const plain = new TurndownService({ bulletListMarker: '-' })
// Plain text keeps literal punctuation and structural quote/list markers,
// without Markdown's escaping or inline formatting delimiters.
plain.escape = (text) => text
plain.addRule('formatting', {
  filter: ['strong', 'b', 'em', 'i', 's', 'del', 'u', 'code'],
  replacement: (content) => content,
})
plain.addRule('heading', {
  filter: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6'],
  replacement: (content) => `\n\n${content}\n\n`,
})
plain.addRule('lineBreak', { filter: 'br', replacement: () => '\n' })
plain.addRule('preformatted', {
  filter: 'pre',
  replacement: (_content, node) => `\n\n${node.textContent ?? ''}\n\n`,
})
plain.addRule('link', {
  filter: 'a',
  replacement: (content, node) => {
    const href = node.getAttribute('href') ?? ''
    if (!href || content === href || href === `mailto:${content}`) return content
    return content ? `${content} (${href})` : href
  },
})
plain.addRule('image', {
  filter: 'img',
  replacement: (_content, node) => node.getAttribute('alt') ?? '',
})
// Turndown indents list content for Markdown. One space is enough for plain
// mail; continuation lines still need indentation to keep nested lists clear.
plain.addRule('listItem', {
  filter: 'li',
  replacement: (content, node) => {
    const parent = node.parentElement
    const index = parent ? Array.from(parent.children).indexOf(node) : 0
    const start = Number(parent?.getAttribute('start') ?? 1)
    const prefix = parent?.nodeName === 'OL' ? `${start + index}. ` : '- '
    return prefix + content.trim().replace(/\n/g, '\n' + ' '.repeat(prefix.length)) + '\n'
  },
})

/** Convert rich HTML to readable plain mail, including nested quotes and links. */
export function htmlToText(html: string): string {
  // Turndown trims each block, which would cut the signature delimiter down
  // to "--". Mail clients only recognize it with its trailing space, directly
  // above the signature.
  return plain.turndown(inertContent(html)).replace(/^--$(\n\n)?/gm, (_, gap?: string) => (gap ? '-- \n' : '-- '))
}

// A reply opens with empty lines above the quote for the answer. Neither
// renderer keeps leading blank lines, so they are carried across as an empty
// paragraph and back.
const leadingBlankLines = /^[ \t]*\n/
const leadingEmptyParagraph = /^\s*<p>(?:<br[^>]*>)?<\/p>/


/**
 * A private-use character that stands in for the caret while a body is
 * converted: put it into the source and it comes out where the caret lands.
 */
export const caretMark = '\uE000'

/** Convert the current body when changing editors; source mode determines its format. */
export function convertEditorBody(body: string, from: EditorMode, to: EditorMode): string {
  if (from === to) return body
  if (to === 'wysiwyg') {
    const html = from === 'markdown' ? renderMarkdown(body) : textToHtml(body)
    return leadingBlankLines.test(body) ? `<p></p>${html}` : html
  }
  if (from === 'wysiwyg') {
    const text = to === 'markdown' ? markdown.turndown(inertContent(body)) : htmlToText(body)
    return leadingEmptyParagraph.test(body) ? `\n\n${text}` : text
  }
  // Plain and Markdown share editable source text, also used for the plain
  // MIME part. Rendering here would lose literal punctuation and line breaks.
  return body
}
