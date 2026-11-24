// autoencrypt.ts decides, from what the backend knows about the recipients,
// whether a message is encrypted and how the composer says so. The backend
// has the last word: a message set to be encrypted that cannot be is refused
// there, never sent in the clear.

import { autoEncryptStatus } from './api'
import type { AutoEncryptStatus, RecipientEncryption } from './types'

/** The user's choice for one message; unset follows the recommendation. */
export type EncryptOverride = 'on' | 'off' | undefined

/** willEncrypt says whether the message goes out encrypted. */
export function willEncrypt(status: AutoEncryptStatus | null, override: EncryptOverride): boolean {
  if (!status?.enabled || override === 'off') {
    return false
  }
  return override === 'on' || status.recommendation === 'encrypt'
}

function readable(r: RecipientEncryption): boolean {
  return r.state === 'ready' || r.state === 'verified'
}

/** canEncrypt says whether every recipient can be encrypted to right now. */
export function canEncrypt(status: AutoEncryptStatus | null): boolean {
  return Boolean(status?.enabled && status.ownKey && status.recipients.length > 0 && status.recipients.every(readable))
}

/** What the lock says, in one of a few plain cases. */
export type LockSummary =
  | { kind: 'nobody' }
  | { kind: 'noOwnKey' }
  | { kind: 'encrypted'; verified: boolean }
  | { kind: 'blocked'; missing: string[]; changed: string[] }
  | { kind: 'plainMissing'; missing: string[]; withKey: number }
  | { kind: 'plainChoice' }

/** lockSummary describes the state of a message for the lock and its menu. */
export function lockSummary(status: AutoEncryptStatus | null, encrypt: boolean): LockSummary {
  const recipients = status?.recipients ?? []
  if (recipients.length === 0) {
    return { kind: 'nobody' }
  }
  if (!status?.ownKey) {
    return { kind: 'noOwnKey' }
  }
  const missing = recipients.filter((r) => r.state === 'missing').map((r) => r.email)
  const changed = recipients.filter((r) => r.state === 'changed').map((r) => r.email)
  if (encrypt) {
    if (missing.length > 0 || changed.length > 0) {
      return { kind: 'blocked', missing, changed }
    }
    return { kind: 'encrypted', verified: recipients.every((r) => r.state === 'verified') }
  }
  if (missing.length > 0) {
    return { kind: 'plainMissing', missing, withKey: recipients.length - missing.length }
  }
  return { kind: 'plainChoice' }
}

/** needsPlaintextConfirm is true for a reply to encrypted mail about to go out unencrypted. */
export function needsPlaintextConfirm(status: AutoEncryptStatus | null, encrypt: boolean, confirmed: boolean): boolean {
  return Boolean(status?.enabled && status.replyToEncrypted && !encrypt && !confirmed)
}

/** changedRecipients are the recipients whose key changed and was not accepted. */
export function changedRecipients(status: AutoEncryptStatus | null): RecipientEncryption[] {
  return (status?.recipients ?? []).filter((r) => r.state === 'changed')
}

/** sourceWords returns the locale key and server name for where a key came from. */
export function sourceWords(source: string): { key: string; server: string } {
  if (source.startsWith('server:')) {
    return { key: 'autoEncrypt.source.server', server: source.slice('server:'.length).replace(/^https:\/\//, '') }
  }
  const known: Record<string, string> = {
    manual: 'autoEncrypt.source.manual',
    autocrypt: 'autoEncrypt.source.autocrypt',
    gossip: 'autoEncrypt.source.gossip',
    wkd: 'autoEncrypt.source.wkd',
    'keys.openpgp.org': 'autoEncrypt.source.koo',
  }
  return { key: known[source] ?? 'autoEncrypt.source.none', server: '' }
}

/** groupFingerprint splits a fingerprint into groups of four for reading aloud. */
export function groupFingerprint(fp: string): string {
  return (fp.toUpperCase().match(/.{1,4}/g) ?? []).join(' ')
}

/** looksComplete is true for an address worth asking a key server about. */
export function looksComplete(email: string): boolean {
  return /^[^@\s]+@[^@\s]+\.[a-z]{2,}$/i.test(email)
}

/** The composer's question. */
export interface EncryptQuery {
  accountId: number
  to: string[]
  cc: string[]
  bcc: string[]
  inReplyTo: string
}

/**
 * AutoEncryptWatch keeps the encryption status of one composer. It asks the
 * backend again only when the question changed, unless told to.
 */
export class AutoEncryptWatch {
  /** The last answer, null until the first arrives. */
  status: AutoEncryptStatus | null = null
  private statusFor = ''
  private asked = new Set<string>()

  /** refresh asks for the status and reports whether it changed. A failure keeps the last answer. */
  async refresh(query: EncryptQuery, force = false): Promise<boolean> {
    const key = JSON.stringify(query)
    if (!force && key === this.statusFor && this.status !== null) {
      return false
    }
    this.statusFor = key
    try {
      const status = await autoEncryptStatus(query)
      if (key !== this.statusFor) {
        return false
      }
      this.status = status
      return true
    } catch {
      return false
    }
  }

  /** lookupCandidates lists recipients without a key that were not asked about yet, and marks them asked. */
  lookupCandidates(): string[] {
    const out = (this.status?.recipients ?? [])
      .filter((r) => r.state === 'missing' && looksComplete(r.email) && !this.asked.has(r.email))
      .map((r) => r.email)
    for (const email of out) {
      this.asked.add(email)
    }
    return out
  }
}
