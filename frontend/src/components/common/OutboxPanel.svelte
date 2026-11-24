<script lang="ts">
  // the popover that opens from the status bar's outbox indicator. it lists every
  // queued, sending and failed message with its recipients and state, an
  // indeterminate progress bar while sending, and the error for failed ones. a
  // queued row whose nextAttemptAt is meaningfully in the future (a scheduled
  // "send later", not just the short undo-send delay) shows its scheduled time
  // and a cancel button instead of the generic "queued" label; cancelling pulls
  // it back to a draft the same way undo-send does.
  import { formatWeekdayTime, type TimeFormat } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { IconSend, IconClock, IconAlertTriangle, IconX, IconRefresh } from '@tabler/icons-svelte'
  import { createEventDispatcher } from 'svelte'
  import { outbox } from '../../stores/outbox'
  import { cancelScheduled as cancelRow, discard as discardRow, isScheduled, recipientLabel, retry as retryRow } from '../../lib/outboxactions'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ close: void }>()

  function formatScheduled(iso: string): string {
    return formatWeekdayTime(new Date(iso), $prefs.timeFormat as TimeFormat)
  }

  let cancelling: number | null = null

  async function cancelScheduled(id: number): Promise<void> {
    cancelling = id
    await cancelRow(id)
    cancelling = null
  }

  // a failed message is out of retries, so it sits here until it is dealt with
  // by hand (#238). Retrying is the useful case, since the usual reason is a
  // server or password that has since been fixed.
  let busy: number | null = null
  // the row currently asking whether to discard. Discarding is not recoverable:
  // the outbox holds the only copy of a message once it has left compose.
  let confirmingDiscard: number | null = null

  async function retry(id: number): Promise<void> {
    busy = id
    await retryRow(id)
    busy = null
  }

  async function discard(id: number): Promise<void> {
    busy = id
    confirmingDiscard = null
    await discardRow(id)
    busy = null
  }
</script>

<div class="panel" role="dialog" aria-label={$t('common.outboxPanel.title')}>
  <header>
    <span class="title">{$t('common.outboxPanel.title')}</span>
    <button type="button" class="close" aria-label={$t('common.outboxPanel.close')} on:click={() => dispatch('close')}>
      <IconX size={14} stroke={1.8} />
    </button>
  </header>

  {#if $outbox.length === 0}
    <p class="empty">{$t('common.outboxPanel.empty')}</p>
  {:else}
    <ul>
      {#each $outbox as row (row.id)}
        <li class="item" class:failed={row.state === 'failed'}>
          <span class="state-icon">
            {#if row.state === 'sending'}
              <IconSend size={15} stroke={1.7} />
            {:else if row.state === 'failed'}
              <IconAlertTriangle size={15} stroke={1.7} />
            {:else}
              <IconClock size={15} stroke={1.7} />
            {/if}
          </span>
          <span class="body">
            <span class="to">{recipientLabel(row, $t)}</span>
            {#if row.state === 'sending'}
              <span class="bar"><span class="fill"></span></span>
            {:else if row.state === 'failed'}
              <span class="err">{row.lastError || $t('common.outboxPanel.sendFailed')}</span>
              {#if confirmingDiscard === row.id}
                <span class="confirm">
                  <span class="muted">{$t('common.outboxPanel.discardConfirm')}</span>
                  <button type="button" class="danger" on:click={() => discard(row.id)}>
                    {$t('common.outboxPanel.discard')}
                  </button>
                  <button type="button" class="ghost" on:click={() => (confirmingDiscard = null)}>
                    {$t('common.outboxPanel.discardCancel')}
                  </button>
                </span>
              {/if}
            {:else if isScheduled(row)}
              <span class="muted">
                {$t('common.outboxPanel.scheduledFor').replace('{when}', formatScheduled(row.nextAttemptAt))}
              </span>
            {:else}
              <span class="muted">{$t('common.outboxPanel.queued')}</span>
            {/if}
          </span>
          {#if isScheduled(row)}
            <button
              type="button"
              class="cancel"
              disabled={cancelling === row.id}
              aria-label={$t('common.outboxPanel.cancelScheduled')}
              title={$t('common.outboxPanel.cancelScheduled')}
              on:click={() => cancelScheduled(row.id)}
            >
              <IconX size={13} stroke={1.8} />
            </button>
          {:else if row.state === 'failed' && confirmingDiscard !== row.id}
            <button
              type="button"
              class="cancel"
              disabled={busy === row.id}
              aria-label={$t('common.outboxPanel.retry')}
              title={$t('common.outboxPanel.retry')}
              on:click={() => retry(row.id)}
            >
              <IconRefresh size={13} stroke={1.8} />
            </button>
            <button
              type="button"
              class="cancel"
              disabled={busy === row.id}
              aria-label={$t('common.outboxPanel.discard')}
              title={$t('common.outboxPanel.discard')}
              on:click={() => (confirmingDiscard = row.id)}
            >
              <IconX size={13} stroke={1.8} />
            </button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .panel {
    width: 320px;
    max-height: 360px;
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  .close {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
  }

  .close:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .empty {
    margin: 0;
    padding: var(--space-5);
    text-align: center;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: var(--space-2);
    overflow-y: auto;
  }

  .item {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-3);
    border-radius: var(--radius-control);
  }

  .item + .item {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .state-icon {
    color: var(--text-tertiary);
    flex-shrink: 0;
    margin-top: 1px;
  }

  .item.failed .state-icon {
    color: var(--danger);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
    flex: 1;
  }

  .to {
    font-size: var(--fz-label);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .muted {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .err {
    font-size: var(--fz-meta);
    color: var(--danger);
    word-break: break-word;
  }

  /* the inline discard confirmation, inside the row rather than as a dialog:
     it belongs to one message and the panel is already a popover. */
  .confirm {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .confirm button {
    border: var(--hairline) solid var(--border-default);
    background: transparent;
    border-radius: var(--radius-control);
    padding: 2px var(--space-2);
    font-size: var(--fz-meta);
    cursor: pointer;
  }

  .confirm .danger {
    border-color: var(--danger);
    color: var(--danger);
  }

  .confirm .danger:hover {
    background: var(--danger-bg);
  }

  .confirm .ghost {
    color: var(--text-secondary);
  }

  .confirm .ghost:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .cancel {
    display: inline-flex;
    align-self: flex-start;
    flex-shrink: 0;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
  }

  .cancel:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .cancel:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* indeterminate progress while sending. */
  .bar {
    position: relative;
    height: 3px;
    border-radius: 999px;
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .fill {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 40%;
    border-radius: 999px;
    background: var(--accent);
    animation: slide 1.1s ease-in-out infinite;
  }

  @keyframes slide {
    0% {
      left: -40%;
    }
    100% {
      left: 100%;
    }
  }
</style>
