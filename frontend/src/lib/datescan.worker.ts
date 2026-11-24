// datescan.worker.ts reads the dates in a mail's text off the main thread, so
// a long message never holds up the reader while chrono works through it.

import { loadChrono } from './chronoload'
import { dateMentions } from './eventphrase'
import type { ScanReply, ScanRequest } from './datescan'

self.onmessage = async (event: MessageEvent<ScanRequest>) => {
  const req = event.data
  let reply: ScanReply
  try {
    const chrono = await loadChrono(req.lang)
    const found = dateMentions(req.text, { ref: new Date(req.ref), chrono, defaultMinutes: req.defaultMinutes })
    reply = { seq: req.seq, mentions: found.map((m) => ({ index: m.index, length: m.length, start: m.start.getTime(), end: m.end.getTime(), allDay: m.allDay })) }
  } catch (err) {
    reply = { seq: req.seq, mentions: [], error: String(err) }
  }
  ;(self as unknown as Worker).postMessage(reply)
}
