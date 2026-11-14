<script lang="ts">
  // the Sync page: how often and how much mail is fetched, what is kept for
  // offline use, how the app behaves in the background, and local logs.
  import { onMount } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import DateTimePicker from '../../common/DateTimePicker.svelte'
  import StepSlider from '../../settings/StepSlider.svelte'
  import {
    prefs,
    setLowPowerMode,
    setAutoSyncInterval,
    setVerboseSync,
    setSyncMessageLimit,
    setSyncAutoBackfill,
    setFlagColorSync,
    setDownloadIncludeAttachments,
    setLogToFile,
    setLogLevel,
    setLogMessageMetadata,
    setCrashLogs,
  } from '../../../stores/prefs'
  import { downloadRange, cancelDownload, getLogStatus, openLogFolder, deleteLogs } from '../../../lib/api'
  import { downloadProgress } from '../../../stores/progress'
  import { toastInfo, toastError, errorMessage } from '../../../stores/toast'
  import { isMac, t } from '../../../lib/i18n'
  import type { LogLevel, LogStatus } from '../../../lib/types'
  import '../../../theme/settings2-sections-system.css'

  // auto-sync interval presets, in seconds (0 = off).
  $: autoSyncOptions = [
    { key: '0', label: $t('settingsPanel.unit.off') },
    { key: '30', label: $t('settingsPanel.unit.s30') },
    { key: '300', label: $t('settingsPanel.unit.m5') },
    { key: '900', label: $t('settingsPanel.unit.m15') },
    { key: '1800', label: $t('settingsPanel.unit.m30') },
    { key: '3600', label: $t('settingsPanel.unit.h1') },
    { key: '21600', label: $t('settingsPanel.unit.h6') },
    { key: '43200', label: $t('settingsPanel.unit.h12') },
    { key: '86400', label: $t('settingsPanel.unit.h24') },
  ]

  // how many of a folder's newest messages a first sync fetches.
  $: syncLimitOptions = [
    { key: '50', label: '50' },
    { key: '100', label: '100' },
    { key: '250', label: '250' },
    { key: '500', label: '500' },
    { key: '1000', label: '1000' },
    { key: '0', label: $t('settingsPanel.syncLimit.all') },
  ]

  // offline range download state. the start date defaults to one year ago.
  let downloadStart = defaultDownloadStart()
  function defaultDownloadStart(): string {
    const d = new Date()
    d.setFullYear(d.getFullYear() - 1)
    return d.toISOString().slice(0, 10)
  }

  $: downloadPresets = [
    { key: '1w', label: $t('settingsPanel.preset.lastWeek'), days: 7 },
    { key: '1m', label: $t('settingsPanel.preset.lastMonth'), days: 30 },
    { key: '3m', label: $t('settingsPanel.preset.last3Months'), days: 90 },
    { key: '6m', label: $t('settingsPanel.preset.last6Months'), days: 180 },
    { key: '1y', label: $t('settingsPanel.preset.lastYear'), days: 365 },
    { key: 'all', label: $t('settingsPanel.preset.allTime'), days: 0 },
  ]
  function applyPreset(days: number): void {
    const d = new Date()
    if (days === 0) {
      d.setFullYear(d.getFullYear() - 20) // effectively "everything"
    } else {
      d.setDate(d.getDate() - days)
    }
    downloadStart = d.toISOString().slice(0, 10)
  }

  async function startDownload(): Promise<void> {
    if (!downloadStart) {
      return
    }
    try {
      await downloadRange(downloadStart, $prefs.downloadIncludeAttachments)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // the log status comes from the backend rather than the prefs store because
  // it describes the disk (folder, size) and because --debug can have logging
  // on with the setting off.
  let logStatus: LogStatus | null = null
  let confirmDeleteLogs = false

  $: logLevelOptions = [
    { key: 'debug', label: $t('settingsPanel.logLevel.debug') },
    { key: 'info', label: $t('settingsPanel.logLevel.info') },
    { key: 'warn', label: $t('settingsPanel.logLevel.warn') },
    { key: 'error', label: $t('settingsPanel.logLevel.error') },
  ]

  async function refreshLogStatus(): Promise<void> {
    try {
      logStatus = await getLogStatus()
    } catch {
      logStatus = null
    }
  }

  onMount(() => {
    void refreshLogStatus()
  })

  function onLogToFile(checked: boolean): void {
    setLogToFile(checked)
    // the backend opens or closes the file as part of saving the setting, so
    // give it a moment before reading the state back.
    setTimeout(refreshLogStatus, 200)
  }

  async function onOpenLogFolder(): Promise<void> {
    try {
      await openLogFolder()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onDeleteLogs(): Promise<void> {
    confirmDeleteLogs = false
    try {
      await deleteLogs()
      await refreshLogStatus()
      toastInfo($t('settingsPanel.logs.deleted'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) {
      return `${bytes} B`
    }
    if (bytes < 1024 * 1024) {
      return `${Math.round(bytes / 1024)} KB`
    }
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }
</script>

<Card title={$t('settings2.sync.sync')}>
  <Row id="auto-sync-interval" label={$t('settingsPanel.label.autoSyncInterval')} hint={$t('settingsPanel.hint.autoSyncDetail')} stacked>
    <div class="s2-slider">
      <StepSlider
        label={$t('settingsPanel.label.autoSyncInterval')}
        value={String($prefs.autoSyncIntervalSeconds)}
        options={autoSyncOptions}
        on:change={(e) => setAutoSyncInterval(Number(e.detail))}
      />
    </div>
  </Row>
  <Row id="sync-message-limit" label={$t('settingsPanel.label.syncMessageLimit')} hint={$t('settingsPanel.hint.syncMessageLimit')} stacked>
    <div class="s2-slider">
      <StepSlider
        label={$t('settingsPanel.label.syncMessageLimit')}
        value={String($prefs.syncMessageLimit)}
        options={syncLimitOptions}
        on:change={(e) => setSyncMessageLimit(Number(e.detail))}
      />
    </div>
  </Row>
  <Row id="sync-auto-backfill" label={$t('settingsPanel.toggle.syncAutoBackfill')} hint={$t('settingsPanel.hint.syncAutoBackfillDetail')}>
    <ToggleSwitch
      checked={$prefs.syncAutoBackfill}
      label={$t('settingsPanel.toggle.syncAutoBackfill')}
      on:change={(e) => setSyncAutoBackfill(e.detail)}
    />
  </Row>
  <Row id="verbose-sync" label={$t('settingsPanel.toggle.verboseSync')} hint={$t('settingsPanel.hint.verboseSyncDetail')}>
    <ToggleSwitch checked={$prefs.verboseSync} label={$t('settingsPanel.toggle.verboseSync')} on:change={(e) => setVerboseSync(e.detail)} />
  </Row>
</Card>

<Card title={$t('settingsPanel.category.offline')}>
  <Row id="flag-color-sync" label={$t('settingsPanel.toggle.flagColorSync')} hint={$t('settingsPanel.hint.flagColorSync')}>
    <ToggleSwitch checked={$prefs.flagColorSync} label={$t('settingsPanel.toggle.flagColorSync')} on:change={(e) => setFlagColorSync(e.detail)} />
  </Row>
  <Row id="download-offline" label={$t('settingsPanel.label.downloadOffline')} hint={$t('settingsPanel.hint.downloadOffline')} stacked>
    <div class="stack">
      <div class="presets">
        {#each downloadPresets as p (p.key)}
          <button type="button" class="s2-btn" on:click={() => applyPreset(p.days)}>{p.label}</button>
        {/each}
      </div>
      <div class="download">
        <DateTimePicker mode="date" bind:value={downloadStart} />
        {#if $downloadProgress && $downloadProgress.running}
          <button type="button" class="s2-btn" on:click={() => cancelDownload()}>
            {$t('settingsPanel.button.cancelDownload')}
          </button>
        {:else}
          <button type="button" class="s2-btn" on:click={startDownload}>
            {$t('settingsPanel.button.download')}
          </button>
        {/if}
      </div>
    </div>
  </Row>
  <Row id="include-attachments" label={$t('settingsPanel.toggle.includeAttachments')} hint={$t('settingsPanel.hint.includeAttachments')}>
    <ToggleSwitch
      checked={$prefs.downloadIncludeAttachments}
      label={$t('settingsPanel.toggle.includeAttachments')}
      on:change={(e) => setDownloadIncludeAttachments(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('settings2.sync.power')}>
  <Row id="low-power-mode" label={$t('settingsPanel.toggle.lowPowerMode')} hint={$t('settingsPanel.hint.lowPowerDetail')}>
    <ToggleSwitch checked={$prefs.lowPowerMode} label={$t('settingsPanel.toggle.lowPowerMode')} on:change={(e) => setLowPowerMode(e.detail)} />
  </Row>
</Card>

<Card title={$t('settingsPanel.category.logs')} description={$t('settingsPanel.hint.logsIntro')}>
  <Row id="log-to-file" label={$t('settingsPanel.toggle.logToFile')} hint={$t('settingsPanel.hint.logToFile')}>
    <ToggleSwitch checked={$prefs.logToFile} label={$t('settingsPanel.toggle.logToFile')} on:change={(e) => onLogToFile(e.detail)} />
  </Row>
  {#if logStatus?.forced}
    <p class="note">{$t('settingsPanel.hint.logForced')}</p>
  {/if}
  {#if $prefs.logToFile}
    <Row id="log-level" label={$t('settingsPanel.label.logLevel')} hint={$t('settingsPanel.hint.logLevel')}>
      <Segmented
        label={$t('settingsPanel.label.logLevel')}
        value={$prefs.logLevel}
        options={logLevelOptions}
        on:change={(e) => setLogLevel(e.detail as LogLevel)}
      />
    </Row>
    <Row id="log-message-metadata" label={$t('settingsPanel.toggle.logMessageMetadata')} hint={$t('settingsPanel.hint.logMessageMetadataDetail')}>
      <ToggleSwitch
        checked={$prefs.logMessageMetadata}
        label={$t('settingsPanel.toggle.logMessageMetadata')}
        on:change={(e) => setLogMessageMetadata(e.detail)}
      />
    </Row>
  {/if}
  <Row id="crash-logs" label={$t('settingsPanel.toggle.crashLogs')} hint={$t('settingsPanel.hint.crashLogs')}>
    <ToggleSwitch checked={$prefs.crashLogs} label={$t('settingsPanel.toggle.crashLogs')} on:change={(e) => setCrashLogs(e.detail)} />
  </Row>
  {#if logStatus}
    <Row
      id="log-folder"
      label={$t('settingsPanel.label.logFolder')}
      hint={$t('settingsPanel.hint.logSize').replace('{size}', formatBytes(logStatus.sizeBytes))}
      stacked
    >
      <div class="stack">
        <code class="path">{logStatus.dir}</code>
        <div class="actions">
          <button type="button" class="s2-btn" on:click={onOpenLogFolder}>
            {$t('settingsPanel.button.openLogFolder')}
          </button>
          <button type="button" class="s2-btn" disabled={logStatus.sizeBytes === 0} on:click={() => (confirmDeleteLogs = true)}>
            {$t('settingsPanel.button.deleteLogs')}
          </button>
        </div>
      </div>
    </Row>
    {#if confirmDeleteLogs}
      <div class="s2-warn">
        <p>{$t('settingsPanel.warn.deleteLogs')}</p>
        <div class="actions">
          <button type="button" class="s2-btn" on:click={() => (confirmDeleteLogs = false)}>{$t('settingsPanel.button.cancel')}</button>
          <button type="button" class="s2-btn danger" on:click={onDeleteLogs}>{$t('settingsPanel.button.deleteLogs')}</button>
        </div>
      </div>
    {/if}
  {/if}
</Card>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .presets,
  .download,
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .path {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
    user-select: text;
    -webkit-user-select: text;
  }

  .note {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }
</style>
