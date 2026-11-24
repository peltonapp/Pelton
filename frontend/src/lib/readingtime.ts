// readingtime.ts estimates how long a message takes to read, from the words
// of its visible text: the quoted original a reply carries is left out, since
// the reader shows it folded.

import { splitHtmlQuote, splitPlainQuote } from './quotes'

/** Words read per minute, a typical pace for prose read on a screen. */
export const wordsPerMinute = 230

/** Counts the words in a text: runs of letters or digits, in any script. */
export function countWords(text: string): number {
  return text.match(/[\p{L}\p{N}][\p{L}\p{N}'’-]*/gu)?.length ?? 0
}

/**
 * The minutes a text of this many words takes, rounded, or 0 under about a
 * minute, which is not worth saying.
 */
export function readingMinutes(words: number, pace: number = wordsPerMinute): number {
  const minutes = words / pace
  return minutes < 0.85 ? 0 : Math.max(1, Math.round(minutes))
}

/** The text a message shows before its folded quote, for counting. */
export function visibleText(body: { isHtml: boolean; bodyHtmlSafe: string; bodyPlain: string }): string {
  if (!body.isHtml) {
    return splitPlainQuote(body.bodyPlain).head
  }
  const doc = new DOMParser().parseFromString(splitHtmlQuote(body.bodyHtmlSafe).head, 'text/html')
  // text nodes joined with a space, so two paragraphs never run into one word.
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT)
  const parts: string[] = []
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    parts.push(node.nodeValue ?? '')
  }
  return parts.join(' ')
}
