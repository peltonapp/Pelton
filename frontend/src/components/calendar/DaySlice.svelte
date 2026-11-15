<script lang="ts">
  // a few hours of one day drawn as a short column: the hour lines with their
  // labels, the timed events laid out side by side where they overlap, and a
  // proposed time drawn over them. it only shows; the date preview of a mail
  // opens it to say what is around the time the mail names.
  import { atMinutes, layoutDay, type Placed } from '../../lib/caldate'
  import { calendarColor, formatHour } from '../../lib/calformat'
  import { isBusy } from '../../lib/daypeek'
  import { occurrenceKey } from './occurrence'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence } from '../../lib/types'

  /** The day shown. */
  export let day: Date
  /** The timed events of the day. */
  export let timed: Placed<Occurrence>[]
  /** The hours shown, minutes after midnight. */
  export let from: number
  export let to: number
  /** The proposed time, drawn over the events. */
  export let slot: { start: Date; end: Date } | null = null
  /** The slot overlaps a busy event. */
  export let clash: boolean = false
  export let timeFormat: TimeFormat = 'auto'

  $: span = Math.max(60, to - from)
  $: hours = Array.from({ length: Math.floor(span / 60) + 1 }, (_, i) => from + i * 60)
  $: boxes = layoutDay(timed, day, 15).filter((b) => b.bottom > from && b.top < to)
  $: slotTop = slot ? clampMinutes((slot.start.getTime() - atMinutes(day, 0).getTime()) / 60000) : 0
  $: slotBottom = slot ? clampMinutes((slot.end.getTime() - atMinutes(day, 0).getTime()) / 60000) : 0

  function clampMinutes(m: number): number {
    return Math.min(to, Math.max(from, m))
  }

  function pct(minutes: number): string {
    return `${((minutes - from) / span) * 100}%`
  }

  function colorOf(occ: Occurrence): string {
    return calendarColor({ id: occ.calendarId, color: occ.calendarColor })
  }
</script>

<div class="slice" style={`--hours:${span / 60}`}>
  <div class="gutter" aria-hidden="true">
    {#each hours as m (m)}
      <span class="hour" style={`top:${pct(m)}`}>{formatHour(atMinutes(day, m), timeFormat)}</span>
    {/each}
  </div>
  <div class="col">
    {#each hours as m (m)}
      <i class="line" style={`top:${pct(m)}`} aria-hidden="true"></i>
    {/each}
    {#each boxes as b (occurrenceKey(b.placed.occ))}
      {@const occ = b.placed.occ}
      <div
        class="ev"
        class:free={!isBusy(occ)}
        style={`top:${pct(Math.max(from, b.top))};height:${pct(from + Math.min(to, b.bottom) - Math.max(from, b.top))};--col:${b.col};--cols:${b.cols};--ev:${colorOf(occ)}`}
      >
        <span class="title">{occ.summary}</span>
      </div>
    {/each}
    {#if slot && slotBottom > slotTop}
      <div class="slot" class:clash style={`top:${pct(slotTop)};height:${pct(from + slotBottom - slotTop)}`} aria-hidden="true"></div>
    {/if}
  </div>
</div>

<style>
  .slice {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-2);
    height: calc(var(--hours) * var(--date-peek-hour));
    padding-block: var(--space-2);
  }

  .gutter {
    position: relative;
    min-width: calc(var(--cal-gutter) * 0.7);
  }

  .hour {
    position: absolute;
    inset-inline-end: 0;
    transform: translateY(-50%);
    font-size: calc(var(--fz-meta) - 1px);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .col {
    position: relative;
  }

  .line {
    position: absolute;
    inset-inline: 0;
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .ev {
    position: absolute;
    inset-inline-start: calc(var(--col) / var(--cols) * 100%);
    width: calc(100% / var(--cols) - 2 * var(--hairline));
    min-height: var(--hairline);
    overflow: hidden;
    padding: 0 var(--space-1);
    border-inline-start: var(--cal-edge) solid var(--ev);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--ev) 16%, var(--surface-raised));
    color: var(--text-primary);
    font-size: calc(var(--fz-meta) - 1px);
    line-height: 1.35;
  }

  .ev.free {
    background: transparent;
    border: var(--hairline) dashed color-mix(in srgb, var(--ev) 60%, var(--border-default));
    border-inline-start: var(--cal-edge) solid var(--ev);
  }

  .title {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .slot {
    position: absolute;
    inset-inline: 0;
    border: calc(var(--hairline) * 1.5) solid var(--accent);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    pointer-events: none;
  }

  .slot.clash {
    border-color: var(--danger);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }
</style>
