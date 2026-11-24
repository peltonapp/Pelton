import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'sync', id: 'auto-sync-interval', labelKey: 'settingsPanel.label.autoSyncInterval', keywords: 'sync interval refresh check mail minutes' },
  { page: 'sync', id: 'sync-message-limit', labelKey: 'settingsPanel.label.syncMessageLimit', keywords: 'sync limit messages older initial download first' },
  { page: 'sync', id: 'sync-max-parallel', labelKey: 'settingsPanel.label.syncMaxParallel', keywords: 'parallel connections sync concurrent pool' },
  { page: 'sync', id: 'sync-full-reconcile', labelKey: 'settingsPanel.label.syncFullReconcileDays', keywords: 'full check reconcile verify days sync' },
  { page: 'sync', id: 'sync-auto-backfill', labelKey: 'settingsPanel.toggle.syncAutoBackfill', keywords: 'older mail backfill scroll load more' },
  { page: 'sync', id: 'verbose-sync', labelKey: 'settingsPanel.toggle.verboseSync', keywords: 'sync status mailbox name' },
  { page: 'sync', id: 'flag-color-sync', labelKey: 'settingsPanel.toggle.flagColorSync', keywords: 'flag color colour sync keywords' },
  { page: 'sync', id: 'download-offline', labelKey: 'settingsPanel.label.downloadOffline', keywords: 'download offline cache range date' },
  { page: 'sync', id: 'include-attachments', labelKey: 'settingsPanel.toggle.includeAttachments', keywords: 'offline download attachments' },
  { page: 'sync', id: 'low-power-mode', labelKey: 'settingsPanel.toggle.lowPowerMode', keywords: 'battery energy power saving' },
  { page: 'sync', id: 'log-to-file', labelKey: 'settingsPanel.toggle.logToFile', keywords: 'log logging debug file troubleshooting' },
  { page: 'sync', id: 'log-level', labelKey: 'settingsPanel.label.logLevel', keywords: 'log level debug info warn error' },
  { page: 'sync', id: 'log-message-metadata', labelKey: 'settingsPanel.toggle.logMessageMetadata', keywords: 'log subject sender date metadata' },
  { page: 'sync', id: 'crash-logs', labelKey: 'settingsPanel.toggle.crashLogs', keywords: 'crash report stack trace panic' },
  { page: 'sync', id: 'log-folder', labelKey: 'settingsPanel.label.logFolder', keywords: 'logs folder open delete size' },
]

export default entries
