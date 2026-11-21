import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { MailVault } from '../../lib/types'
import { expectAccessible } from '../../test/a11y'
import { expectAxeClean } from '../../test/axe'

const api = vi.hoisted(() => ({
  listMailVaults: vi.fn(),
  updateMailVaultSettings: vi.fn(),
  lockAllMailVaults: vi.fn(),
}))

vi.mock('../../lib/mailvaultapi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/mailvaultapi')>()),
  ...api,
}))

import VaultCard from './VaultCard.svelte'
import { mailVaults } from '../../stores/mailvault'

function vault(patch: Partial<MailVault> = {}): MailVault {
  return {
    id: 3, name: 'Private', locked: false, state: 'ready', pending: 0, sealed: 12, members: [{ accountId: 1, folderId: 0 }],
    folderIds: [4, 5], autoLockMinutes: 15, notify: 'generic', hideLocked: false, allowScripts: false, allowMcp: false, allowCli: false, retryAfterMs: 0,
    ...patch,
  }
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.updateMailVaultSettings.mockResolvedValue(undefined)
  api.lockAllMailVaults.mockResolvedValue(undefined)
})

describe('VaultCard', () => {
  it('keeps scripts and MCP off by default and saves a switch through the backend', async () => {
    api.listMailVaults.mockResolvedValue([vault()])
    render(VaultCard)
    await waitFor(() => expect(api.listMailVaults).toHaveBeenCalled())
    expectAccessible()
    await expectAxeClean()
    const scripts = screen.getByRole('switch', { name: 'Scripts may read this vault' })
    const mcp = screen.getByRole('switch', { name: 'MCP clients may read this vault' })
    expect(scripts).not.toBeChecked()
    expect(mcp).not.toBeChecked()
    expect(screen.getByRole('switch', { name: 'The pelton command may read this vault' })).not.toBeChecked()
    await userEvent.click(scripts)
    await waitFor(() => expect(api.updateMailVaultSettings).toHaveBeenCalledWith(expect.objectContaining({ id: 3, allowScripts: true, allowMcp: false, allowCli: false })))
    expect(screen.getByText(/protects this device, not the server/)).toBeInTheDocument()
  })

  it('offers lock now while unlocked and refuses removal while locked', async () => {
    api.listMailVaults.mockResolvedValue([vault()])
    render(VaultCard)
    await userEvent.click(await screen.findByRole('button', { name: 'Lock vault now' }))
    expect(api.lockAllMailVaults).toHaveBeenCalled()
    mailVaults.set([vault({ locked: true })])
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remove vault' })).toBeDisabled())
  })
})
