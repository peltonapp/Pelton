// selection.ts holds the ephemeral navigation state: which view or folder the
// message list is showing, which message is open, and the current search query.
// this is pure ui state so in-memory stores are correct here.

import { get, writable } from 'svelte/store'
import type { Selection, Folder, ViewKey } from '../lib/types'
import type { SidebarData } from './accounts'
import { setSetting, getSetting, SettingKeys } from '../lib/api'
import { t } from '../lib/i18n'

// the default startup selection is the unified inbox, per the design.
export const defaultSelection: Selection = { kind: 'view', view: 'inbox', label: 'Unified Inbox' }

export const selection = writable<Selection>(defaultSelection)

// A selection carries its label rather than looking it up, which means the
// label is a snapshot of whatever language was active when it was made. The
// window title and the list header read it, so switching language used to leave
// both reading in the old one until the user clicked something else.
//
// Only the built-in views are re-resolved. A folder's label is its name on the
// server and a saved View's is what the user called it; neither is translated,
// and rewriting them from a catalog would replace what the user named with
// something they never chose.
// t rather than locale: the catalog is fetched after the locale changes, so
// locale alone would re-resolve against the strings that are about to be
// replaced. Writing only on a real difference keeps this from handing out a new
// selection object every time the store settles.
t.subscribe(() => {
  selection.update((sel) => {
    if (sel.kind !== 'view') {
      return sel
    }
    const label = unifiedViewLabel(sel.view)
    return label === sel.label ? sel : { ...sel, label }
  })
})

// the "remember what I had open" value of the startup setting. anything else is
// a fixed target, stored as "view:<key>" or "folder:<id>".
const startupLast = 'last'

// storedSelection renders a selection into the "view:<key>" / "folder:<id>" form
// the startup and last-selection settings share. Saved Views are not offered as
// a startup target, so they store as nothing and leave the last value alone.
function storedSelection(sel: Selection): string {
  if (sel.kind === 'view') {
    return `view:${sel.view}`
  }
  if (sel.kind === 'folder') {
    return `folder:${sel.folderId}`
  }
  return ''
}

// unifiedViewLabel localizes a unified view key the same way the sidebar rows
// do, so a restored selection reads identically to a clicked one.
export function unifiedViewLabel(key: string): string {
  const translate = get(t)
  const lookup = key === 'inbox' ? 'sidebar.unifiedInbox' : `sidebar.view.${key}`
  const translated = translate(lookup)
  return translated === lookup ? key : translated
}

// resolveSelection turns a stored "view:<key>" / "folder:<id>" value into a
// selection, checking it against what actually exists. A folder that has since
// been deleted, or a key from a future version, returns null so the caller can
// fall back to the unified inbox.
export function resolveSelection(stored: string, data: SidebarData): Selection | null {
  const split = stored.indexOf(':')
  if (split < 0) {
    return null
  }
  const kind = stored.slice(0, split)
  const rest = stored.slice(split + 1)
  if (kind === 'view') {
    if (!data.views.some((v) => v.key === rest)) {
      return null
    }
    return { kind: 'view', view: rest as ViewKey, label: unifiedViewLabel(rest) }
  }
  if (kind === 'folder') {
    const id = Number(rest)
    for (const folders of Object.values(data.foldersByAccount)) {
      const folder = folders.find((f) => f.id === id)
      if (folder) {
        return { kind: 'folder', folderId: folder.id, accountId: folder.accountId, label: folder.name }
      }
    }
  }
  return null
}

// applyStartupSelection sets the selection the user configured for launch: a
// fixed unified view or folder, or whatever was open when the app last closed.
// Anything that cannot be resolved falls back to the unified inbox rather than
// leaving the list empty. Call once, after the sidebar data has loaded.
export async function applyStartupSelection(pref: string, data: SidebarData): Promise<void> {
  let target = pref
  if (pref === startupLast) {
    try {
      const { value, found } = await getSetting(SettingKeys.lastSelection)
      target = found ? value : ''
    } catch {
      target = ''
    }
  }
  const resolved = target ? resolveSelection(target, data) : null
  selection.set(resolved ?? { ...defaultSelection, label: unifiedViewLabel('inbox') })
}

// rememberSelection persists the current selection so the "last used" startup
// option can restore it. Fire and forget: a failed write only costs the restore.
function rememberSelection(sel: Selection): void {
  const value = storedSelection(sel)
  if (value) {
    void setSetting(SettingKeys.lastSelection, value)
  }
}

// the currently open message id, or null when the detail pane is empty.
export const openMessageId = writable<number | null>(null)

// the active search query. an empty string means the normal list is shown.
export const searchQuery = writable<string>('')

// selectView switches the list to a unified view and clears the open message.
export function selectView(view: ViewKey, label: string): void {
  const sel: Selection = { kind: 'view', view, label }
  selection.set(sel)
  rememberSelection(sel)
  openMessageId.set(null)
  searchQuery.set('')
}

// selectSavedView switches the list to a user-defined saved View (preset search).
export function selectSavedView(viewId: number, label: string): void {
  selection.set({ kind: 'savedView', viewId, label })
  openMessageId.set(null)
  searchQuery.set('')
}

// leaveDeletedSavedView moves the list back to the unified inbox when the saved
// View it is showing has just been deleted. Staying put leaves the list, the
// header and the search bar bound to a view id the backend no longer has.
export function leaveDeletedSavedView(viewId: number): void {
  const sel = get(selection)
  if (sel.kind === 'savedView' && sel.viewId === viewId) {
    selectView('inbox', unifiedViewLabel('inbox'))
  }
}

// leaveMissingSavedView does the same for a View that is simply no longer in
// the saved list, whatever became of it: deleted from another window, dropped
// by a profile switch, or gone from the server. The delete button is not the
// only way a View can stop existing under the list showing it.
export function leaveMissingSavedView(ids: readonly number[]): void {
  const sel = get(selection)
  if (sel.kind === 'savedView' && !ids.includes(sel.viewId)) {
    selectView('inbox', unifiedViewLabel('inbox'))
  }
}

// selectFolder switches the list to a single account folder.
export function selectFolder(folder: Folder): void {
  const sel: Selection = {
    kind: 'folder',
    folderId: folder.id,
    accountId: folder.accountId,
    label: folder.name,
  }
  selection.set(sel)
  rememberSelection(sel)
  openMessageId.set(null)
  searchQuery.set('')
}

// openMessage sets the open message for the detail pane.
export function openMessage(id: number): void {
  openMessageId.set(id)
}

// revealMessage moves the list to where a message lives and then opens it. It
// is for requests that arrive from outside the ui, where the list is very
// likely showing something else: a clicked new-mail notification is the only
// one today. A folder that is not in the sidebar (removed since, or belonging
// to an account this profile does not see) leaves the list where it is and just
// opens the message, which is still what was asked for.
export function revealMessage(
  messageId: number,
  accountId: number,
  folderId: number,
  data: SidebarData | null,
): void {
  const folder = data?.foldersByAccount[accountId]?.find((f) => f.id === folderId)
  if (folder) {
    // selectFolder clears the open message, so it has to happen first.
    selectFolder(folder)
  }
  openMessage(messageId)
}
