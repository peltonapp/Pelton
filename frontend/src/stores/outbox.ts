// outbox.ts tracks the durable send queue and the live sync state. it refetches
// when the backend emits outbox:changed and reflects sync:state so the ui can
// show sending, queued and failed messages plus a sync indicator.

import { writable } from 'svelte/store'
import type { OutboxRow } from '../lib/types'
import type { SyncPhase } from '../lib/syncstatus'
import { listOutbox, clearSentOutbox } from '../lib/api'
import { errorMessage, toastSuccess } from './toast'

export const outbox = writable<OutboxRow[]>([])

// syncing reflects whether a background or manual sync is currently running.
export const syncing = writable<boolean>(false)

// lastSynced holds the epoch ms of the last completed sync, or null if none yet
// this session. the status bar renders it as a relative time.
export const lastSynced = writable<number | null>(null)

// syncFolder holds the name of the mailbox currently being synced, for the
// verbose-sync status line (#128). empty when idle or between folders. it is
// fed by sync:progress events and cleared when a sync ends.
export const syncFolder = writable<string>('')

// syncServer holds the imap host and port that mailbox is coming from, and
// syncAccount the address it belongs to, shown beside the mailbox in the
// verbose status line so two accounts syncing at once are telling apart. Both
// are empty when idle or between folders.
export const syncServer = writable<string>('')
export const syncAccount = writable<string>('')

// syncPhase is stubs or bodies during a sync's first pass, verify while only
// the background folder check runs, else empty.
export const syncPhase = writable<SyncPhase>('')

// SyncCounts is how far the running sync has got, in message bodies. total is
// what the reconcile plans have asked for so far and grows as mailboxes open;
// 0 means nothing is known yet and the bar is indeterminate.
export interface SyncCounts {
  done: number
  total: number
  folderDone: number
  folderTotal: number
  foldersDone: number
  foldersTotal: number
}

export const emptySyncCounts: SyncCounts = {
  done: 0,
  total: 0,
  folderDone: 0,
  folderTotal: 0,
  foldersDone: 0,
  foldersTotal: 0,
}

// syncCounts drives the progress bar, and is reset when a run ends so a
// finished bar does not linger into the next one.
export const syncCounts = writable<SyncCounts>(emptySyncCounts)

// loadOutbox refetches the queue. it swallows errors into an empty list since the
// outbox view is secondary; a transient failure should not break the app. when
// the refetch reveals freshly-sent messages it shows a brief confirmation and
// prunes them so the queue does not keep completed rows around.
export async function loadOutbox(): Promise<void> {
  try {
    const rows = await listOutbox()
    const sent = rows.filter((r) => r.state === 'sent')
    outbox.set(rows.filter((r) => r.state !== 'sent'))
    if (sent.length > 0) {
      toastSuccess(sent.length === 1 ? 'Message sent.' : `${sent.length} messages sent.`)
      // prune the sent rows; the resulting outbox:changed event triggers another
      // load that simply finds them gone.
      void clearSentOutbox()
    }
  } catch (err) {
    // keep the previous contents; log to the console for diagnosis.
    console.error('load outbox failed:', errorMessage(err))
  }
}
