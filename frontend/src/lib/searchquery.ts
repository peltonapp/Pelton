// searchquery.ts is the chip model shared by the search fields: typed operator
// tokens (from:, to:, subject:, in:, has:, is:, before:, after:, note:) become chips,
// and the chips become the structured SearchFilter the backend runs. Free text
// travels beside them as the query.

/** A field a chip constrains. account only comes from the results filters. */
export type ChipField = 'from' | 'to' | 'subject' | 'in' | 'has' | 'is' | 'before' | 'after' | 'note' | 'account'

/** The operators that can be typed, in the order suggestions list them. */
export const operatorKeys = ['from', 'to', 'subject', 'in', 'has', 'is', 'before', 'after', 'note'] as const

/** One typeable operator. */
export type OperatorKey = (typeof operatorKeys)[number]

const aliases: Record<string, ChipField> = {
  from: 'from',
  sender: 'from',
  to: 'to',
  subject: 'subject',
  in: 'in',
  folder: 'in',
  has: 'has',
  is: 'is',
  before: 'before',
  after: 'after',
  note: 'note',
}

/**
 * One committed constraint. value is what the filter uses: an address or text
 * for from/to/subject/note, 'attachment' or 'note' for has, 'unread' or 'flagged' for is, a
 * YYYY-MM-DD date for before/after, and a name for in/account, whose id carries
 * the folder or account id. label overrides what the chip shows.
 */
export interface SearchChip {
  field: ChipField
  value: string
  id?: number
  label?: string
}

/**
 * The structured constraints a search runs with: an optional date window (0 on
 * a side leaves it open), field-scoped terms, flag filters and a scope. Free
 * text is passed separately.
 */
export interface SearchFilter {
  afterUnix: number
  beforeUnix: number
  from: string
  to: string
  subject: string
  hasAttachment: boolean
  unreadOnly: boolean
  flaggedOnly: boolean
  accountId: number
  folderId: number
  // note matches the text of private notes; hasNote keeps messages with one.
  note: string
  hasNote: boolean
}

/** A filter that constrains nothing. */
export const emptyFilter: SearchFilter = {
  afterUnix: 0,
  beforeUnix: 0,
  from: '',
  to: '',
  subject: '',
  hasAttachment: false,
  unreadOnly: false,
  flaggedOnly: false,
  accountId: 0,
  folderId: 0,
  note: '',
  hasNote: false,
}

/** Reports whether any constraint in the filter is set. */
export function filterActive(f: SearchFilter): boolean {
  return (
    f.afterUnix > 0 ||
    f.beforeUnix > 0 ||
    f.from !== '' ||
    f.to !== '' ||
    f.subject !== '' ||
    f.hasAttachment ||
    f.unreadOnly ||
    f.flaggedOnly ||
    f.accountId > 0 ||
    f.folderId > 0 ||
    f.note !== '' ||
    f.hasNote
  )
}

/**
 * What a chip reads as. Flags read as a plain label; everything else as the
 * field and its value, or the chip's own label when it has one.
 */
export function chipLabel(chip: SearchChip, t: (key: string) => string): string {
  if (chip.field === 'has') {
    return chip.value === 'note' ? t('search.chip.hasNote') : t('messageList.search.chip.hasAttachment')
  }
  if (chip.field === 'is') {
    return chip.value === 'flagged' ? t('sidebar.view.flagged') : t('messageList.search.chip.isUnread')
  }
  const field =
    chip.field === 'in'
      ? t('search.chip.in')
      : chip.field === 'account'
        ? t('search.chip.account')
        : chip.field === 'note'
          ? t('search.chip.note')
          : t(`messageList.search.chip.${chip.field}`)
  return `${field}: ${chip.label ?? chip.value}`
}

/** Maps a typed operator name, aliases included, onto its field. */
export function operatorField(name: string): ChipField | null {
  return aliases[name.toLowerCase()] ?? null
}

/**
 * The identity a chip is replaced by. has: and is: chips each stand for one
 * flag and can sit side by side; every other field holds one value.
 */
export function chipKey(chip: SearchChip): string {
  return chip.field === 'has' || chip.field === 'is' ? `${chip.field}:${chip.value}` : chip.field
}

/** Adds a chip, replacing an existing chip with the same key. */
export function addChip(chips: SearchChip[], chip: SearchChip): SearchChip[] {
  const key = chipKey(chip)
  return [...chips.filter((c) => chipKey(c) !== key), chip]
}

/** Removes the chip with the same key. */
export function removeChip(chips: SearchChip[], chip: SearchChip): SearchChip[] {
  const key = chipKey(chip)
  return chips.filter((c) => chipKey(c) !== key)
}

/** Reports whether a chip with this key is present. */
export function hasChip(chips: SearchChip[], chip: SearchChip): boolean {
  const key = chipKey(chip)
  return chips.some((c) => chipKey(c) === key)
}

/** Adds the chip when absent and removes it when present. */
export function toggleChip(chips: SearchChip[], chip: SearchChip): SearchChip[] {
  return hasChip(chips, chip) ? removeChip(chips, chip) : addChip(chips, chip)
}

/** Looks a folder up by the name typed after in:. */
export type FolderResolver = (name: string) => { id: number; label: string } | null

/** Formats a date as YYYY-MM-DD in local time. */
export function isoDate(d: Date): string {
  const pad = (n: number): string => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** The date shortcuts offered for before: and after:. */
export const dateShortcuts = ['today', 'yesterday', 'thisWeek', 'lastMonth'] as const

/** One date shortcut. */
export type DateShortcut = (typeof dateShortcuts)[number]

/**
 * The date a shortcut stands for: today, yesterday, the Monday of this week,
 * or the first day of last month.
 */
export function shortcutDate(shortcut: DateShortcut, now: Date = new Date()): string {
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  if (shortcut === 'yesterday') {
    d.setDate(d.getDate() - 1)
  } else if (shortcut === 'thisWeek') {
    d.setDate(d.getDate() - ((d.getDay() + 6) % 7))
  } else if (shortcut === 'lastMonth') {
    d.setDate(1)
    d.setMonth(d.getMonth() - 1)
  }
  return isoDate(d)
}

function parseDateValue(raw: string, now: Date): string | null {
  const lower = raw.toLowerCase()
  if (lower === 'today' || lower === 'yesterday') {
    return shortcutDate(lower, now)
  }
  return Number.isNaN(Date.parse(raw)) ? null : raw
}

/**
 * Turns a "keyword:value" token into a chip, or null when it is not a
 * recognized, valid operator token. Quotes around the value are dropped.
 */
export function parseToken(token: string, resolveFolder?: FolderResolver, now: Date = new Date()): SearchChip | null {
  const at = token.indexOf(':')
  if (at <= 0) {
    return null
  }
  const field = operatorField(token.slice(0, at))
  const raw = unquote(token.slice(at + 1).trim())
  if (!field || raw === '') {
    return null
  }
  const lower = raw.toLowerCase()
  switch (field) {
    case 'has':
      if (lower.startsWith('note')) {
        return { field, value: 'note' }
      }
      return lower.startsWith('attach') ? { field, value: 'attachment' } : null
    case 'is':
      if (lower.startsWith('unread')) {
        return { field, value: 'unread' }
      }
      if (lower.startsWith('flag') || lower.startsWith('star')) {
        return { field, value: 'flagged' }
      }
      return null
    case 'before':
    case 'after': {
      const date = parseDateValue(raw, now)
      return date ? { field, value: date } : null
    }
    case 'in': {
      const folder = resolveFolder?.(raw)
      return folder ? { field, value: raw, id: folder.id, label: folder.label } : null
    }
    default:
      return { field, value: raw }
  }
}

function unquote(value: string): string {
  if (value.startsWith('"')) {
    return value.slice(1, value.endsWith('"') && value.length > 1 ? -1 : undefined)
  }
  return value
}

/** Splits text on whitespace, keeping a quoted operator value in one piece. */
export function tokenize(text: string): string[] {
  const out: string[] = []
  const re = /[^\s"]*"[^"]*"?|\S+/g
  for (const match of text.matchAll(re)) {
    out.push(match[0])
  }
  return out
}

/**
 * Pulls every complete operator token out of typed text into chips and
 * returns the free text that is left. Tokens that do not parse stay as text.
 */
export function parseQuery(
  text: string,
  resolveFolder?: FolderResolver,
  now: Date = new Date(),
): { text: string; chips: SearchChip[] } {
  let chips: SearchChip[] = []
  const rest: string[] = []
  for (const token of tokenize(text)) {
    const chip = parseToken(token, resolveFolder, now)
    if (chip) {
      chips = addChip(chips, chip)
    } else {
      rest.push(token)
    }
  }
  return { text: rest.join(' '), chips }
}

/** The operator token being typed at the end of the text, if there is one. */
export interface TrailingOperator {
  field: ChipField
  partial: string
  start: number
}

/**
 * Finds an operator being typed at the end of the text ("from:ad",
 * 'from:"Ada L'), with where its token starts so it can be replaced.
 */
export function trailingOperator(text: string): TrailingOperator | null {
  const match = /(^|\s)([a-z]+):("[^"]*|[^\s"]*)$/i.exec(text)
  if (!match) {
    return null
  }
  const field = operatorField(match[2])
  if (!field) {
    return null
  }
  return { field, partial: unquote(match[3]), start: match.index + match[1].length }
}

/** The last whitespace-separated word of the text and where it starts. */
export function trailingWord(text: string): { word: string; start: number } {
  const match = /(\S*)$/.exec(text)
  const word = match ? match[1] : ''
  return { word, start: text.length - word.length }
}

const day = 86400

/** Maps chips onto the structured filter the search runs with. */
export function buildFilter(chips: SearchChip[]): SearchFilter {
  const f: SearchFilter = { ...emptyFilter }
  for (const c of chips) {
    switch (c.field) {
      case 'from':
        f.from = c.value
        break
      case 'to':
        f.to = c.value
        break
      case 'subject':
        f.subject = c.value
        break
      case 'has':
        if (c.value === 'note') {
          f.hasNote = true
        } else {
          f.hasAttachment = true
        }
        break
      case 'note':
        f.note = c.value
        break
      case 'is':
        if (c.value === 'flagged') {
          f.flaggedOnly = true
        } else {
          f.unreadOnly = true
        }
        break
      case 'in':
        f.folderId = c.id ?? 0
        break
      case 'account':
        f.accountId = c.id ?? 0
        break
      case 'after':
        f.afterUnix = Math.floor(Date.parse(c.value) / 1000) || 0
        break
      case 'before':
        // include the whole "before" day.
        f.beforeUnix = Math.floor(Date.parse(c.value) / 1000) + day - 1 || 0
        break
    }
  }
  return f
}

/**
 * The words a search matched on, for highlighting: the free text and the
 * text of the from, to and subject chips. Operator syntax and single
 * characters are left out.
 */
export function searchTerms(text: string, chips: SearchChip[]): string[] {
  const words = tokenize(text).map(unquote)
  for (const c of chips) {
    if (c.field === 'from' || c.field === 'to' || c.field === 'subject') {
      words.push(...c.value.split(/\s+/))
      if (c.label && c.label !== c.value) {
        words.push(...c.label.split(/\s+/))
      }
    }
  }
  const seen = new Set<string>()
  const out: string[] = []
  for (const word of words) {
    const clean = word.replace(/^["']+|["']+$/g, '').trim()
    const key = clean.toLowerCase()
    if (clean.length < 2 || seen.has(key)) {
      continue
    }
    seen.add(key)
    out.push(clean)
  }
  return out
}
