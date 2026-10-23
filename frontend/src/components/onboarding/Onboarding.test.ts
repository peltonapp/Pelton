import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  defaultMailClientStatus: vi.fn(),
  setDefaultMailClient: vi.fn(),
  discoverConfig: vi.fn(),
  testConnection: vi.fn(),
  addPasswordAccount: vi.fn(),
  listFolders: vi.fn(),
  setFolderSyncExcluded: vi.fn(),
  startAccountSync: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

// jsdom has no Element.animate, which Svelte's transitions call.
vi.mock('svelte/transition', () => ({
  fade: () => ({ duration: 0 }),
  scale: () => ({ duration: 0 }),
}))

const toast = vi.hoisted(() => ({ toastError: vi.fn() }))

vi.mock('../../stores/toast', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/toast')>()),
  ...toast,
}))

vi.mock('../../lib/liability', () => ({ acceptLiability: vi.fn() }))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  BrowserOpenURL: vi.fn(),
}))

import Onboarding from './Onboarding.svelte'
import { account, fillImapFormAndAdd } from '../../test/fixtures'

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  toast.toastError.mockReset()
  api.defaultMailClientStatus.mockResolvedValue({ known: false, isDefault: false })
  // two or more folders is what puts the wizard on its folder picker.
  api.listFolders.mockResolvedValue([
    { id: 1, accountId: 7, name: 'INBOX' },
    { id: 2, accountId: 7, name: 'Archive' },
  ])
  // Confetti on the done step reads matchMedia, which jsdom lacks, unless motion is reduced.
  document.documentElement.setAttribute('data-reduce-motion', '')
  api.startAccountSync.mockResolvedValue(undefined)
  api.addPasswordAccount.mockResolvedValue(account())
  api.testConnection.mockResolvedValue({ untrusted: [] })
})

async function addMailboxThroughOnboarding(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: /Get started/i }))
  await user.click(await screen.findByRole('checkbox'))
  await user.click(screen.getByRole('button', { name: /Continue/i }))
  while (!screen.queryByText('Set up your mail')) {
    await user.click(await screen.findByRole('button', { name: /Continue/i }))
  }

  await user.click(screen.getByRole('button', { name: /Add a mailbox/i }))
  await fillImapFormAndAdd(user, api)
}

describe('Onboarding mailbox step', () => {
  it('starts the first sync once for a mailbox added there and still reaches the done step', async () => {
    const user = userEvent.setup({ delay: null })
    const added = vi.fn()
    render(Onboarding, { events: { added } })

    await addMailboxThroughOnboarding(user)

    await screen.findByRole('button', { name: /Start using Pelton/i })
    expect(added).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(api.startAccountSync).toHaveBeenCalledTimes(1))
    expect(api.startAccountSync).toHaveBeenCalledWith(7)
  })

  it('still reaches the done step and reports the error when the first sync fails to start', async () => {
    const user = userEvent.setup({ delay: null })
    api.startAccountSync.mockRejectedValue(new Error('sync refused'))
    render(Onboarding)
    await addMailboxThroughOnboarding(user)

    await screen.findByRole('button', { name: /Start using Pelton/i })
    expect(toast.toastError).toHaveBeenCalledWith('sync refused')
  })
})
