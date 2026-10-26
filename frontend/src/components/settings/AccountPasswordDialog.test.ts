import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { account as fixtureAccount } from '../../test/fixtures'

// Modal uses the Web Animations API; jsdom does not implement it.
beforeAll(() => {
  if (!Element.prototype.animate) {
    Element.prototype.animate = () =>
      ({
        finished: Promise.resolve(),
        cancel: () => {},
        finish: () => {},
        onfinish: null,
      }) as unknown as Animation
  }
})

const api = vi.hoisted(() => ({
  checkAccountPassword: vi.fn(),
  setAccountPassword: vi.fn(),
  syncAccountNow: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import AccountPasswordDialog from './AccountPasswordDialog.svelte'

const account = fixtureAccount()

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.checkAccountPassword.mockResolvedValue({ ok: true, rejected: false, error: '' })
  api.setAccountPassword.mockResolvedValue(undefined)
  api.syncAccountNow.mockResolvedValue(undefined)
})

describe('AccountPasswordDialog', () => {
  it('syncs the mailbox as soon as its password is saved', async () => {
    const user = userEvent.setup()
    const onDone = vi.fn()
    render(AccountPasswordDialog, { account, onDone })

    await user.type(screen.getByLabelText('Password'), 'pw')
    await user.click(screen.getByRole('button', { name: /^Save$/i }))
    await waitFor(() => expect(onDone).toHaveBeenCalledWith('saved'))
    expect(api.setAccountPassword).toHaveBeenCalledWith(7, 'pw')
    expect(api.syncAccountNow).toHaveBeenCalledWith(7)
  })

  it('leaves the sync to the caller when one already follows the prompt', async () => {
    const user = userEvent.setup()
    const onDone = vi.fn()
    render(AccountPasswordDialog, { account, onDone, syncOnSave: false })

    await user.type(screen.getByLabelText('Password'), 'pw')
    await user.click(screen.getByRole('button', { name: /^Save$/i }))
    await waitFor(() => expect(onDone).toHaveBeenCalledWith('saved'))
    expect(api.setAccountPassword).toHaveBeenCalledWith(7, 'pw')
    expect(api.syncAccountNow).not.toHaveBeenCalled()
  })

  it('does not sync when no password is saved', async () => {
    const user = userEvent.setup()
    const onDone = vi.fn()
    render(AccountPasswordDialog, { account, onDone })

    expect(screen.getByRole('button', { name: /^Save$/i })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: /Skip for now/i }))
    expect(onDone).toHaveBeenCalledWith('skipped')
    expect(api.setAccountPassword).not.toHaveBeenCalled()
    expect(api.syncAccountNow).not.toHaveBeenCalled()
  })

  it('does not sync a password the server refused', async () => {
    const user = userEvent.setup()
    api.checkAccountPassword.mockResolvedValue({ ok: false, rejected: true, error: 'no' })
    render(AccountPasswordDialog, { account, onDone: vi.fn() })

    await user.type(screen.getByLabelText('Password'), 'bad')
    await user.click(screen.getByRole('button', { name: /^Save$/i }))
    await screen.findByRole('alert')
    expect(api.setAccountPassword).not.toHaveBeenCalled()
    expect(api.syncAccountNow).not.toHaveBeenCalled()
  })
})
