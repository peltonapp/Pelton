// syncstatus builds the status-bar sync line, including stub vs body phases.

import type { SyncCounts } from '../stores/outbox'

/** Which half of a sync run the status line describes: listing stubs, downloading bodies, or the background full folder check (verify). Empty when idle. */
export type SyncPhase = '' | 'stubs' | 'bodies' | 'verify'

/** The status bar text for the running sync: the phase line when one applies, else plain "Syncing", or with verbose on and a folder, the mailbox/account/server line. */
export function buildSyncLabel(
  verbose: boolean,
  phase: SyncPhase,
  folder: string,
  account: string,
  server: string,
  counts: SyncCounts,
  t: (key: string) => string,
): string {
  if (phase === 'bodies' && counts.total > 0) {
    const line = t('common.statusBar.syncPhaseBodies')
      .replace('{done}', counts.done.toLocaleString())
      .replace('{total}', counts.total.toLocaleString())
    if (verbose && folder !== '') {
      return `${line} · ${folder}`
    }
    return line
  }
  if (phase === 'verify') {
    return t('common.statusBar.syncPhaseVerify')
      .replace('{foldersDone}', String(counts.foldersDone))
      .replace('{foldersTotal}', String(counts.foldersTotal))
  }
  if (phase === 'stubs') {
    if (verbose && folder !== '') {
      const foldersDone = counts.foldersDone + 1
      return t('common.statusBar.syncPhaseStubsMailbox')
        .replace('{mailbox}', folder)
        .replace('{foldersDone}', String(foldersDone))
        .replace('{foldersTotal}', String(counts.foldersTotal))
    }
    return t('common.statusBar.syncPhaseStubs')
  }
  if (!verbose || folder === '') {
    return t('common.statusBar.syncing')
  }
  if (account !== '' && server !== '') {
    return t('common.statusBar.syncingMailboxFull')
      .replace('{mailbox}', folder)
      .replace('{account}', account)
      .replace('{server}', server)
  }
  if (server !== '') {
    return t('common.statusBar.syncingMailboxOn').replace('{mailbox}', folder).replace('{server}', server)
  }
  return t('common.statusBar.syncingMailbox').replace('{mailbox}', folder)
}
