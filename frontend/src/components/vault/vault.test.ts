import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { MailVault } from '../../lib/types'
import { expectAccessible } from '../../test/a11y'
import { expectAxeClean } from '../../test/axe'

const api = vi.hoisted(() => ({
  listMailVaults: vi.fn(),
  unlockMailVault: vi.fn(),
  recoverMailVault: vi.fn(),
  createMailVault: vi.fn(),
}))

vi.mock('../../lib/mailvaultapi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/mailvaultapi')>()),
  ...api,
}))

import VaultUnlockDialog from './VaultUnlockDialog.svelte'
import VaultCreateDialog from './VaultCreateDialog.svelte'
import VaultConsentDialog from './VaultConsentDialog.svelte'
import { mailVaults } from '../../stores/mailvault'
import { passphraseStrength } from '../../lib/vaultstrength'

// the dialogs open with a transition, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

function vault(patch: Partial<MailVault> = {}): MailVault {
  return {
    id: 1, name: 'Private', locked: true, state: 'ready', pending: 0, sealed: 3, members: [{ accountId: 1, folderId: 2 }],
    folderIds: [2], autoLockMinutes: 15, notify: 'generic', hideLocked: false, allowScripts: false, allowMcp: false, allowCli: false, retryAfterMs: 0,
    ...patch,
  }
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.listMailVaults.mockResolvedValue([vault({ locked: false })])
  mailVaults.set([vault()])
})

describe('VaultUnlockDialog', () => {
  it('unlocks with the passphrase and closes', async () => {
    api.unlockMailVault.mockResolvedValue({ ok: true })
    const close = vi.fn()
    render(VaultUnlockDialog, { props: { vaultId: 1 }, events: { close } })
    expectAccessible()
    await expectAxeClean()
    await userEvent.type(screen.getByLabelText('Passphrase'), 'correct horse')
    await userEvent.click(screen.getByRole('button', { name: 'Unlock' }))
    await waitFor(() => expect(close).toHaveBeenCalled())
    expect(api.unlockMailVault).toHaveBeenCalledWith(1, 'correct horse')
  })

  it('says when the passphrase is wrong and how long to wait', async () => {
    api.unlockMailVault.mockResolvedValueOnce({ ok: false, reason: 'wrong' })
    api.unlockMailVault.mockResolvedValueOnce({ ok: false, reason: 'wait', retryAfterMs: 5000 })
    render(VaultUnlockDialog, { props: { vaultId: 1 } })
    const field = screen.getByLabelText('Passphrase')
    await userEvent.type(field, 'nope')
    await userEvent.click(screen.getByRole('button', { name: 'Unlock' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('That did not open the vault.')
    await userEvent.type(field, 'again')
    await userEvent.click(screen.getByRole('button', { name: 'Unlock' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Wait 5 seconds')
  })

  it('recovers with the code and a new passphrase', async () => {
    api.recoverMailVault.mockResolvedValue({ ok: true })
    render(VaultUnlockDialog, { props: { vaultId: 1 } })
    await userEvent.click(screen.getByRole('button', { name: 'Use the recovery code instead' }))
    await userEvent.type(screen.getByLabelText('Recovery code'), 'ABCD-EFGH')
    await userEvent.type(screen.getByLabelText('New passphrase'), 'a new passphrase')
    await userEvent.type(screen.getByLabelText('Repeat the passphrase'), 'a different one')
    await userEvent.click(screen.getByRole('button', { name: 'Unlock' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('do not match')
    expect(api.recoverMailVault).not.toHaveBeenCalled()
    const repeat = screen.getByLabelText('Repeat the passphrase')
    await userEvent.clear(repeat)
    await userEvent.type(repeat, 'a new passphrase')
    await userEvent.click(screen.getByRole('button', { name: 'Unlock' }))
    await waitFor(() => expect(api.recoverMailVault).toHaveBeenCalledWith(1, 'ABCD-EFGH', 'a new passphrase'))
  })
})

describe('VaultCreateDialog', () => {
  it('explains the limits, then will not finish until the recovery code is accounted for', async () => {
    api.createMailVault.mockResolvedValue({ id: 1, recoveryCode: 'AAAA-BBBB-CCCC' })
    const close = vi.fn()
    render(VaultCreateDialog, { props: { member: { accountId: 1, folderId: 2 } }, events: { close } })
    expect(screen.getByText(/protects this device, not the server/)).toBeInTheDocument()
    expect(screen.getByText(/cannot be guaranteed erased/)).toBeInTheDocument()
    expectAccessible()
    await expectAxeClean()

    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await userEvent.type(screen.getByLabelText('Passphrase'), 'short')
    expect(screen.getByText(/Strength: weak/)).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('Repeat the passphrase'), 'short')
    await userEvent.click(screen.getByRole('button', { name: 'Create vault' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('at least 8')
    expect(api.createMailVault).not.toHaveBeenCalled()

    await userEvent.type(screen.getByLabelText('Passphrase'), 'Correct horse battery staple 42')
    await userEvent.type(screen.getByLabelText('Repeat the passphrase'), 'Correct horse battery staple 42')
    await userEvent.click(screen.getByRole('button', { name: 'Create vault' }))

    expect(await screen.findByText('AAAA-BBBB-CCCC')).toBeInTheDocument()
    const done = screen.getByRole('button', { name: 'Done' })
    expect(done).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Close' })).not.toBeInTheDocument()
    expectAccessible()
    await userEvent.click(screen.getByLabelText('I understand I may lose access to the local copy'))
    expect(done).toBeEnabled()
    await userEvent.click(done)
    expect(close).toHaveBeenCalled()
    expect(api.createMailVault.mock.calls[0][0]).toMatchObject({ members: [{ accountId: 1, folderId: 2 }] })
  })
})

describe('VaultConsentDialog', () => {
  it('answers no when closed without a choice', async () => {
    const answer = vi.fn()
    const { unmount } = render(VaultConsentDialog, { props: { answer } })
    expectAccessible()
    unmount()
    expect(answer).toHaveBeenCalledWith(false)
  })

  it('answers yes once when the user opens', async () => {
    const answer = vi.fn()
    const { unmount } = render(VaultConsentDialog, { props: { answer } })
    await userEvent.click(screen.getByRole('button', { name: 'Open' }))
    unmount()
    expect(answer).toHaveBeenCalledTimes(1)
    expect(answer).toHaveBeenCalledWith(true)
  })
})

describe('passphraseStrength', () => {
  it.each([
    ['', 'weak'],
    ['password', 'weak'],
    ['aaaaaaaaaaaaaaaa', 'weak'],
    ['Tr0ub4dor&3x', 'good'],
    ['correct horse battery staple', 'strong'],
  ])('rates %j as %s', (p, want) => {
    expect(passphraseStrength(p)).toBe(want)
  })
})
