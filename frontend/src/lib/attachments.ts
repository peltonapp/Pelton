// attachments.ts reads files picked or dropped into a composer. Files cross the
// bindings boundary as base64 strings, so each one is read whole, reporting
// progress for the large ones.

import type { ComposeAttachment } from './types'

/**
 * readAsBase64 returns a file's bytes as base64, without the data-url prefix,
 * calling progress with the share read so far (0 to 100).
 */
export function readAsBase64(file: File, progress?: (percent: number) => void): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onprogress = (e) => {
      if (e.lengthComputable) {
        progress?.(Math.round((e.loaded / e.total) * 100))
      }
    }
    reader.onload = () => {
      const result = String(reader.result)
      const comma = result.indexOf(',')
      resolve(comma >= 0 ? result.slice(comma + 1) : result)
    }
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

/** readAttachment reads a file into the shape a compose session carries. */
export async function readAttachment(file: File, progress?: (percent: number) => void): Promise<ComposeAttachment> {
  const contentType = file.type || 'application/octet-stream'
  const contentBase64 = await readAsBase64(file, progress)
  return { filename: file.name, contentType, contentBase64, inline: false, contentId: '' }
}

/** attachmentSize is the decoded size of an attachment in bytes. */
export function attachmentSize(att: Pick<ComposeAttachment, 'contentBase64'>): number {
  return att.contentBase64.length * 0.75
}
