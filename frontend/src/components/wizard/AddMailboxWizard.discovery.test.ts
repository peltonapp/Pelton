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

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.testConnection.mockResolvedValue({ untrusted: [] })
  render(AddMailboxWizard, { props: { initialProviderId: 'custom', offerImport: false } })
})

describe('late autodiscovery in the add-mailbox wizard', () => {
  it('keeps the servers the user typed before discovery resolved', async () => {
    const d = pending()
    api.discoverConfig.mockReturnValue(d.promise)

    await userEvent.type(screen.getByLabelText('Email'), 'me@example.org')
    await userEvent.tab()
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    await typeInto(ports()[0], '1143')
    await typeInto(screen.getByLabelText('SMTP host'), '127.0.0.2')
    await typeInto(ports()[1], '1025')
    d.resolve(discovered('example.org'))
    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalled())
    await d.promise
    await tick()

    expect(screen.getByLabelText('IMAP host')).toHaveValue('127.0.0.1')
    expect(ports()[0]).toHaveValue(1143)
    expect(screen.getByLabelText('SMTP host')).toHaveValue('127.0.0.2')
    expect(ports()[1]).toHaveValue(1025)
  })

  it('ignores a discovery for an address that was changed since', async () => {
    const first = pending()
    const second = pending()
    api.discoverConfig.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)

    const email = screen.getByLabelText('Email')
    await userEvent.type(email, 'me@old.example')
    await userEvent.tab()
    await typeInto(email, 'me@new.example')
    await userEvent.tab()
    second.resolve(discovered('new.example'))
    await vi.waitFor(() => expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.new.example'))
    first.resolve(discovered('old.example'))
    await first.promise
    await tick()

    expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.new.example')
    expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.new.example')
  })

  it('still fills the fields the user left alone', async () => {
    const d = pending()
    api.discoverConfig.mockReturnValue(d.promise)

    await userEvent.type(screen.getByLabelText('Email'), 'me@example.org')
    await userEvent.tab()
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    d.resolve(discovered('example.org'))
    await vi.waitFor(() => expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.example.org'))

    expect(screen.getByLabelText('IMAP host')).toHaveValue('127.0.0.1')
  })

  it('keeps a passed connection test valid when discovery lands after it', async () => {
    const d = pending()
    api.discoverConfig.mockReturnValue(d.promise)

    await userEvent.type(screen.getByLabelText('Email'), 'me@example.org')
    await userEvent.tab()
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    await typeInto(screen.getByLabelText('SMTP host'), '127.0.0.1')
    await userEvent.type(screen.getByLabelText('Password'), 'mail-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    await screen.findByText('Connection works.')
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
    await userEvent.type(screen.getByLabelText('Email'), 'me@example.org')
    await userEvent.tab()
    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalled())
    await tick()
    await tick()

    expect(screen.getByLabelText('IMAP host')).toHaveValue('127.0.0.1')
    expect(screen.getByLabelText('SMTP host')).toHaveValue('127.0.0.2')
  })

  it('does not overwrite hosts or clear a passed test when the email is blurred again', async () => {
    api.discoverConfig.mockResolvedValue(discovered('example.org'))

    const email = screen.getByLabelText('Email')
    await userEvent.type(email, 'me@example.org')
    await userEvent.tab()
    await vi.waitFor(() => expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.example.org'))
    await typeInto(screen.getByLabelText('IMAP host'), '127.0.0.1')
    await typeInto(screen.getByLabelText('SMTP host'), '127.0.0.1')
    await userEvent.type(screen.getByLabelText('Password'), 'mail-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    await screen.findByText('Connection works.')

    await userEvent.click(email)
    await userEvent.tab()
    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalledTimes(2))
    await tick()
    await tick()

    expect(screen.getByLabelText('IMAP host')).toHaveValue('127.0.0.1')
    expect(screen.getByLabelText('SMTP host')).toHaveValue('127.0.0.1')
    expect(screen.getByText('Connection works.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add mailbox' })).toBeEnabled()
  })

  it('treats a field typed back to its old value as the user\'s', async () => {
    const d = pending()
    api.discoverConfig.mockReturnValue(d.promise)

    await userEvent.type(screen.getByLabelText('Email'), 'me@example.org')
    await userEvent.tab()
    await typeInto(screen.getByLabelText('IMAP host'), 'x')
    await userEvent.clear(screen.getByLabelText('IMAP host'))
    d.resolve(discovered('example.org'))
    await vi.waitFor(() => expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.example.org'))

    expect(screen.getByLabelText('IMAP host')).toHaveValue('')
  })

  it('fills untouched fields on a later discovery', async () => {
    api.discoverConfig.mockResolvedValueOnce(discovered('old.example')).mockResolvedValueOnce(discovered('new.example'))

    const email = screen.getByLabelText('Email')
    await userEvent.type(email, 'me@old.example')
    await userEvent.tab()
    await vi.waitFor(() => expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.old.example'))
    await typeInto(email, 'me@new.example')
    await userEvent.tab()

    await vi.waitFor(() => expect(screen.getByLabelText('IMAP host')).toHaveValue('imap.new.example'))
    expect(screen.getByLabelText('SMTP host')).toHaveValue('smtp.new.example')
  })
})
