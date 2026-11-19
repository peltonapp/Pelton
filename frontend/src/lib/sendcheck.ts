// sendcheck.ts decides what to ask before a message goes out: that it has
// nobody to go to, that its text speaks of an attachment it does not carry,
// or that it has no subject. It is pure; the composer shows the answer as a
// bar above the text.
//
// Only what the user wrote is read for an attachment: the subject they typed
// and the body above the quoted original and their signature, so "see
// attached" in an earlier message of the thread does not count. The phrases
// come from every language in lib/locales/phrases/attachments.ts.

import type { ComposeAttachment, EditorMode } from './types'
import { attachmentPhrases, type AttachmentPhrases } from './locales/phrases/attachments'
import { htmlToText } from './richtext'

/** One thing to ask about before sending, most pressing first. */
export type SendIssue = 'noRecipients' | 'missingAttachment' | 'emptySubject'

/** Which of the optional checks are on. */
export interface SendCheckOptions {
  missingAttachment: boolean
  emptySubject: boolean
}

/** What a send is checked against. */
export interface SendCheckInput {
  recipients: number
  subject: string
  /** The subject a reply or forward started with, to tell a typed one from it. */
  originalSubject?: string
  body: string
  mode: EditorMode
  attachments: Pick<ComposeAttachment, 'inline'>[]
}

interface CompiledPhrases {
  mention: RegExp
  negation: RegExp | null
}

// compile turns a language's fragments into two expressions, wrapping each
// fragment in word edges where the language has them.
function compile(p: AttachmentPhrases): CompiledPhrases {
  const wrap = (fragments: string[]): string =>
    fragments.map((f) => (p.wordBounded ? `(?<![\\p{L}\\p{N}])(?:${f})(?![\\p{L}\\p{N}])` : `(?:${f})`)).join('|')
  return {
    mention: new RegExp(wrap(p.mentions), 'iu'),
    negation: p.negations.length > 0 ? new RegExp(wrap(p.negations), 'iu') : null,
  }
}

const compiled: CompiledPhrases[] = Object.values(attachmentPhrases).map(compile)

// a sentence ends at its punctuation, in any of the scripts above, or at a
// line break.
const sentenceEnd = /[.!?。！？؟\n]+/

/**
 * mentionsAttachment reports whether text says something is attached, in any
 * language. A sentence that also says nothing is attached, or that it comes
 * later, does not count, whichever language says so.
 */
export function mentionsAttachment(text: string): boolean {
  for (const sentence of text.split(sentenceEnd)) {
    if (sentence.trim() === '' || compiled.some((lang) => lang.negation?.test(sentence))) {
      continue
    }
    if (compiled.some((lang) => lang.mention.test(sentence))) {
      return true
    }
  }
  return false
}

// lines that begin the original under a reply or forward, as the composer
// writes them, and the signature delimiter.
const originalStarts = [/^-{2,}\s*(forwarded|original) message\s*-{2,}/i, /^on .+ wrote:\s*$/i]
const signatureDelimiter = /^--\s?$/

/**
 * ownText is what the user wrote of a body: in the rich editor without quoted
 * blocks, and in either editor cut at the signature delimiter or at the start
 * of a quoted or forwarded original, with quoted ("> ") lines left out.
 */
export function ownText(body: string, mode: EditorMode): string {
  const text = mode === 'wysiwyg' ? htmlToText(withoutBlockquotes(body)) : body
  const out: string[] = []
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (signatureDelimiter.test(line) || originalStarts.some((p) => p.test(trimmed))) {
      break
    }
    if (trimmed.startsWith('>')) {
      continue
    }
    out.push(line)
  }
  return out.join('\n')
}

// withoutBlockquotes drops quoted blocks from html.
function withoutBlockquotes(html: string): string {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  for (const quote of Array.from(doc.querySelectorAll('blockquote'))) {
    quote.remove()
  }
  return doc.body.innerHTML
}

/**
 * typedSubject is the part of a subject the user wrote: all of it, or nothing
 * when it is the original's with a reply or forward prefix.
 */
export function typedSubject(subject: string, original?: string): string {
  if (!original) {
    return subject
  }
  const bare = (s: string): string =>
    s
      .trim()
      .replace(/^((re|fwd?|aw|wg|sv|vs|tr|rv|r|i)\s*:\s*)+/i, '')
      .trim()
      .toLowerCase()
  return bare(subject) === bare(original) ? '' : subject
}

/** hasFileAttached reports whether a message carries a file, inline images aside. */
export function hasFileAttached(attachments: Pick<ComposeAttachment, 'inline'>[]): boolean {
  return attachments.some((a) => !a.inline)
}

/**
 * sendIssues lists what to ask about before sending, most pressing first.
 * Nobody to send to is always asked; the other two follow the options.
 */
export function sendIssues(input: SendCheckInput, options: SendCheckOptions): SendIssue[] {
  const out: SendIssue[] = []
  if (input.recipients === 0) {
    out.push('noRecipients')
  }
  if (
    options.missingAttachment &&
    !hasFileAttached(input.attachments) &&
    (mentionsAttachment(typedSubject(input.subject, input.originalSubject)) || mentionsAttachment(ownText(input.body, input.mode)))
  ) {
    out.push('missingAttachment')
  }
  if (options.emptySubject && input.subject.trim() === '') {
    out.push('emptySubject')
  }
  return out
}
