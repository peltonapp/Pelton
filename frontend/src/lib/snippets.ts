// snippets.ts is the pure side of snippets: filling in the placeholders a
// snippet carries, spotting a trigger word typed before the caret, and
// narrowing the list for the picker. Nothing here touches a store or the
// editor, so all of it is tested on its own.

import type { Snippet } from './types'
import { parseAddressList } from './mailcompose'
import { fuzzyMatch } from './fuzzy'

/** The prefix a trigger word is typed with, as in ";thanks". */
export const triggerPrefix = ';'

/** What the built-in placeholders are filled from. */
export interface SnippetContext {
  /** The first recipient's first name, '' when unknown. */
  firstName: string
  /** The first recipient's full name, '' when unknown. */
  fullName: string
  /** The sender's own name. */
  myName: string
  /** Today's date, already formatted. */
  date: string
}

/** The placeholders filled in on insert; any other {name} is tabbed through. */
export const builtinPlaceholders = ['first name', 'full name', 'my name', 'date'] as const

// a placeholder: braces around anything but braces or a line break.
const placeholderPattern = /\{([^{}\n]+)\}/g

/**
 * expandPlaceholders fills in the built-in placeholders, matched without
 * regard to case or surrounding space. One with nothing to fill it with, a
 * first name when the recipient gave none, stays as it is, so it is tabbed
 * to like a free placeholder.
 */
export function expandPlaceholders(body: string, ctx: SnippetContext): string {
  const values: Record<string, string> = {
    'first name': ctx.firstName,
    'full name': ctx.fullName,
    'my name': ctx.myName,
    date: ctx.date,
  }
  return body.replace(placeholderPattern, (whole, name: string) => {
    const value = values[name.trim().toLowerCase().replace(/\s+/g, ' ')]
    return value ? value : whole
  })
}

/** placeholdersIn lists the placeholders left in text, in order. */
export function placeholdersIn(text: string): string[] {
  return [...text.matchAll(placeholderPattern)].map((m) => m[0])
}

/**
 * recipientNames reads the names a snippet greets from a To line: the first
 * recipient's name and its first word, or the part of the address before the
 * @ when no name is given.
 */
export function recipientNames(to: string): { firstName: string; fullName: string } {
  const first = parseAddressList(to)[0]
  if (!first) {
    return { firstName: '', fullName: '' }
  }
  const name = first.name.trim()
  if (name !== '') {
    const words = name.includes(',') ? name.split(',').reverse().map((w) => w.trim()) : name.split(/\s+/)
    return { firstName: words.find((w) => w !== '') ?? name, fullName: name }
  }
  const local = first.email.split('@')[0] ?? ''
  const word = local.split(/[._-]/)[0] ?? ''
  const cap = word ? word.charAt(0).toUpperCase() + word.slice(1) : ''
  return { firstName: cap, fullName: '' }
}

/** A trigger typed before the caret and the snippet it names. */
export interface TriggerMatch {
  snippet: Snippet
  /** How many characters before the caret the trigger takes, prefix included. */
  length: number
}

/**
 * matchTrigger finds a snippet trigger at the end of the text before the
 * caret: the prefix and a word, at the start of the text or after a space.
 * Case does not matter; a word no snippet uses matches nothing.
 */
export function matchTrigger(before: string, snippets: Snippet[]): TriggerMatch | null {
  const match = /(^|\s);([\p{L}\p{N}_-]+)$/u.exec(before)
  if (!match) {
    return null
  }
  const word = match[2].toLowerCase()
  const snippet = snippets.find((s) => s.trigger !== '' && s.trigger.toLowerCase() === word)
  return snippet ? { snippet, length: word.length + triggerPrefix.length } : null
}

/**
 * filterSnippets narrows snippets to a typed query, best first, matching the
 * name, the trigger and the text. An empty query keeps the user's order.
 */
export function filterSnippets(snippets: Snippet[], query: string): Snippet[] {
  const q = query.trim().replace(/^;/, '')
  if (q === '') {
    return snippets
  }
  const scored: { snippet: Snippet; score: number }[] = []
  for (const snippet of snippets) {
    const onName = fuzzyMatch(q, snippet.name)
    const onTrigger = snippet.trigger ? fuzzyMatch(q, snippet.trigger) : null
    const inBody = snippet.body.toLowerCase().includes(q.toLowerCase())
    const score = Math.max(onName?.score ?? -Infinity, (onTrigger?.score ?? -Infinity) * 0.9, inBody ? 0 : -Infinity)
    if (score !== -Infinity) {
      scored.push({ snippet, score })
    }
  }
  return scored.sort((a, b) => b.score - a.score).map((s) => s.snippet)
}

/** The header fields a snippet fills, each only where the composer has none. */
export interface SnippetFields {
  subject?: string
  to?: string
  cc?: string
  showCc?: boolean
}

/** snippetFields is what inserting a snippet changes besides the body. */
export function snippetFields(snippet: Snippet, current: { subject: string; to: string; cc: string }): SnippetFields {
  const out: SnippetFields = {}
  if (snippet.subject && current.subject.trim() === '') {
    out.subject = snippet.subject
  }
  if (snippet.to && current.to.trim() === '') {
    out.to = snippet.to
  }
  if (snippet.cc && current.cc.trim() === '') {
    out.cc = snippet.cc
    out.showCc = true
  }
  return out
}
