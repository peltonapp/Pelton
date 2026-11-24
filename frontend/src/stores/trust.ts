// trust.ts holds the sender passport and the privacy shield of the messages
// open in the new design's reader, and the checks a strong warning adds before
// a reply or a link. Both are read when a message is opened, never for a list.

import { get, writable } from 'svelte/store'
import { privacyShield, senderPassport } from '../lib/api'
import type { EditorMode, MessageDetail, Passport, Shield, ShieldItem } from '../lib/types'

/** Passports by message id, for the messages opened so far. */
export const passports = writable<Map<number, Passport>>(new Map())
/** Shields by message id, for the messages opened so far. */
export const shields = writable<Map<number, Shield>>(new Map())

// a long reading session should not keep every passport it ever opened.
const keep = 60
const loading = new Set<number>()

function remember<T>(map: Map<number, T>, id: number, value: T): Map<number, T> {
  const next = new Map(map)
  next.delete(id)
  next.set(id, value)
  while (next.size > keep) {
    next.delete(next.keys().next().value as number)
  }
  return next
}

/** Reads the passport and shield of one message; force reads them again. */
export async function loadTrust(id: number, force = false): Promise<void> {
  if (loading.has(id) || (!force && get(passports).has(id))) {
    return
  }
  loading.add(id)
  try {
    const [p, s] = await Promise.allSettled([senderPassport(id), privacyShield(id)])
    if (p.status === 'fulfilled') {
      passports.update((m) => remember(m, id, p.value))
    }
    if (s.status === 'fulfilled') {
      shields.update((m) => remember(m, id, s.value))
    }
  } finally {
    loading.delete(id)
  }
}

const acknowledged = new Set<number>()

/** The passport of a message whose strong warning has not been acknowledged yet, else null. */
export function guarded(id: number): Passport | null {
  const p = get(passports).get(id)
  return p && p.warnings.length > 0 && !acknowledged.has(id) ? p : null
}

/** Lets replies and links of a message through without asking again. */
export function acknowledge(id: number): void {
  acknowledged.add(id)
}

/** A reply waiting for the user to confirm where it goes. */
export interface ReplyCheck {
  detail: MessageDetail
  mode: EditorMode
  kind: 'reply' | 'replyAll'
  typed: string
  to: string
}

/** The reply being checked, null when none is. */
export const replyCheck = writable<ReplyCheck | null>(null)

/** A link waiting for the user to confirm where it goes. */
export interface LinkCheck {
  messageId: number
  url: string
  item: ShieldItem | null
}

/** The link being checked, null when none is. */
export const linkCheck = writable<LinkCheck | null>(null)

/** The tracked link item for an href of a message, when the shield found one. */
export function trackedLink(id: number, href: string): ShieldItem | null {
  return get(shields).get(id)?.items.find((i) => i.kind === 'link' && i.url === href) ?? null
}

/** A message whose images the shield asked the reader to load, with trackers kept out. */
export const imageRequest = writable<number | null>(null)

/** What the reader loaded per message: 'images' without trackers, 'all' with them. */
export const loadedRemote = writable<Map<number, 'images' | 'all'>>(new Map())

/** The sender the tracking report opens filtered to, '' for everyone. */
export const reportFilter = writable('')

/** Records what the reader loaded for a message. */
export function noteLoaded(id: number, what: 'images' | 'all'): void {
  loadedRemote.update((m) => remember(m, id, what))
}
