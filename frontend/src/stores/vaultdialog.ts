// vaultdialog.ts is the part of the mail vault the window needs from the
// start: which vault dialog is open and the question asked before a vault
// file is written for opening. The backend marks each folder with its vault
// and whether it is locked, so the sidebar needs nothing more. The rest,
// mailvault.ts, mailvaultapi.ts and the dialogs, loads on first use.

import { writable } from 'svelte/store'
import { setVaultTempConsent } from '../lib/api'
import type { MailVaultMember } from '../lib/types'

/** The vault dialog on show, if any. */
export type VaultDialog =
  | { kind: 'unlock'; vaultId: number }
  | { kind: 'create'; member: MailVaultMember | null }
  | { kind: 'consent'; answer: (yes: boolean) => void }

export const vaultDialog = writable<VaultDialog | null>(null)

/** Opens the unlock dialog of a vault. */
export function openVaultUnlock(vaultId: number): void {
  vaultDialog.set({ kind: 'unlock', vaultId })
}

/** Opens the dialog that makes a vault, with a folder or account already chosen. */
export function openVaultCreate(member: MailVaultMember | null): void {
  vaultDialog.set({ kind: 'create', member })
}

/** Locks every vault, loading the vault's api on first use. */
export function lockVaultsNow(): void {
  void import('../lib/mailvaultapi').then((m) => m.lockAllMailVaults())
}

setVaultTempConsent(() => new Promise((answer) => vaultDialog.set({ kind: 'consent', answer })))
