// mailvault.ts holds the mail vaults for the dialogs and the settings, which
// load it on first use. It is never on the window's startup path: the sidebar
// reads the vault marks the backend puts on each folder, and vaultdialog.ts
// opens the dialogs.
//
// Keeping locked mail out of lists, counts, search and everything else is the
// backend's job, which returns none of it. What this module adds is that
// nothing of a vault stays on screen once it locks: the reader closes and the
// undo history forgets the vault mail it acted on.

import { get, writable } from 'svelte/store'
import { addToMailVault, listMailVaults } from '../lib/api'
import type { MailVault, MailVaultMember } from '../lib/types'
import { messageList } from './messages'
import { messageDetail, clearMessage } from './message'
import { dropHistoryTouching } from './undohistory'
import { sidebar } from './accounts'
import { openVaultCreate, openVaultUnlock } from './vaultdialog'

/** Every vault, as the backend last reported them. */
export const mailVaults = writable<MailVault[]>([])

/** Reads the vaults again. */
export async function refreshMailVaults(): Promise<void> {
  try {
    mailVaults.set(await listMailVaults())
  } catch {
    mailVaults.set([])
  }
}

/**
 * Puts a folder or account into the vault: a new one when there is none, the
 * existing one when it is unlocked, and asks for its passphrase first when it
 * is locked. The interface keeps to one vault; the backend allows several.
 */
export async function protectWithVault(member: MailVaultMember): Promise<void> {
  await refreshMailVaults()
  const vault = get(mailVaults)[0]
  if (!vault) {
    openVaultCreate(member)
    return
  }
  if (vault.locked) {
    openVaultUnlock(vault.id)
    return
  }
  await addToMailVault(vault.id, member)
  await refreshMailVaults()
}

// ids of vault mail the list showed while its vault was unlocked.
const seen = new Set<number>()

function vaultFolders(): Set<number> {
  const out = new Set<number>()
  for (const f of Object.values(get(sidebar).data?.foldersByAccount ?? {}).flat()) {
    if (f.vaultId) {
      out.add(f.id)
    }
  }
  return out
}

messageList.subscribe((list) => {
  const folders = vaultFolders()
  for (const m of list.data?.items ?? []) {
    if (folders.has(m.folderId)) {
      seen.add(m.id)
    }
  }
})

/** Takes a vault change: on a lock, nothing of the vault's mail stays on screen. */
export function vaultChanged(locked: boolean): void {
  void refreshMailVaults()
  if (!locked) {
    return
  }
  dropHistoryTouching(seen)
  const open = get(messageDetail).data
  if (open && (seen.has(open.id) || vaultFolders().has(open.folderId))) {
    clearMessage()
  }
  seen.clear()
}
