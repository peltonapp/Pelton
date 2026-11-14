// text recognition for scanned pdf pages. "native" is the platform's own
// recognizer (Vision on macOS, through the backend); "tesseract" is the bundled
// tesseract.js, which runs in a worker with every file served by the app, so
// nothing leaves the device either way.
import type { Worker as TesseractWorker } from 'tesseract.js'
import { nativeOCRAvailable, readOCRLanguage, recognizeTextNative, type OCRWord } from './api'
import { bundledOCRCodes, bundledOCRLanguages } from './ocrlanguages'

export type { OCRWord }
export type OCREngine = 'native' | 'tesseract'

let nativeAvailable: Promise<boolean> | null = null

// hasNativeOCR reports, once per session, whether a native recognizer exists.
export function hasNativeOCR(): Promise<boolean> {
  nativeAvailable ??= nativeOCRAvailable().catch(() => false)
  return nativeAvailable
}

// ocrEngine resolves the preference against what this platform has.
export async function ocrEngine(pref: string): Promise<OCREngine> {
  return pref !== 'tesseract' && (await hasNativeOCR()) ? 'native' : 'tesseract'
}

// ocrLanguageList is the tesseract languages to read with: the chosen ones,
// or the ui language plus English.
export function ocrLanguageList(setting: string, uiLocale: string): string[] {
  const chosen = setting.split('+').map((s) => s.trim()).filter(Boolean)
  if (chosen.length > 0) {
    return chosen
  }
  const code = bundledOCRLanguages[uiLocale] ?? 'eng'
  return code === 'eng' ? ['eng'] : [code, 'eng']
}

// recognize reads the words on a rendered page.
export async function recognize(canvas: HTMLCanvasElement, engine: OCREngine, langs: string[]): Promise<OCRWord[]> {
  if (engine === 'native') {
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'))
    if (!blob) {
      return []
    }
    return recognizeTextNative(bytesToBase64(new Uint8Array(await blob.arrayBuffer())))
  }
  const worker = await tesseractWorker(langs)
  const { data } = await worker.recognize(canvas, {}, { blocks: true })
  const width = canvas.width
  const height = canvas.height
  const words: OCRWord[] = []
  let line = 0
  for (const block of data.blocks ?? []) {
    for (const para of block.paragraphs) {
      for (const l of para.lines) {
        for (const w of l.words) {
          const text = w.text.trim()
          if (!text) {
            continue
          }
          words.push({
            text,
            x: w.bbox.x0 / width,
            y: w.bbox.y0 / height,
            w: (w.bbox.x1 - w.bbox.x0) / width,
            h: (w.bbox.y1 - w.bbox.y0) / height,
            line,
          })
        }
        line++
      }
    }
  }
  scheduleIdle()
  return words
}

// one worker, kept for the language set it was made with and closed after a
// minute without work, since it holds the models in memory.
let worker: Promise<TesseractWorker> | null = null
let workerLangs = ''
let idleTimer: ReturnType<typeof setTimeout> | null = null
const idleMs = 60_000

function scheduleIdle(): void {
  if (idleTimer) {
    clearTimeout(idleTimer)
  }
  idleTimer = setTimeout(stopTesseract, idleMs)
}

export function stopTesseract(): void {
  const old = worker
  worker = null
  workerLangs = ''
  void old?.then((w) => w.terminate()).catch(() => {})
}

function tesseractWorker(langs: string[]): Promise<TesseractWorker> {
  if (idleTimer) {
    clearTimeout(idleTimer)
    idleTimer = null
  }
  const key = langs.join('+')
  if (worker && workerLangs === key) {
    return worker
  }
  stopTesseract()
  workerLangs = key
  const made = (async () => {
    const T = await import('tesseract.js')
    const url = (p: string) => new URL(p, document.baseURI).href
    // bundled languages load from the app by name; ones the user added come
    // from the backend as bytes.
    const langArgs = await Promise.all(
      langs.map(async (code) => (bundledOCRCodes.includes(code) ? code : { code, data: base64ToBytes(await readOCRLanguage(code)) })),
    )
    // the core is picked here rather than by tesseract.js, which would also
    // try a relaxed-simd build the app does not ship.
    const core = wasmSimd() ? 'tesseract-core-simd-lstm.wasm.js' : 'tesseract-core-lstm.wasm.js'
    // the typings take names or data objects, not a mix, but the worker
    // handles each entry on its own.
    return T.createWorker(langArgs as Parameters<typeof T.createWorker>[0], T.OEM.LSTM_ONLY, {
      workerPath: url('tesseract/worker.min.js'),
      corePath: url(`tesseract/core/${core}`),
      langPath: url('tesseract/lang'),
      workerBlobURL: false,
      cacheMethod: 'none',
      gzip: true,
    })
  })()
  worker = made
  made.catch(() => {
    if (worker === made) {
      worker = null
      workerLangs = ''
    }
  })
  return made
}

// wasmSimd reports whether the webview runs WebAssembly simd: a minimal module
// using one simd instruction either validates or does not.
function wasmSimd(): boolean {
  try {
    return WebAssembly.validate(
      new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0, 1, 5, 1, 96, 0, 1, 123, 3, 2, 1, 0, 10, 10, 1, 8, 0, 65, 0, 253, 15, 253, 98, 11]),
    )
  } catch {
    return false
  }
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    bin += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(bin)
}

function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {
    bytes[i] = bin.charCodeAt(i)
  }
  return bytes
}
