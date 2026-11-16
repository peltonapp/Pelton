import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  chatStatus: vi.fn(),
  chatDevices: vi.fn(),
  chatRefreshEncryption: vi.fn(),
  chatStartVerification: vi.fn(),
  chatSignOutDevice: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

import Chat from './Chat.svelte'
import { prefs } from '../../../stores/prefs'
import { chatStatus, signedOutStatus } from '../../../stores/chat'
import type { ChatStatus } from '../../../lib/types'

function signedIn(over: Partial<ChatStatus['encryption']> = {}): ChatStatus {
  return {
    ...signedOutStatus,
    state: 'online',
    userId: '@arne:example.org',
    deviceId: 'ABCDEF',
    homeserver: 'https://matrix.example.org',
    displayName: 'Arne',
    encryption: {
      ...signedOutStatus.encryption,
      ready: true,
      deviceVerified: true,
      crossSigning: true,
      secretStorage: true,
      backupActive: true,
      backupKeys: 12,
      backupVersion: '3',
      fingerprint: 'AAAA BBBB',
      ...over,
    },
  }
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.chatDevices.mockResolvedValue([])
  api.chatRefreshEncryption.mockResolvedValue(undefined)
  api.chatStartVerification.mockResolvedValue('v1')
  api.setSetting.mockResolvedValue(undefined)
  chatStatus.set(signedOutStatus)
})

describe('Chat settings page', () => {
  it('shows only the switch while chat is off', () => {
    prefs.update((p) => ({ ...p, chatEnabled: false }))
    render(Chat)
    expect(screen.getByRole('switch', { name: 'Turn on chat' })).toBeInTheDocument()
    expect(screen.queryByText('Account')).not.toBeInTheDocument()
    expect(screen.queryByText('Encryption')).not.toBeInTheDocument()
  })

  it('shows the sign-in form when enabled and signed out', async () => {
    prefs.update((p) => ({ ...p, chatEnabled: true }))
    api.chatStatus.mockResolvedValue(signedOutStatus)
    render(Chat)
    expect(await screen.findByLabelText('Server or Matrix ID')).toBeInTheDocument()
  })

  it('shows the account and the encryption rows when signed in', async () => {
    prefs.update((p) => ({ ...p, chatEnabled: true }))
    api.chatStatus.mockResolvedValue(signedIn())
    chatStatus.set(signedIn())
    render(Chat)
    expect(await screen.findByText('@arne:example.org')).toBeInTheDocument()
    expect(screen.getByText('https://matrix.example.org')).toBeInTheDocument()
    expect(screen.getByText('ABCDEF')).toBeInTheDocument()
    expect(screen.getByText('This device is verified')).toBeInTheDocument()
    expect(screen.getByText('Cross-signing')).toBeInTheDocument()
    expect(screen.getByText('12 keys backed up, version 3.')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Unlock with recovery key' })).not.toBeInTheDocument()
  })

  it('warns about an unverified device and offers verify and unlock', async () => {
    const user = userEvent.setup()
    prefs.update((p) => ({ ...p, chatEnabled: true }))
    const status = signedIn({ deviceVerified: false, backupActive: false })
    api.chatStatus.mockResolvedValue(status)
    chatStatus.set(status)
    render(Chat)
    expect(await screen.findByRole('alert')).toHaveTextContent('This device is not verified')
    expect(screen.getByRole('button', { name: 'Unlock with recovery key' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Verify with another device' }))
    expect(api.chatStartVerification).toHaveBeenCalledWith('')
  })

  it('asks for the password when signing out a session needs it, then retries', async () => {
    const user = userEvent.setup()
    prefs.update((p) => ({ ...p, chatEnabled: true }))
    const status = signedIn()
    api.chatStatus.mockResolvedValue(status)
    chatStatus.set(status)
    api.chatDevices.mockResolvedValue([
      { id: 'ABCDEF', name: 'Pelton', lastSeen: Date.now(), current: true, verified: true },
      { id: 'OTHER', name: 'Phone', lastSeen: Date.now(), current: false, verified: false },
    ])
    api.chatSignOutDevice.mockRejectedValueOnce(new Error('chat: the server wants your account password to confirm this'))
    api.chatSignOutDevice.mockResolvedValueOnce(undefined)
    render(Chat)
    await user.click(await screen.findByRole('button', { name: 'Sign out session' }))
    expect(api.chatSignOutDevice).toHaveBeenCalledWith('OTHER', '')
    await user.type(await screen.findByLabelText('Account password'), 'secret')
    const buttons = screen.getAllByRole('button', { name: 'Sign out session' })
    await user.click(buttons[buttons.length - 1])
    expect(api.chatSignOutDevice).toHaveBeenLastCalledWith('OTHER', 'secret')
  })
})
