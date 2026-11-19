// preview.ts drives the in-app attachment previewer. an attachment card opens it
// with the message id and the attachment; the modal (mounted once at the app
// root) fetches the bytes and renders pdf, images, or text/code/markdown.
// a file that is not a mail attachment, such as a chat file, brings its own
// read and save instead.

import { writable } from 'svelte/store'
import type { Attachment, AttachmentContent } from '../lib/types'

export interface PreviewTarget {
  messageId: number
  attachment: Attachment
  /** Identifies a file that is not a mail attachment; set with read. */
  key?: string
  /** Reads the file, in place of reading a mail attachment. */
  read?: () => Promise<AttachmentContent>
  /** Saves the file, in place of saving a mail attachment; resolves false when cancelled. */
  save?: () => Promise<boolean>
}

export const previewTarget = writable<PreviewTarget | null>(null)

export function openPreview(messageId: number, attachment: Attachment): void {
  previewTarget.set({ messageId, attachment })
}

/** Opens the previewer on a file that brings its own read and save. */
export function openCustomPreview(key: string, attachment: Attachment, read: () => Promise<AttachmentContent>, save: () => Promise<boolean>): void {
  previewTarget.set({ messageId: 0, attachment, key, read, save })
}

export function closePreview(): void {
  previewTarget.set(null)
}
