// outboxlog.ts holds the send activity log for the send activity window: what
// the backend has recorded this run, then each new line as it is pushed. It
// lives in memory only, like the backend's copy.

import { writable } from 'svelte/store'
import { readOutboxLog } from '../lib/api'
import { onOutboxLog } from '../lib/events'
import type { OutboxLogEntry } from '../lib/types'

// outboxLogLimit matches what the backend keeps.
const outboxLogLimit = 400

/** The send activity log, oldest line first. */
export const outboxLog = writable<OutboxLogEntry[]>([])

/** appendLogEntries adds lines the log does not have yet, in order, trimmed to the limit. */
export function appendLogEntries(list: OutboxLogEntry[], add: OutboxLogEntry[]): OutboxLogEntry[] {
  const last = list.length > 0 ? list[list.length - 1].seq : 0
  const fresh = add.filter((e) => e.seq > last).sort((a, b) => a.seq - b.seq)
  if (fresh.length === 0) {
    return list
  }
  return [...list, ...fresh].slice(-outboxLogLimit)
}

/**
 * followOutboxLog fills the log from the backend and keeps it current. Lines
 * pushed while the first read is on its way are kept. Returns the unsubscribe.
 */
export function followOutboxLog(): () => void {
  const early: OutboxLogEntry[] = []
  let loaded = false
  const stop = onOutboxLog((entry) => {
    if (loaded) {
      outboxLog.update((list) => appendLogEntries(list, [entry]))
    } else {
      early.push(entry)
    }
  })
  void readOutboxLog()
    .catch(() => [] as OutboxLogEntry[])
    .then((entries) => {
      loaded = true
      outboxLog.set(appendLogEntries(appendLogEntries([], entries), early))
    })
  return stop
}
