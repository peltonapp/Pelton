<script lang="ts">
  // the snooze ("resend to me") dialog. it offers friendly presets and a custom
  // date-time picker, plus an unchecked-by-default option to also hide the mail
  // from the inbox until it resurfaces. on confirm it schedules the snooze; the
  // message reappears marked unread when the time comes.
  import { formatWeekdayTime, type TimeFormat } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { fade, scale } from 'svelte/transition'
  import {
    IconClock,
    IconSunset2,
    IconSun,
    IconCalendarWeek,
    IconArrowRight,
    IconX,
  } from '@tabler/icons-svelte'
  import { snoozeTarget, closeSnooze } from '../../stores/snooze'
  import { snoozeMessage } from '../../lib/api'
  import { get } from 'svelte/store'
  import { messageList, patchInList, removeFromList } from '../../stores/messages'
  import { recordSnooze } from '../../lib/organize'
  import { toastError, toastInfo, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import DateTimePicker from '../common/DateTimePicker.svelte'

  let hideNow = false
  let customValue = ''
  let busy = false

  interface Preset {
    label: string
    when: Date
    icon: typeof IconClock
  }

  // presets are recomputed each time the dialog opens so "tomorrow" is always
  // relative to now. past options are filtered out.
  function buildPresets(now: Date, tr: (key: string) => string): Preset[] {
    const out: Preset[] = []
    const laterToday = new Date(now.getTime() + 3 * 60 * 60 * 1000)
    out.push({ label: tr('detail.snooze.in3Hours'), when: laterToday, icon: IconClock })

    const evening = atTime(now, 18, 0)
    if (evening.getTime() > now.getTime() + 60 * 1000) {
      out.push({ label: tr('detail.snooze.thisEvening'), when: evening, icon: IconSunset2 })
    }
    const tomorrow = atTime(addDays(now, 1), 9, 0)
    out.push({ label: tr('detail.snooze.tomorrow'), when: tomorrow, icon: IconSun })

    const saturday = atTime(nextWeekday(now, 6), 9, 0)
    out.push({ label: tr('detail.snooze.thisWeekend'), when: saturday, icon: IconCalendarWeek })

    const monday = atTime(nextWeekday(now, 1), 9, 0)
    out.push({ label: tr('detail.snooze.nextWeek'), when: monday, icon: IconArrowRight })
    return out
  }

  function atTime(d: Date, h: number, m: number): Date {
    const c = new Date(d)
    c.setHours(h, m, 0, 0)
    return c
  }
  function addDays(d: Date, n: number): Date {
    const c = new Date(d)
    c.setDate(c.getDate() + n)
    return c
  }
  // nextWeekday returns the next date whose day-of-week is target (0=Sun..6=Sat),
  // strictly after today.
  function nextWeekday(from: Date, target: number): Date {
    const c = new Date(from)
    do {
      c.setDate(c.getDate() + 1)
    } while (c.getDay() !== target)
    return c
  }

  $: presets = $snoozeTarget ? buildPresets(new Date(), $t) : []
  $: formattedPresets = presets.map((p) => ({ ...p, sub: formatWhen(p.when) }))

  function formatWhen(d: Date): string {
    return formatWeekdayTime(d, $prefs.timeFormat as TimeFormat)
  }

  async function confirm(when: Date): Promise<void> {
    const target = $snoozeTarget
    if (!target || busy) {
      return
    }
    // the picker only offers minute granularity, so a time picked inside the
    // current minute comes back with :00 seconds and reads as a few seconds
    // ago. Comparing against the start of the current minute is what makes
    // "in three minutes" behave like three minutes rather than being rejected
    // for being in the past.
    const thisMinute = new Date()
    thisMinute.setSeconds(0, 0)
    if (when.getTime() <= thisMinute.getTime()) {
      toastError($t('detail.snooze.pickFutureTime'))
      return
    }
    busy = true
    // one time for the whole target, which is a single message from a row menu
    // and the whole selection from a bulk one. A failure on one message is
    // reported and does not hold back the rest.
    let failure = ''
    const snoozed: number[] = []
    const rows = new Map((get(messageList).data?.items ?? []).filter((m) => target.ids.includes(m.id)).map((m) => [m.id, m]))
    for (const id of target.ids) {
      try {
        await snoozeMessage(id, when.toISOString(), hideNow)
        snoozed.push(id)
        if (hideNow) {
          removeFromList(id)
        } else {
          patchInList(id, { snoozeUntil: when.toISOString() })
        }
      } catch (err) {
        failure = errorMessage(err)
      }
    }
    busy = false
    if (snoozed.length > 0) {
      recordSnooze(snoozed, rows, target.subject, when.toISOString(), hideNow)
    }
    if (failure !== '') {
      toastError(failure)
      return
    }
    toastInfo(
      target.ids.length === 1
        ? $t('detail.snooze.snoozedUntil').replace('{when}', formatWhen(when))
        : $t('detail.snooze.snoozedCount').replace('{n}', String(target.ids.length)).replace('{when}', formatWhen(when)),
    )
    done()
  }

  function confirmCustom(): void {
    if (!customValue) {
      return
    }
    void confirm(new Date(customValue))
  }

  function done(): void {
    hideNow = false
    customValue = ''
    closeSnooze()
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      done()
    }
  }
</script>

<svelte:window on:keydown={$snoozeTarget ? onKeydown : undefined} />

{#if $snoozeTarget}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="backdrop" transition:fade={{ duration: 120 }} on:click={done}></div>
  <div class="dialog" role="dialog" aria-modal="true" aria-label={$t('detail.snooze.dialogLabel')} transition:scale={{ duration: 150, start: 0.94 }}>
    <header>
      <div class="titles">
        <h2>{$t('detail.snooze.title')}</h2>
        <p class="subject">
          {$snoozeTarget.ids.length > 1
            ? $t('messageList.bulk.messagesCount').replace('{n}', String($snoozeTarget.ids.length))
            : $snoozeTarget.subject || $t('detail.noSubject')}
        </p>
      </div>
      <button type="button" class="close" aria-label={$t('detail.attachments.close')} on:click={done}>
        <IconX size={16} stroke={1.8} />
      </button>
    </header>

    <div class="presets">
      {#each formattedPresets as p}
        <button type="button" class="preset" disabled={busy} on:click={() => confirm(p.when)}>
          <span class="p-icon"><svelte:component this={p.icon} size={18} stroke={1.6} /></span>
          <span class="p-label">{p.label}</span>
          <span class="p-sub">{p.sub}</span>
        </button>
      {/each}
    </div>

    <div class="custom">
      <label for="snooze-custom">{$t('detail.snooze.pickDateTime')}</label>
      <div class="custom-row">
        <div class="custom-picker">
          <DateTimePicker id="snooze-custom" mode="datetime" bind:value={customValue} />
        </div>
        <button type="button" class="go" disabled={!customValue || busy} on:click={confirmCustom}>
          {$t('detail.snooze.title')}
        </button>
      </div>
    </div>

    <label class="hide-now">
      <input type="checkbox" bind:checked={hideNow} />
      <span>{$t('detail.snooze.hideFromInbox')}</span>
    </label>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 300;
    background: var(--scrim, rgba(0, 0, 0, 0.4));
    backdrop-filter: blur(2px);
  }

  .dialog {
    position: fixed;
    z-index: 301;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(440px, calc(100vw - 2 * var(--space-5)));
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .subject {
    margin: 2px 0 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 320px;
  }

  .close {
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

  .presets {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-2);
  }

  .preset {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    text-align: start;
    transition: border-color 0.1s ease, background 0.1s ease;
  }
  .preset:hover {
    border-color: var(--accent);
    background: var(--surface-hover);
  }
  .preset:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .p-icon {
    display: inline-flex;
    color: var(--accent);
  }
  .p-label {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }
  .p-sub {
    margin-inline-start: auto;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .custom {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .custom label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }
  .custom-row {
    display: flex;
    gap: var(--space-2);
  }
  .custom-picker {
    flex: 1;
    min-width: 0;
  }
  .go {
    padding: var(--space-2) var(--space-4);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: #fff;
    cursor: var(--cursor-action);
    font-weight: var(--fw-medium);
  }
  .go:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .hide-now {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }
</style>
