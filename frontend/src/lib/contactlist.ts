// contactlist.ts holds the pure rules of the contacts list in the new design:
// what a contact is called, which ones a search keeps, in what order they come
// and which section header they sit under.

import type { Contact } from './types'

/** How the contact list is ordered. */
export type ContactSort = 'name' | 'nameDesc' | 'organization' | 'recent'

/** One run of contacts under a shared header. An empty label has no header. */
export interface ContactSection {
  key: string
  label: string
  contacts: Contact[]
}

/** The name a contact is listed under: its name, else its first address. */
export function displayName(contact: Contact): string {
  return contact.fullName.trim() || contact.emails[0]?.value.trim() || ''
}

/** Up to two letters to draw in an avatar. */
export function initials(contact: Contact): string {
  const words = displayName(contact)
    .split(/[\s@.]+/)
    .filter(Boolean)
  if (words.length === 0) {
    return ''
  }
  const first = Array.from(words[0])[0] ?? ''
  const last = words.length > 1 ? (Array.from(words[words.length - 1])[0] ?? '') : ''
  return (first + last).toLocaleUpperCase()
}

/** Whether a contact matches a search: a substring of name, organization, address or number. */
export function matchesQuery(contact: Contact, query: string): boolean {
  const needle = query.trim().toLocaleLowerCase()
  if (needle === '') {
    return true
  }
  return (
    contact.fullName.toLocaleLowerCase().includes(needle) ||
    contact.organization.toLocaleLowerCase().includes(needle) ||
    contact.emails.some((e) => e.value.toLocaleLowerCase().includes(needle)) ||
    contact.phones.some((p) => p.value.toLocaleLowerCase().includes(needle))
  )
}

/** The section letter of a name: its first letter without accents, or # for anything else. */
export function sectionLetter(name: string): string {
  const first = Array.from(name.trim())[0] ?? ''
  const bare = first.normalize('NFD').replace(/\p{M}/gu, '')
  return /\p{L}/u.test(bare) ? bare.toLocaleUpperCase() : '#'
}

function byName(a: Contact, b: Contact): number {
  return displayName(a).localeCompare(displayName(b), undefined, { sensitivity: 'base', numeric: true })
}

/** Returns the contacts in the given order, leaving the input as it was. */
export function sortContacts(list: Contact[], sort: ContactSort): Contact[] {
  const out = [...list]
  switch (sort) {
    case 'nameDesc':
      return out.sort((a, b) => byName(b, a))
    case 'organization':
      return out.sort((a, b) => {
        const left = a.organization.trim()
        const right = b.organization.trim()
        if (left === '' || right === '') {
          return left === right ? byName(a, b) : left === '' ? 1 : -1
        }
        return left.localeCompare(right, undefined, { sensitivity: 'base' }) || byName(a, b)
      })
    case 'recent':
      return out.sort((a, b) => (Date.parse(b.updated) || 0) - (Date.parse(a.updated) || 0) || byName(a, b))
    default:
      return out.sort(byName)
  }
}

/**
 * Splits an ordered list into the sections its order implies: letters for the
 * name orders, organizations for the organization order, one headerless run for
 * the recent order. noOrganization labels contacts without one.
 */
export function sectionContacts(list: Contact[], sort: ContactSort, noOrganization: string): ContactSection[] {
  if (sort === 'recent') {
    return list.length ? [{ key: '', label: '', contacts: list }] : []
  }
  const sections: ContactSection[] = []
  for (const contact of list) {
    let key: string
    let label: string
    if (sort === 'organization') {
      label = contact.organization.trim() || noOrganization
      key = contact.organization.trim().toLocaleLowerCase() || '\u0000'
    } else {
      label = sectionLetter(displayName(contact))
      key = label
    }
    const last = sections[sections.length - 1]
    if (last && last.key === key) {
      last.contacts.push(contact)
    } else {
      sections.push({ key, label, contacts: [contact] })
    }
  }
  // symbols and digits sort before letters by locale, so their section moves to the end.
  if (sort === 'name') {
    const hash = sections.findIndex((s) => s.label === '#')
    if (hash >= 0 && hash !== sections.length - 1) {
      sections.push(...sections.splice(hash, 1))
    }
  }
  return sections
}

/** Every organization in the list with how many contacts carry it, by name. */
export function organizationCounts(list: Contact[]): { name: string; count: number }[] {
  const counts = new Map<string, { name: string; count: number }>()
  for (const contact of list) {
    const name = contact.organization.trim()
    if (name === '') {
      continue
    }
    const key = name.toLocaleLowerCase()
    const entry = counts.get(key)
    if (entry) {
      entry.count += 1
    } else {
      counts.set(key, { name, count: 1 })
    }
  }
  return [...counts.values()].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }))
}
