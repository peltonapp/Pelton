// thumbnails.ts draws the small pictures of the attachments browser's grid:
// an image scaled down, or the first page of a pdf. Nothing is read up front:
// a tile asks when it scrolls into view, the work runs two at a time, and only
// files already stored here are read, so the grid never reaches the server.
// The pictures are kept as small object urls, the oldest dropped past a cap.

import { readAttachment } from './api'
import type { AttachmentEntry } from './types'

const edge = 256
const maxImageBytes = 12 << 20
const maxPdfBytes = 20 << 20
const keep = 240
const parallel = 2

const cache = new Map<number, Promise<string | null>>()
const urls = new Map<number, string>()
const queue: (() => Promise<void>)[] = []
let running = 0

/** Reports whether a file can get a thumbnail without fetching anything. */
export function canThumbnail(entry: AttachmentEntry): boolean {
  if (!entry.downloaded) {
    return false
  }
  if (entry.kind === 'image') {
    return entry.sizeBytes <= maxImageBytes && !/svg/i.test(entry.contentType) && !/\.svg$/i.test(entry.filename)
  }
  return entry.kind === 'pdf' && entry.sizeBytes <= maxPdfBytes
}

/** Returns an object url for the file's thumbnail, or null when there is none. */
export function thumbnailFor(entry: AttachmentEntry): Promise<string | null> {
  if (!canThumbnail(entry)) {
    return Promise.resolve(null)
  }
  const known = cache.get(entry.id)
  if (known) {
    // touch it, so the cap drops what has not been looked at for longest.
    cache.delete(entry.id)
    cache.set(entry.id, known)
    return known
  }
  const pending = new Promise<string | null>((resolve) => {
    queue.push(async () => {
      try {
        resolve(await draw(entry))
      } catch {
        resolve(null)
      }
    })
    pump()
  })
  cache.set(entry.id, pending)
  void pending.then((url) => {
    if (url) {
      urls.set(entry.id, url)
    }
    trim()
  })
  return pending
}

function pump(): void {
  while (running < parallel && queue.length > 0) {
    const job = queue.shift()!
    running += 1
    void job().finally(() => {
      running -= 1
      pump()
    })
  }
}

function trim(): void {
  while (cache.size > keep) {
    const oldest = cache.keys().next().value as number
    cache.delete(oldest)
    const url = urls.get(oldest)
    if (url) {
      URL.revokeObjectURL(url)
      urls.delete(oldest)
    }
  }
}

/** Decodes base64 to bytes. */
export function base64ToBytes(b64: string): Uint8Array<ArrayBuffer> {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {
    bytes[i] = bin.charCodeAt(i)
  }
  return bytes
}

async function draw(entry: AttachmentEntry): Promise<string | null> {
  const content = await readAttachment(entry.messageId, entry.id)
  if (!content.data) {
    return null
  }
  const bytes = base64ToBytes(content.data)
  const canvas = entry.kind === 'pdf' ? await pdfPage(bytes) : await scaledImage(bytes, content.contentType)
  if (!canvas) {
    return null
  }
  const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'))
  return blob ? URL.createObjectURL(blob) : null
}

async function scaledImage(bytes: Uint8Array<ArrayBuffer>, type: string): Promise<HTMLCanvasElement | null> {
  const bitmap = await createImageBitmap(new Blob([bytes], { type: type || 'application/octet-stream' }))
  const ratio = Math.min(1, edge / Math.max(bitmap.width, bitmap.height))
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(bitmap.width * ratio))
  canvas.height = Math.max(1, Math.round(bitmap.height * ratio))
  canvas.getContext('2d')?.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
  bitmap.close()
  return canvas
}

async function pdfPage(bytes: Uint8Array<ArrayBuffer>): Promise<HTMLCanvasElement | null> {
  const pdfjs = await import('pdfjs-dist/legacy/build/pdf.mjs')
  const worker = await import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url')
  pdfjs.GlobalWorkerOptions.workerSrc = worker.default
  const base = (dir: string) => new URL(`pdfjs/${dir}/`, document.baseURI).href
  const task = pdfjs.getDocument({
    data: bytes,
    cMapUrl: base('cmaps'),
    cMapPacked: true,
    standardFontDataUrl: base('standard_fonts'),
    wasmUrl: base('wasm'),
    enableXfa: false,
  })
  try {
    const doc = await task.promise
    const page = await doc.getPage(1)
    const unit = page.getViewport({ scale: 1 })
    const viewport = page.getViewport({ scale: edge / Math.max(unit.width, unit.height) })
    const canvas = document.createElement('canvas')
    canvas.width = Math.ceil(viewport.width)
    canvas.height = Math.ceil(viewport.height)
    await page.render({ canvas, viewport }).promise
    return canvas
  } finally {
    await task.destroy()
  }
}
