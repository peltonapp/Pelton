import { describe, expect, it } from 'vitest'
import { buildSyncLabel, type SyncPhase } from './syncstatus'
import { emptySyncCounts, type SyncCounts } from '../stores/outbox'

const catalogs: Record<string, string> = {
  'common.statusBar.syncPhaseBodies': 'Downloading messages: {done}/{total}',
  'common.statusBar.syncPhaseStubs': 'Listing messages…',
  'common.statusBar.syncPhaseStubsMailbox': 'Listing messages: {mailbox} ({foldersDone}/{foldersTotal})',
  'common.statusBar.syncPhaseVerify': 'Checking folders ({foldersDone}/{foldersTotal})',
  'common.statusBar.syncing': 'Syncing…',
}
const t = (key: string) => catalogs[key] ?? key

describe('buildSyncLabel', () => {
  it.each<{ name: string; verbose: boolean; phase: SyncPhase; folder: string; account: string; server: string; counts: Partial<SyncCounts>; want: string }>([
    { name: 'names the body phase with counts', verbose: false, phase: 'bodies', folder: 'Allegro', account: '', server: '',
      counts: { done: 502, total: 1576 }, want: 'Downloading messages: 502/1,576' },
    { name: 'uses stub phase copy while listing folders', verbose: true, phase: 'stubs', folder: 'INBOX', account: 'a@b.com', server: 'mail.example',
      counts: { foldersDone: 0, foldersTotal: 5 }, want: 'Listing messages: INBOX (1/5) · a@b.com · mail.example' },
    { name: 'names account and server on the verbose body line', verbose: true, phase: 'bodies', folder: 'INBOX', account: 'a@b.com', server: 'mail.example',
      counts: { done: 3, total: 10 }, want: 'Downloading messages: 3/10 · INBOX · a@b.com · mail.example' },
    { name: 'never counts past the last folder', verbose: true, phase: 'stubs', folder: 'Sent', account: '', server: '',
      counts: { foldersDone: 5, foldersTotal: 5 }, want: 'Listing messages: Sent (5/5)' },
    { name: 'names the background folder check with folder counts', verbose: false, phase: 'verify', folder: 'INBOX', account: '', server: '',
      counts: { foldersDone: 2, foldersTotal: 12 }, want: 'Checking folders (2/12)' },
  ])('$name', ({ verbose, phase, folder, account, server, counts, want }) => {
    expect(buildSyncLabel(verbose, phase, folder, account, server, { ...emptySyncCounts, ...counts }, t)).toBe(want)
  })
})
