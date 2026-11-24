import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  getAutoEncryptSettings: vi.fn(),
  enableAutoEncrypt: vi.fn(),
  setKeySources: vi.fn(),
  listContactKeys: vi.fn(),
  listKeyLookupLog: vi.fn(),
  setSetting: vi.fn(),
}))
vi.mock('../../lib/api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../lib/api')>()), ...api }))

// dialogs open with transitions and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

import AutoEncryptSetup from './AutoEncryptSetup.svelte'
import AutoEncryptSection from './AutoEncryptSection.svelte'
import { prefs } from '../../stores/prefs'
import { expectAccessible } from '../../test/a11y'
import type { AutoEncryptSettings, KeySources } from '../../lib/types'

const off: KeySources = { autocrypt: false, wkd: false, keysOpenpgpOrg: false, keyServers: false, servers: [] }

function settings(over: Partial<AutoEncryptSettings> = {}): AutoEncryptSettings {
  return {
    enabled: true,
    sources: { ...off, autocrypt: true },
    protectSubject: true,
    accounts: [
      { accountId: 1, email: 'me@example.org', fingerprint: 'ABCDEF0123456789', created: '2026-11-19T00:00:00Z', expires: '2028-11-18T00:00:00Z', generated: true, usable: true, mode: 'auto' },
    ],
    ...over,
  }
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.listContactKeys.mockResolvedValue([])
  api.listKeyLookupLog.mockResolvedValue([])
  api.enableAutoEncrypt.mockResolvedValue(undefined)
  api.setKeySources.mockResolvedValue(undefined)
  api.setSetting.mockResolvedValue(undefined)
})

describe('the setup walk through', () => {
  it('explains each source and allows nothing on the network unless asked', async () => {
    const done = vi.fn()
    render(AutoEncryptSetup, { events: { done: (e: CustomEvent) => done(e.detail) } })
    expectAccessible()
    await userEvent.click(screen.getByRole('button', { name: 'Start' }))

    expect(screen.getByRole('heading', { name: 'Keys in mail' })).toBeInTheDocument()
    expect(screen.getByText('Nothing is contacted')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Allow' }))

    expect(screen.getByRole('heading', { name: "The recipient's own mail domain" })).toBeInTheDocument()
    expect(screen.getByText(/That domain learns that someone looked up that address/)).toBeInTheDocument()
    expect(screen.getByText('Contacts the internet')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Skip' }))

    expect(screen.getByRole('heading', { name: 'keys.openpgp.org' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Allow' }))

    expect(screen.getByRole('heading', { name: 'Key servers you add' })).toBeInTheDocument()
    expectAccessible()
    await userEvent.click(screen.getByRole('button', { name: 'Skip' }))

    expect(screen.getByText('Pelton will use: Keys in mail, keys.openpgp.org.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Switch on' }))
    expect(done).toHaveBeenCalledWith({ autocrypt: true, wkd: false, keysOpenpgpOrg: true, keyServers: false, servers: [] })
  })

  it('can go back and change an answer', async () => {
    const done = vi.fn()
    render(AutoEncryptSetup, { events: { done: (e: CustomEvent) => done(e.detail) } })
    await userEvent.click(screen.getByRole('button', { name: 'Start' }))
    await userEvent.click(screen.getByRole('button', { name: 'Skip' }))
    await userEvent.click(screen.getByRole('button', { name: 'Back' }))
    await userEvent.click(screen.getByRole('button', { name: 'Allow' }))
    for (let i = 0; i < 3; i++) {
      await userEvent.click(screen.getByRole('button', { name: 'Skip' }))
    }
    await userEvent.click(screen.getByRole('button', { name: 'Switch on' }))
    expect(done).toHaveBeenCalledWith({ ...off, autocrypt: true })
  })
})

describe('the settings section', () => {
  it('shows only the switch while automatic encryption is off, and starts the setup from it', async () => {
    prefs.update((p) => ({ ...p, autoEncrypt: false }))
    api.getAutoEncryptSettings.mockResolvedValue(settings({ enabled: false }))
    render(AutoEncryptSection)
    expect(screen.queryByText('Where keys come from')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('switch', { name: 'Encrypt automatically' }))
    expect(screen.getByRole('dialog', { name: 'Set up automatic encryption' })).toBeInTheDocument()
    expect(api.enableAutoEncrypt).not.toHaveBeenCalled()
  })

  it('lists sources, own keys, contacts and the log once it is on', async () => {
    prefs.update((p) => ({ ...p, autoEncrypt: true }))
    api.getAutoEncryptSettings.mockResolvedValue(settings())
    api.listContactKeys.mockResolvedValue([
      { email: 'anna@example.org', fingerprint: 'AAAA', source: 'wkd', firstSeen: '2026-11-01T00:00:00Z', expires: '', state: 'ready', problem: '', preference: '', previous: '' },
    ])
    api.listKeyLookupLog.mockResolvedValue([{ at: '2026-11-19T10:00:00Z', source: 'wkd', email: 'anna@example.org', outcome: 'found' }])
    render(AutoEncryptSection)
    expect(await screen.findByText('ABCD EF01 2345 6789')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'keys.openpgp.org' })).not.toBeChecked()
    expect(screen.getByRole('switch', { name: 'Keys in mail' })).toBeChecked()
    expect(await screen.findByText(/published by their mail domain/)).toBeInTheDocument()
    expect(screen.getByText('the domain example.org')).toBeInTheDocument()
    expectAccessible()

    await userEvent.click(screen.getByRole('switch', { name: 'keys.openpgp.org' }))
    expect(api.setKeySources).toHaveBeenCalledWith({ ...off, autocrypt: true, keysOpenpgpOrg: true })
  })
})
