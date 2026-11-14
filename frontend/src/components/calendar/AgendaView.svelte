<script lang="ts">
  // the agenda: the coming days that have events, each as a heading with its
  // events under it in order. days without events are left out, so a quiet
  // month reads as a short list.
  import { createEventDispatcher } from 'svelte'
  import mapPinIcon from '@phosphor-icons/core/assets/duotone/map-pin-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { addDays, dayKey, eventsOnDay, sameDay, startOfDay, type Placed } from '../../lib/caldate'
  import { formatLongDay, formatTime } from '../../lib/calformat'
  import { occurrenceKey } from './occurrence'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence } from '../../lib/types'

  /** The first day listed and the day after the last. */
  export let from: Date
  export let to: Date
  export let placed: Placed<Occurrence>[]
  export let colors: Map<number, string>
  export let now: Date
  export let timeFormat: TimeFormat = 'auto'
  export let selectedKey: string = ''

  const dispatch = createEventDispatcher<{ open: { occ: Occurrence; anchor: HTMLElement }; day: Date }>()

  $: days = listDays(from, to, placed)

  function listDays(first: Date, end: Date, list: Placed<Occurrence>[]): { day: Date; items: Placed<Occurrence>[] }[] {
    const out: { day: Date; items: Placed<Occurrence>[] }[] = []
    for (let day = startOfDay(first); day < end; day = addDays(day, 1)) {
      const items = eventsOnDay(list, day)
      if (items.length) {
        out.push({ day, items })
      }
    }
    return out
  }

  function colorOf(occ: Occurrence): string {
    return colors.get(occ.calendarId) ?? occ.calendarColor ?? 'var(--cal-1)'
  }

  function timeOf(p: Placed<Occurrence>, day: Date): string {
    if (p.occ.allDay) {
      return $t('calendar.allDay')
    }
    const startsToday = sameDay(p.start, day)
    const endsToday = p.end <= addDays(startOfDay(day), 1)
    if (!startsToday && !endsToday) {
      return $t('calendar.allDay')
    }
    const start = startsToday ? formatTime(p.start, timeFormat) : ''
    const end = endsToday ? formatTime(p.end, timeFormat) : ''
    return `${start} - ${end}`.trim()
  }
</script>

<div class="agenda">
  {#if days.length === 0}
    <div class="empty">
      <Ph svg={calendarIcon} size={28} />
      <p>{$t('calendar.agendaEmpty')}</p>
    </div>
  {:else}
    {#each days as { day, items } (dayKey(day))}
      <section class="day" class:today={sameDay(day, now)}>
        <button type="button" class="date" on:click={() => dispatch('day', day)}>
          <span class="num">{day.getDate()}</span>
          <span class="label">{formatLongDay(day)}</span>
        </button>
        <ul>
          {#each items as p (occurrenceKey(p.occ))}
            <li>
              <button
                type="button"
                class="row"
                class:selected={selectedKey === occurrenceKey(p.occ)}
                class:cancelled={p.occ.status === 'CANCELLED'}
                style={`--ev:${colorOf(p.occ)}`}
                on:click={(e) => dispatch('open', { occ: p.occ, anchor: e.currentTarget })}
              >
                <span class="time">{timeOf(p, day)}</span>
                <span class="edge" aria-hidden="true"></span>
                <span class="what">
                  <span class="title">
                    {p.occ.summary || $t('calendar.untitled')}
                    {#if p.occ.recurring}<Ph svg={repeatIcon} size={11} />{/if}
                  </span>
                  {#if p.occ.location}
                    <span class="where"><Ph svg={mapPinIcon} size={11} />{p.occ.location}</span>
                  {/if}
                </span>
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  {/if}
</div>

<style>
  .agenda {
    height: 100%;
    overflow-y: auto;
    padding: var(--space-3) var(--space-5) var(--space-6);
  }

  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    height: 100%;
    color: var(--text-tertiary);
    font-size: var(--fz-list);
  }

  .empty p {
    margin: 0;
  }

  .day {
    display: grid;
    grid-template-columns: minmax(0, calc(var(--cal-sidebar-width) * 0.8)) minmax(0, 1fr);
    gap: var(--space-4);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .date {
    display: flex;
    align-items: baseline;
    align-self: start;
    gap: var(--space-3);
    min-width: 0;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .date:hover {
    background: var(--surface-hover);
  }

  .num {
    font-size: var(--fz-title);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .label {
    min-width: 0;
    font-size: var(--fz-label);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .today .num,
  .today .label {
    color: var(--accent);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, calc(var(--cal-gutter) * 2.4)) var(--cal-edge) minmax(0, 1fr);
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    min-height: var(--control-height);
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .row:hover,
  .row.selected {
    background: var(--surface-hover);
  }

  .row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .time {
    font-size: var(--fz-label);
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .edge {
    align-self: stretch;
    border-radius: var(--radius-control);
    background: var(--ev);
  }

  .what {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .title {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cancelled .title {
    text-decoration: line-through;
    opacity: 0.7;
  }

  .where {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
