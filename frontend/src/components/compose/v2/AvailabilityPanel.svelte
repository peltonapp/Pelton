<script lang="ts">
  // the composer's availability panel: pick a stretch of days, how long the
  // meeting is, the working hours and which calendars count as busy, and the
  // free times found are drawn on a small week, each one a toggle. Insert puts
  // the times picked into the message as plain lines of text with the time
  // zone named. Nothing is sent anywhere; the calendar is only read.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import '../../calendar/controls.css'
  import Segmented from '../../settings2/ui/Segmented.svelte'
  import Select from '../../common/Select.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import DateField from '../../calendar/DateField.svelte'
  import { listOccurrences } from '../../../lib/api'
  import { addDays, atMinutes, daysBetween, resolveWeekStart, startOfDay, startOfWeek, toRFC3339 } from '../../../lib/caldate'
  import { calendarColor, formatTime } from '../../../lib/calformat'
  import { busySpans, formatAvailability, freeSlots, type Span } from '../../../lib/freeslots'
  import { onCalendarChanged } from '../../../lib/events'
  import { calendars, calendarSettings, calendarSettingsLoaded, loadCalendars, loadCalendarSettings } from '../../../stores/calendar'
  import { prefs } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
  import type { TimeFormat } from '../../../lib/format'

  /** Fixed for tests; the clock otherwise. */
  export let now: () => Date = () => new Date()

  const dispatch = createEventDispatcher<{ insert: string; close: void }>()

  /** The most days the week view draws. */
  const maxDays = 14

  type RangeKind = 'this' | 'next' | 'custom'

  let range: RangeKind = 'this'
  let minutes = 30
  let workStart = $calendarSettings.workDayStart
  let workEnd = $calendarSettings.workDayEnd
  let weekends = false
  let picked: Set<number> | null = null
  let busyIds: Set<number> | null = null
  let customFrom = startOfDay(now())
  let customTo = addDays(startOfDay(now()), 6)
  let busy: Span[] = []
  let loading = true
  let seq = 0

  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: weekStart = resolveWeekStart($calendarSettings.weekStart, navigator.language)
  $: span = spanOf(range, weekStart, customFrom, customTo)
  $: days = Array.from({ length: daysBetween(span.from, span.to) }, (_, i) => addDays(span.from, i)).filter((d) => weekends || (d.getDay() !== 0 && d.getDay() !== 6))
  $: shownCalendars = $calendars
  $: if (busyIds === null && $calendars.length > 0) {
    busyIds = new Set($calendars.filter((c) => c.visible).map((c) => c.id))
  }
  $: void load(span.from, span.to, busyIds)
  $: slots = freeSlots(busy, { from: span.from, to: span.to, minutes, workStart, workEnd, weekends, now: now() })
  $: picked = new Set(slots.map((s) => s.start.getTime()))
  $: chosen = slots.filter((s) => picked?.has(s.start.getTime()))
  $: hours = Math.max(1, (workEnd - workStart) / 60)

  function spanOf(kind: RangeKind, first: number, from: Date, to: Date): { from: Date; to: Date } {
    const today = startOfDay(now())
    if (kind === 'custom') {
      const start = from <= to ? from : to
      const end = from <= to ? to : from
      return { from: startOfDay(start), to: addDays(startOfDay(start), Math.min(maxDays, daysBetween(start, end) + 1)) }
    }
    const week = startOfWeek(today, first)
    if (kind === 'next') {
      return { from: addDays(week, 7), to: addDays(week, 14) }
    }
    return { from: today, to: addDays(week, 7) }
  }

  async function load(from: Date, to: Date, ids: Set<number> | null): Promise<void> {
    const mine = ++seq
    if (ids === null) {
      return
    }
    loading = true
    try {
      const list = ids.size === 0 ? [] : await listOccurrences(toRFC3339(from), toRFC3339(to), [...ids])
      if (mine === seq) {
        busy = busySpans(list, ids)
      }
    } catch {
      if (mine === seq) {
        busy = []
      }
    } finally {
      if (mine === seq) {
        loading = false
      }
    }
  }

  function toggleSlot(s: Span): void {
    const next = new Set(picked)
    const key = s.start.getTime()
    if (next.has(key)) {
      next.delete(key)
    } else {
      next.add(key)
    }
    picked = next
  }

  function toggleCalendar(id: number): void {
    const next = new Set(busyIds)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    busyIds = next
  }

  function insert(): void {
    const text = formatAvailability(chosen, {
      words: { line: $t('avail.line'), range: $t('avail.range'), zone: $t('avail.zone') },
      time: (d) => formatTime(d, timeFormat),
    })
    if (text) {
      dispatch('insert', text)
    }
  }

  // where a span sits in a day's column, as shares of the working hours.
  function place(s: Span, day: Date): { top: number; height: number } | null {
    const open = atMinutes(day, workStart).getTime()
    const close = atMinutes(day, workEnd).getTime()
    const from = Math.max(open, s.start.getTime())
    const to = Math.min(close, s.end.getTime())
    if (to <= from || close <= open) {
      return null
    }
    return { top: ((from - open) / (close - open)) * 100, height: ((to - from) / (close - open)) * 100 }
  }

  function slotLabel(s: Span): string {
    const day = s.start.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
    return `${day}, ${$t('avail.range').replace('{from}', formatTime(s.start, timeFormat)).replace('{to}', formatTime(s.end, timeFormat))}`
  }

  $: rangeOptions = [
    { key: 'this', label: $t('avail.thisWeek') },
    { key: 'next', label: $t('avail.nextWeek') },
    { key: 'custom', label: $t('avail.custom') },
  ]

  $: lengthItems = [15, 30, 45, 60, 90, 120].map((m) => ({ value: String(m), label: $t('avail.minutes').replace('{n}', String(m)) }))

  const halfHours = Array.from({ length: 49 }, (_, i) => i * 30)
  $: hourItems = (from: number, to: number) =>
    halfHours.filter((m) => m >= from && m <= to).map((m) => ({ value: String(m), label: m === 24 * 60 ? $t('calendarSettings.midnight') : formatTime(atMinutes(new Date(2026, 0, 5), m), timeFormat) }))

  let off: (() => void) | null = null

  onMount(async () => {
    off = onCalendarChanged(() => void load(span.from, span.to, busyIds))
    const waits: Promise<void>[] = []
    if ($calendars.length === 0) {
      waits.push(loadCalendars())
    }
    if (!$calendarSettingsLoaded) {
      waits.push(
        loadCalendarSettings().then(() => {
          workStart = $calendarSettings.workDayStart
          workEnd = $calendarSettings.workDayEnd
        }),
      )
    }
    await Promise.all(waits)
    if ($calendars.length === 0) {
      busyIds = new Set()
    }
  })

  onDestroy(() => off?.())
</script>

<div class="avail">
  <header>
    <h2>{$t('avail.title')}</h2>
  </header>

  <div class="form">
    <span class="label">{$t('avail.range.label')}</span>
    <div class="field">
      <Segmented value={range} options={rangeOptions} label={$t('avail.range.label')} on:change={(e) => (range = e.detail as RangeKind)} />
    </div>
    {#if range === 'custom'}
      <span class="label">{$t('avail.days')}</span>
      <div class="field">
        <DateField value={customFrom} {weekStart} label={$t('avail.from')} on:change={(e) => (customFrom = e.detail)} />
        <span class="to">{$t('calendar.to')}</span>
        <DateField value={customTo} {weekStart} label={$t('avail.until')} on:change={(e) => (customTo = e.detail)} />
      </div>
    {/if}
    <span class="label">{$t('avail.length')}</span>
    <div class="field">
      <Select value={String(minutes)} items={lengthItems} ariaLabel={$t('avail.length')} on:change={(e) => (minutes = Number(e.detail))} />
    </div>
    <span class="label">{$t('calendarSettings.workingHours')}</span>
    <div class="field">
      <Select value={String(workStart)} items={hourItems(0, workEnd - 30)} ariaLabel={$t('calendarSettings.workStart')} on:change={(e) => (workStart = Number(e.detail))} />
      <span class="to">{$t('calendar.to')}</span>
      <Select value={String(workEnd)} items={hourItems(workStart + 30, 24 * 60)} ariaLabel={$t('calendarSettings.workEnd')} on:change={(e) => (workEnd = Number(e.detail))} />
      <span class="weekends">
        <ToggleSwitch checked={weekends} label={$t('avail.weekends')} on:change={(e) => (weekends = e.detail)} />
        <span aria-hidden="true">{$t('avail.weekends')}</span>
      </span>
    </div>
    {#if shownCalendars.length > 0}
      <span class="label">{$t('avail.busyFrom')}</span>
      <div class="field cals">
        {#each shownCalendars as cal (cal.id)}
          <button
            type="button"
            class="cal"
            aria-pressed={busyIds?.has(cal.id) ?? false}
            style={`--cal:${calendarColor(cal)}`}
            on:click={() => toggleCalendar(cal.id)}
          >
            <span class="dot" aria-hidden="true"></span>
            {cal.name || $t('calendar.defaultName')}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="week" class:loading style={`--days:${Math.max(1, days.length)};--hours:${hours}`} aria-busy={loading}>
    {#each days as day (day.getTime())}
      <div class="day">
        <span class="dh">{day.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric' })}</span>
        <div class="col">
          {#each busy as b, i (i)}
            {@const at = place(b, day)}
            {#if at}
              <div class="busy" style={`top:${at.top}%;height:${at.height}%`} aria-hidden="true"></div>
            {/if}
          {/each}
          {#each slots as s (s.start.getTime())}
            {@const at = place(s, day)}
            {#if at}
              <button
                type="button"
                class="slot"
                aria-pressed={picked?.has(s.start.getTime()) ?? false}
                aria-label={slotLabel(s)}
                title={slotLabel(s)}
                style={`top:${at.top}%;height:${at.height}%`}
                on:click={() => toggleSlot(s)}
              ></button>
            {/if}
          {/each}
        </div>
      </div>
    {/each}
    {#if days.length === 0}
      <p class="none">{$t('avail.noDays')}</p>
    {/if}
  </div>

  <footer>
    <span class="count" aria-live="polite">
      {#if loading}
        {$t('avail.loading')}
      {:else if slots.length === 0}
        {$t('avail.noneFree')}
      {:else}
        {$t('avail.picked').replace('{n}', String(chosen.length)).replace('{total}', String(slots.length))}
      {/if}
    </span>
    <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('avail.cancel')}</button>
    <button type="button" class="cal-btn primary" disabled={chosen.length === 0} on:click={insert}>{$t('avail.insert')}</button>
  </footer>
</div>

<style>
  .avail {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: var(--avail-width);
    padding: var(--space-4);
    font-size: var(--fz-label);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .form {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .label {
    color: var(--text-secondary);
  }

  .field {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .to {
    color: var(--text-tertiary);
  }

  .weekends {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin-inline-start: auto;
    color: var(--text-secondary);
  }

  .cal {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .cal[aria-pressed='true'] {
    border-color: var(--border-strong);
    background: var(--surface-sunken);
    color: var(--text-primary);
  }

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    border: var(--hairline) solid var(--cal);
  }

  .cal[aria-pressed='true'] .dot {
    background: var(--cal);
  }

  .week {
    display: grid;
    grid-template-columns: repeat(var(--days), minmax(0, 1fr));
    gap: var(--space-1);
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .week.loading {
    opacity: 0.6;
  }

  .day {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .dh {
    color: var(--text-secondary);
    font-size: calc(var(--fz-meta) - 1px);
    text-align: center;
    white-space: nowrap;
    overflow: hidden;
  }

  .col {
    position: relative;
    height: calc(var(--hours) * var(--avail-hour));
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }

  .busy {
    position: absolute;
    inset-inline: 0;
    background: color-mix(in srgb, var(--text-tertiary) 22%, transparent);
  }

  .slot {
    position: absolute;
    inset-inline: var(--hairline);
    padding: 0;
    border: var(--hairline) dashed var(--border-strong);
    border-radius: var(--radius-control);
    background: transparent;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .slot[aria-pressed='true'] {
    border: var(--hairline) solid var(--accent);
    background: color-mix(in srgb, var(--accent) 28%, transparent);
  }

  .slot:hover {
    border-color: var(--accent);
  }

  .slot:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .none {
    grid-column: 1 / -1;
    margin: 0;
    color: var(--text-tertiary);
    text-align: center;
  }

  footer {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .count {
    flex: 1;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }

  :global(:root[data-reduce-motion]) .week,
  :global(:root[data-reduce-motion]) .slot,
  :global(:root[data-reduce-motion]) .cal {
    transition: none;
  }
</style>
