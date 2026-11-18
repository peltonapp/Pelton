<script lang="ts">
  // the live send log of the send activity window: one line per step the
  // outbox worker took, with its time. it follows the newest line until the
  // user scrolls up, and offers a way back down and a copy of the whole log.
  import copyIcon from '@phosphor-icons/core/assets/duotone/copy-duotone.svg?raw'
  import downIcon from '@phosphor-icons/core/assets/duotone/arrow-down-duotone.svg?raw'
  import { afterUpdate, tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import Ph from '../common/Ph.svelte'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { recipientLabel } from '../../lib/outboxactions'
  import { formatLogTime, formatWeekdayTime, type TimeFormat } from '../../lib/format'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
  import type { OutboxLogEntry } from '../../lib/types'
  import { dur } from '../../lib/motion'

  /** The log lines, oldest first. */
  export let entries: OutboxLogEntry[]

  // how close to the end, in pixels, still counts as reading the newest line.
  const followSlack = 8

  let scroller: HTMLOListElement
  let follow = true
  let unseen = false
  let seen = 0

  $: motion = $prefs.reduceMotion ? 0 : 170
  $: timeFormat = $prefs.timeFormat as TimeFormat

  function stepText(entry: OutboxLogEntry, tr: (key: string) => string, clock: TimeFormat): string {
    const key = `outbox.log.step.${entry.step}`
    const text = tr(key)
    if (text === key) {
      return entry.step
    }
    if (entry.step === 'retry' && entry.retryAt) {
      return text.replace('{when}', formatWeekdayTime(new Date(entry.retryAt), clock))
    }
    return text
  }

  function atEnd(): boolean {
    return scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <= followSlack
  }

  function onScroll(): void {
    follow = atEnd()
    if (follow) {
      unseen = false
    }
  }

  function toEnd(smooth: boolean): void {
    if (smooth && typeof scroller.scrollTo === 'function') {
      scroller.scrollTo({ top: scroller.scrollHeight, behavior: dur(1) ? 'smooth' : 'auto' })
    } else {
      scroller.scrollTop = scroller.scrollHeight
    }
  }

  afterUpdate(() => {
    const last = entries.length > 0 ? entries[entries.length - 1].seq : 0
    if (last === seen) {
      return
    }
    seen = last
    if (follow) {
      toEnd(false)
    } else {
      unseen = true
    }
  })

  async function jumpToLatest(): Promise<void> {
    follow = true
    unseen = false
    await tick()
    toEnd(!$prefs.reduceMotion)
  }

  function line(entry: OutboxLogEntry): string {
    const parts = [
      formatLogTime(new Date(entry.at), timeFormat),
      stepText(entry, $t, timeFormat),
      recipientLabel(entry, $t),
    ]
    if (entry.detail) {
      parts.push(entry.detail)
    }
    return parts.join('  ')
  }

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(entries.map(line).join('\n'))
      toastSuccess($t('outbox.log.copied'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<section class="log" aria-labelledby="outbox-log-title">
  <header>
    <h2 id="outbox-log-title">{$t('outbox.log.title')}</h2>
    <button type="button" class="c2-tool compact" disabled={entries.length === 0} aria-label={$t('outbox.log.copy')} use:tip={{ text: $t('outbox.log.copy') }} on:click={copy}>
      <Ph svg={copyIcon} size={14} />
    </button>
  </header>

  <div class="frame">
    <ol bind:this={scroller} role="log" aria-labelledby="outbox-log-title" on:scroll={onScroll}>
      {#each entries as entry (entry.seq)}
        <li class:failed={entry.step === 'failed'} class:done={entry.step === 'sent'} in:fly={{ y: 4, duration: motion }}>
          <time datetime={entry.at}>{formatLogTime(new Date(entry.at), timeFormat)}</time>
          <span class="step">{stepText(entry, $t, timeFormat)}</span>
          <span class="to">{recipientLabel(entry, $t)}</span>
          {#if entry.detail}
            <span class="detail">{entry.detail}</span>
          {/if}
        </li>
      {/each}
    </ol>
    {#if entries.length === 0}
      <p class="empty">{$t('outbox.log.empty')}</p>
    {/if}
    {#if unseen}
      <button type="button" class="c2-tool latest" transition:fly={{ y: 4, duration: motion }} on:click={jumpToLatest}>
        <Ph svg={downIcon} size={13} />
        <span>{$t('outbox.log.latest')}</span>
      </button>
    {/if}
  </div>
</section>

<style>
  .log {
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
    height: 100%;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    min-height: var(--control-height);
    padding: 0 var(--space-2) 0 var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  header .c2-tool {
    height: calc(var(--control-height) - var(--space-3));
    min-width: calc(var(--control-height) - var(--space-3));
  }

  h2 {
    margin: 0;
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .frame {
    position: relative;
    flex: 1;
    min-height: 0;
  }

  ol {
    position: absolute;
    inset: 0;
    margin: 0;
    padding: var(--space-2) 0;
    list-style: none;
    overflow-y: auto;
    font-size: var(--fz-label);
  }

  li {
    display: grid;
    grid-template-columns: max-content minmax(0, max-content) minmax(0, 1fr);
    column-gap: var(--space-4);
    align-items: baseline;
    padding: var(--space-1) var(--space-4);
  }

  li:hover {
    background: var(--surface-hover);
  }

  time {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    color: var(--text-tertiary);
  }

  .step {
    color: var(--text-primary);
    white-space: nowrap;
  }

  li.done .step {
    color: var(--success);
  }

  li.failed .step {
    color: var(--danger);
  }

  .to {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-secondary);
  }

  .detail {
    grid-column: 2 / -1;
    font-size: var(--fz-meta);
    color: var(--danger);
    word-break: break-word;
  }

  .empty {
    position: absolute;
    inset: 0 0 auto 0;
    margin: 0;
    padding: var(--space-5);
    text-align: center;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .latest {
    position: absolute;
    bottom: var(--space-4);
    left: 50%;
    transform: translateX(-50%);
    gap: var(--space-2);
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fz-meta);
  }
</style>
