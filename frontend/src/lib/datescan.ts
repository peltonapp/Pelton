// datescan.ts finds the dates in the text of the open mail. It hands the text
// to a worker so chrono runs off the main thread; where a worker cannot start,
// it reads the text on the main thread once the browser is idle. Either way
// the reader renders first and the marks follow.

import { loadChrono } from './chronoload'
import { dateMentions, type DateMention } from './eventphrase'

/** What the worker is asked. */
export interface ScanRequest {
  seq: number
  text: string
  /** When the mail was written, epoch milliseconds. */
  ref: number
  lang: string
  defaultMinutes?: number
}

/** What the worker answers, with times as epoch milliseconds. */
export interface ScanReply {
  seq: number
  mentions: { index: number; length: number; start: number; end: number; allDay: boolean }[]
  error?: string
}

/** The longest text read; a mail longer than this is read up to here. */
export const scanLimit = 60_000

let worker: Worker | null = null
let workerFailed = false
let seq = 0
const waiting = new Map<number, (reply: ScanReply | null) => void>()

function startWorker(): Worker | null {
  if (worker || workerFailed) {
    return worker
  }
  try {
    worker = new Worker(new URL('./datescan.worker.ts', import.meta.url), { type: 'module' })
  } catch {
    workerFailed = true
    return null
  }
  worker.onmessage = (event: MessageEvent<ScanReply>) => {
    const done = waiting.get(event.data.seq)
    waiting.delete(event.data.seq)
    done?.(event.data)
  }
  worker.onerror = () => {
    workerFailed = true
    worker?.terminate()
    worker = null
    for (const done of waiting.values()) {
      done(null)
    }
    waiting.clear()
  }
  return worker
}

function idle(): Promise<void> {
  return new Promise((resolve) => {
    const w = window as Window & { requestIdleCallback?: (cb: () => void, opts?: { timeout: number }) => number }
    if (w.requestIdleCallback) {
      w.requestIdleCallback(() => resolve(), { timeout: 400 })
    } else {
      setTimeout(resolve, 0)
    }
  })
}

async function scanHere(text: string, ref: Date, lang: string, defaultMinutes?: number): Promise<DateMention[]> {
  await idle()
  const chrono = await loadChrono(lang)
  return dateMentions(text, { ref, chrono, defaultMinutes })
}

/** Finds the dates a mail's text names, read against when it was written. */
export async function scanDates(text: string, ref: Date, lang: string, defaultMinutes?: number): Promise<DateMention[]> {
  const body = text.slice(0, scanLimit)
  if (body.trim() === '') {
    return []
  }
  const w = typeof Worker === 'undefined' ? null : startWorker()
  if (!w) {
    return scanHere(body, ref, lang, defaultMinutes)
  }
  const id = ++seq
  const reply = await new Promise<ScanReply | null>((resolve) => {
    waiting.set(id, resolve)
    w.postMessage({ seq: id, text: body, ref: ref.getTime(), lang, defaultMinutes } satisfies ScanRequest)
  })
  if (!reply || reply.error !== undefined) {
    return scanHere(body, ref, lang, defaultMinutes)
  }
  return reply.mentions.map((m) => ({ index: m.index, length: m.length, start: new Date(m.start), end: new Date(m.end), allDay: m.allDay }))
}
