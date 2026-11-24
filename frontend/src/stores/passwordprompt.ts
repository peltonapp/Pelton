// passwordprompt.ts drives the missing-password prompt (#266, #290). The prompt
// is raised from three places now: the sync path, the sidebar and the settings
// mailbox list, so the request goes through this store rather than each of them
// rendering its own copy of the dialog.
//
// It also holds which accounts have no stored password, since that is what the
// warning marker next to a mailbox reads.

import { get, writable } from 'svelte/store'
import { accountsNeedingPassword, dismissPasswordPrompt } from '../lib/api'
import type { Account } from '../lib/types'

/**
 * How a prompt ended: a password was stored, the user closed it for now, or the
 * user asked not to be interrupted about this account again.
 */
export type PasswordPromptResult = 'saved' | 'skipped' | 'dismissed'

/** The account the dialog is currently asking about, or null when closed. */
export const passwordPrompt = writable<Account | null>(null)

/**
 * Whether a sync of every account already follows the open prompt. When it
 * does (the prompt came from runSync), the dialog must not start its own sync
 * of the account on save, or the mailbox syncs twice in a row.
 */
export const passwordPromptSyncFollows = writable(false)

/**
 * The accounts that cannot sync because no password is stored, dismissed ones
 * included. The sidebar and the settings list mark these.
 */
export const missingPassword = writable<Set<number>>(new Set())

/** The accounts whose prompt the user dismissed, so it stays quiet for them. */
const dismissed = new Set<number>()
// accounts skipped with Escape or Cancel. Unlike a dismissal this is not
// persisted: closing the prompt means "not now", and next launch is a new now.
const skipped = new Set<number>()

// one dialog at a time. A click on a second mailbox's marker while the first is
// open queues rather than losing the click or replacing what is on screen.
const queue: { account: Account; syncFollows: boolean; resolve: (result: PasswordPromptResult) => void }[] = []
let active: ((result: PasswordPromptResult) => void) | null = null

/**
 * Refreshes which accounts have no stored password and returns them. Safe to
 * call often; it is one keyring read per account.
 */
export async function refreshMissingPasswords(): Promise<Account[]> {
  let pending: Account[]
  try {
    pending = await accountsNeedingPassword()
  } catch {
    // whatever is wrong will surface on the sync itself. Leaving the previous
    // set alone beats clearing the markers on a transient failure.
    return []
  }
  dismissed.clear()
  for (const account of pending) {
    if (account.passwordPromptDismissed) {
      dismissed.add(account.id)
    }
  }
  missingPassword.set(new Set(pending.map((a) => a.id)))
  return pending
}

/**
 * Opens the prompt for one account and resolves once it is answered.
 * syncFollows says the caller syncs the account itself afterwards.
 */
export function askForPassword(account: Account, syncFollows = false): Promise<PasswordPromptResult> {
  return new Promise<PasswordPromptResult>((resolve) => {
    queue.push({ account, syncFollows, resolve })
    if (!active) {
      next()
    }
  })
}

function next(): void {
  const entry = queue.shift()
  if (!entry) {
    active = null
    passwordPrompt.set(null)
    passwordPromptSyncFollows.set(false)
    return
  }
  active = entry.resolve
  passwordPromptSyncFollows.set(entry.syncFollows)
  passwordPrompt.set(entry.account)
}

/** Closes the open prompt with the given answer. The dialog calls this. */
export function answerPasswordPrompt(result: PasswordPromptResult): void {
  const account = get(passwordPrompt)
  const resolve = active
  active = null
  passwordPrompt.set(null)

  if (account) {
    if (result === 'saved') {
      skipped.delete(account.id)
      dismissed.delete(account.id)
      missingPassword.update((set) => {
        const out = new Set(set)
        out.delete(account.id)
        return out
      })
    } else if (result === 'skipped') {
      skipped.add(account.id)
    } else {
      dismissed.add(account.id)
      void dismissPasswordPrompt(account.id)
    }
  }

  resolve?.(result)
  next()
}

/**
 * Asks about every account that has no stored password, one after another.
 * Accounts the user dismissed or already skipped this session are left alone:
 * they keep their marker instead.
 *
 * syncFollows is for runSync alone, which syncs every account once this
 * returns, so its prompts must not sync on their own. Any other caller (the
 * Thunderbird import) leaves it off: there a saved password is what starts
 * the account's first sync.
 */
export async function promptForMissingPasswords(syncFollows = false): Promise<void> {
  for (const account of await refreshMissingPasswords()) {
    if (dismissed.has(account.id) || skipped.has(account.id)) {
      continue
    }
    await askForPassword(account, syncFollows)
  }
}
