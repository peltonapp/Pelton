// recipientmoves.ts is the pure side of the composer's quick recipient moves:
// a recipient from one of To, Cc and Bcc to another, the introducer of a
// thread to Bcc, the original sender into Cc, and the thank-you line that goes
// with moving an introducer. Fields are the comma separated strings the
// composer edits; recipients are matched by address, whatever name they carry.

import type { EditorMode } from './types'
import { formatAddress, parseAddressList, splitAddressList } from './mailcompose'
import { escapeHtml } from './richtext'

/** One recipient field. */
export type RecipientField = 'to' | 'cc' | 'bcc'

/** The three recipient fields of a message. */
export interface RecipientFields {
  to: string
  cc: string
  bcc: string
}

/** The fields in the order they are moved through. */
export const recipientFields: readonly RecipientField[] = ['to', 'cc', 'bcc']

/** addressOf is the lowercased address of one recipient as typed. */
export function addressOf(recipient: string): string {
  return (parseAddressList(recipient)[0]?.email ?? recipient).trim().toLowerCase()
}

/**
 * fieldBeside is the field a recipient moves to: down goes To, Cc, Bcc and up
 * the other way. null at either end.
 */
export function fieldBeside(field: RecipientField, dir: 'down' | 'up'): RecipientField | null {
  const at = recipientFields.indexOf(field) + (dir === 'down' ? 1 : -1)
  return recipientFields[at] ?? null
}

// without drops every recipient with the address from a field.
function without(field: string, address: string): string {
  return splitAddressList(field)
    .filter((r) => addressOf(r) !== address)
    .join(', ')
}

// withAdded appends a recipient to a field unless its address is there.
function withAdded(field: string, recipient: string): string {
  const list = splitAddressList(field)
  if (list.some((r) => addressOf(r) === addressOf(recipient))) {
    return list.join(', ')
  }
  return [...list, recipient].join(', ')
}

/** holds reports whether a field has the address. */
export function holds(field: string, address: string): boolean {
  const key = address.trim().toLowerCase()
  return splitAddressList(field).some((r) => addressOf(r) === key)
}

/**
 * moveRecipient takes a recipient out of the field it is in and adds it to
 * another, keeping how it was written. A recipient already in the target
 * field is only taken out of the other one.
 */
export function moveRecipient(fields: RecipientFields, recipient: string, from: RecipientField, to: RecipientField): RecipientFields {
  if (from === to) {
    return fields
  }
  const address = addressOf(recipient)
  return { ...fields, [from]: without(fields[from], address), [to]: withAdded(fields[to], recipient) }
}

/**
 * introducerToBcc moves the person who made an introduction from To and Cc to
 * Bcc. When that leaves To empty, the Cc recipients take its place, since
 * they are who the reply is now for. null when the introducer is not a
 * recipient at all.
 */
export function introducerToBcc(fields: RecipientFields, introducer: { name: string; email: string }): RecipientFields | null {
  const address = introducer.email.trim().toLowerCase()
  if (address === '' || (!holds(fields.to, address) && !holds(fields.cc, address))) {
    return null
  }
  const written =
    [...splitAddressList(fields.to), ...splitAddressList(fields.cc)].find((r) => addressOf(r) === address) ?? formatAddress(introducer)
  let to = without(fields.to, address)
  let cc = without(fields.cc, address)
  if (to === '' && cc !== '') {
    to = cc
    cc = ''
  }
  return { to, cc, bcc: withAdded(fields.bcc, written) }
}

/**
 * senderToCc adds the original sender to Cc, or returns null when they
 * already get the message in any field.
 */
export function senderToCc(fields: RecipientFields, sender: { name: string; email: string }): RecipientFields | null {
  const address = sender.email.trim().toLowerCase()
  if (address === '' || recipientFields.some((f) => holds(fields[f], address))) {
    return null
  }
  return { ...fields, cc: withAdded(fields.cc, formatAddress(sender)) }
}

/**
 * withLeadLine puts a line at the top of a body, above whatever the user
 * wrote: a paragraph of its own in the rich editor, a line of its own in
 * plain text. The empty lines a reply opens with stay under it, to write in.
 */
export function withLeadLine(body: string, line: string, mode: EditorMode): string {
  if (mode === 'wysiwyg') {
    return `<p>${escapeHtml(line)}</p>${body}`
  }
  if (body === '') {
    return line
  }
  return body.startsWith('\n') ? `${line}${body}` : `${line}\n\n${body}`
}
