// searchsuggest.ts builds the dropdown under the top bar search: operator
// completion with real values (people, folders, flags, dates) while an
// operator is being typed, and people, folders and recent searches for plain
// text. People arrive from the address autocomplete; everything else is local.

import {
  dateShortcuts,
  operatorKeys,
  shortcutDate,
  trailingOperator,
  trailingWord,
  type ChipField,
  type DateShortcut,
  type OperatorKey,
  type SearchChip,
} from './searchquery'

/** A person the address autocomplete offered. */
export interface Person {
  email: string
  name: string
}

/** A folder that in: can scope a search to. */
export interface FolderOption {
  id: number
  name: string
  accountLabel: string
}

/** A search as recent searches hold it. */
export interface RecentSearch {
  text: string
  chips: SearchChip[]
}

/** One row of the dropdown. */
export type Suggestion =
  | { kind: 'operator'; key: OperatorKey }
  | { kind: 'chip'; chip: SearchChip; group: 'people' | 'folders' | 'values' | 'dates'; shortcut?: DateShortcut }
  | { kind: 'datePicker'; field: 'before' | 'after' }
  | { kind: 'recent'; search: RecentSearch }
  | { kind: 'clearRecent' }

/** What the suggestions are built from. */
export interface SuggestInput {
  text: string
  people: Person[]
  folders: FolderOption[]
  recents: RecentSearch[]
  now?: Date
}

/** Where a suggestion's chip replaces text from, and what it searches people for. */
export interface SuggestContext {
  field: ChipField | null
  partial: string
  start: number
}

const plainLimit = 3

/**
 * What the end of the text is asking for: an operator's value, or a plain
 * word, with where that token starts.
 */
export function suggestContext(text: string): SuggestContext {
  const op = trailingOperator(text)
  if (op) {
    return op
  }
  const { word, start } = trailingWord(text)
  return { field: null, partial: word, start }
}

function folderMatches(folders: FolderOption[], partial: string, limit: number): Suggestion[] {
  const needle = partial.toLowerCase()
  return folders
    .filter((f) => needle === '' || f.name.toLowerCase().includes(needle))
    .slice(0, limit)
    .map((f) => ({
      kind: 'chip' as const,
      group: 'folders' as const,
      chip: { field: 'in' as const, value: f.name, id: f.id, label: f.name },
    }))
}

function personChips(field: 'from' | 'to', people: Person[], limit: number): Suggestion[] {
  return people.slice(0, limit).map((p) => ({
    kind: 'chip' as const,
    group: 'people' as const,
    chip: { field, value: p.email, label: p.name || p.email },
  }))
}

function recentText(r: RecentSearch): string {
  return [r.text, ...r.chips.map((c) => c.label ?? c.value)].join(' ').toLowerCase()
}

/** Builds the dropdown rows for the current text. */
export function suggest(input: SuggestInput): Suggestion[] {
  const now = input.now ?? new Date()
  const ctx = suggestContext(input.text)
  const partial = ctx.partial.trim()
  const lower = partial.toLowerCase()

  switch (ctx.field) {
    case 'from':
    case 'to': {
      const out = personChips(ctx.field, input.people, 6)
      if (partial !== '' && !out.some((s) => s.kind === 'chip' && s.chip.value.toLowerCase() === lower)) {
        out.push({ kind: 'chip', group: 'values', chip: { field: ctx.field, value: partial } })
      }
      return out
    }
    case 'in':
      return folderMatches(input.folders, partial, 8)
    case 'has':
      return 'attachment'.startsWith(lower) ? [{ kind: 'chip', group: 'values', chip: { field: 'has', value: 'attachment' } }] : []
    case 'is':
      return (['unread', 'flagged'] as const)
        .filter((v) => v.startsWith(lower))
        .map((value) => ({ kind: 'chip' as const, group: 'values' as const, chip: { field: 'is' as const, value } }))
    case 'before':
    case 'after': {
      const field = ctx.field
      const out: Suggestion[] = dateShortcuts.map((shortcut) => ({
        kind: 'chip' as const,
        group: 'dates' as const,
        shortcut,
        chip: { field, value: shortcutDate(shortcut, now) },
      }))
      if (/^\d{4}-\d{1,2}-\d{1,2}$/.test(partial) && !Number.isNaN(Date.parse(partial))) {
        out.unshift({ kind: 'chip', group: 'dates', chip: { field, value: partial } })
      }
      out.push({ kind: 'datePicker', field })
      return out
    }
    case 'subject':
      return partial === '' ? [] : [{ kind: 'chip', group: 'values', chip: { field: 'subject', value: partial } }]
    case 'account':
      return []
  }

  const out: Suggestion[] = []
  if (input.text.trim() === '') {
    for (const search of input.recents) {
      out.push({ kind: 'recent', search })
    }
    if (input.recents.length > 0) {
      out.push({ kind: 'clearRecent' })
    }
    for (const key of operatorKeys) {
      out.push({ kind: 'operator', key })
    }
    return out
  }

  if (lower !== '') {
    for (const key of operatorKeys) {
      if (key.startsWith(lower) && key !== lower) {
        out.push({ kind: 'operator', key })
      }
    }
    out.push(...personChips('from', input.people, plainLimit))
    out.push(...folderMatches(input.folders, partial, plainLimit))
  }
  const needle = input.text.trim().toLowerCase()
  out.push(
    ...input.recents
      .filter((r) => recentText(r).includes(needle))
      .slice(0, plainLimit)
      .map((search) => ({ kind: 'recent' as const, search })),
  )
  return out
}
