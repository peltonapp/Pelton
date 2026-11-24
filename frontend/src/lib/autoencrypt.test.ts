import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({ autoEncryptStatus: vi.fn() }))
vi.mock('./api', async (importOriginal) => ({ ...(await importOriginal<typeof import('./api')>()), ...api }))

import {
  AutoEncryptWatch,
  canEncrypt,
  changedRecipients,
  groupFingerprint,
  lockSummary,
  looksComplete,
  needsPlaintextConfirm,
  sourceWords,
  willEncrypt,
} from './autoencrypt'
import type { AutoEncryptStatus, RecipientEncryption } from './types'

function person(email: string, over: Partial<RecipientEncryption> = {}): RecipientEncryption {
  return {
    email,
    state: 'ready',
    source: 'autocrypt',
    fingerprint: 'ABCD',
    recommendation: 'encrypt',
    preference: '',
    previous: '',
    changedAt: '',
    problem: '',
    bcc: false,
    ...over,
  }
}

function status(over: Partial<AutoEncryptStatus> = {}): AutoEncryptStatus {
  return { enabled: true, ownKey: true, recommendation: 'encrypt', replyToEncrypted: false, mode: 'auto', recipients: [person('anna@example.org')], ...over }
}

beforeEach(() => api.autoEncryptStatus.mockReset())

describe('deciding to encrypt', () => {
  it('follows the recommendation unless the user chose', () => {
    expect(willEncrypt(status(), undefined)).toBe(true)
    expect(willEncrypt(status({ recommendation: 'available' }), undefined)).toBe(false)
    expect(willEncrypt(status({ recommendation: 'available' }), 'on')).toBe(true)
    expect(willEncrypt(status(), 'off')).toBe(false)
  })

  it('never encrypts with the feature off or no answer yet', () => {
    expect(willEncrypt(null, 'on')).toBe(false)
    expect(willEncrypt(status({ enabled: false }), 'on')).toBe(false)
  })

  it('can encrypt only when every recipient has an accepted key and the mailbox has its own', () => {
    expect(canEncrypt(status())).toBe(true)
    expect(canEncrypt(status({ ownKey: false }))).toBe(false)
    expect(canEncrypt(status({ recipients: [] }))).toBe(false)
    expect(canEncrypt(status({ recipients: [person('a@x.org'), person('b@x.org', { state: 'missing' })] }))).toBe(false)
    expect(canEncrypt(status({ recipients: [person('a@x.org', { state: 'changed' })] }))).toBe(false)
  })
})

describe('what the lock says', () => {
  it('names everyone without a key when it is not encrypted', () => {
    const s = status({ recommendation: 'disable', recipients: [person('a@x.org'), person('b@x.org', { state: 'missing' })] })
    expect(lockSummary(s, false)).toEqual({ kind: 'plainMissing', missing: ['b@x.org'], withKey: 1 })
  })

  it('says blocked when encryption was asked for and someone cannot be encrypted to', () => {
    const s = status({ recipients: [person('a@x.org', { state: 'changed' }), person('b@x.org', { state: 'missing' })] })
    expect(lockSummary(s, true)).toEqual({ kind: 'blocked', missing: ['b@x.org'], changed: ['a@x.org'] })
  })

  it('tells verified from merely encrypted', () => {
    expect(lockSummary(status(), true)).toEqual({ kind: 'encrypted', verified: false })
    expect(lockSummary(status({ recipients: [person('a@x.org', { state: 'verified' })] }), true)).toEqual({ kind: 'encrypted', verified: true })
  })

  it('covers no recipients and no own key', () => {
    expect(lockSummary(status({ recipients: [] }), false).kind).toBe('nobody')
    expect(lockSummary(status({ ownKey: false }), false).kind).toBe('noOwnKey')
    expect(lockSummary(status(), false).kind).toBe('plainChoice')
  })
})

describe('downgrades and key changes', () => {
  it('asks before a reply to encrypted mail goes out unencrypted', () => {
    const reply = status({ replyToEncrypted: true })
    expect(needsPlaintextConfirm(reply, false, false)).toBe(true)
    expect(needsPlaintextConfirm(reply, false, true)).toBe(false)
    expect(needsPlaintextConfirm(reply, true, false)).toBe(false)
    expect(needsPlaintextConfirm(status(), false, false)).toBe(false)
  })

  it('lists recipients whose key changed', () => {
    const s = status({ recipients: [person('a@x.org'), person('b@x.org', { state: 'changed' })] })
    expect(changedRecipients(s).map((r) => r.email)).toEqual(['b@x.org'])
  })
})

describe('words and helpers', () => {
  it('maps every source to words', () => {
    expect(sourceWords('wkd').key).toBe('autoEncrypt.source.wkd')
    expect(sourceWords('keys.openpgp.org').key).toBe('autoEncrypt.source.koo')
    expect(sourceWords('server:https://keys.example.org')).toEqual({ key: 'autoEncrypt.source.server', server: 'keys.example.org' })
    expect(sourceWords('').key).toBe('autoEncrypt.source.none')
  })

  it('groups a fingerprint by four', () => {
    expect(groupFingerprint('abcd1234ef56')).toBe('ABCD 1234 EF56')
  })

  it('only treats finished looking addresses as worth a lookup', () => {
    expect(looksComplete('anna@example.org')).toBe(true)
    expect(looksComplete('anna@example')).toBe(false)
    expect(looksComplete('anna@exa.o')).toBe(false)
    expect(looksComplete('anna')).toBe(false)
  })
})

describe('AutoEncryptWatch', () => {
  const q = { accountId: 1, to: ['anna@example.org'], cc: [], bcc: [], inReplyTo: '' }

  it('asks again only when the question changed or it is told to', async () => {
    api.autoEncryptStatus.mockResolvedValue(status())
    const watch = new AutoEncryptWatch()
    expect(await watch.refresh(q)).toBe(true)
    expect(await watch.refresh({ ...q })).toBe(false)
    expect(await watch.refresh(q, true)).toBe(true)
    expect(await watch.refresh({ ...q, to: ['bob@example.org'] })).toBe(true)
    expect(api.autoEncryptStatus).toHaveBeenCalledTimes(3)
  })

  it('keeps the last answer when asking fails', async () => {
    api.autoEncryptStatus.mockResolvedValueOnce(status()).mockRejectedValueOnce(new Error('down'))
    const watch = new AutoEncryptWatch()
    await watch.refresh(q)
    expect(await watch.refresh(q, true)).toBe(false)
    expect(watch.status?.recommendation).toBe('encrypt')
  })

  it('offers each missing address for a lookup once', async () => {
    api.autoEncryptStatus.mockResolvedValue(
      status({ recipients: [person('a@example.org', { state: 'missing' }), person('b@example.org'), person('c@half', { state: 'missing' })] }),
    )
    const watch = new AutoEncryptWatch()
    await watch.refresh(q)
    expect(watch.lookupCandidates()).toEqual(['a@example.org'])
    expect(watch.lookupCandidates()).toEqual([])
  })
})
