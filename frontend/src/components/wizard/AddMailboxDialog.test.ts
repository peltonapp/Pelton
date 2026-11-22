import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  discoverConfig: vi.fn(),
  testConnection: vi.fn(),
  addPasswordAccount: vi.fn(),
  addOAuthAccount: vi.fn(),
  listFolders: vi.fn(),
  setFolderSyncExcluded: vi.fn(),
  startAccountSync: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/accounts', () => ({ refreshSidebar: vi.fn().mockResolvedValue(undefined) }))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  BrowserOpenURL: vi.fn(),
}))

// the dialog imports the wizard lazily; loading it here keeps that off the clock.
import './AddMailboxWizard.svelte'
import AddMailboxDialog from './AddMailboxDialog.svelte'
import { account, fillImapFormAndAdd } from '../../test/fixtures'

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.listFolders.mockResolvedValue([
    { id: 1, accountId: 7, name: 'INBOX' },
    { id: 2, accountId: 7, name: 'Archive' },
  ])
  api.startAccountSync.mockResolvedValue(undefined)
  api.addPasswordAccount.mockResolvedValue(account())
  api.testConnection.mockResolvedValue({ untrusted: [] })
})

async function addMailboxThroughForm(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: /Add a mailbox/i }))
  await fillImapFormAndAdd(user, api)
}

describe('AddMailboxDialog', () => {
  // the main window must not tear the wizard down on 'added', or the folder
  // picker never renders and the first sync never starts.
  it('keeps the wizard open through the folder picker, and closes it from the done step', async () => {
    const user = userEvent.setup()
    const added = vi.fn()
    const close = vi.fn()
    render(AddMailboxDialog, { events: { added, close } })

    await addMailboxThroughForm(user)

    await screen.findByText('Archive')
    await waitFor(() => expect(added).toHaveBeenCalledTimes(1))
    expect(close).not.toHaveBeenCalled()
    expect(api.startAccountSync).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: /^Sync everything$/i }))
    await waitFor(() => expect(api.startAccountSync).toHaveBeenCalledTimes(1))
    expect(close).not.toHaveBeenCalled()

    await user.click(await screen.findByRole('button', { name: /^Done$/i }))
    expect(close).toHaveBeenCalledTimes(1)
    expect(api.startAccountSync).toHaveBeenCalledTimes(1)
  })

  // Escape in the main window unmounts the wizard on the folder step.
  it('starts the first sync when the dialog is destroyed on the folder step', async () => {
    const user = userEvent.setup()
    const { unmount } = render(AddMailboxDialog)
    await addMailboxThroughForm(user)
    await screen.findByText('Archive')
    expect(api.startAccountSync).not.toHaveBeenCalled()

    unmount()
    await waitFor(() => expect(api.startAccountSync).toHaveBeenCalledTimes(1))
    expect(api.startAccountSync).toHaveBeenCalledWith(7)
  })

  it('does not sync twice when Start syncing ran before destroy', async () => {
    const user = userEvent.setup()
    const { unmount } = render(AddMailboxDialog)
    await addMailboxThroughForm(user)
    await screen.findByText('Archive')
    await user.click(screen.getByRole('button', { name: /^Sync everything$/i }))
    await waitFor(() => expect(api.startAccountSync).toHaveBeenCalledTimes(1))

    unmount()
    await Promise.resolve()
    expect(api.startAccountSync).toHaveBeenCalledTimes(1)
  })

  // the X stays on the saving spinner; a save that lands after the close must
  // still start the sync, without a folder step nobody can see.
  it('starts the first sync when closed while the mailbox is being saved', async () => {
    let save!: (a: ReturnType<typeof account>) => void
    api.addPasswordAccount.mockReturnValue(new Promise((r) => (save = r)))
    const user = userEvent.setup()
    const { unmount } = render(AddMailboxDialog)
    await addMailboxThroughForm(user)
    await waitFor(() => expect(api.addPasswordAccount).toHaveBeenCalled())

    unmount()
    save(account())
    await waitFor(() => expect(api.startAccountSync).toHaveBeenCalledTimes(1))
    expect(api.startAccountSync).toHaveBeenCalledWith(7)
    expect(api.listFolders).not.toHaveBeenCalled()
  })

  it('still starts the first sync when excluding a folder fails', async () => {
    api.setFolderSyncExcluded.mockRejectedValue(new Error('folder gone'))
    const user = userEvent.setup()
    render(AddMailboxDialog)
    await addMailboxThroughForm(user)
    await user.click(await screen.findByRole('checkbox', { name: 'Archive' }))
    await user.click(screen.getByRole('button', { name: /^Start syncing$/i }))

    await waitFor(() => expect(api.startAccountSync).toHaveBeenCalledTimes(1))
    expect(api.setFolderSyncExcluded).toHaveBeenCalledWith(2, true)
    expect(await screen.findByRole('button', { name: /^Done$/i })).toBeInTheDocument()
  })
})
