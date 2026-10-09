import { describe, expect, it } from 'vitest'
import { buildSyncLabel } from './syncstatus'
import { emptySyncCounts } from '../stores/outbox'

const catalogs: Record<string, string> = {
  'common.statusBar.syncPhaseBodies': 'Bodies: {done}/{total}',
  'common.statusBar.syncPhaseStubs': 'Listing stubs…',
  'common.statusBar.syncPhaseStubsMailbox': 'Stubs: {mailbox} ({foldersDone}/{foldersTotal})',
  'common.statusBar.syncPhaseVerify': 'Checking folders ({foldersDone}/{foldersTotal})',
  'common.statusBar.syncing': 'Syncing…',
}
const t = (key: string) => catalogs[key] ?? key

describe('buildSyncLabel', () => {
  it('names the body phase with counts', () => {
    const label = buildSyncLabel(
      false,
      'bodies',
      'Allegro',
      '',
      '',
      { ...emptySyncCounts, done: 502, total: 1576 },
      t,
    )
    expect(label).toBe('Bodies: 502/1,576')
  })

  it('uses stub phase copy while listing folders', () => {
    const label = buildSyncLabel(
      true,
      'stubs',
      'INBOX',
      'a@b.com',
      'mail.example',
      { ...emptySyncCounts, foldersDone: 0, foldersTotal: 5 },
      t,
    )
    expect(label).toBe('Stubs: INBOX (1/5)')
  })

  it('names the background folder check with folder counts', () => {
    const label = buildSyncLabel(
      false,
      'verify',
      'INBOX',
      '',
      '',
      { ...emptySyncCounts, foldersDone: 2, foldersTotal: 12 },
      t,
    )
    expect(label).toBe('Checking folders (2/12)')
  })
})
