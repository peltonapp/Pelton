// peltonlinks.ts builds and reads the pelton:// addresses a note uses to link
// to things in Pelton: a mail or its conversation by Message-ID, an event by
// its UID, a contact by address and a task by its UID. They sit in ordinary
// Markdown links ([Subject](pelton://mail/...)), so a note stays valid
// Markdown in any other editor, and the ids survive a resync because none of
// them is a local row id.

/** The kinds of Pelton object a note can link to. */
export type PeltonKind = 'mail' | 'conversation' | 'event' | 'contact' | 'task'

const kinds: readonly PeltonKind[] = ['mail', 'conversation', 'event', 'contact', 'task']

/** The pelton:// address of an object. */
export function peltonUrl(kind: PeltonKind, id: string): string {
  return `pelton://${kind}/${encodeURIComponent(id.trim())}`
}

/** Reads a pelton:// address, or returns null when it is not one. */
export function parsePeltonUrl(url: string): { kind: PeltonKind; id: string } | null {
  const m = /^pelton:\/\/([a-z]+)\/(.+)$/i.exec(url.trim())
  if (!m) {
    return null
  }
  const kind = m[1].toLowerCase() as PeltonKind
  if (!kinds.includes(kind)) {
    return null
  }
  try {
    return { kind, id: decodeURIComponent(m[2]) }
  } catch {
    return null
  }
}

/** A Markdown link to an object, with the label's brackets escaped. */
export function peltonMarkdownLink(kind: PeltonKind, id: string, label: string): string {
  const text = (label.trim() || id).replace(/([[\]\\])/g, '\\$1').replace(/\n+/g, ' ')
  return `[${text}](${peltonUrl(kind, id)})`
}

/** The Message-ID a mid: URL (RFC 2392) names. */
export function messageIdFromMid(mid: string): string {
  const raw = mid.replace(/^mid:/i, '').split('/')[0]
  try {
    return decodeURIComponent(raw).replace(/^<|>$/g, '')
  } catch {
    return raw
  }
}

/** The mid: URL of a Message-ID, which the backend looks mail up by. */
export function midFromMessageId(id: string): string {
  return `mid:${encodeURIComponent(id.replace(/^<|>$/g, ''))}`
}
