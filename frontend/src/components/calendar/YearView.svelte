<script lang="ts">
  // the year view: the twelve months of a year on one screen, four across
  // when there is room and fewer as the window narrows. each day is shaded in
  // a few neutral steps by how many events it holds, multi-day events run as
  // thin bars across their days where the days are wide enough, and today and
  // the focused day take the accent. hovering or focusing a day lists its
  // events in a small popover; clicking a day opens it, clicking a month's
  // title opens that month. arrow keys move between days.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import { weekdayName } from '../../lib/recurrence'
  import { dayKey, sameDay, startOfDay, type Placed } from '../../lib/caldate'
  import { formatLongDay, formatTime } from '../../lib/calformat'
  import { aggregateYear, barsFit, busyLevel, multiDayEntries, stepDay, yearColumns, yearMonth } from '../../lib/yearplan'
  import { occurrenceKey } from './occurrence'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence } from '../../lib/types'

  /** The year shown. */
  export let year: number
  /** The day the calendar is on; it carries the selection when it falls in the year. */
  export let focus: Date
  export let now: Date
  /** First day of the week, 0 for Sunday. */
  export let weekStart: number
  /** The entries of the year; hidden calendars are left out by the caller. */
  export let placed: Placed<Occurrence>[]
  export let colors: Map<number, string>
  export let timeFormat: TimeFormat = 'auto'

  const dispatch = createEventDispatcher<{ day: Date; month: Date }>()

  const hoverDelay = 220
  const popoverGap = 6
  const listed = 6

  let width = 0
  let scroller: HTMLDivElement
  let tipEl: HTMLDivElement | undefined
  let tipDay: { key: string; day: Date; anchor: HTMLElement } | null = null
  let tipPos = { left: 0, top: 0 }
  let timer: ReturnType<typeof setTimeout> | undefined
  let cursorKey = ''

  $: index = aggregateYear(placed, year)
  $: multi = multiDayEntries(placed)
  $: cols = yearColumns(width)
  $: bars = barsFit(width, cols)
  $: months = Array.from({ length: 12 }, (_, m) => ({
    month: m,
    date: new Date(year, m, 1),
    name: new Date(year, m, 1).toLocaleDateString(undefined, { month: 'long' }),
    weeks: yearMonth(year, m, weekStart, multi),
  }))
  $: heads = Array.from({ length: 7 }, (_, i) => (weekStart + i) % 7)
  $: today = startOfDay(now)
  $: selectedKey = focus.getFullYear() === year ? dayKey(focus) : ''
  $: homeKey = selectedKey || (today.getFullYear() === year ? dayKey(today) : `${year}-01-01`)
  $: if (!cursorKey || cursorKey.slice(0, 4) !== String(year)) {
    cursorKey = homeKey
  }
  $: rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl'

  function colorOf(occ: Occurrence): string {
    return colors.get(occ.calendarId) ?? occ.calendarColor ?? 'var(--cal-1)'
  }

  function countLabel(n: number): string {
    if (n === 0) {
      return $t('calendar.year.noEvents')
    }
    return n === 1 ? $t('calendar.year.oneEvent') : $t('calendar.year.events').replace('{n}', String(n))
  }

  function dayLabel(day: Date): string {
    return `${formatLongDay(day, undefined, now)}, ${countLabel(index.get(dayKey(day))?.length ?? 0)}`
  }

  // --- the day popover ------------------------------------------------------------

  function showTip(day: Date, anchor: HTMLElement): void {
    tipDay = { key: dayKey(day), day, anchor }
    void placeTip()
  }

  function hideTip(): void {
    clearTimeout(timer)
    tipDay = null
  }

  function onEnter(day: Date, event: PointerEvent): void {
    clearTimeout(timer)
    const anchor = event.currentTarget as HTMLElement
    timer = setTimeout(() => showTip(day, anchor), hoverDelay)
  }

  // the popover sits under its day, and flips to the other side of it or the
  // other edge where it would leave the scrolled area.
  async function placeTip(): Promise<void> {
    await tick()
    if (!tipDay || !tipEl || !scroller) {
      return
    }
    let x = 0
    let y = 0
    for (let el: HTMLElement | null = tipDay.anchor; el && el !== scroller; el = el.offsetParent as HTMLElement | null) {
      x += el.offsetLeft
      y += el.offsetTop
    }
    const w = tipEl.offsetWidth
    const h = tipEl.offsetHeight
    const cellW = tipDay.anchor.offsetWidth
    const cellH = tipDay.anchor.offsetHeight
    let left = x
    if (left + w > scroller.clientWidth) {
      left = Math.max(0, x + cellW - w)
    }
    let top = y + cellH + popoverGap
    if (top + h > scroller.scrollTop + scroller.clientHeight && y - popoverGap - h >= scroller.scrollTop) {
      top = y - popoverGap - h
    }
    tipPos = { left, top }
  }

  onDestroy(() => clearTimeout(timer))

  // --- keys -----------------------------------------------------------------------

  async function moveTo(day: Date): Promise<void> {
    if (day.getFullYear() !== year) {
      return
    }
    cursorKey = dayKey(day)
    await tick()
    scroller?.querySelector<HTMLElement>(`[data-day="${cursorKey}"]`)?.focus()
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && tipDay) {
      hideTip()
      return
    }
    if (event.metaKey || event.ctrlKey || event.altKey || !(event.target as HTMLElement).dataset?.day) {
      return
    }
    const next = stepDay(new Date(`${(event.target as HTMLElement).dataset.day}T00:00:00`), event.key, rtl)
    if (next) {
      event.preventDefault()
      event.stopPropagation()
      void moveTo(next)
    }
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div
  class="year"
  role="group"
  aria-label={$t('calendar.year.label').replace('{year}', String(year))}
  bind:this={scroller}
  bind:clientWidth={width}
  on:keydown={onKeydown}
  on:scroll={hideTip}
>
  <div class="months" style={`--cols:${cols}`}>
    {#each months as m (m.month)}
      <section class="month" aria-label={m.name}>
        <button
          type="button"
          class="title"
          aria-label={$t('calendar.year.openMonth').replace('{month}', m.name)}
          on:click={() => dispatch('month', m.date)}
        >
          {m.name}
        </button>
        <div class="heads" aria-hidden="true">
          <span class="wn"></span>
          {#each heads as d (d)}
            <span class:weekend={d === 0 || d === 6}>{weekdayName(d, navigator.language, 'narrow')}</span>
          {/each}
        </div>
        {#each m.weeks as week (dayKey(week.days.find((d) => d) ?? m.date))}
          <div class="week">
            <span class="wn" title={$t('calendar.year.weekNumber').replace('{n}', String(week.number))}>{week.number}</span>
            {#each week.days as day, i (i)}
              {#if day}
                {@const key = dayKey(day)}
                {@const entries = index.get(key) ?? []}
                <button
                  type="button"
                  class="day lv-{busyLevel(entries.length)}"
                  class:today={sameDay(day, today)}
                  class:selected={key === selectedKey}
                  class:weekend={day.getDay() === 0 || day.getDay() === 6}
                  aria-current={sameDay(day, today) ? 'date' : undefined}
                  aria-pressed={key === selectedKey}
                  data-day={key}
                  tabindex={key === cursorKey ? 0 : -1}
                  aria-label={dayLabel(day)}
                  aria-describedby={tipDay?.key === key ? 'year-tip' : undefined}
                  on:click={() => dispatch('day', day)}
                  on:pointerenter={(e) => onEnter(day, e)}
                  on:pointerleave={hideTip}
                  on:focus={(e) => {
                    cursorKey = key
                    showTip(day, e.currentTarget)
                  }}
                  on:blur={hideTip}
                >
                  {day.getDate()}
                </button>
              {:else}
                <span class="blank" aria-hidden="true"></span>
              {/if}
            {/each}
            {#if bars && week.bars.length > 0}
              <div class="bars" aria-hidden="true">
                {#each week.bars as s (occurrenceKey(s.placed.occ))}
                  <span
                    class="bar"
                    class:before={s.before}
                    class:after={s.after}
                    style={`grid-column:${s.col + 1} / span ${s.span};grid-row:${s.lane + 1};--ev:${colorOf(s.placed.occ)}`}
                  ></span>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </section>
    {/each}
  </div>

  {#if tipDay}
    {@const entries = index.get(tipDay.key) ?? []}
    <div id="year-tip" class="tip" role="tooltip" bind:this={tipEl} style={`left:${tipPos.left}px;top:${tipPos.top}px`}>
      <h4>{formatLongDay(tipDay.day, undefined, now)}</h4>
      {#if entries.length === 0}
        <p class="none">{$t('calendar.year.noEvents')}</p>
      {:else}
        <ul>
          {#each entries.slice(0, listed) as p (occurrenceKey(p.occ))}
            <li style={`--ev:${colorOf(p.occ)}`}>
              <span class="dot" aria-hidden="true"></span>
              <span class="when">{p.occ.allDay ? $t('calendar.allDay') : formatTime(p.start, timeFormat)}</span>
              <span class="what">{p.occ.summary || $t('calendar.untitled')}</span>
            </li>
          {/each}
        </ul>
        {#if entries.length > listed}
          <p class="none">{$t('calendar.more').replace('{n}', String(entries.length - listed))}</p>
        {/if}
      {/if}
    </div>
  {/if}
</div>

<style>
  .year {
    position: relative;
    height: 100%;
    padding: var(--space-5) var(--space-5) var(--space-6);
    overflow: auto;
    user-select: none;
  }

  .months {
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    gap: var(--space-6) var(--space-5);
  }

  .month {
    min-width: 0;
  }

  .title {
    display: block;
    margin-bottom: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    cursor: var(--cursor-action);
  }

  .title:hover {
    background: var(--surface-hover);
  }

  .heads,
  .week {
    position: relative;
    display: grid;
    grid-template-columns: calc(var(--fz-meta) * 2) repeat(7, minmax(0, 1fr));
    column-gap: var(--hairline);
  }

  .heads span {
    padding-bottom: var(--space-1);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    text-align: center;
    text-transform: uppercase;
  }

  .wn {
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
  }

  .day {
    position: relative;
    height: calc(var(--fz-body) * 2.1);
    padding: var(--space-1) 0 0;
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    font-variant-numeric: tabular-nums;
    line-height: 1.2;
    text-align: center;
    vertical-align: top;
    cursor: var(--cursor-action);
  }

  .day.weekend {
    color: var(--text-tertiary);
  }

  .day.lv-1 {
    background: var(--year-busy-1);
  }

  .day.lv-2 {
    background: var(--year-busy-2);
  }

  .day.lv-3 {
    background: var(--year-busy-3);
  }

  .day.lv-4 {
    background: var(--year-busy-4);
  }

  .day:hover {
    border-color: var(--border-strong);
  }

  .day.today {
    color: var(--accent);
    font-weight: var(--fw-semibold);
  }

  .day.selected {
    border-color: var(--accent);
  }

  .day:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .bars {
    position: absolute;
    inset-block-end: var(--space-1);
    inset-inline: calc(var(--fz-meta) * 2) 0;
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    grid-template-rows: repeat(2, var(--year-bar));
    row-gap: var(--hairline);
    pointer-events: none;
  }

  .bar {
    margin-inline: var(--space-1);
    border-radius: var(--year-bar);
    background: var(--ev);
  }

  .bar.before {
    margin-inline-start: 0;
    border-start-start-radius: 0;
    border-end-start-radius: 0;
  }

  .bar.after {
    margin-inline-end: 0;
    border-start-end-radius: 0;
    border-end-end-radius: 0;
  }

  .tip {
    position: absolute;
    z-index: 5;
    width: var(--year-popover-width);
    max-width: calc(100% - var(--space-4));
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-primary);
    pointer-events: none;
  }

  .tip h4 {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .tip ul {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .tip li {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    min-width: 0;
    font-size: var(--fz-label);
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    background: var(--ev);
  }

  .when {
    flex-shrink: 0;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .what {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .none {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }
</style>
