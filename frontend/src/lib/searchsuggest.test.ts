import { describe, expect, it } from 'vitest'
import { suggest, suggestContext, type FolderOption, type Person, type RecentSearch } from './searchsuggest'

const now = new Date(2026, 10, 14)
const people: Person[] = [
  { email: 'ada@example.com', name: 'Ada Lovelace' },
  { email: 'alan@example.com', name: '' },
]
const folders: FolderOption[] = [
  { id: 1, name: 'INBOX', accountLabel: 'Work' },
  { id: 2, name: 'Archive', accountLabel: 'Work' },
  { id: 3, name: 'Archive', accountLabel: 'Home' },
]
const recents: RecentSearch[] = [
  { text: 'invoice', chips: [] },
  { text: '', chips: [{ field: 'from', value: 'ada@example.com', label: 'Ada Lovelace' }] },
]

function base(text: string): Parameters<typeof suggest>[0] {
  return { text, people, folders, recents, now }
}

describe('suggestContext', () => {
  it('tells an operator value from a plain word', () => {
    expect(suggestContext('report from:ad')).toEqual({ field: 'from', partial: 'ad', start: 7 })
    expect(suggestContext('report ad')).toEqual({ field: null, partial: 'ad', start: 7 })
  })
})

describe('suggest', () => {
  it('offers recent searches, a way to clear them and the operators on an empty field', () => {
    const rows = suggest(base(''))
    expect(rows.slice(0, 3).map((r) => r.kind)).toEqual(['recent', 'recent', 'clearRecent'])
    expect(rows.filter((r) => r.kind === 'operator')).toHaveLength(9)
  })

  it('leaves out the clear entry when nothing is recent', () => {
    const rows = suggest({ ...base(''), recents: [] })
    expect(rows.some((r) => r.kind === 'clearRecent')).toBe(false)
  })

  it('completes from: with people and the typed text', () => {
    const rows = suggest(base('from:a'))
    expect(rows).toEqual([
      { kind: 'chip', group: 'people', chip: { field: 'from', value: 'ada@example.com', label: 'Ada Lovelace' } },
      { kind: 'chip', group: 'people', chip: { field: 'from', value: 'alan@example.com', label: 'alan@example.com' } },
      { kind: 'chip', group: 'values', chip: { field: 'from', value: 'a' } },
    ])
  })

  it('completes in: with matching folders across accounts', () => {
    const rows = suggest(base('in:arch'))
    expect(rows.map((r) => (r.kind === 'chip' ? r.chip.id : null))).toEqual([2, 3])
  })

  it('completes has: and is: with the values they take', () => {
    expect(suggest(base('has:'))).toEqual([
      { kind: 'chip', group: 'values', chip: { field: 'has', value: 'attachment' } },
      { kind: 'chip', group: 'values', chip: { field: 'has', value: 'note' } },
    ])
    expect(suggest(base('has:n'))).toEqual([{ kind: 'chip', group: 'values', chip: { field: 'has', value: 'note' } }])
    expect(suggest(base('note:venue'))).toEqual([{ kind: 'chip', group: 'values', chip: { field: 'note', value: 'venue' } }])
    expect(suggest(base('is:f'))).toEqual([{ kind: 'chip', group: 'values', chip: { field: 'is', value: 'flagged' } }])
    expect(suggest(base('is:'))).toHaveLength(2)
  })

  it('completes before: and after: with date shortcuts and a picker', () => {
    const rows = suggest(base('after:'))
    expect(rows.map((r) => r.kind)).toEqual(['chip', 'chip', 'chip', 'chip', 'datePicker'])
    expect(rows[0]).toMatchObject({ shortcut: 'today', chip: { field: 'after', value: '2026-11-14' } })
    expect(suggest(base('before:2026-01-02'))[0]).toMatchObject({ chip: { field: 'before', value: '2026-01-02' } })
  })

  it('offers operators, people, folders and recents for plain text', () => {
    const rows = suggest({ ...base('i'), people: [], recents: [] })
    expect(rows[0]).toEqual({ kind: 'operator', key: 'in' })
    expect(rows[1]).toEqual({ kind: 'operator', key: 'is' })
    const kinds = suggest(base('arch')).map((r) => (r.kind === 'chip' ? r.group : r.kind))
    expect(kinds).toEqual(['people', 'people', 'folders', 'folders'])
    const fromRecent = suggest({ ...base('lovelace'), people: [] })
    expect(fromRecent).toEqual([{ kind: 'recent', search: recents[1] }])
  })

  it('completes an operator name being typed', () => {
    const rows = suggest({ ...base('su'), people: [] })
    expect(rows[0]).toEqual({ kind: 'operator', key: 'subject' })
  })
})
