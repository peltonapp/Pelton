import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const backend = vi.hoisted(() => ({
  calls: [] as [number, string][],
  answer: (): Promise<void> => Promise.resolve(),
}))
vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  importKeyFromSetupMessage: (id: number, code: string) => {
    backend.calls.push([id, code])
    return backend.answer()
  },
}))

import EncryptionIndicator from './EncryptionIndicator.svelte'
import { expectAccessible } from '../../test/a11y'
import type { MessageEncryption } from '../../lib/types'

function enc(over: Partial<MessageEncryption> = {}): MessageEncryption {
  return { encrypted: false, signature: '', signerEmail: '', subject: '', setupMessage: false, ...over }
}

beforeEach(() => {
  backend.calls = []
  backend.answer = () => Promise.resolve()
})

describe('EncryptionIndicator', () => {
  it.each([
    ['known', 'Signed by anna@example.org with a key seen before'],
    ['verified', 'Signed by anna@example.org with a key you checked'],
    ['new', 'Signed by anna@example.org with a key seen for the first time'],
    ['invalid', 'The signature does not match: the message was changed after it was signed'],
    ['unchecked', 'Signed, but the signature could not be checked'],
  ] as const)('describes a %s signature in words', (signature, words) => {
    render(EncryptionIndicator, { props: { encryption: enc({ signature, signerEmail: 'anna@example.org' }), messageId: 1 } })
    expect(screen.getByText(words)).toBeInTheDocument()
  })

  it('says a message was encrypted and shows its hidden subject', () => {
    render(EncryptionIndicator, { props: { encryption: enc({ encrypted: true, subject: 'The real subject' }), messageId: 1 } })
    expect(screen.getByRole('region', { name: 'Encryption and signature' })).toBeInTheDocument()
    expect(screen.getByText('Encrypted on its way to you')).toBeInTheDocument()
    expect(screen.getByText('The real subject')).toBeInTheDocument()
    expectAccessible()
  })

  it('imports a key from another device with its setup code', async () => {
    render(EncryptionIndicator, { props: { encryption: enc({ setupMessage: true }), messageId: 7 } })
    expectAccessible()
    const button = screen.getByRole('button', { name: 'Use this key' })
    expect(button).toBeDisabled()
    await userEvent.type(screen.getByRole('textbox', { name: 'Setup code' }), '1234-5678')
    await userEvent.click(button)
    expect(backend.calls).toEqual([[7, '1234-5678']])
    expect(await screen.findByText('The key from your other device is now used here.')).toBeInTheDocument()
  })

  it('reports a wrong code', async () => {
    backend.answer = () => Promise.reject(new Error('that setup code does not open this message'))
    render(EncryptionIndicator, { props: { encryption: enc({ setupMessage: true }), messageId: 7 } })
    await userEvent.type(screen.getByRole('textbox', { name: 'Setup code' }), '0000')
    await userEvent.click(screen.getByRole('button', { name: 'Use this key' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('does not open this message'))
  })
})
