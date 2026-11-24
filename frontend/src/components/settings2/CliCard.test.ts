import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { expectAccessible } from '../../test/a11y'
import { expectAxeClean } from '../../test/axe'

const api = vi.hoisted(() => ({
  getCLIConfig: vi.fn(),
  setCLIEnabled: vi.fn(),
  setCLIPermission: vi.fn(),
  cliLog: vi.fn(),
  clearCLILog: vi.fn(),
  cliInstallInfo: vi.fn(),
  chooseCLIInstallDir: vi.fn(),
  installCLI: vi.fn(),
  onCLILog: vi.fn(() => () => undefined),
}))

vi.mock('../../lib/cliapi', () => api)

import CliCard from './CliCard.svelte'

const caps = ['read', 'search', 'change', 'send', 'send_unasked', 'delete_unasked', 'scripts', 'export', 'pim_read', 'pim_change']

function config(enabled: boolean, allowed: string[] = []) {
  return {
    enabled,
    running: true,
    socket: '/home/me/.config/Pelton/cli/pelton.sock',
    tokenFile: '/home/me/.config/Pelton/cli/token',
    permissions: caps.map((name) => ({ name, allowed: allowed.includes(name) })),
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  api.getCLIConfig.mockResolvedValue(config(false))
  api.cliLog.mockResolvedValue([])
  api.cliInstallInfo.mockResolvedValue({ supported: true, executable: '/Applications/Pelton.app/Contents/MacOS/Pelton', onPath: '', dirs: [{ dir: '/usr/local/bin', installed: false }] })
  api.setCLIEnabled.mockResolvedValue(undefined)
  api.setCLIPermission.mockResolvedValue(undefined)
  api.installCLI.mockResolvedValue({ path: '', command: "sudo ln -s '/Applications/Pelton.app/Contents/MacOS/Pelton' '/usr/local/bin/pelton'" })
})

describe('CliCard', () => {
  it('starts off, shows no switches until turned on, and is accessible', async () => {
    render(CliCard)
    const enable = await screen.findByRole('switch', { name: 'Allow the pelton command' })
    await waitFor(() => expect(enable).toBeEnabled())
    expect(enable).not.toBeChecked()
    expect(screen.queryByRole('switch', { name: 'Read mail' })).toBeNull()
    expectAccessible()
    await expectAxeClean()

    api.getCLIConfig.mockResolvedValue(config(true))
    await userEvent.click(enable)
    expect(api.setCLIEnabled).toHaveBeenCalledWith(true)
    const read = await screen.findByRole('switch', { name: 'Read mail' })
    expect(read).not.toBeChecked()
    for (const name of ['Send without asking', 'Delete permanently without asking', 'Run automations', 'Add tasks']) {
      expect(screen.getByRole('switch', { name })).not.toBeChecked()
    }
    expect(screen.getByText('/home/me/.config/Pelton/cli/token')).toBeInTheDocument()
    expectAccessible()
    await expectAxeClean()

    api.getCLIConfig.mockResolvedValue(config(true, ['read']))
    await userEvent.click(read)
    expect(api.setCLIPermission).toHaveBeenCalledWith('read', true)
    await waitFor(() => expect(screen.getByRole('switch', { name: 'Read mail' })).toBeChecked())
  })

  it('shows the command to run for a folder that needs admin rights', async () => {
    render(CliCard)
    await userEvent.click(await screen.findByRole('button', { name: 'Install' }))
    expect(api.installCLI).toHaveBeenCalledWith('/usr/local/bin')
    expect(await screen.findByText(/sudo ln -s/)).toBeInTheDocument()
  })

  it('lists the log and clears it', async () => {
    api.cliLog.mockResolvedValue([
      { id: 2, command: 'send', touched: '', result: 'permission_off', switch: 'send', when: 'now' },
      { id: 1, command: 'list', touched: 'list of INBOX', result: 'ok', switch: '', when: 'now' },
    ])
    api.clearCLILog.mockResolvedValue(undefined)
    render(CliCard)
    expect(await screen.findByText('list: list of INBOX')).toBeInTheDocument()
    expect(screen.getByText('permission_off (send)')).toBeInTheDocument()
    expectAccessible()
    api.cliLog.mockResolvedValue([])
    await userEvent.click(screen.getByRole('button', { name: 'Clear the log' }))
    expect(api.clearCLILog).toHaveBeenCalled()
    expect(await screen.findByText('Nothing yet.')).toBeInTheDocument()
  })
})
