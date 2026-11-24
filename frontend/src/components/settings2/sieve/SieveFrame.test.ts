import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Account, SieveState } from '../../../lib/types'
import { normalizeState } from '../../../lib/sieve'

const api = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  sieveState: vi.fn(),
  recheckSieve: vi.fn(),
  setSieveConnection: vi.fn(),
  restoreSievePrevious: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

import SieveFrame from './SieveFrame.svelte'

// the connection form slides open, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const account = {
  id: 3,
  email: 'me@example.org',
  displayName: '',
  localLabel: '',
  useLocalLabel: false,
  imapHost: 'imap.example.org',
  local: false,
} as Account

function state(patch: Partial<SieveState>): SieveState {
  const base = normalizeState({ accountId: 3, status: 'ok', detail: '', host: 'imap.example.org', port: 4190, tls: '', manual: false } as SieveState)
  return { ...base, ...patch }
}

describe('SieveFrame', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.listAccounts.mockResolvedValue([account, { ...account, id: 9, local: true }])
  })

  it('says plainly when the server has no filter service, and offers the connection by hand', async () => {
    api.sieveState.mockResolvedValue(state({ status: 'unsupported', remembered: true }))
    render(SieveFrame, { props: { anchor: 'test' } })

    expect(await screen.findByText(/This mail server has no filter service/)).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Host' })).toBeInTheDocument()

    api.setSieveConnection.mockResolvedValue(state({ status: 'ok', manual: true, host: 'sieve.example.org', port: 2000 }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Host' }), 'sieve.example.org')
    await userEvent.type(screen.getByRole('textbox', { name: 'Port' }), '2000')
    await userEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(api.setSieveConnection).toHaveBeenCalledWith(3, 'sieve.example.org', 2000, 'starttls')
    expect(await screen.findByText('Connected to the filter service at sieve.example.org:2000')).toBeInTheDocument()
  })

  it('looks again on request', async () => {
    api.sieveState.mockResolvedValue(state({ status: 'unsupported', remembered: true }))
    api.recheckSieve.mockResolvedValue(state({ status: 'ok' }))
    render(SieveFrame, { props: { anchor: 'test' } })
    await userEvent.click(await screen.findByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(api.recheckSieve).toHaveBeenCalledWith(3))
  })

  it('leaves the local folders out and names the quiet server note', async () => {
    api.sieveState.mockResolvedValue(state({ status: 'ok' }))
    render(SieveFrame, { props: { anchor: 'test' } })
    await waitFor(() => expect(api.sieveState).toHaveBeenCalledTimes(1))
    expect(api.sieveState).toHaveBeenCalledWith(3)
    expect(screen.queryByRole('combobox', { name: 'Mailbox' })).not.toBeInTheDocument()
    expect(screen.getByText('These rules run on the mail server, so they work while Pelton is closed.')).toBeInTheDocument()
  })
})
