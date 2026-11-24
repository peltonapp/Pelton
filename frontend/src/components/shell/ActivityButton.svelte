<script lang="ts">
  // the top bar's activity button: a pie while mail syncs or sends, a count
  // while messages wait in the outbox, and the outbox itself in a popover
  // under it. the popover lists the queue (OutboxList), ends on the sync
  // state, and can move the whole send activity into a window of its own.
  import trayIcon from '@phosphor-icons/core/assets/duotone/tray-arrow-up-duotone.svg?raw'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import windowIcon from '@phosphor-icons/core/assets/duotone/app-window-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import { onDestroy } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import OutboxList from './OutboxList.svelte'
  import { outbox, syncing, syncCounts, syncFolder, syncPhase, lastSynced } from '../../stores/outbox'
  import { buildSyncLabel } from '../../lib/syncstatus'
  import { errorMessage, toastError } from '../../stores/toast'
  import { isScheduled } from '../../lib/outboxactions'
  import { openOutboxWindow } from '../../lib/api'
  import { formatRelative, formatWeekdayTime, type TimeFormat } from '../../lib/format'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-ringing-duotone.svg?raw'
  import { followUps, dropFollowUp } from '../../stores/followups'
  import { prefs } from '../../stores/prefs'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'

  let button: HTMLButtonElement
  let open = false

  function when(iso: string): string {
    return formatWeekdayTime(new Date(iso), $prefs.timeFormat as TimeFormat)
  }

  // a slow tick keeps "scheduled" and "last synced" honest while open.
  let now = Date.now()
  const timer = setInterval(() => (now = Date.now()), 30000)
  onDestroy(() => clearInterval(timer))

  $: scheduled = $outbox.filter((row) => isScheduled(row, now))
  $: failed = $outbox.filter((row) => row.state === 'failed')
  $: sending = $outbox.filter((row) => row.state !== 'failed' && !isScheduled(row, now))
  $: waiting = sending.length + scheduled.length
  $: active = $syncing || sending.length > 0
  $: syncShare = $syncing && $syncCounts.total > 0 ? $syncCounts.done / $syncCounts.total : null
  $: summary = failed.length > 0
    ? $t('topbar.activity.failed').replace('{count}', String(failed.length))
    : $syncing
      ? $t('topbar.syncing')
      : waiting > 0
        ? $t('topbar.sending').replace('{count}', String(waiting))
        : $t('common.outboxPanel.title')
  // the phase of the run (listing, bodies, the background folder check) is
  // said the way the status bar says it.
  $: syncLine = $syncing || $syncPhase === 'verify'
    ? buildSyncLabel(true, $syncPhase, $syncFolder, '', '', $syncCounts, $t)
    : $lastSynced
      ? `${$t('common.statusBar.synced')} ${relative($lastSynced, now)}`
      : ''

  function relative(at: number, _now: number): string {
    return formatRelative(at, $t)
  }

  async function popOut(): Promise<void> {
    open = false
    try {
      await openOutboxWindow()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<button
  type="button"
  class="activity"
  class:failed={failed.length > 0}
  class:open
  bind:this={button}
  aria-haspopup="dialog"
  aria-expanded={open}
  aria-label={summary}
  use:tip={{ text: open ? '' : summary, side: 'bottom' }}
  on:click={() => (open = !open)}
>
  {#if active}
    <PieProgress value={syncShare} size={14} label={summary} />
  {:else if failed.length > 0}
    <Ph svg={warnIcon} size={15} />
  {:else}
    <Ph svg={trayIcon} size={15} />
  {/if}
  {#if failed.length > 0 || waiting > 0}
    <span class="count">{failed.length > 0 ? failed.length : waiting}</span>
  {/if}
</button>

{#if open}
  <AnchoredPopover anchor={button} side="below" align="end" label={$t('common.outboxPanel.title')} on:close={() => (open = false)}>
    <div class="pop">
      <header>
        <span class="title">{$t('common.outboxPanel.title')}</span>
        <span class="tools">
          <button type="button" class="c2-tool compact" aria-label={$t('outbox.window.open')} use:tip={{ text: $t('outbox.window.open') }} on:click={popOut}>
            <Ph svg={windowIcon} size={14} />
          </button>
          <button type="button" class="c2-tool compact" aria-label={$t('common.outboxPanel.close')} on:click={() => (open = false)}>
            <Ph svg={xIcon} size={13} />
          </button>
        </span>
      </header>

      <div class="body">
        <OutboxList rows={$outbox} {now} on:opened={() => (open = false)} />

        {#if $followUps.length > 0}
          <section class="follow" aria-label={$t('followUp.section')}>
            <h3>{$t('followUp.section')}</h3>
            {#each $followUps as item (item.id)}
              <div class="fu-row" class:due={item.firedAt !== ''}>
                <span class="fu-glyph"><Ph svg={bellIcon} size={15} /></span>
                <span class="fu-main">
                  <span class="fu-subject">{item.subject || $t('messageList.noSubject')}</span>
                  <span class="fu-meta">
                    {item.firedAt !== ''
                      ? $t('followUp.notice.fired')
                      : $t('followUp.remindsAt').replace('{when}', when(item.dueAt))}
                  </span>
                </span>
                <button
                  type="button"
                  class="c2-tool compact"
                  aria-label={item.firedAt !== '' ? $t('followUp.dismiss') : $t('followUp.cancel')}
                  use:tip={{ text: item.firedAt !== '' ? $t('followUp.dismiss') : $t('followUp.cancel') }}
                  on:click={() => void dropFollowUp(item.id)}
                >
                  <Ph svg={xIcon} size={13} />
                </button>
              </div>
            {/each}
          </section>
        {/if}
      </div>

      {#if syncLine}
        <footer>
          {#if $syncing}
            <PieProgress value={syncShare} size={12} label={syncLine} />
          {:else}
            <Ph svg={syncIcon} size={13} />
          {/if}
          <span>{syncLine}</span>
        </footer>
      {/if}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .activity {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-2));
    min-width: calc(var(--control-height) - var(--space-2));
    justify-content: center;
    padding: 0 var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    cursor: var(--cursor-action);
    --wails-draggable: no-drag;
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .activity:hover,
  .activity.open {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .activity:active {
    transform: scale(var(--press-scale));
  }

  .activity.failed {
    color: var(--danger);
  }

  .count {
    animation: pop var(--motion-base) var(--ease-out);
  }

  @keyframes pop {
    from {
      transform: scale(0.6);
      opacity: 0;
    }
  }

  .pop {
    display: flex;
    flex-direction: column;
    width: var(--popover-width);
    max-width: 100%;
    min-height: 0;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .tools {
    display: inline-flex;
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-2) 0;
  }

  .follow {
    margin-top: var(--space-2);
    padding-top: var(--space-2);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .follow h3 {
    margin: 0;
    padding: var(--space-2) var(--space-4);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .fu-row {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4);
  }

  .fu-row:hover {
    background: var(--surface-hover);
  }

  .fu-glyph {
    display: inline-flex;
    padding-top: var(--space-1);
    color: var(--text-tertiary);
  }

  .fu-row.due .fu-glyph {
    color: var(--warning);
  }

  .fu-main {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .fu-subject {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
  }

  .fu-meta {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  footer {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
