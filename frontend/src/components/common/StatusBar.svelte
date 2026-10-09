<script lang="ts">
  // the bottom status line. the left side surfaces the durable send queue: a
  // clickable "N sending" that opens the outbox panel, plus a failed count. the
  // right side shows live sync state and the last-synced relative time. it is the
  // honest, always-visible window into background activity.
  import { onDestroy, onMount } from 'svelte'
  import { IconSend, IconAlertTriangle, IconRefresh, IconCheck, IconDownload, IconBatteryEco, IconX, IconBug, IconWifiOff } from '@tabler/icons-svelte'
  import { outbox, syncing, lastSynced, syncFolder, syncServer, syncAccount, syncCounts, syncPhase } from '../../stores/outbox'
  import { buildSyncLabel } from '../../lib/syncstatus'
  import { online } from '../../stores/network'
  import { failedSyncs, showSyncFailure } from '../../stores/syncfailures'
  import { downloadProgress, attachmentProgress } from '../../stores/progress'
  import { formatRelative } from '../../lib/format'
  import { cancelDownload, isDevMode, isNightly } from '../../lib/api'
  import ProfileChip from './ProfileChip.svelte'
  import AgentProposals from './AgentProposals.svelte'
  import { profiles, currentProfile } from '../../stores/profiles'
  import { prefs, setLowPowerMode } from '../../stores/prefs'
  import OutboxPanel from './OutboxPanel.svelte'
  import { devToolsAvailable } from '../../stores/devoverlays'
  import { t } from '../../lib/i18n'

  // devMode is read once at startup: it's fixed for the lifetime of the
  // process (set by the PELTON_DEV env var a dev run launches with), not
  // something that can change while the app is open.
  let devMode = false
  // nightly is fixed at build time, so it is read once alongside devMode. it
  // stays on screen for the whole session: the launch dialog is dismissed, this
  // is the reminder that outlives it.
  let nightly = false
  onMount(async () => {
    devMode = await isDevMode().catch(() => false)
    nightly = await isNightly().catch(() => false)
  })

  // format an eta in seconds as m:ss (or just seconds under a minute).
  function formatEta(sec: number): string {
    if (sec <= 0) {
      return ''
    }
    if (sec < 60) {
      return `${sec}s`
    }
    const m = Math.floor(sec / 60)
    const s = sec % 60
    return `${m}:${String(s).padStart(2, '0')}`
  }

  // attachment percent from bytes, guarding a zero total.
  $: attPercent =
    $attachmentProgress && $attachmentProgress.bytesTotal > 0
      ? Math.round(($attachmentProgress.bytesDone / $attachmentProgress.bytesTotal) * 100)
      : 0

  // the sync line. Verbose names the mailbox, and the account and server behind
  // it, which is the difference between two identical "Syncing INBOX" lines and
  // knowing which mailbox is the slow one.
  $: syncLabel = buildSyncLabel(
    $prefs.verboseSync,
    $syncPhase,
    $syncFolder,
    $syncAccount,
    $syncServer,
    $syncCounts,
    $t,
  )

  // a total of 0 means no folder has been reconciled yet, so there is nothing
  // honest to draw a proportion from and the bar sweeps instead.
  $: determinate = $syncCounts.total > 0
  $: percent = determinate ? Math.min(100, Math.round(($syncCounts.done / $syncCounts.total) * 100)) : 0

  let panelOpen = false
  // the Developer menu hanging off the DEV badge. Never persisted: the overlays
  // are scratch tools and every launch starts with the menu shut.
  let devMenuOpen = false

  $: pending = $outbox.filter((r) => r.state === 'queued' || r.state === 'sending')
  $: failed = $outbox.filter((r) => r.state === 'failed')

  // re-render the relative time on a slow tick so "2m ago" stays current. the
  // tick variable is referenced in the reactive label so it recomputes.
  let tick = Date.now()
  const timer = setInterval(() => (tick = Date.now()), 30000)
  onDestroy(() => clearInterval(timer))
  // one mailbox is named; more than one is counted, since the names would not
  // fit and the dialog lists them anyway.
  $: failedLabel =
    $failedSyncs.length === 1
      ? $t('common.statusBar.syncFailedOne').replace('{mailbox}', $failedSyncs[0].email)
      : $t('common.statusBar.syncFailedMany').replace('{n}', String($failedSyncs.length))

  $: syncedLabel = $lastSynced ? relativeAt($lastSynced, tick, $t) : ''
  function relativeAt(ts: number, _tick: number, translate: (key: string) => string): string {
    return formatRelative(ts, translate)
  }
</script>

<footer class="statusbar">
  <div class="left">
    {#if nightly}
      <span class="dev-badge nightly-badge" title={$t('common.statusBar.nightlyTitle')}>
        <IconAlertTriangle size={13} stroke={1.8} />
        {$t('common.statusBar.nightly')}
      </span>
    {/if}
    <!-- the badge is a button whenever the overlays are available, since it is
         the only thing on screen that says they exist (#188). A PELTON_DEVTOOLS
         run has the overlays without the separate data directory, so it gets the
         badge too, with a title that says which of the two it means. -->
    {#if devMode || $devToolsAvailable}
      <div class="dev-wrap">
        {#if $devToolsAvailable}
          <button
            type="button"
            class="dev-badge clickable"
            aria-expanded={devMenuOpen}
            title={devMode ? $t('common.statusBar.devModeTitle') : $t('common.statusBar.devToolsTitle')}
            on:click={() => (devMenuOpen = !devMenuOpen)}
          >
            <IconBug size={13} stroke={1.8} />
            {$t('common.statusBar.devMode')}
          </button>
        {:else}
          <span class="dev-badge" title={$t('common.statusBar.devModeTitle')}>
            <IconBug size={13} stroke={1.8} />
            {$t('common.statusBar.devMode')}
          </span>
        {/if}

        {#if devMenuOpen}
          <div class="popover dev-popover">
            {#await import('../dev/DevMenu.svelte') then m}
              <svelte:component this={m.default} on:close={() => (devMenuOpen = false)} />
            {/await}
          </div>
        {/if}
      </div>
    {/if}
    <!-- which profile you are writing from. Only once there is more than one:
         an install with a single profile has no question to answer. -->
    {#if $profiles.length > 1 && $currentProfile}
      <AgentProposals />
      <ProfileChip />
    {/if}
    {#if pending.length > 0 || failed.length > 0}
      <button type="button" class="outbox-btn" class:has-failed={failed.length > 0} on:click={() => (panelOpen = !panelOpen)}>
        {#if pending.length > 0}
          <IconSend size={13} stroke={1.7} />
          <span>{pending.length} {$t('common.outbox.sendingSuffix')}</span>
        {/if}
        {#if failed.length > 0}
          <IconAlertTriangle size={13} stroke={1.7} class="fail-icon" />
          <span class="fail">{failed.length} {$t('common.outbox.failedSuffix')}</span>
        {/if}
      </button>
    {/if}

    {#if panelOpen}
      <div class="popover">
        <OutboxPanel on:close={() => (panelOpen = false)} />
      </div>
    {/if}
  </div>

  <div class="center">
    {#if $downloadProgress}
      <div class="progress" title={$downloadProgress.label}>
        <IconDownload size={12} stroke={1.7} />
        <span class="p-label">
          {#if $downloadProgress.error}
            {$t('common.statusBar.downloadFailed')}
          {:else if $downloadProgress.total > 0}
            {$downloadProgress.done}/{$downloadProgress.total}
          {:else}
            {$downloadProgress.label || $t('common.statusBar.downloading')}
          {/if}
        </span>
        {#if $downloadProgress.total > 0 && !$downloadProgress.error}
          <span class="bar"><span class="fill" style={`width:${$downloadProgress.percent}%`}></span></span>
          <span class="p-num">{$downloadProgress.percent}%</span>
          {#if $downloadProgress.etaSeconds > 0}
            <span class="p-eta">~{formatEta($downloadProgress.etaSeconds)}</span>
          {/if}
        {/if}
        {#if $downloadProgress.running}
          <button
            type="button"
            class="cancel-dl"
            aria-label={$t('common.statusBar.cancelDownload')}
            title={$t('common.statusBar.cancelDownload')}
            on:click={() => cancelDownload()}
          >
            <IconX size={12} stroke={2} />
          </button>
        {/if}
      </div>
    {:else if $attachmentProgress}
      <div class="progress">
        <IconDownload size={12} stroke={1.7} />
        <span class="p-label">
          {#if $attachmentProgress.filesTotal > 1}
            {$t('common.statusBar.saving')} {$attachmentProgress.filesDone + 1}/{$attachmentProgress.filesTotal}
          {:else}
            {$t('common.statusBar.saving')}
          {/if}
        </span>
        <span class="bar"><span class="fill" style={`width:${attPercent}%`}></span></span>
      </div>
    {/if}
  </div>

  <div class="right">
    {#if !$online}
      <span class="offline" title={$t('common.network.offlineTitle')}>
        <IconWifiOff size={13} stroke={1.7} />
        {$t('common.network.offline')}
      </span>
    {/if}
    {#if $prefs.lowPowerMode}
      <button
        type="button"
        class="low-power"
        title={$t('common.statusBar.lowPowerTitle')}
        on:click={() => setLowPowerMode(false)}
      >
        <IconBatteryEco size={13} stroke={1.7} />
        {$t('common.statusBar.lowPower')}
      </button>
    {/if}
    {#if $syncing}
      <span class="sync syncing">
        <IconRefresh size={13} stroke={1.7} class="spin" />
        <span class="sync-text">{syncLabel}</span>
        {#if $prefs.syncProgressBar}
          <!-- the bar sits after the text at a fixed width, so a label that
               grows from "Syncing" to a mailbox name and a count does not shove
               it sideways while you are watching it. -->
          <span
            class="bar"
            class:indeterminate={!determinate}
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={determinate ? $syncCounts.total : undefined}
            aria-valuenow={determinate ? $syncCounts.done : undefined}
            aria-label={$t('common.statusBar.syncProgress')}
          >
            <span class="bar-fill" style={determinate ? `width:${percent}%` : ''}></span>
          </span>
          {#if determinate && $syncPhase !== 'bodies'}
            <span class="counts">{$syncCounts.done.toLocaleString()} / {$syncCounts.total.toLocaleString()}</span>
          {/if}
        {/if}
      </span>
    {:else if $failedSyncs.length > 0}
      <!-- a run that failed one mailbox out of several used to report a clean
           sync, which is how a mailbox goes quiet for weeks without anyone
           noticing (#322). -->
      <button type="button" class="sync sync-failed" on:click={() => showSyncFailure($failedSyncs[0])}>
        <IconAlertTriangle size={13} stroke={1.8} />
        {failedLabel}
      </button>
    {:else if $syncPhase === 'verify'}
      <!-- the background folder check is not a sync the user waits on: no
           spinner and no bar, just where it has got to. -->
      <span class="sync">
        <IconRefresh size={13} stroke={1.7} />
        <span class="sync-text">{syncLabel}</span>
      </span>
    {:else if $lastSynced}
      <span class="sync">
        <IconCheck size={13} stroke={1.7} />
        {$t('common.statusBar.synced')} {syncedLabel}
      </span>
    {/if}
  </div>
</footer>

{#if panelOpen}
  <!-- click-away closes the popover. -->
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="scrim" on:click={() => (panelOpen = false)}></div>
{/if}

{#if devMenuOpen}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="scrim" on:click={() => (devMenuOpen = false)}></div>
{/if}

<style>
  .statusbar {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    height: 26px;
    padding: 0 var(--space-4);
    background: var(--surface-sunken);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .left,
  .right {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
  }
  .right {
    justify-content: flex-end;
  }

  .center {
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .progress {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-secondary);
    white-space: nowrap;
  }

  .bar {
    width: 120px;
    height: 5px;
    border-radius: 999px;
    background: var(--surface-hover);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    background: var(--accent);
    border-radius: 999px;
    transition: width 0.2s ease;
  }
  .p-num {
    font-variant-numeric: tabular-nums;
    color: var(--text-primary);
  }
  .p-eta {
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .cancel-dl {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: 1px;
    border-radius: var(--radius-control);
  }
  .cancel-dl:hover {
    background: var(--surface-hover);
    color: var(--danger);
  }

  .outbox-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
  }

  .outbox-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .outbox-btn .fail {
    color: var(--danger);
  }

  .outbox-btn :global(.fail-icon) {
    color: var(--danger);
  }

  .popover {
    position: absolute;
    bottom: calc(100% + 6px);
    inset-inline-start: var(--space-3);
    z-index: 90;
  }

  .scrim {
    position: fixed;
    inset: 0;
    z-index: 80;
  }

  .offline {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--danger, var(--text-secondary));
    font-weight: var(--fw-semibold);
  }

  .low-power {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    border: none;
    background: transparent;
    color: var(--warning, var(--text-secondary));
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
  }

  .dev-badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    background: var(--danger-bg, var(--warning-bg, var(--surface-sunken)));
    color: var(--danger, var(--warning));
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    letter-spacing: 0.02em;
    /* the button form has to look identical to the span form. */
    border: none;
    font-family: inherit;
  }

  .dev-badge.clickable {
    cursor: var(--cursor-action);
  }
  .dev-badge.clickable:hover {
    filter: brightness(1.15);
  }

  /* the badge sits in the middle of the row, so its menu is positioned against
     the badge rather than against the bar's left edge like the outbox panel. */
  .dev-wrap {
    position: relative;
    display: inline-flex;
  }

  .dev-popover {
    left: 0;
  }

  /* the nightly marker takes the badge shape but the purple of the nightly
     logo, so it reads as a build channel rather than as an error. */
  .nightly-badge {
    background: var(--nightly-bg);
    color: var(--nightly);
  }

  .low-power:hover {
    background: var(--surface-hover);
  }

  .sync {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .sync.syncing {
    color: var(--text-secondary);
  }

  .sync-failed {
    padding: 0;
    font: inherit;
    color: var(--danger);
    background: transparent;
    border: none;
    cursor: var(--cursor-action);
  }
  .sync-failed:hover {
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  /* the label is the only part allowed to change width; the bar and the count
     keep their own space so nothing shifts as a sync runs. */
  .sync-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 46ch;
  }

  .bar {
    position: relative;
    flex-shrink: 0;
    width: 90px;
    height: 4px;
    overflow: hidden;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .bar-fill {
    display: block;
    height: 100%;
    width: 0;
    border-radius: inherit;
    background: var(--accent);
    transition: width 200ms linear;
  }

  /* nothing to be proportional about yet, so it sweeps rather than sitting at
     zero and looking stuck. */
  .bar.indeterminate .bar-fill {
    width: 40%;
    animation: sweep 1.1s ease-in-out infinite;
  }

  @keyframes sweep {
    0% {
      transform: translateX(-100%);
    }
    100% {
      transform: translateX(250%);
    }
  }

  .counts {
    flex-shrink: 0;
    font-variant-numeric: tabular-nums;
    color: var(--text-tertiary);
  }

  .sync :global(.spin) {
    /* svg transform-origin defaults differ across the webviews wails embeds
       per platform (webkit vs webview2); pin it explicitly so the icon spins
       in place instead of wobbling around an off-center pivot on some os. */
    transform-box: border-box;
    transform-origin: 50% 50%;
    animation: statusspin 0.8s linear infinite;
  }

  @keyframes statusspin {
    to {
      transform: rotate(-360deg);
    }
  }
</style>
