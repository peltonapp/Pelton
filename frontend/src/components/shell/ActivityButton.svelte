<script lang="ts">
  // the top bar's activity button: a pie while mail syncs or sends, a count
  // while messages wait in the outbox, and the outbox itself in a popover
  // under it. the popover groups the queue into Sending, Scheduled and
  // Failed, each with its actions, and ends on the sync state.
  import trayIcon from '@phosphor-icons/core/assets/duotone/tray-arrow-up-duotone.svg?raw'
  import sendIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import editIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import retryIcon from '@phosphor-icons/core/assets/duotone/arrow-clockwise-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import { flip } from 'svelte/animate'
  import { fly } from 'svelte/transition'
  import { onDestroy } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { outbox, syncing, syncCounts, syncFolder, lastSynced } from '../../stores/outbox'
  import { prefs } from '../../stores/prefs'
  import { cancelScheduled, discard, edit, isScheduled, recipientLabel, retry, sendNow } from '../../lib/outboxactions'
  import { formatRelative, formatWeekdayTime, type TimeFormat } from '../../lib/format'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
  import type { OutboxRow } from '../../lib/types'

  let button: HTMLButtonElement
  let open = false
  let busy: number | null = null
  let confirming: number | null = null

  // a slow tick keeps "scheduled" and "last synced" honest while open.
  let now = Date.now()
  const timer = setInterval(() => (now = Date.now()), 30000)
  onDestroy(() => clearInterval(timer))

  $: motion = $prefs.reduceMotion ? 0 : 170
  $: scheduled =$outbox.filter((row) => isScheduled(row, now))
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
  $: syncLine = $syncing
    ? $syncFolder
      ? $t('common.statusBar.syncingMailbox').replace('{mailbox}', $syncFolder)
      : $t('common.statusBar.syncing')
    : $lastSynced
      ? `${$t('common.statusBar.synced')} ${relative($lastSynced, now)}`
      : ''

  function relative(at: number, _now: number): string {
    return formatRelative(at, $t)
  }

  function when(iso: string): string {
    return formatWeekdayTime(new Date(iso), $prefs.timeFormat as TimeFormat)
  }

  async function run(row: OutboxRow, action: (id: number) => Promise<void>): Promise<void> {
    busy = row.id
    confirming = null
    await action(row.id)
    busy = null
  }

  async function editRow(row: OutboxRow): Promise<void> {
    await run(row, edit)
    open = false
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
        <button type="button" class="c2-tool compact" aria-label={$t('common.outboxPanel.close')} on:click={() => (open = false)}>
          <Ph svg={xIcon} size={13} />
        </button>
      </header>

      <div class="body">
        {#if $outbox.length === 0}
          <p class="empty">{$t('common.outboxPanel.empty')}</p>
        {/if}

        {#if sending.length > 0}
          <section aria-label={$t('topbar.activity.sending')}>
            <h3>{$t('topbar.activity.sending')}</h3>
            {#each sending as row (row.id)}
              <div class="row" animate:flip={{ duration: motion }} in:fly={{ y: -4, duration: motion }}>
                <span class="glyph"><Ph svg={sendIcon} size={15} /></span>
                <span class="main">
                  <span class="to">{recipientLabel(row, $t)}</span>
                  {#if row.state === 'sending'}
                    <span class="meta"><PieProgress size={11} label={$t('compose.action.sending')} /> {$t('compose.action.sending')}</span>
                  {:else}
                    <span class="meta">{$t('common.outboxPanel.queued')}</span>
                  {/if}
                </span>
              </div>
            {/each}
          </section>
        {/if}

        {#if scheduled.length > 0}
          <section aria-label={$t('topbar.activity.scheduled')}>
            <h3>{$t('topbar.activity.scheduled')}</h3>
            {#each scheduled as row (row.id)}
              <div class="row" animate:flip={{ duration: motion }}>
                <span class="glyph"><Ph svg={clockIcon} size={15} /></span>
                <span class="main">
                  <span class="to">{recipientLabel(row, $t)}</span>
                  <span class="meta">{when(row.nextAttemptAt)}</span>
                </span>
                <span class="actions">
                  {#if row.editable}
                    <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('action.edit')} use:tip={{ text: $t('action.edit') }} on:click={() => editRow(row)}>
                      <Ph svg={editIcon} size={14} />
                    </button>
                  {/if}
                  <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('topbar.activity.sendNow')} use:tip={{ text: $t('topbar.activity.sendNow') }} on:click={() => run(row, sendNow)}>
                    <Ph svg={sendIcon} size={14} />
                  </button>
                  <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('common.outboxPanel.cancelScheduled')} use:tip={{ text: $t('common.outboxPanel.cancelScheduled') }} on:click={() => run(row, cancelScheduled)}>
                    <Ph svg={xIcon} size={13} />
                  </button>
                </span>
              </div>
            {/each}
          </section>
        {/if}

        {#if failed.length > 0}
          <section aria-label={$t('topbar.activity.failedTitle')}>
            <h3>{$t('topbar.activity.failedTitle')}</h3>
            {#each failed as row (row.id)}
              <div class="row failed" animate:flip={{ duration: motion }}>
                <span class="glyph"><Ph svg={warnIcon} size={15} /></span>
                <span class="main">
                  <span class="to">{recipientLabel(row, $t)}</span>
                  <span class="err">{row.lastError || $t('common.outboxPanel.sendFailed')}</span>
                  {#if confirming === row.id}
                    <span class="confirm">
                      <span>{$t('common.outboxPanel.discardConfirm')}</span>
                      <button type="button" class="c2-tool danger" on:click={() => run(row, discard)}>{$t('common.outboxPanel.discard')}</button>
                      <button type="button" class="c2-tool" on:click={() => (confirming = null)}>{$t('common.outboxPanel.discardCancel')}</button>
                    </span>
                  {/if}
                </span>
                {#if confirming !== row.id}
                  <span class="actions">
                    <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('common.outboxPanel.retry')} use:tip={{ text: $t('common.outboxPanel.retry') }} on:click={() => run(row, retry)}>
                      <Ph svg={retryIcon} size={14} />
                    </button>
                    {#if row.editable}
                      <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('action.edit')} use:tip={{ text: $t('action.edit') }} on:click={() => editRow(row)}>
                        <Ph svg={editIcon} size={14} />
                      </button>
                    {/if}
                    <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('common.outboxPanel.discard')} use:tip={{ text: $t('common.outboxPanel.discard') }} on:click={() => (confirming = row.id)}>
                      <Ph svg={trashIcon} size={14} />
                    </button>
                  </span>
                {/if}
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

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-2) 0;
  }

  .empty {
    margin: 0;
    padding: var(--space-5);
    text-align: center;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  section + section {
    margin-top: var(--space-2);
    padding-top: var(--space-2);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  h3 {
    margin: 0;
    padding: var(--space-2) var(--space-4);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .row {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .glyph {
    display: inline-flex;
    padding-top: var(--space-1);
    color: var(--text-tertiary);
  }

  .row.failed .glyph {
    color: var(--danger);
  }

  .main {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
  }

  .to {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
  }

  .meta {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .err {
    font-size: var(--fz-meta);
    color: var(--danger);
    word-break: break-word;
  }

  .actions {
    display: inline-flex;
    flex-shrink: 0;
  }

  .actions .c2-tool {
    height: calc(var(--control-height) - var(--space-3));
    min-width: calc(var(--control-height) - var(--space-3));
  }

  .confirm {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .confirm .c2-tool {
    height: calc(var(--control-height) - var(--space-3));
    font-size: var(--fz-meta);
  }

  .danger {
    color: var(--danger);
  }

  .danger:hover {
    background: var(--danger-bg);
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
