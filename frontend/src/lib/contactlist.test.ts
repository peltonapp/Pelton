import { describe, expect, it } from 'vitest'
import {
  displayName,
  initials,
  matchesQuery,
  organizationCounts,
  sectionContacts,
  sectionLetter,
  sortContacts,
} from './contactlist'
import type { Contact } from './types'

function contact(id: number, fullName: string, patch: Partial<Contact> = {}): Contact {
  return {
    id,
    bookId: 1,
    bookName: 'Main',
    uid: `uid-${id}`,
    fullName,
    organization: '',
    title: '',
    note: '',
    emails: [],
    phones: [],
    matrixId: '',
    readOnly: false,
    updated: '',
    extra: [],
    ...patch,
  }
}

describe('displayName and initials', () => {
  it('falls back to the first address for a nameless contact', () => {
    const c = contact(1, ' ', { emails: [{ value: 'ada@example.com', label: '' }] })
    expect(displayName(c)).toBe('ada@example.com')
  })

  it('takes the first and last word for initials', () => {
    expect(initials(contact(1, 'Ada Byron Lovelace'))).toBe('AL')
    expect(initials(contact(2, 'cher'))).toBe('C')
    expect(initials(contact(3, ''))).toBe('')
  })
})

describe('matchesQuery', () => {
  const c = contact(1, 'Ada Lovelace', {
    organization: 'Analytical Engines',
    emails: [{ value: 'ada@example.com', label: '' }],
    phones: [{ value: '+44 20 7946 0000', label: '' }],
  })

  it('looks at name, organization, address and number', () => {
    expect(matchesQuery(c, 'LOVE')).toBe(true)
    expect(matchesQuery(c, 'engines')).toBe(true)
    expect(matchesQuery(c, 'example.com')).toBe(true)
    expect(matchesQuery(c, '7946')).toBe(true)
    expect(matchesQuery(c, 'babbage')).toBe(false)
  })

  it('keeps everything for a blank query', () => {
    expect(matchesQuery(c, '   ')).toBe(true)
  })
})

describe('sectionLetter', () => {
  it('strips accents and groups the rest under #', () => {
    expect(sectionLetter('Émile')).toBe('E')
    expect(sectionLetter('42 Club')).toBe('#')
    expect(sectionLetter('')).toBe('#')
  })
})

describe('sortContacts', () => {
  const list = [
    contact(1, 'Zoe', { organization: 'Acme', updated: '2026-01-01T00:00:00Z' }),
    contact(2, 'adam', { updated: '2026-03-01T00:00:00Z' }),
    contact(3, 'Mia', { organization: 'Zed', updated: '2026-02-01T00:00:00Z' }),
  ]

  it('sorts by name without regard to case, and reverses', () => {
    expect(sortContacts(list, 'name').map((c) => c.id)).toEqual([2, 3, 1])
    expect(sortContacts(list, 'nameDesc').map((c) => c.id)).toEqual([1, 3, 2])
  })

  it('puts contacts without an organization last', () => {
    expect(sortContacts(list, 'organization').map((c) => c.id)).toEqual([1, 3, 2])
  })

  it('puts the newest first and leaves the input alone', () => {
    expect(sortContacts(list, 'recent').map((c) => c.id)).toEqual([2, 3, 1])
    expect(list.map((c) => c.id)).toEqual([1, 2, 3])
  })
})

describe('sectionContacts', () => {
  it('opens a section per letter and moves # to the end', () => {
    const list = sortContacts([contact(1, 'Bob'), contact(2, 'Anna'), contact(3, '7up'), contact(4, 'Alice')], 'name')
    const sections = sectionContacts(list, 'name', 'None')
    expect(sections.map((s) => s.label)).toEqual(['A', 'B', '#'])
    expect(sections[0].contacts.map((c) => c.id)).toEqual([4, 2])
  })

  it('sections by organization with a label for none', () => {
    const list = sortContacts([contact(1, 'Bob', { organization: 'Acme' }), contact(2, 'Anna')], 'organization')
    expect(sectionContacts(list, 'organization', 'No organization').map((s) => s.label)).toEqual([
      'Acme',
      'No organization',
    ])
  })

  it('draws one headerless run for recent', () => {
    const sections = sectionContacts([contact(1, 'Bob'), contact(2, 'Anna')], 'recent', 'None')
    expect(sections).toHaveLength(1)
    expect(sections[0].label).toBe('')
  })
})

describe('organizationCounts', () => {
  it('merges spellings that differ in case and skips blanks', () => {
    const counts = organizationCounts([
      contact(1, 'A', { organization: 'Acme' }),
      contact(2, 'B', { organization: 'ACME' }),
      contact(3, 'C', { organization: ' ' }),
      contact(4, 'D', { organization: 'Beta' }),
    ])
    expect(counts).toEqual([
      { name: 'Acme', count: 2 },
      { name: 'Beta', count: 1 },
    ])
  })
})
