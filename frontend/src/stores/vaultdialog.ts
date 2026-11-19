// vaultdialog.ts is the part of the mail vault the window needs from the
// start: which vault dialog is open, the question asked before a vault file
// is written for opening, and the palette entries. The backend marks each
// folder with its vault and whether it is locked, so the sidebar needs nothing
// more. Everything else, mailvault.ts and the dialogs, loads on first use.

import { get, writable } from 'svelte/store'
import { IconLock } from '@tabler/icons-svelte'
import { lockAllMailVaults, setVaultTempConsent } from '../lib/api'
import { registerPaletteSource } from '../lib/palettesources'
import type { PaletteCommand } from '../lib/commands'
import type { MailVaultMember } from '../lib/types'
import { sidebar } from './accounts'

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

setVaultTempConsent(() => new Promise((answer) => vaultDialog.set({ kind: 'consent', answer })))

registerPaletteSource('mailvault', (t) => {
  const folders = Object.values(get(sidebar).data?.foldersByAccount ?? {}).flat()
  const out: PaletteCommand[] = []
  if (folders.some((f) => f.vaultId && !f.vaultLocked)) {
    out.push({ id: 'mailvault:lock', group: 'action', label: t('mailvault.lockNow'), icon: IconLock, run: () => void lockAllMailVaults() })
  }
  const locked = folders.find((f) => f.vaultLocked)
  if (locked) {
    out.push({ id: 'mailvault:unlock', group: 'action', label: t('mailvault.unlock'), icon: IconLock, run: () => openVaultUnlock(locked.vaultId ?? 0) })
  }
  return out
})
