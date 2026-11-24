// attachments.ts holds the attachments browser of the new design: whether it
// fills the mail surface instead of the list and the reader, its filters and
// its layout. Picking anything else in the sidebar puts the list back.

import { get, writable } from 'svelte/store'
import { selection, revealMessage } from './selection'
import { sidebar } from './accounts'
import type { AttachmentEntry, AttachmentFilter, Selection } from '../lib/types'

/** Whether the browser is on screen in place of the list and the reader. */
export const attachmentsOpen = writable<boolean>(false)

/** A filter that narrows nothing. */
export function emptyAttachmentFilter(): AttachmentFilter {
  return { kinds: [], accountId: 0, sender: '', since: '', until: '', name: '' }
}

/** The browser's filters; they stay while the browser is closed. */
export const attachmentFilter = writable<AttachmentFilter>(emptyAttachmentFilter())

/** Rows with details, or a grid of thumbnails. */
export type AttachmentLayout = 'list' | 'grid'

/** How the browser lays its files out. */
export const attachmentLayout = writable<AttachmentLayout>('list')

/** Shows the browser, optionally narrowed to one filter. */
export function openAttachments(filter?: Partial<AttachmentFilter>): void {
  if (filter) {
    attachmentFilter.set({ ...emptyAttachmentFilter(), ...filter })
  }
  attachmentsOpen.set(true)
}

/** Puts the list and the reader back. */
export function closeAttachments(): void {
  attachmentsOpen.set(false)
}

/** Shows the browser when it is hidden and hides it when it shows. */
export function toggleAttachments(): void {
  attachmentsOpen.update((open) => !open)
}

// choosing a folder or a view in the sidebar means the list is wanted again,
// even the one already showing. A relabel after a language switch is not a
// choice, so it is told apart by the label being all that changed.
let previous: Selection | null = null
selection.subscribe((sel) => {
  if (previous && !(sameTarget(previous, sel) && previous.label !== sel.label)) {
    attachmentsOpen.set(false)
  }
  previous = sel
})

function sameTarget(a: Selection, b: Selection): boolean {
  return JSON.stringify({ ...a, label: '' }) === JSON.stringify({ ...b, label: '' })
}

/** Leaves the browser and opens the message a file came with in its folder. */
export function revealAttachment(entry: AttachmentEntry): void {
  closeAttachments()
  revealMessage(entry.messageId, entry.accountId, entry.folderId, get(sidebar).data)
}
