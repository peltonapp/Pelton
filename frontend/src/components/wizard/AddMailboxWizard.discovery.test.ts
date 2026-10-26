import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import userEvent from '@testing-library/user-event'
import type { Discovered } from '../../lib/types'

const api = vi.hoisted(() => ({
  discoverConfig: vi.fn(),
  testConnection: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  BrowserOpenURL: vi.fn(),
}))

import AddMailboxWizard from './AddMailboxWizard.svelte'

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
function pending(): { promise: Promise<Discovered>; resolve: (d: Discovered) => void } {
  let resolve!: (d: Discovered) => void
  const promise = new Promise<Discovered>((r) => (resolve = r))
  return { promise, resolve }
}

async function typeInto(input: HTMLElement, text: string): Promise<void> {
  await userEvent.clear(input)
  await userEvent.type(input, text)
}

function ports(): HTMLInputElement[] {
  return screen.getAllByLabelText('Port') as HTMLInputElement[]
}

// leaving the email field is what starts discovery.
async function enterEmail(address: string): Promise<void> {
  await userEvent.type(screen.getByLabelText('Email'), address)
  await userEvent.tab()
}

function deferDiscovery(): ReturnType<typeof pending> {
  const d = pending()
  api.discoverConfig.mockReturnValue(d.promise)
  return d
}

async function passConnectionTest(): Promise<void> {
  await userEvent.type(screen.getByLabelText('Password'), 'mail-pass')
  await userEvent.click(screen.getByRole('button', { name: 'Test connection' }))
  await screen.findByText('Connection works.')
}

function expectHosts(imap: string, smtp: string): void {
  expect(screen.getByLabelText('IMAP host')).toHaveValue(imap)
  expect(screen.getByLabelText('SMTP host')).toHaveValue(smtp)
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.testConnection.mockResolvedValue({ untrusted: [] })
  render(AddMailboxWizard, { props: { initialProviderId: 'custom', offerImport: false } })
})

describe('late autodiscovery in the add-mailbox wizard', () => {
  it('keeps the servers the user typed before discovery resolved', async () => {
    const d = deferDiscovery()

    await enterEmail('me@example.org')
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    await typeInto(ports()[0], '1143')
    await typeInto(screen.getByLabelText('SMTP host'), '127.0.0.2')
    await typeInto(ports()[1], '1025')
    d.resolve(discovered('example.org'))
    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalled())
    await d.promise
    await tick()

    expectHosts('127.0.0.1', '127.0.0.2')
    expect(ports()[0]).toHaveValue(1143)
    expect(ports()[1]).toHaveValue(1025)
  })

  it('ignores a discovery for an address that was changed since', async () => {
    const first = pending()
    const second = pending()
    api.discoverConfig.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)

    const email = screen.getByLabelText('Email')
    await enterEmail('me@old.example')
    await typeInto(email, 'me@new.example')
    await userEvent.tab()
    second.resolve(discovered('new.example'))
    await vi.waitFor(() => expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.new.example'))
    first.resolve(discovered('old.example'))
    await first.promise
    await tick()

    expectHosts('imap.new.example', 'smtp.new.example')
  })

  // each value is typed over the field ('' only clears it); the last one must survive.
  it.each([
    ['still fills the fields the user left alone', ['127.0.0.1']],
    ["treats a field typed back to its old value as the user's", ['x', '']],
  ])('%s', async (_name, typed) => {
    const d = deferDiscovery()

    await enterEmail('me@example.org')
    for (const value of typed) {
      await userEvent.clear(screen.getByLabelText('IMAP host'))
      if (value) await userEvent.type(screen.getByLabelText('IMAP host'), value)
    }
    d.resolve(discovered('example.org'))
    await vi.waitFor(() => expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.example.org'))

    expect(screen.getByLabelText('IMAP host')).toHaveValue(typed.at(-1))
  })

  it('keeps a passed connection test valid when discovery lands after it', async () => {
    const d = deferDiscovery()

    await enterEmail('me@example.org')
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    await typeInto(screen.getByLabelText('SMTP host'), '127.0.0.1')
    await passConnectionTest()
    d.resolve(discovered('example.org'))
    await d.promise
    await tick()

    expect(screen.getByText('Connection works.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add mailbox' })).toBeEnabled()
  })

  it('keeps servers typed before the email was entered', async () => {
    api.discoverConfig.mockResolvedValue(discovered('example.org'))

    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    await typeInto(screen.getByLabelText('SMTP host'), '127.0.0.2')
    await enterEmail('me@example.org')
    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalled())
    await tick()
    await tick()

    expectHosts('127.0.0.1', '127.0.0.2')
  })

  it('does not overwrite hosts or clear a passed test when the email is blurred again', async () => {
    api.discoverConfig.mockResolvedValue(discovered('example.org'))

    const email = screen.getByLabelText('Email')
    await enterEmail('me@example.org')
    await vi.waitFor(() => expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.example.org'))
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    await typeInto(screen.getByLabelText('SMTP host'), '127.0.0.1')
    await passConnectionTest()

    await userEvent.click(email)
    await userEvent.tab()
    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalledTimes(2))
    await tick()
    await tick()

    expectHosts('127.0.0.1', '127.0.0.1')
    expect(screen.getByText('Connection works.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add mailbox' })).toBeEnabled()
  })

  it('fills untouched fields on a later discovery', async () => {
    api.discoverConfig.mockResolvedValueOnce(discovered('old.example')).mockResolvedValueOnce(discovered('new.example'))

    const email = screen.getByLabelText('Email')
    await enterEmail('me@old.example')
    await vi.waitFor(() => expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.old.example'))
    await typeInto(email, 'me@new.example')
    await userEvent.tab()

    await vi.waitFor(() => expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.new.example'))
    expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.new.example')
  })

  it('leaves the whole IMAP server alone once one of its fields was typed', async () => {
    const d = deferDiscovery()

    await enterEmail('me@example.org')
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    d.resolve({ ...discovered('example.org'), imapPort: 143, imapTls: 'starttls', smtpPort: 587, smtpTls: 'starttls' })
    await vi.waitFor(() => expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.example.org'))

    // the typed host keeps the port it was typed against; SMTP, untouched, follows discovery.
    expect(screen.getByLabelText('IMAP host')).toHaveValue('127.0.0.1')
    expect(ports()[0]).toHaveValue(993)
    expect(ports()[1]).toHaveValue(587)
  })

  it('leaves the whole SMTP server alone once its port was typed', async () => {
    const d = deferDiscovery()

    await enterEmail('me@example.org')
    await typeInto(ports()[1], '2525')
    d.resolve(discovered('example.org'))
    await vi.waitFor(() => expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.example.org'))

    expect(screen.getByLabelText('SMTP host')).toHaveValue('')
    expect(ports()[1]).toHaveValue(2525)
  })

  it('drops a discovery still running when another provider is picked', async () => {
    const d = deferDiscovery()

    await enterEmail('me@example.org')
    await userEvent.click(screen.getByRole('button', { name: 'Back' }))
    await userEvent.click(screen.getByRole('button', { name: /Gmail/ }))
    await enterEmail('me@example.org')
    d.resolve(discovered('example.org'))
    await d.promise
    await tick()
    await tick()

    expectHosts('imap.gmail.com', 'smtp.gmail.com')
  })
})
