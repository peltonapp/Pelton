import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import userEvent from '@testing-library/user-event'
import type { Discovered } from '../../lib/types'

const api = vi.hoisted(() => ({
  discoverConfig: vi.fn(),
  testConnection: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../lib/browser', () => ({
  openURL: vi.fn(),
}))

import AddMailboxWizard from './AddMailboxWizard.svelte'
import { prefs } from '../../stores/prefs'

function discovered(host: string): Discovered {
  return {
    imapHost: `imap.${host}`,
    imapPort: 993,
    smtpHost: `smtp.${host}`,
    smtpPort: 465,
    imapTls: 'ssl',
    smtpTls: 'ssl',
    oauth: false,
    oauthProvider: '',
    source: 'guess',
  }
}

// a discovery the test resolves by hand, to land it after the user has typed.
function deferDiscovery(): (d: Discovered) => Promise<void> {
  let resolve!: (d: Discovered) => void
  api.discoverConfig.mockReturnValueOnce(new Promise<Discovered>((r) => (resolve = r)))
  return async (d) => {
    resolve(d)
    await tick()
    await tick()
  }
}

async function typeInto(input: HTMLElement, text: string): Promise<void> {
  await userEvent.clear(input)
  await userEvent.type(input, text)
}

// the custom form with its server fields open.
async function openCustomForm(): Promise<void> {
  render(AddMailboxWizard, { props: { initialProviderId: 'custom' } })
  await userEvent.click(screen.getByRole('button', { name: /Advanced/ }))
}

// leaving the email field is what starts discovery.
async function enterEmail(address: string): Promise<void> {
  await typeInto(screen.getByLabelText('Email'), address)
  await userEvent.tab()
}

function value(label: string, index = 0): string {
  return (screen.getAllByLabelText(label)[index] as HTMLInputElement).value
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.setSetting.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, autoconfigLookup: 'allow' }))
})

describe('late autodiscovery in the add-mailbox wizard', () => {
  it('keeps the server the user typed before discovery resolved, and fills the other', async () => {
    await openCustomForm()
    const land = deferDiscovery()
    await enterEmail('me@example.com')
    await typeInto(screen.getByLabelText('IMAP host'), 'mail.typed.example')

    await land(discovered('example.com'))

    expect(value('IMAP host')).toBe('mail.typed.example')
    expect(value('SMTP host')).toBe('smtp.example.com')
  })

  it('ignores a discovery for an address that was changed since', async () => {
    await openCustomForm()
    const landOld = deferDiscovery()
    await enterEmail('me@old.example')
    const landNew = deferDiscovery()
    await enterEmail('me@new.example')

    await landNew(discovered('new.example'))
    await landOld(discovered('old.example'))

    expect(value('IMAP host')).toBe('imap.new.example')
    expect(value('SMTP host')).toBe('smtp.new.example')
  })

  it('fills untouched fields on a later discovery', async () => {
    await openCustomForm()
    api.discoverConfig.mockResolvedValueOnce(discovered('first.example'))
    await enterEmail('me@first.example')
    await vi.waitFor(() => expect(value('IMAP host')).toBe('imap.first.example'))

    api.discoverConfig.mockResolvedValueOnce(discovered('second.example'))
    await enterEmail('me@second.example')
    await vi.waitFor(() => expect(value('IMAP host')).toBe('imap.second.example'))
  })

  it('leaves the whole IMAP server alone once its port was typed', async () => {
    await openCustomForm()
    const land = deferDiscovery()
    await enterEmail('me@example.com')
    await typeInto(screen.getAllByLabelText('Port')[0], '1993')

    await land(discovered('example.com'))

    expect(value('IMAP host')).toBe('')
    expect(value('Port', 0)).toBe('1993')
    expect(value('SMTP host')).toBe('smtp.example.com')
  })

  it('leaves the whole SMTP server alone once its port was typed', async () => {
    await openCustomForm()
    const land = deferDiscovery()
    await enterEmail('me@example.com')
    await typeInto(screen.getAllByLabelText('Port')[1], '2525')

    await land(discovered('example.com'))

    expect(value('SMTP host')).toBe('')
    expect(value('Port', 1)).toBe('2525')
    expect(value('IMAP host')).toBe('imap.example.com')
  })
})
