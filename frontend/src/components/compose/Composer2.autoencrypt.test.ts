import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  setSetting: vi.fn(),
  composeProtectionStatus: vi.fn(),
  autoEncryptStatus: vi.fn(),
  lookupRecipientKeys: vi.fn(),
  acceptContactKey: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/outbox', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/outbox')>()),
  loadOutbox: vi.fn(),
}))

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 0 }),
}))

import Composer2 from './Composer2.svelte'
import { composeSessions, getSession, openCompose, updateCompose, type ComposeSession } from '../../stores/compose'
import { prefs } from '../../stores/prefs'
import { expectAccessible } from '../../test/a11y'
import type { AutoEncryptStatus, RecipientEncryption } from '../../lib/types'

let unsubscribe: (() => void) | null = null

function mount(id: number): void {
  updateCompose(id, { signaturesApplied: true })
  const session = getSession(id) as ComposeSession
  const { rerender } = render(Composer2, { props: { session, layout: 'window' } })
  unsubscribe = composeSessions.subscribe((list) => {
    const next = list.find((s) => s.id === id)
    if (next) {
      void rerender({ session: next, layout: 'window' })
    }
  })
}

function person(email: string, over: Partial<RecipientEncryption> = {}): RecipientEncryption {
  return { email, state: 'ready', source: 'autocrypt', fingerprint: 'AB', recommendation: 'encrypt', preference: '', previous: '', changedAt: '', problem: '', bcc: false, ...over }
}

function status(over: Partial<AutoEncryptStatus> = {}): AutoEncryptStatus {
  return { enabled: true, ownKey: true, recommendation: 'encrypt', replyToEncrypted: false, mode: 'auto', recipients: [person('ann@example.com')], ...over }
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.sendMessage.mockResolvedValue(7)
  api.setSetting.mockResolvedValue(undefined)
  api.composeProtectionStatus.mockResolvedValue({ canSign: false, canEncrypt: false, suggested: 'none', recipients: [] })
  api.lookupRecipientKeys.mockResolvedValue(false)
  prefs.update((p) => ({ ...p, reduceMotion: true, composeVimMode: true, sendDelaySeconds: 0, warnMissingAttachment: false, warnEmptySubject: false, autoEncrypt: true }))
})

afterEach(() => {
  vi.useRealTimers()
  document.documentElement.removeAttribute('data-reduce-motion')
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
  prefs.update((p) => ({ ...p, autoEncrypt: false }))
})

function draft(over: Partial<ComposeSession> = {}): number {
  const id = openCompose(1, 'plaintext')
  updateCompose(id, { to: 'ann@example.com', subject: 'Plan', body: 'hello', ...over })
  return id
}

describe('automatic encryption in the composer', () => {
  it('encrypts and signs when every recipient can read it, and says so', async () => {
    api.autoEncryptStatus.mockResolvedValue(status())
    const id = draft()
    mount(id)
    expect(await screen.findByRole('button', { name: 'Encryption: Encrypted' })).toBeInTheDocument()
    await waitFor(() => expect(getSession(id)?.protection).toBe('signencrypt'))
    expect(api.composeProtectionStatus).not.toHaveBeenCalled()
    expectAccessible()

    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    expect(api.sendMessage.mock.calls[0][0]).toMatchObject({ protection: 'signencrypt', autoEncrypt: true })
  })

  it('leaves the message unencrypted and names who has no key', async () => {
    api.autoEncryptStatus.mockResolvedValue(status({ recommendation: 'disable', recipients: [person('ann@example.com', { state: 'missing', source: '' })] }))
    const id = draft()
    mount(id)
    await userEvent.click(await screen.findByRole('button', { name: 'Encryption: Not encrypted' }))
    expect(screen.getByText('Not encrypted, because there is no key for ann@example.com.')).toBeInTheDocument()
    expect(getSession(id)?.protection).toBe('none')
  })

  it('looks a missing key up once typing pauses, not per keystroke', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    api.autoEncryptStatus.mockResolvedValue(status({ recommendation: 'disable', recipients: [person('ann@example.com', { state: 'missing', source: '' })] }))
    const id = draft()
    mount(id)
    await screen.findByRole('button', { name: 'Encryption: Not encrypted' })
    expect(api.lookupRecipientKeys).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1500)
    await waitFor(() => expect(api.lookupRecipientKeys).toHaveBeenCalledOnce())
    expect(api.lookupRecipientKeys).toHaveBeenCalledWith(1, ['ann@example.com'])
    // the same address is not asked about again.
    updateCompose(id, { subject: 'Plan B' })
    await vi.advanceTimersByTimeAsync(1500)
    expect(api.lookupRecipientKeys).toHaveBeenCalledOnce()
  })

  it('does not send a reply to encrypted mail unencrypted without a choice', async () => {
    api.autoEncryptStatus.mockResolvedValue(
      status({ recommendation: 'disable', replyToEncrypted: true, recipients: [person('ann@example.com', { state: 'missing', source: '' })] }),
    )
    const id = draft({ inReplyTo: 'parent@example.com' })
    mount(id)
    expect(await screen.findByText('This answers an encrypted message, but your reply would go out unencrypted.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(api.sendMessage).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Send unencrypted' }))
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    expect(api.sendMessage.mock.calls[0][0]).toMatchObject({ protection: 'none', plaintextConfirmed: true })
  })

  it('stops on a changed key until the user decides', async () => {
    const changed = person('ann@example.com', { state: 'changed', recommendation: 'disable', previous: 'OLD', fingerprint: 'NEW' })
    api.autoEncryptStatus.mockResolvedValue(status({ recommendation: 'disable', recipients: [changed] }))
    api.acceptContactKey.mockResolvedValue(undefined)
    const id = draft({ encrypt: 'on' })
    mount(id)
    await userEvent.click(await screen.findByRole('button', { name: 'Use the new key' }))
    expect(api.acceptContactKey).toHaveBeenCalledWith('ann@example.com', 'NEW')
  })

  it('keeps the old controls with automatic encryption off', async () => {
    prefs.update((p) => ({ ...p, autoEncrypt: false }))
    const id = draft()
    mount(id)
    expect(await screen.findByRole('button', { name: 'Protection' })).toBeInTheDocument()
    expect(api.autoEncryptStatus).not.toHaveBeenCalled()
    expect(api.lookupRecipientKeys).not.toHaveBeenCalled()
  })
})
