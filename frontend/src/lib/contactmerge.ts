// contactmerge.ts is the front half of merging contacts: what the compare view
// pre-selects, what the merged contact will hold, and the request sent to the
// backend. The backend does the real combining; the previews here follow the
// same rules so the result shown is the result written.

import type { Contact, ContactValue, DuplicateReason, MergeRequest } from './types'

/** The single-valued fields a merge picks between, as the contact carries them. */
export type MergeField = 'fullName' | 'organization' | 'title' | 'note'

/** The fields in the order the compare view lists them. */
export const mergeFields: MergeField[] = ['fullName', 'organization', 'title', 'note']

/** The choice that keeps the notes of every contact. */
export const keepBoth = -1

const choiceKey: Record<MergeField, string> = {
  fullName: 'name',
  organization: 'organization',
  title: 'title',
  note: 'note',
}

const minPhoneDigits = 7

/** The ids a field choice can hold: a contact's id or keepBoth. */
export type Choices = Record<MergeField, number>

function filled(value: string): boolean {
  return value.trim() !== ''
}

/** The digits of a number without leading zeros, or '' when too few to compare. */
export function phoneKey(value: string): string {
  const digits = value.replace(/\D/g, '').replace(/^0+/, '')
  return digits.length >= minPhoneDigits ? digits : ''
}

/** Whether two numbers are the same line, nationally or internationally written. */
export function phonesMatch(a: string, b: string): boolean {
  const ka = phoneKey(a)
  const kb = phoneKey(b)
  if (!ka || !kb) {
    return false
  }
  return ka.length > kb.length ? ka.endsWith(kb) : kb.endsWith(ka)
}

/** How much a contact holds, to pick the card that keeps the most as the survivor. */
export function detailCount(c: Contact): number {
  return mergeFields.filter((f) => filled(c[f])).length + c.emails.length + c.phones.length + c.extra.length
}

/** The contact to keep by default: the one with the most details, the first on a tie. */
export function defaultSurvivor(group: Contact[]): Contact {
  return group.reduce((best, c) => (detailCount(c) > detailCount(best) ? c : best), group[0])
}

/** The distinct non-empty values a field has across the group. */
export function distinctValues(group: Contact[], field: MergeField): string[] {
  return [...new Set(group.map((c) => c[field].trim()).filter(filled))]
}

/** True when the cards disagree about a field, so the user has something to pick. */
export function differs(group: Contact[], field: MergeField): boolean {
  return distinctValues(group, field).length > 1
}

/** Each field comes from the survivor, or from the first contact that has a value when the survivor has none. */
export function defaultChoices(group: Contact[], survivor: Contact): Choices {
  const out = {} as Choices
  for (const field of mergeFields) {
    out[field] = filled(survivor[field]) ? survivor.id : (group.find((c) => filled(c[field]))?.id ?? survivor.id)
  }
  return out
}

/** The value a field ends up with under a choice. */
export function chosenValue(group: Contact[], field: MergeField, choice: number): string {
  if (field === 'note' && choice === keepBoth) {
    return [...new Set(group.map((c) => c.note.trim()).filter(filled))].join('\n\n')
  }
  const from = group.find((c) => c.id === choice && filled(c[field])) ?? group.find((c) => filled(c[field]))
  return from ? from[field].trim() : ''
}

/**
 * The addresses or numbers the merged contact holds: every contact's, the
 * survivor's first, each once. An address matches without regard to case, a
 * number when both write the same line, and then the form with a country code
 * wins.
 */
export function unionValues(group: Contact[], survivor: Contact, kind: 'emails' | 'phones'): ContactValue[] {
  const ordered = [survivor, ...group.filter((c) => c.id !== survivor.id)]
  const out: ContactValue[] = []
  for (const contact of ordered) {
    for (const v of contact[kind]) {
      const value = v.value.trim()
      if (!value) {
        continue
      }
      const at = out.findIndex((have) =>
        kind === 'emails'
          ? have.value.toLowerCase() === value.toLowerCase()
          : phonesMatch(have.value, value) || (phoneKey(have.value) === '' && have.value.toLowerCase() === value.toLowerCase()),
      )
      if (at === -1) {
        out.push({ value, label: v.label })
        continue
      }
      if (!out[at].label) {
        out[at] = { ...out[at], label: v.label }
      }
      if (kind === 'phones' && value.startsWith('+') && !out[at].value.startsWith('+')) {
        out[at] = { ...out[at], value }
      }
    }
  }
  return out
}

/** Every vCard property name the cards carry that Pelton has no field for. */
export function extraProperties(group: Contact[]): string[] {
  return [...new Set(group.flatMap((c) => c.extra))].sort()
}

/** The ids of the address books the contacts are in. */
export function bookIds(group: Contact[]): number[] {
  return [...new Set(group.map((c) => c.bookId))]
}

/** True when the contacts span address books, so the user must pick where the merged one goes. */
export function needsTarget(group: Contact[]): boolean {
  return bookIds(group).length > 1
}

/** What the user picked in the compare view. */
export interface MergeSelection {
  survivorId: number
  choices: Choices
  /** 0 until a book is picked. */
  targetBookId: number
}

/** The request for a selection. The target is only sent when the contacts are in several books. */
export function buildRequest(group: Contact[], sel: MergeSelection): MergeRequest {
  const choices: Record<string, number> = {}
  for (const field of mergeFields) {
    choices[choiceKey[field]] = sel.choices[field]
  }
  return {
    survivorId: sel.survivorId,
    mergeIds: group.filter((c) => c.id !== sel.survivorId).map((c) => c.id),
    targetBookId: needsTarget(group) ? sel.targetBookId : 0,
    choices,
  }
}

/** The word for how sure a match is. */
export function matchLevel(score: number): 'high' | 'medium' | 'low' {
  if (score >= 90) {
    return 'high'
  }
  return score >= 70 ? 'medium' : 'low'
}

/** The text for one reason, from the locale strings. */
export function reasonText(reason: DuplicateReason, t: (key: string) => string): string {
  if (reason.kind === 'name' && reason.overlap.length > 0) {
    const overlaps = reason.overlap.map((o) => t(`contacts.duplicates.overlap.${o}`)).join(', ')
    return t('contacts.duplicates.reason.nameWith').replace('{overlaps}', overlaps).replace('{value}', reason.value)
  }
  return t(`contacts.duplicates.reason.${reason.kind}`).replace('{value}', reason.value)
}
