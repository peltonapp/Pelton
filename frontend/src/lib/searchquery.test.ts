import { describe, expect, it } from 'vitest'
import {
  addChip,
  buildFilter,
  emptyFilter,
  filterActive,
  parseQuery,
  parseToken,
  searchTerms,
  shortcutDate,
  toggleChip,
  tokenize,
  trailingOperator,
  trailingWord,
  type FolderResolver,
} from './searchquery'

// a Saturday, so "this week" reaches back to a Monday in the same month.
const now = new Date(2026, 10, 14, 15, 30)

const folders: FolderResolver = (name) => (name.toLowerCase() === 'archive' ? { id: 7, label: 'Archive' } : null)

describe('parseToken', () => {
  it('reads the field operators and their aliases', () => {
    expect(parseToken('from:ada@example.com')).toEqual({ field: 'from', value: 'ada@example.com' })
    expect(parseToken('sender:ada')).toEqual({ field: 'from', value: 'ada' })
    expect(parseToken('to:bob')).toEqual({ field: 'to', value: 'bob' })
    expect(parseToken('subject:invoice')).toEqual({ field: 'subject', value: 'invoice' })
  })

  it('drops quotes around a value', () => {
    expect(parseToken('from:"Ada Lovelace"')).toEqual({ field: 'from', value: 'Ada Lovelace' })
  })

  it('accepts only the flags it knows', () => {
    expect(parseToken('has:attachment')).toEqual({ field: 'has', value: 'attachment' })
    expect(parseToken('has:pictures')).toBeNull()
    expect(parseToken('is:unread')).toEqual({ field: 'is', value: 'unread' })
    expect(parseToken('is:flagged')).toEqual({ field: 'is', value: 'flagged' })
    expect(parseToken('is:starred')).toEqual({ field: 'is', value: 'flagged' })
    expect(parseToken('is:old')).toBeNull()
  })

  it('needs a real date or a shortcut for before and after', () => {
    expect(parseToken('after:2026-01-05', undefined, now)).toEqual({ field: 'after', value: '2026-01-05' })
    expect(parseToken('before:today', undefined, now)).toEqual({ field: 'before', value: '2026-11-14' })
    expect(parseToken('after:yesterday', undefined, now)).toEqual({ field: 'after', value: '2026-11-13' })
    expect(parseToken('after:soon', undefined, now)).toBeNull()
  })

  it('resolves in: against the folders it is given', () => {
    expect(parseToken('in:archive', folders)).toEqual({ field: 'in', value: 'archive', id: 7, label: 'Archive' })
    expect(parseToken('in:nowhere', folders)).toBeNull()
    expect(parseToken('in:archive')).toBeNull()
  })

  it('leaves anything else alone', () => {
    expect(parseToken('hello')).toBeNull()
    expect(parseToken('from:')).toBeNull()
    expect(parseToken('color:red')).toBeNull()
    expect(parseToken('https://example.com')).toBeNull()
  })
})

describe('parseQuery', () => {
  it('splits operators from free text', () => {
    const got = parseQuery('invoice from:ada is:unread march', folders, now)
    expect(got.text).toBe('invoice march')
    expect(got.chips).toEqual([
      { field: 'from', value: 'ada' },
      { field: 'is', value: 'unread' },
    ])
  })

  it('keeps a quoted value with spaces together', () => {
    expect(tokenize('from:"Ada Lovelace" notes')).toEqual(['from:"Ada Lovelace"', 'notes'])
    expect(parseQuery('from:"Ada Lovelace" notes').chips).toEqual([{ field: 'from', value: 'Ada Lovelace' }])
  })

  it('lets a later operator of the same field win', () => {
    expect(parseQuery('from:a from:b').chips).toEqual([{ field: 'from', value: 'b' }])
  })
})

describe('chips', () => {
  it('keeps unread and flagged side by side', () => {
    let chips = addChip([], { field: 'is', value: 'unread' })
    chips = addChip(chips, { field: 'is', value: 'flagged' })
    expect(chips).toHaveLength(2)
    chips = toggleChip(chips, { field: 'is', value: 'unread' })
    expect(chips).toEqual([{ field: 'is', value: 'flagged' }])
  })

  it('replaces a field that holds one value', () => {
    const chips = addChip([{ field: 'in', value: 'a', id: 1 }], { field: 'in', value: 'b', id: 2 })
    expect(chips).toEqual([{ field: 'in', value: 'b', id: 2 }])
  })
})

describe('buildFilter', () => {
  it('maps every chip onto the filter', () => {
    const f = buildFilter([
      { field: 'from', value: 'ada' },
      { field: 'to', value: 'bob' },
      { field: 'subject', value: 'notes' },
      { field: 'has', value: 'attachment' },
      { field: 'is', value: 'unread' },
      { field: 'is', value: 'flagged' },
      { field: 'in', value: 'Archive', id: 7 },
      { field: 'account', value: 'Work', id: 3 },
      { field: 'after', value: '2026-01-01' },
      { field: 'before', value: '2026-01-02' },
    ])
    expect(f).toEqual({
      from: 'ada',
      to: 'bob',
      subject: 'notes',
      hasAttachment: true,
      unreadOnly: true,
      flaggedOnly: true,
      folderId: 7,
      accountId: 3,
      afterUnix: Date.parse('2026-01-01') / 1000,
      beforeUnix: Date.parse('2026-01-02') / 1000 + 86399,
    })
    expect(filterActive(f)).toBe(true)
  })

  it('reports an empty filter as inactive', () => {
    expect(filterActive(buildFilter([]))).toBe(false)
    expect(buildFilter([])).toEqual(emptyFilter)
    expect(filterActive({ ...emptyFilter, folderId: 2 })).toBe(true)
    expect(filterActive({ ...emptyFilter, flaggedOnly: true })).toBe(true)
  })
})

describe('trailing tokens', () => {
  it('finds an operator being typed', () => {
    expect(trailingOperator('invoice from:ad')).toEqual({ field: 'from', partial: 'ad', start: 8 })
    expect(trailingOperator('from:"Ada L')).toEqual({ field: 'from', partial: 'Ada L', start: 0 })
    expect(trailingOperator('in:')).toEqual({ field: 'in', partial: '', start: 0 })
    expect(trailingOperator('from:ada ')).toBeNull()
    expect(trailingOperator('nope:x')).toBeNull()
  })

  it('finds the last word', () => {
    expect(trailingWord('quarterly rep')).toEqual({ word: 'rep', start: 10 })
    expect(trailingWord('done ')).toEqual({ word: '', start: 5 })
  })
})

describe('shortcutDate', () => {
  it('resolves each shortcut from the given day', () => {
    expect(shortcutDate('today', now)).toBe('2026-11-14')
    expect(shortcutDate('yesterday', now)).toBe('2026-11-13')
    expect(shortcutDate('thisWeek', now)).toBe('2026-11-09')
    expect(shortcutDate('lastMonth', now)).toBe('2026-10-01')
    expect(shortcutDate('lastMonth', new Date(2026, 0, 31))).toBe('2025-12-01')
  })
})

describe('searchTerms', () => {
  it('collects the words to highlight', () => {
    const terms = searchTerms('invoice "q3" a', [
      { field: 'from', value: 'ada@example.com', label: 'Ada Lovelace' },
      { field: 'is', value: 'unread' },
      { field: 'subject', value: 'Invoice' },
    ])
    expect(terms).toEqual(['invoice', 'q3', 'ada@example.com', 'Ada', 'Lovelace'])
  })
})
