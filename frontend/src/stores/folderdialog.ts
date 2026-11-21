// folderdialog.ts drives the create/rename/delete mailbox dialog (#132). The
// sidebar rows are deeply nested, so they raise a request through this store
// rather than each rendering its own copy of the dialog.

import { writable } from 'svelte/store'
import type { Folder } from '../lib/types'

/** What the dialog is being opened for. */
export type FolderDialogMode = 'create' | 'rename' | 'delete' | 'empty' | 'role'

/** An open request for the folder dialog. */
export interface FolderDialogRequest {
  mode: FolderDialogMode
  accountId: number
  /** The folder being renamed or deleted; the parent when creating a subfolder. */
  folder: Folder | null
  /** Every trash folder at once, when emptying from the unified Bin. */
  folders?: Folder[]
}

export const folderDialog = writable<FolderDialogRequest | null>(null)

/**
 * Opens the dialog to create a mailbox. Pass the parent to create a subfolder
 * under it, or null to create at the root of the account.
 */
export function openCreateFolder(accountId: number, parent: Folder | null = null): void {
  folderDialog.set({ mode: 'create', accountId, folder: parent })
}

/** Opens the dialog to rename a mailbox. */
export function openRenameFolder(folder: Folder): void {
  folderDialog.set({ mode: 'rename', accountId: folder.accountId, folder })
}

/** Opens the delete confirmation for a mailbox. */
export function openDeleteFolder(folder: Folder): void {
  folderDialog.set({ mode: 'delete', accountId: folder.accountId, folder })
}

/**
 * Opens the confirmation for permanently deleting everything in the trash. The
 * folder is already the discard pile, so this confirms rather than asking for
 * the name to be typed back the way deleting a whole mailbox does.
 */
export function openEmptyTrash(folder: Folder): void {
  folderDialog.set({ mode: 'empty', accountId: folder.accountId, folder })
}

/** Opens the same confirmation for the trash of every account, from the unified Bin. */
export function openEmptyAllTrash(folders: Folder[]): void {
  folderDialog.set({ mode: 'empty', accountId: 0, folder: null, folders })
}

/**
 * Opens the role picker for a mailbox. Assigning a role is what makes a folder
 * reachable from the unified views when the server neither flags it with a
 * special-use attribute nor names it the way Pelton expects (#186).
 */
export function openFolderRole(folder: Folder): void {
  folderDialog.set({ mode: 'role', accountId: folder.accountId, folder })
}

/** Closes the dialog without acting. */
export function closeFolderDialog(): void {
  folderDialog.set(null)
}
