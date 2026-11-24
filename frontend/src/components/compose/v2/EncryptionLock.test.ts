import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import EncryptionLock from './EncryptionLock.svelte'
import EncryptionNotice from './EncryptionNotice.svelte'
import { expectAccessible } from '../../../test/a11y'
import type { AutoEncryptStatus, RecipientEncryption } from '../../../lib/types'

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

describe('EncryptionLock', () => {
  it('says plainly that the message is encrypted, and why', async () => {
    render(EncryptionLock, { props: { status: status(), encrypt: true } })
    const button = screen.getByRole('button', { name: 'Encryption: Encrypted' })
    await userEvent.click(button)
    expect(screen.getByText('Encrypted for everyone on it. Only they can read it.')).toBeInTheDocument()
    expect(screen.getByText('Has a key, from their own mail')).toBeInTheDocument()
    expectAccessible()
  })

  it('names the recipients holding encryption back', async () => {
    const s = status({
      recommendation: 'disable',
      recipients: [person('anna@example.org'), person('bob@example.org', { state: 'missing', problem: 'expired', source: '' })],
    })
    render(EncryptionLock, { props: { status: s, encrypt: false } })
    await userEvent.click(screen.getByRole('button', { name: 'Encryption: Not encrypted' }))
    expect(screen.getByText(/there is no key for bob@example.org/)).toBeInTheDocument()
    expect(screen.getByText('1 of 2 recipients could read it encrypted.', { exact: false })).toBeInTheDocument()
    expect(screen.getByText('No usable key: their key expired')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Encrypt this message' })).toBeDisabled()
    expectAccessible()
  })

  it('turns encryption off and back to automatic in one click each', async () => {
    const onOverride = vi.fn()
    const { rerender } = render(EncryptionLock, { props: { status: status(), encrypt: true }, events: { override: (e: CustomEvent) => onOverride(e.detail) } })
    await userEvent.click(screen.getByRole('button', { name: /Encryption:/ }))
    await userEvent.click(screen.getByRole('switch', { name: 'Encrypt this message' }))
    expect(onOverride).toHaveBeenLastCalledWith('off')
    await rerender({ status: status(), encrypt: false, override: 'off' })
    await userEvent.click(screen.getByRole('button', { name: 'Decide automatically again' }))
    expect(onOverride).toHaveBeenLastCalledWith('auto')
  })

  it('sets a contact preference', async () => {
    const onPref = vi.fn()
    render(EncryptionLock, { props: { status: status(), encrypt: true }, events: { preference: (e: CustomEvent) => onPref(e.detail) } })
    await userEvent.click(screen.getByRole('button', { name: /Encryption:/ }))
    const group = screen.getByRole('radiogroup', { name: 'Encryption for anna@example.org' })
    await userEvent.click(group.querySelectorAll('[role="radio"]')[2] as HTMLElement)
    expect(onPref).toHaveBeenCalledWith({ email: 'anna@example.org', pref: 'never' })
  })

  it('explains that Bcc recipients stay hidden', async () => {
    render(EncryptionLock, { props: { status: status({ recipients: [person('a@example.org'), person('b@example.org', { bcc: true })] }), encrypt: true } })
    await userEvent.click(screen.getByRole('button', { name: /Encryption:/ }))
    expect(screen.getByText(/without their key being named/)).toBeInTheDocument()
    expect(screen.getByText('Bcc')).toBeInTheDocument()
  })
})

describe('EncryptionNotice', () => {
  it('shows a changed key with both choices', async () => {
    const changed = person('anna@example.org', { state: 'changed', changedAt: '2026-11-12T10:00:00Z', previous: 'OLD' })
    const onAccept = vi.fn()
    const onPlain = vi.fn()
    render(EncryptionNotice, {
      props: { status: status({ recommendation: 'disable', recipients: [changed] }), encrypt: true },
      events: { accept: (e: CustomEvent) => onAccept(e.detail), plain: onPlain },
    })
    expect(screen.getByRole('status', { name: 'Encryption notices' })).toBeInTheDocument()
    expect(screen.getByText(/The key of anna@example.org changed on/)).toBeInTheDocument()
    expectAccessible()
    await userEvent.click(screen.getByRole('button', { name: 'Use the new key' }))
    expect(onAccept).toHaveBeenCalledWith(changed)
    await userEvent.click(screen.getByRole('button', { name: 'Send unencrypted this time' }))
    expect(onPlain).toHaveBeenCalled()
  })

  it('warns before a reply to encrypted mail goes out unencrypted', async () => {
    const onConfirm = vi.fn()
    const onEncrypt = vi.fn()
    const { rerender } = render(EncryptionNotice, {
      props: { status: status({ replyToEncrypted: true, recommendation: 'available' }), encrypt: false },
      events: { confirmPlain: onConfirm, encrypt: onEncrypt },
    })
    expect(screen.getByText('This answers an encrypted message, but your reply would go out unencrypted.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Encrypt it' }))
    expect(onEncrypt).toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Send unencrypted' }))
    expect(onConfirm).toHaveBeenCalled()
    await rerender({ status: status({ replyToEncrypted: true }), encrypt: false, plaintextConfirmed: true })
    expect(screen.queryByText(/This answers an encrypted message/)).not.toBeInTheDocument()
    // the region stays mounted for the next notice.
    expect(screen.getByRole('status', { name: 'Encryption notices' })).toBeInTheDocument()
  })
})
