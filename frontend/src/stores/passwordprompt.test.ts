import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { account as fixtureAccount } from '../test/fixtures'

const api = vi.hoisted(() => ({
  accountsNeedingPassword: vi.fn(),
  dismissPasswordPrompt: vi.fn(),
}))

vi.mock('../lib/api', () => api)

import { answerPasswordPrompt, askForPassword, passwordPrompt, passwordPromptSyncFollows, promptForMissingPasswords } from './passwordprompt'

const account = fixtureAccount()

beforeEach(() => {
  api.accountsNeedingPassword.mockReset().mockResolvedValue([account])
  api.dismissPasswordPrompt.mockReset().mockResolvedValue(undefined)
})

// runSync asks for missing passwords and then syncs every account. A prompt it
// raised must not sync the account on save too, or that mailbox syncs twice.
describe('which prompts leave the sync to their caller', () => {
  it('marks the prompts raised before a full sync', async () => {
    const done = promptForMissingPasswords(true)
    await vi.waitFor(() => expect(get(passwordPrompt)?.id).toBe(account.id))

    expect(get(passwordPromptSyncFollows)).toBe(true)
    answerPasswordPrompt('saved')
    await done
    expect(get(passwordPromptSyncFollows)).toBe(false)
  })

  // the Thunderbird import asks for the passwords of the mailboxes it just
  // created and syncs nothing itself: a saved password must start the sync.
  it('leaves the prompts raised after an import to sync on their own', async () => {
    const done = promptForMissingPasswords()
    await vi.waitFor(() => expect(get(passwordPrompt)?.id).toBe(account.id))

    expect(get(passwordPromptSyncFollows)).toBe(false)
    answerPasswordPrompt('saved')
    await done
  })

  it('leaves a prompt opened from a mailbox marker to sync on its own', async () => {
    const answered = askForPassword(account)

    expect(get(passwordPrompt)?.id).toBe(account.id)
    expect(get(passwordPromptSyncFollows)).toBe(false)
    answerPasswordPrompt('saved')
    await answered
  })
})
