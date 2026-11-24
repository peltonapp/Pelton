// mailvaultapi.ts is the mail vault's part of the Go api. It calls the bound
// methods by name rather than through the generated bindings: those are one
// module shared with api.ts, so importing them here would keep the vault's
// calls and their types in the window's startup code. The backend test
// TestMailVaultAPINamesAreBound checks every name below against the App.

import { Call } from '@wailsio/runtime'
import type { MailVault, MailVaultAuth, MailVaultMember, MailVaultSettings } from './types'

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(`github.com/peltonapp/Pelton/internal/desktop.App.${method}`, ...args) as Promise<T>
}

/** Every mail vault with its state and sealing progress. */
export function listMailVaults(): Promise<MailVault[]> {
  return call('ListMailVaults')
}

/** Makes a vault and starts sealing; the recovery code is the only copy there will be. */
export function createMailVault(req: {
  name: string
  passphrase: string
  members: MailVaultMember[]
  autoLockMinutes: number
  notify: 'none' | 'generic'
  hideLocked: boolean
}): Promise<{ id: number; recoveryCode: string }> {
  return call('CreateMailVault', req)
}

/** Puts a folder, or a whole account when folderId is 0, into an unlocked vault. */
export function addToMailVault(vaultId: number, member: MailVaultMember): Promise<void> {
  return call('AddToMailVault', vaultId, member.accountId, member.folderId)
}

/** Opens a vault with its passphrase. */
export function unlockMailVault(id: number, passphrase: string): Promise<MailVaultAuth> {
  return call('UnlockMailVault', id, passphrase)
}

/** Opens a vault with its recovery code and sets a new passphrase. */
export function recoverMailVault(id: number, code: string, passphrase: string): Promise<MailVaultAuth> {
  return call('RecoverMailVault', id, code, passphrase)
}

/** Rewraps a vault's key under a new passphrase. */
export function changeMailVaultPassphrase(id: number, current: string, next: string): Promise<MailVaultAuth> {
  return call('ChangeMailVaultPassphrase', id, current, next)
}

/** Locks every vault now. */
export function lockAllMailVaults(): Promise<void> {
  return call('LockAllMailVaults')
}

/** Stores a vault's settings. */
export function updateMailVaultSettings(s: MailVaultSettings): Promise<void> {
  return call('UpdateMailVaultSettings', s)
}

/** Unseals an unlocked vault's mail and removes the vault. */
export function removeMailVault(id: number): Promise<void> {
  return call('RemoveMailVault', id)
}
