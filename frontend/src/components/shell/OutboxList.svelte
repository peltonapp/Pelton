<script lang="ts">
  // the outbox as the new design shows it, in the activity popover and the
  // send activity window: grouped into Sending, Scheduled and Failed, each
  // row with its actions. a message going out now can be cancelled, and once
  // more than two are waiting, any but the first can be sent first.
  import sendIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import editIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import retryIcon from '@phosphor-icons/core/assets/duotone/arrow-clockwise-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import firstIcon from '@phosphor-icons/core/assets/duotone/arrow-line-up-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import { createEventDispatcher } from 'svelte'
  import { flip } from 'svelte/animate'
  import { fly } from 'svelte/transition'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { prefs } from '../../stores/prefs'
  import {
    cancelScheduled,
    cancelSending,
    discard,
    edit,
    isScheduled,
    recipientLabel,
    retry,
    sendFirst,
    sendFirstIds,
    sendingOrder,
    sendNow,
  } from '../../lib/outboxactions'
  import { formatWeekdayTime, type TimeFormat } from '../../lib/format'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
  import type { OutboxRow } from '../../lib/types'

  /** The outbox rows to show, sent ones left out. */
  export let rows: OutboxRow[]
  /** The time scheduled sends are measured against, in epoch ms. */
  export let now: number

  // opened fires when a message was taken back into a composer.
  const dispatch = createEventDispatcher<{ opened: void }>()

  let busy: number | null = null
  let confirming: number | null = null

  $: motion = $prefs.reduceMotion ? 0 : 170
  $: scheduled = rows.filter((row) => isScheduled(row, now))
  $: failed = rows.filter((row) => row.state === 'failed')
  $: sending = sendingOrder(rows.filter((row) => row.state !== 'failed' && !isScheduled(row, now)))
  $: movable = sendFirstIds(sending)

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
    dispatch('opened')
  }
</script>

{#if rows.length === 0}
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
        <span class="actions">
          {#if movable.has(row.id)}
            <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('outbox.sendFirst.action')} use:tip={{ text: $t('outbox.sendFirst.action') }} on:click={() => run(row, sendFirst)}>
              <Ph svg={firstIcon} size={14} />
            </button>
          {/if}
          <button type="button" class="c2-tool compact" disabled={busy === row.id} aria-label={$t('outbox.cancel.action')} use:tip={{ text: $t('outbox.cancel.action') }} on:click={() => run(row, cancelSending)}>
            <Ph svg={xIcon} size={13} />
          </button>
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
          <span class="err">{row.lastError === 'cancelled' ? $t('outbox.cancelled') : row.lastError || $t('common.outboxPanel.sendFailed')}</span>
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

<style>
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
</style>
