<script lang="ts">
  // the event editor's scheduling view: one line of the event's day per
  // person, the user first, with their busy time drawn on it and the hours
  // outside working time shaded. the proposed time is a band across every
  // line; dragging it, or its edges, moves the event or changes its length on
  // the quarter hour, and the arrow keys do the same when it has focus. under
  // it, who the time runs into, and the next times everyone known is free.
  //
  // the user's time comes from the calendars stored here and the attendees'
  // from the user's own CalDAV server; nothing else is asked. a lookup is
  // cancelled when the day or the people change and when the view closes.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import caretLeft from '@phosphor-icons/core/assets/regular/caret-left.svg?raw'
  import caretRight from '@phosphor-icons/core/assets/regular/caret-right.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import './controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import DateField from './DateField.svelte'
  import { lookupFreeBusy, type Cancellable } from '../../lib/api'
  import { addDays, atMinutes, dayKey, dayMinutes, minutesOfDay, snapMinutes, startOfDay, toRFC3339 } from '../../lib/caldate'
  import { formatHour, formatShortDay, formatTime } from '../../lib/calformat'
  import { conflictsAt, overlapping, suggestTimes, toBlocks, type Availability, type BusyBlock } from '../../lib/scheduling'
  import { calendarSettings, calendarSettingsLoaded } from '../../stores/calendar'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Attendee, AttendeeFreeBusy, FreeBusy } from '../../lib/types'

  /** The event's start and end. */
  export let start: Date
  export let end: Date
  export let allDay: boolean = false
  /** The people invited. */
  export let attendees: Attendee[]
  /** The calendar the event goes in; its server is asked about the attendees. */
  export let calendarId: number
  /** The address the user invites as, empty for the server's own choice. */
  export let organizer: string = ''
  /** The event being edited, left out of the user's own time; 0 for a new one. */
  export let eventId: number = 0
  export let weekStart: number = 1
  export let timeFormat: TimeFormat = 'auto'
  /** Fixed for tests; the clock otherwise. */
  export let now: () => Date = () => new Date()

  const dispatch = createEventDispatcher<{ change: { start: Date; end: Date } }>()

  const step = 15
  /** Days looked ahead for suggested times. */
  const lookahead = 7
  const hintId = `sched-hint-${Math.random().toString(36).slice(2)}`

  type Row = { key: string; name: string; email: string; you: boolean; info: AttendeeFreeBusy | null; busy: BusyBlock[] }

  let data: FreeBusy | null = null
  let loading = false
  let failed = false
  let call: Cancellable<FreeBusy> | null = null
  let seq = 0

  $: workStart = $calendarSettingsLoaded ? $calendarSettings.workDayStart : 8 * 60
  $: workEnd = $calendarSettingsLoaded ? $calendarSettings.workDayEnd : 18 * 60
  $: day = startOfDay(start)
  $: emails = attendees.map((a) => a.email)
  $: lookupKey = `${dayKey(day)}|${calendarId}|${eventId}|${organizer}|${emails.join(',')}`
  $: void load(lookupKey)

  function load(_key: string): void {
    call?.cancel()
    const mine = ++seq
    loading = true
    failed = false
    const pending = lookupFreeBusy({
      calendarId,
      from: toRFC3339(day),
      to: toRFC3339(addDays(day, lookahead)),
      organizer,
      attendees: emails,
      excludeEventId: eventId,
    })
    call = pending
    pending.then(
      (result) => {
        if (mine === seq) {
          data = result
          loading = false
        }
      },
      () => {
        if (mine === seq) {
          failed = true
          loading = false
        }
      },
    )
  }

  onDestroy(() => {
    seq++
    call?.cancel()
  })

  function infoFor(fb: FreeBusy | null, email: string): AttendeeFreeBusy | null {
    return fb?.attendees.find((a) => a.email.toLowerCase() === email.toLowerCase()) ?? null
  }

  $: rows = buildRows(data, attendees, $t('calendar.sched.you'))

  function buildRows(fb: FreeBusy | null, people: Attendee[], you: string): Row[] {
    const out: Row[] = [{ key: '', name: you, email: '', you: true, info: null, busy: toBlocks(fb?.mine ?? []) }]
    for (const a of people) {
      const info = infoFor(fb, a.email)
      out.push({ key: a.email, name: a.name || a.email, email: a.email, you: false, info, busy: info?.known ? toBlocks(info.busy) : [] })
    }
    return out
  }

  // a row whose time is not known yet while the first lookup runs is not
  // "no information", only not loaded.
  function unknown(row: Row, fb: FreeBusy | null): boolean {
    return !row.you && fb !== null && !(row.info?.known ?? false)
  }

  // --- the visible stretch of the day ----------------------------------------------

  let dragging = false
  let frozen: [number, number] = [0, dayMinutes]

  $: startMin = minutesOfDay(start)
  $: endMin = end.getTime() - day.getTime() >= dayMinutes * 60000 ? dayMinutes : Math.max(startMin, minutesOfDay(end))
  $: length = Math.max(step, Math.round((end.getTime() - start.getTime()) / 60000))
  $: span = dragging ? frozen : allDay ? [0, dayMinutes] : visible(workStart, workEnd, startMin, endMin)
  $: viewFrom = span[0]
  $: viewTo = span[1]
  $: hourList = hoursIn(viewFrom, viewTo)

  function visible(ws: number, we: number, from: number, to: number): [number, number] {
    const lo = Math.max(0, Math.floor((Math.min(ws, from) - 60) / 60) * 60)
    const hi = Math.min(dayMinutes, Math.ceil((Math.max(we, to) + 60) / 60) * 60)
    return [lo, hi]
  }

  function hoursIn(from: number, to: number): number[] {
    const every = to - from > 14 * 60 ? 2 : 1
    const out: number[] = []
    for (let h = Math.floor(from / 60) + 1; h * 60 < to; h++) {
      if (h % every === 0) {
        out.push(h)
      }
    }
    return out
  }

  function pct(minutes: number): number {
    return ((Math.min(viewTo, Math.max(viewFrom, minutes)) - viewFrom) / (viewTo - viewFrom)) * 100
  }

  function minutesIn(d: Date): number {
    const diff = Math.round((d.getTime() - day.getTime()) / 60000)
    return Math.min(dayMinutes, Math.max(0, diff))
  }

  function placeOf(b: BusyBlock): string {
    const from = pct(minutesIn(b.start))
    const to = pct(minutesIn(b.end))
    return `left:${from}%;width:${Math.max(0, to - from)}%`
  }

  function shown(busy: BusyBlock[]): BusyBlock[] {
    return overlapping(busy, atMinutes(day, viewFrom), atMinutes(day, viewTo))
  }

  // --- conflicts and suggestions ---------------------------------------------------

  $: known = rows.filter((r) => !unknown(r, data)).map((r): Availability => ({ email: r.email, known: true, busy: r.busy }))
  $: clash = conflictsAt(
    rows.filter((r) => !r.you).map((r) => ({ email: r.email, known: !unknown(r, data) && data !== null, busy: r.busy })),
    start,
    end,
  )
  $: busyNames = new Set(clash.busy.map((e) => e.toLowerCase()))
  $: suggestions =
    data && !allDay
      ? suggestTimes(known, { from: day, to: addDays(day, lookahead), minutes: length, workStart, workEnd, now: now(), limit: 6 }).filter(
          (s) => s.start.getTime() !== start.getTime(),
        )
      : []

  function nameOf(email: string): string {
    const a = attendees.find((x) => x.email.toLowerCase() === email.toLowerCase())
    return a?.name || email
  }

  function names(list: string[]): string {
    return list.map(nameOf).join(', ')
  }

  function suggestionLabel(s: { start: Date; end: Date }): string {
    const time = `${formatTime(s.start, timeFormat)} - ${formatTime(s.end, timeFormat)}`
    if (startOfDay(s.start).getTime() === day.getTime()) {
      return time
    }
    return `${s.start.toLocaleDateString(undefined, { weekday: 'short' })} ${formatShortDay(s.start)}, ${time}`
  }

  function rowLabel(row: Row): string {
    if (data === null) {
      return row.name
    }
    if (unknown(row, data)) {
      return `${row.name}: ${$t('calendar.sched.noInfo')}`
    }
    const list = shown(row.busy)
    if (list.length === 0) {
      return `${row.name}: ${$t('calendar.sched.free')}`
    }
    const parts = list.map((b) => `${$t(`calendar.sched.state.${b.kind}`)} ${formatTime(b.start, timeFormat)} - ${formatTime(b.end, timeFormat)}`)
    return `${row.name}: ${parts.join(', ')}`
  }

  function reasonText(info: AttendeeFreeBusy | null): string {
    return info?.reason ? $t(`calendar.sched.reason.${info.reason}`) : ''
  }

  // --- moving the proposed time ---------------------------------------------------

  let tracks: HTMLDivElement

  function propose(fromMin: number, toMin: number): void {
    dispatch('change', { start: atMinutes(day, fromMin), end: atMinutes(day, toMin) })
  }

  function moveTo(fromMin: number): void {
    const next = atMinutes(day, fromMin)
    dispatch('change', { start: next, end: new Date(next.getTime() + (end.getTime() - start.getTime())) })
  }

  function setDay(d: Date): void {
    const next = atMinutes(startOfDay(d), startMin)
    dispatch('change', { start: next, end: new Date(next.getTime() + (end.getTime() - start.getTime())) })
  }

  function pointerMinutes(clientX: number): number {
    const r = tracks?.getBoundingClientRect()
    if (!r || r.width === 0) {
      return viewFrom
    }
    const ratio = Math.min(1, Math.max(0, (clientX - r.left) / r.width))
    return snapMinutes(viewFrom + ratio * (viewTo - viewFrom), step)
  }

  type Drag = { kind: 'move'; grab: number } | { kind: 'start' } | { kind: 'end' }
  let drag: Drag | null = null

  function down(event: PointerEvent, kind: Drag['kind']): void {
    if (allDay || event.button !== 0) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    drag = kind === 'move' ? { kind, grab: pointerMinutes(event.clientX) - startMin } : { kind }
    frozen = [viewFrom, viewTo]
    dragging = true
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }

  function onMove(event: PointerEvent): void {
    if (!drag) {
      return
    }
    const at = pointerMinutes(event.clientX)
    if (drag.kind === 'move') {
      const from = Math.min(Math.max(viewFrom, at - drag.grab), Math.max(viewFrom, viewTo - Math.min(length, viewTo - viewFrom)))
      if (from !== startMin) {
        moveTo(from)
      }
    } else if (drag.kind === 'start') {
      const from = Math.min(Math.max(viewFrom, at), endMin - step)
      if (from !== startMin) {
        propose(from, endMin)
      }
    } else {
      const to = Math.max(Math.min(viewTo, at), startMin + step)
      if (to !== endMin) {
        propose(startMin, to)
      }
    }
  }

  function onUp(): void {
    drag = null
    dragging = false
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
  }

  onDestroy(onUp)

  function onBandKey(event: KeyboardEvent): void {
    if (allDay || (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight')) {
      return
    }
    event.preventDefault()
    const delta = event.key === 'ArrowRight' ? step : -step
    if (event.shiftKey) {
      const to = Math.min(dayMinutes, Math.max(startMin + step, endMin + delta))
      if (to !== endMin) {
        propose(startMin, to)
      }
      return
    }
    const from = Math.min(dayMinutes - Math.min(length, dayMinutes), Math.max(0, startMin + delta))
    if (from !== startMin) {
      moveTo(from)
    }
  }

  $: bandText = allDay ? $t('calendar.allDay') : `${formatTime(start, timeFormat)} - ${formatTime(end, timeFormat)}`
</script>

<div class="sched">
  <div class="bar">
    <button type="button" class="cal-icon" aria-label={$t('calendar.sched.previousDay')} on:click={() => setDay(addDays(day, -1))}>
      <span class="cal-flip"><Ph svg={caretLeft} size={14} /></span>
    </button>
    <DateField class="ctl" value={day} {weekStart} label={$t('calendar.sched.day')} on:change={(e) => setDay(e.detail)} />
    <button type="button" class="cal-icon" aria-label={$t('calendar.sched.nextDay')} on:click={() => setDay(addDays(day, 1))}>
      <span class="cal-flip"><Ph svg={caretRight} size={14} /></span>
    </button>
    {#if loading}
      <span class="progress"><PieProgress label={$t('calendar.sched.loading')} /></span>
    {/if}
    <span class="spacer"></span>
    <span class="legend" aria-hidden="true">
      <span class="key"><span class="swatch busy"></span>{$t('calendar.sched.legend.busy')}</span>
      <span class="key"><span class="swatch tentative"></span>{$t('calendar.sched.legend.tentative')}</span>
      <span class="key"><span class="swatch unavailable"></span>{$t('calendar.sched.legend.away')}</span>
    </span>
  </div>

  {#if failed}
    <p class="failed" role="alert">
      <Ph svg={warningIcon} size={13} />{$t('calendar.sched.failed')}
      <button type="button" class="link" on:click={() => load(lookupKey)}>{$t('calendar.sched.retry')}</button>
    </p>
  {/if}

  <div class="timeline">
    <div class="labels">
      <div class="head"></div>
      {#each rows as row (row.key)}
        <div class="label" class:you={row.you} class:clash={!row.you && busyNames.has(row.email.toLowerCase())} title={row.email || undefined}>
          <span class="name">{row.name}</span>
        </div>
      {/each}
    </div>
    <div class="tracks" dir="ltr" bind:this={tracks}>
      <div class="head" aria-hidden="true">
        {#each hourList as h (h)}
          <span class="hour" style={`left:${pct(h * 60)}%`}>{formatHour(atMinutes(day, h * 60), timeFormat)}</span>
        {/each}
      </div>
      {#each rows as row (row.key)}
        <div class="track" class:unknown={unknown(row, data)} role="img" aria-label={rowLabel(row)}>
          {#each hourList as h (h)}
            <span class="tick" style={`left:${pct(h * 60)}%`}></span>
          {/each}
          {#if workStart > viewFrom}
            <span class="off" style={`left:0;width:${pct(workStart)}%`}></span>
          {/if}
          {#if workEnd < viewTo}
            <span class="off" style={`left:${pct(workEnd)}%;right:0`}></span>
          {/if}
          {#if unknown(row, data)}
            <span class="none" title={reasonText(row.info)}>{$t('calendar.sched.noInfo')}</span>
          {:else}
            {#each shown(row.busy) as b, i (i)}
              <span class="block {b.kind}" style={placeOf(b)}></span>
            {/each}
          {/if}
        </div>
      {/each}
      <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
      <div
        class="band"
        class:fixed={allDay}
        class:dragging
        style={`left:${pct(allDay ? viewFrom : startMin)}%;width:${pct(allDay ? viewTo : endMin) - pct(allDay ? viewFrom : startMin)}%`}
        role="slider"
        tabindex="0"
        aria-label={$t('calendar.sched.proposed')}
        aria-describedby={allDay ? undefined : hintId}
        aria-valuemin={0}
        aria-valuemax={dayMinutes}
        aria-valuenow={startMin}
        aria-valuetext={bandText}
        aria-readonly={allDay}
        on:pointerdown={(e) => down(e, 'move')}
        on:keydown={onBandKey}
      >
        {#if !allDay}
          <span class="grip start" aria-hidden="true" on:pointerdown={(e) => down(e, 'start')}></span>
          <span class="grip end" aria-hidden="true" on:pointerdown={(e) => down(e, 'end')}></span>
        {/if}
      </div>
    </div>
  </div>

  {#if allDay}
    <p class="note">{$t('calendar.sched.allDayNote')}</p>
  {:else}
    <p class="note" id={hintId}>{$t('calendar.sched.proposedHint')}</p>
  {/if}

  {#if data}
    <div class="summary" aria-live="polite">
      {#if clash.busy.length === 0 && clash.tentative.length === 0}
        <p class="ok">{$t('calendar.sched.allFree')}</p>
      {/if}
      {#if clash.busy.length > 0}
        <p class="clash"><Ph svg={warningIcon} size={13} />{$t('calendar.sched.busy').replace('{names}', names(clash.busy))}</p>
      {/if}
      {#if clash.tentative.length > 0}
        <p>{$t('calendar.sched.tentative').replace('{names}', names(clash.tentative))}</p>
      {/if}
      {#if clash.unknown.length > 0}
        <p class="quiet">{$t('calendar.sched.unknown').replace('{names}', names(clash.unknown))}</p>
      {/if}
    </div>

    {#if !allDay}
      <div class="suggest" role="group" aria-label={$t('calendar.sched.suggested')}>
        <span class="suggest-label">{$t('calendar.sched.suggested')}</span>
        {#each suggestions as s (s.start.getTime())}
          <button type="button" class="pill" on:click={() => dispatch('change', { start: s.start, end: s.end })}>{suggestionLabel(s)}</button>
        {:else}
          <span class="quiet">{$t('calendar.sched.noSuggestions')}</span>
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  .sched {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3) 0;
    --label-width: calc(var(--cal-gutter) * 2.4);
    --row-height: calc(var(--control-height) + var(--space-2));
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
  }

  .bar :global(.ctl) {
    height: var(--control-height);
    padding: 0 var(--space-2);
    border-color: transparent;
    background: transparent;
    font-size: var(--fz-list);
  }

  .bar :global(.ctl:hover) {
    border-color: var(--border-default);
    background: var(--surface-raised);
  }

  .progress {
    display: inline-flex;
    margin-inline-start: var(--space-2);
    color: var(--text-tertiary);
  }

  .spacer {
    flex: 1;
  }

  .legend {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--space-4);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .key {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .swatch {
    width: var(--space-4);
    height: var(--space-3);
    border-radius: calc(var(--radius-control) - 2px);
  }

  .timeline {
    display: flex;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .labels {
    flex: 0 0 var(--label-width);
    min-width: 0;
    border-inline-end: var(--hairline) solid var(--border-subtle);
  }

  .head {
    position: relative;
    height: var(--chip-height);
  }

  .label {
    display: flex;
    align-items: center;
    height: var(--row-height);
    padding-inline: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .label .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .label.you {
    color: var(--text-secondary);
  }

  .label.clash::after {
    content: '';
    flex-shrink: 0;
    width: var(--space-2);
    height: var(--space-2);
    margin-inline-start: var(--space-2);
    border-radius: 50%;
    background: var(--danger);
  }

  .tracks {
    position: relative;
    flex: 1;
    min-width: 0;
  }

  .hour {
    position: absolute;
    top: 50%;
    transform: translate(-50%, -50%);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .track {
    position: relative;
    height: var(--row-height);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .tick {
    position: absolute;
    top: 0;
    bottom: 0;
    border-inline-start: var(--hairline) solid var(--border-subtle);
  }

  .off {
    position: absolute;
    top: 0;
    bottom: 0;
    background: var(--surface-sunken);
    opacity: 0.55;
  }

  .block {
    position: absolute;
    top: var(--space-2);
    bottom: var(--space-2);
    border-radius: calc(var(--radius-control) - 2px);
  }

  .block.busy,
  .swatch.busy {
    background: color-mix(in srgb, var(--text-secondary) 55%, var(--surface-raised));
  }

  .block.tentative,
  .swatch.tentative {
    background: color-mix(in srgb, var(--text-secondary) 18%, var(--surface-raised));
    box-shadow: inset 0 0 0 var(--hairline) var(--text-tertiary);
  }

  .block.unavailable,
  .swatch.unavailable {
    background: color-mix(in srgb, var(--text-primary) 78%, var(--surface-raised));
  }

  .track.unknown {
    background: var(--surface-base);
  }

  .none {
    position: absolute;
    inset: var(--space-2) var(--space-2);
    display: flex;
    align-items: center;
    padding-inline: var(--space-3);
    border: var(--hairline) dashed var(--border-default);
    border-radius: calc(var(--radius-control) - 2px);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .band {
    position: absolute;
    top: var(--chip-height);
    bottom: 0;
    z-index: 1;
    min-width: var(--space-2);
    border-inline: 2px solid var(--accent);
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    cursor: grab;
    touch-action: none;
    transition:
      left var(--motion-fast) var(--ease-out),
      width var(--motion-fast) var(--ease-out);
  }

  .band.dragging {
    cursor: grabbing;
    transition: none;
  }

  .band.fixed {
    cursor: default;
  }

  .band:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .grip {
    position: absolute;
    top: 0;
    bottom: 0;
    width: var(--space-3);
    cursor: ew-resize;
  }

  .grip.start {
    left: calc(-1 * var(--space-2));
  }

  .grip.end {
    right: calc(-1 * var(--space-2));
  }

  .failed,
  .note,
  .summary p,
  .suggest-label,
  .quiet {
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.45;
  }

  .failed {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--danger);
  }

  .link {
    padding: 0 var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    text-decoration: underline;
    cursor: var(--cursor-action);
  }

  .link:hover {
    color: var(--text-primary);
  }

  .note,
  .quiet {
    color: var(--text-tertiary);
  }

  .summary {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    color: var(--text-secondary);
  }

  .summary .ok {
    color: var(--text-secondary);
  }

  .summary .clash {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--danger);
  }

  .suggest {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .suggest-label {
    color: var(--text-tertiary);
  }

  .pill {
    display: inline-flex;
    align-items: center;
    height: var(--chip-height);
    padding-inline: var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .pill:hover {
    background: var(--surface-hover);
    border-color: var(--border-default);
  }

  .pill:focus-visible,
  .link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  @media (prefers-reduced-motion: reduce) {
    .band,
    .pill {
      transition: none;
    }
  }

  :global(:root[data-reduce-motion]) .band,
  :global(:root[data-reduce-motion]) .pill {
    transition: none;
  }
</style>
