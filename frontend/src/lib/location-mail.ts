// location-mail.ts lets Mail take part in back and forward: the list on show
// (a unified view, a folder or a saved view) and the conversation open in it.

import { get } from 'svelte/store'
import { getMessage } from './api'
import { registerLocationProvider, oneSideEmpty, type MailList, type MailLocation } from './locations'
import { sidebar } from '../stores/accounts'
import { views } from '../stores/views'
import { messageDetail } from '../stores/message'
import { mailScreen, closeMailScreen } from '../stores/mailscreen'
import { openMessageId, selection, selectFolder, selectSavedView, selectView, unifiedViewLabel } from '../stores/selection'
import { openSpace } from '../stores/space'
import type { Selection } from './types'

/** The list a selection shows, without its label. */
export function listOf(sel: Selection): MailList {
  if (sel.kind === 'folder') {
    return { kind: 'folder', folderId: sel.folderId, accountId: sel.accountId }
  }
  if (sel.kind === 'savedView') {
    return { kind: 'savedView', viewId: sel.viewId }
  }
  return { kind: 'view', view: sel.view }
}

/** Whether two lists are the same one. */
export function sameList(a: MailList | undefined, b: MailList | undefined): boolean {
  if (!a || !b) {
    return a === b
  }
  if (a.kind === 'folder' && b.kind === 'folder') {
    return a.folderId === b.folderId
  }
  if (a.kind === 'savedView' && b.kind === 'savedView') {
    return a.viewId === b.viewId
  }
  return a.kind === 'view' && b.kind === 'view' && a.view === b.view
}

function folderOf(list: Extract<MailList, { kind: 'folder' }>) {
  return get(sidebar).data?.foldersByAccount[list.accountId]?.find((f) => f.id === list.folderId)
}

function listLabel(list: MailList): string {
  if (list.kind === 'folder') {
    return folderOf(list)?.name ?? ''
  }
  if (list.kind === 'savedView') {
    return get(views).find((v) => v.id === list.viewId)?.name ?? ''
  }
  return unifiedViewLabel(list.view)
}

// shows a list, or says it is gone. A list already on show is left as it is,
// so its search and scroll position stay.
function showList(list: MailList): boolean {
  if (sameList(listOf(get(selection)), list)) {
    return true
  }
  if (list.kind === 'folder') {
    const folder = folderOf(list)
    if (!folder) {
      return false
    }
    selectFolder(folder)
    return true
  }
  if (list.kind === 'savedView') {
    const view = get(views).find((v) => v.id === list.viewId)
    if (!view) {
      return false
    }
    selectSavedView(view.id, view.name)
    return true
  }
  selectView(list.view, unifiedViewLabel(list.view))
  return true
}

registerLocationProvider('mail', {
  watch: [selection, openMessageId, messageDetail],
  describe(): MailLocation {
    const message = get(openMessageId)
    return { space: 'mail', list: listOf(get(selection)), message: message ?? undefined }
  },
  label(loc) {
    const detail = get(messageDetail).data
    if (loc.message !== undefined && detail && detail.id === loc.message && detail.subject) {
      return detail.subject
    }
    return loc.list ? listLabel(loc.list) : ''
  },
  async restore(loc) {
    if (loc.message !== undefined) {
      try {
        await getMessage(loc.message)
      } catch {
        return false
      }
    }
    if (loc.list && !showList(loc.list)) {
      return false
    }
    if (get(mailScreen) !== null) {
      closeMailScreen()
    }
    openSpace('mail')
    openMessageId.set(loc.message ?? null)
    return true
  },
  refines(from, to) {
    return sameList(from.list, to.list) && oneSideEmpty(from.message, to.message)
  },
})
