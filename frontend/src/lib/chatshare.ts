// chatshare.ts builds the Markdown that sharing a mail to a chat room posts:
// the subject, who sent it and when, and a short quoted excerpt of the text.
// The dialog shows this text before anything is sent.

import { formatFullDate, type TimeFormat } from './format'
import type { MessageDetail } from './types'

/** The most characters of the body that are quoted. */
export const excerptLimit = 600

/** The fields of a message the share text is built from. */
export type ShareSource = Pick<MessageDetail, 'subject' | 'fromName' | 'fromAddress' | 'date' | 'bodyPlain' | 'bodyQuote'>

// en and em dashes, which the posted text never carries.
const longDashes = new RegExp(`[${String.fromCharCode(0x2013, 0x2014)}]`, 'g')

// the address inside angle brackets, or the whole field when there are none.
function bareAddress(from: string): string {
  const match = /<([^>]*)>/.exec(from)
  return (match ? match[1] : from).trim()
}

// the body as plain lines: original quotes, a reply attribution and a
// signature are dropped and blank lines collapse to one.
function cleanBody(text: string): string {
  const kept: string[] = []
  for (const raw of text.replace(/\r\n?/g, '\n').split('\n')) {
    const line = raw.replace(/\s+$/, '')
    if (line === '--' || line === '-- ') {
      break
    }
    if (line.trimStart().startsWith('>') || /^on .+ wrote:$/i.test(line.trim())) {
      continue
    }
    if (line === '' && (kept.length === 0 || kept[kept.length - 1] === '')) {
      continue
    }
    kept.push(line)
  }
  while (kept.length > 0 && kept[kept.length - 1] === '') {
    kept.pop()
  }
  return kept.join('\n')
}

/** Cuts text to about limit characters at a word boundary and adds "...". */
export function trimExcerpt(text: string, limit = excerptLimit): string {
  if (text.length <= limit) {
    return text
  }
  let cut = text.slice(0, limit)
  const next = text.charAt(limit)
  if (next !== '' && !/\s/.test(next)) {
    const space = cut.search(/\s\S*$/)
    if (space > 0) {
      cut = cut.slice(0, space)
    }
  }
  return `${cut.replace(/\s+$/, '')}...`
}

/** Builds the Markdown posted for a mail: subject, From, Date and a quoted excerpt. */
export function buildShareText(message: ShareSource, timeFormat: TimeFormat = 'auto'): string {
  const address = bareAddress(message.fromAddress)
  const name = message.fromName.trim()
  const from = name && name !== address ? `${name} <${address}>` : address
  const lines = [`**${message.subject.replace(/\s+/g, ' ').trim()}**`, `From: ${from}`]
  const date = formatFullDate(message.date, timeFormat)
  if (date) {
    lines.push(`Date: ${date}`)
  }
  const excerpt = trimExcerpt(cleanBody(message.bodyQuote || message.bodyPlain || ''))
  if (excerpt) {
    lines.push('', ...excerpt.split('\n').map((line) => (line ? `> ${line}` : '>')))
  }
  return lines.join('\n').replace(longDashes, '-')
}

/** Puts the user's note above the share text, separated by a blank line. */
export function withNote(note: string, text: string): string {
  const trimmed = note.trim()
  return trimmed ? `${trimmed}\n\n${text}` : text
}
