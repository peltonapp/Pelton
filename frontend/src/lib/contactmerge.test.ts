import { describe, expect, it } from 'vitest'
import {
  buildRequest,
  chosenValue,
  defaultChoices,
  defaultSurvivor,
  differs,
  extraProperties,
  keepBoth,
  matchLevel,
  needsTarget,
  phonesMatch,
  reasonText,
  unionValues,
} from './contactmerge'
import type { Contact } from './types'

function contact(id: number, patch: Partial<Contact> = {}): Contact {
  return {
    id,
    bookId: 1,
    bookName: 'Personal',
    uid: `u${id}`,
    fullName: '',
    organization: '',
    title: '',
    note: '',
    emails: [],
    phones: [],
    readOnly: false,
    updated: '',
    extra: [],
    ...patch,
  }
}

const ada = contact(1, { fullName: 'Ada Lovelace', emails: [{ value: 'ada@work.example', label: 'work' }], phones: [{ value: '06 1234 5678', label: '' }] })
const augusta = contact(2, {
  fullName: 'Augusta Ada King',
  organization: 'Engine Co',
  note: 'second',
  emails: [{ value: 'ADA@work.example', label: 'home' }, { value: 'ada@home.example', label: 'home' }],
  phones: [{ value: '+31 6 1234 5678', label: 'mobile' }],
  extra: ['BDAY', 'NICKNAME'],
})

describe('survivor and choices', () => {
  it('keeps the card with the most details by default', () => {
    expect(defaultSurvivor([ada, augusta]).id).toBe(2)
    expect(defaultSurvivor([augusta, ada]).id).toBe(2)
  })

  it('takes a field from the survivor, or from the first card that has one', () => {
    expect(defaultChoices([ada, augusta], ada)).toEqual({ fullName: 1, organization: 2, title: 1, note: 2 })
  })

  it('knows where the cards disagree', () => {
    expect(differs([ada, augusta], 'fullName')).toBe(true)
    expect(differs([ada, augusta], 'organization')).toBe(false)
    expect(differs([contact(1, { note: 'a' }), contact(2, { note: ' a ' })], 'note')).toBe(false)
  })

  it('reads the value a choice gives', () => {
    expect(chosenValue([ada, augusta], 'fullName', 2)).toBe('Augusta Ada King')
    expect(chosenValue([ada, augusta], 'organization', 1)).toBe('Engine Co')
    expect(chosenValue([contact(1, { note: 'a' }), contact(2, { note: 'b' })], 'note', keepBoth)).toBe('a\n\nb')
  })
})

describe('unionValues', () => {
  it('combines addresses without case duplicates, survivor first', () => {
    const merged = unionValues([ada, augusta], ada, 'emails')
    expect(merged.map((e) => e.value)).toEqual(['ada@work.example', 'ada@home.example'])
    expect(merged[0].label).toBe('work')
  })

  it('puts the survivor first even when it is not first in the group', () => {
    expect(unionValues([ada, augusta], augusta, 'emails').map((e) => e.value)).toEqual(['ADA@work.example', 'ada@home.example'])
  })

  it('treats a national and an international number as one and keeps the international form', () => {
    const merged = unionValues([ada, augusta], ada, 'phones')
    expect(merged).toEqual([{ value: '+31 6 1234 5678', label: 'mobile' }])
  })

  it('keeps numbers that differ', () => {
    const other = contact(3, { phones: [{ value: '+44 20 7946 0958', label: '' }] })
    expect(unionValues([ada, other], ada, 'phones')).toHaveLength(2)
  })
})

describe('phonesMatch', () => {
  it('compares the digits, nationally or internationally written', () => {
    expect(phonesMatch('06 1234 5678', '0031 6 1234 5678')).toBe(true)
    expect(phonesMatch('06 1234 5678', '06 1234 5679')).toBe(false)
    expect(phonesMatch('123', '123')).toBe(false)
  })
})

describe('extras and targets', () => {
  it('lists the card properties the cards carry beyond the fields', () => {
    expect(extraProperties([ada, augusta])).toEqual(['BDAY', 'NICKNAME'])
  })

  it('needs a target book only across books', () => {
    expect(needsTarget([ada, augusta])).toBe(false)
    expect(needsTarget([ada, contact(3, { bookId: 2 })])).toBe(true)
  })
})

describe('buildRequest', () => {
  const choices = { fullName: 2, organization: 2, title: 1, note: keepBoth }

  it('names the survivor, the rest and the field choices', () => {
    const req = buildRequest([ada, augusta], { survivorId: 2, choices, targetBookId: 9 })
    expect(req).toEqual({
      survivorId: 2,
      mergeIds: [1],
      targetBookId: 0,
      choices: { name: 2, organization: 2, title: 1, note: -1 },
    })
  })

  it('sends the target book when the cards are in several books', () => {
    const other = contact(3, { bookId: 2 })
    expect(buildRequest([ada, other], { survivorId: 1, choices, targetBookId: 2 }).targetBookId).toBe(2)
  })
})

describe('describing a match', () => {
  const t = (key: string) =>
    ({
      'contacts.duplicates.reason.email': 'Same email address: {value}',
      'contacts.duplicates.reason.nameWith': 'Same name ({overlaps}): {value}',
      'contacts.duplicates.overlap.organization': 'same organisation',
      'contacts.duplicates.overlap.domain': 'same company',
    })[key] ?? key

  it('words the reasons', () => {
    expect(reasonText({ kind: 'email', value: 'a@b.example', overlap: [] }, t)).toBe('Same email address: a@b.example')
    expect(reasonText({ kind: 'name', value: 'Ada', overlap: ['organization', 'domain'] }, t)).toBe('Same name (same organisation, same company): Ada')
  })

  it('grades the score', () => {
    expect([100, 90, 89, 70, 60].map(matchLevel)).toEqual(['high', 'high', 'medium', 'medium', 'low'])
  })
})
