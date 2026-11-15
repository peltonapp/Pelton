// kinds.ts names and draws the file groups the attachments browser filters by,
// in the order its filter bar lists them.

import docIcon from '@phosphor-icons/core/assets/duotone/file-doc-duotone.svg?raw'
import imageIcon from '@phosphor-icons/core/assets/duotone/image-duotone.svg?raw'
import pdfIcon from '@phosphor-icons/core/assets/duotone/file-pdf-duotone.svg?raw'
import sheetIcon from '@phosphor-icons/core/assets/duotone/file-xls-duotone.svg?raw'
import archiveIcon from '@phosphor-icons/core/assets/duotone/file-zip-duotone.svg?raw'
import mediaIcon from '@phosphor-icons/core/assets/duotone/film-strip-duotone.svg?raw'
import fileIcon from '@phosphor-icons/core/assets/duotone/file-duotone.svg?raw'
import type { Attachment, AttachmentEntry, AttachmentKind } from '../../lib/types'

/** The groups in filter bar order. */
export const kindOrder: AttachmentKind[] = ['document', 'image', 'pdf', 'spreadsheet', 'archive', 'media', 'other']

/** Raw Phosphor svg per group. */
export const kindIcon: Record<AttachmentKind, string> = {
  document: docIcon,
  image: imageIcon,
  pdf: pdfIcon,
  spreadsheet: sheetIcon,
  archive: archiveIcon,
  media: mediaIcon,
  other: fileIcon,
}

/** Localization key of each group's name. */
export const kindLabelKey: Record<AttachmentKind, string> = {
  document: 'attachments.kind.document',
  image: 'attachments.kind.image',
  pdf: 'attachments.kind.pdf',
  spreadsheet: 'attachments.kind.spreadsheet',
  archive: 'attachments.kind.archive',
  media: 'attachments.kind.media',
  other: 'attachments.kind.other',
}

/** The reader's attachment shape of a browser entry, for the shared previewer. */
export function asAttachment(entry: AttachmentEntry): Attachment {
  return {
    id: entry.id,
    filename: entry.filename,
    contentType: entry.contentType,
    sizeBytes: entry.sizeBytes,
    inline: false,
  }
}
