<script lang="ts">
  // a small month to pick a day from: the sidebar's navigator, and the date
  // picker of the event editor. it pages through months on its own and follows
  // the selected day when that moves to another month from outside.
  import { createEventDispatcher } from 'svelte'
  import { fly } from 'svelte/transition'
  import caretLeft from '@phosphor-icons/core/assets/regular/caret-left.svg?raw'
  import caretRight from '@phosphor-icons/core/assets/regular/caret-right.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { addMonths, dayKey, monthGrid, sameDay } from '../../lib/caldate'
  import { weekdayName } from '../../lib/recurrence'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'

  /** The selected day. */
  export let value: Date
  /** First day of the week, 0 for Sunday. */
  export let weekStart: number = 1
  /** Days to mark as the range the calendar shows, as [from, to). */
  export let rangeFrom: Date | null = null
  export let rangeTo: Date | null = null
  /** Days that have events, as yyyy-mm-dd, drawn with a dot. */
  export let busy: Set<string> = new Set()

  const dispatch = createEventDispatcher<{ pick: Date }>()
  const today = new Date()

  let shown = new Date(value.getFullYear(), value.getMonth(), 1)
  let direction = 1
  let lastValue = dayKey(value)

  $: if (dayKey(value) !== lastValue) {
    lastValue = dayKey(value)
    if (value.getMonth() !== shown.getMonth() || value.getFullYear() !== shown.getFullYear()) {
      direction = value > shown ? 1 : -1
      shown = new Date(value.getFullYear(), value.getMonth(), 1)
    }
  }

  $: weeks = monthGrid(shown, weekStart)
  $: heads = Array.from({ length: 7 }, (_, i) => weekdayName((weekStart + i) % 7, navigator.language, 'narrow'))
  $: title = shown.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })

  // the title zooms out: from the days of a month to the months of a year to
  // a page of years, so a date far away is a few clicks rather than a click
  // per month.
  type Level = 'days' | 'months' | 'years'
  const yearsPerPage = 12
  let level: Level = 'days'

  $: yearStart = Math.floor(shown.getFullYear() / yearsPerPage) * yearsPerPage
  $: years = Array.from({ length: yearsPerPage }, (_, i) => yearStart + i)
  $: months = Array.from({ length: 12 }, (_, i) =>
    new Date(shown.getFullYear(), i, 1).toLocaleDateString(undefined, { month: 'short' }),
  )
  $: heading =
    level === 'days' ? title : level === 'months' ? String(shown.getFullYear()) : `${yearStart} - ${yearStart + yearsPerPage - 1}`
  $: stepLabels =
    level === 'days'
      ? [$t('calendar.previousMonth'), $t('calendar.nextMonth')]
      : level === 'months'
        ? [$t('calendar.previousYear'), $t('calendar.nextYear')]
        : [$t('calendar.previousYears'), $t('calendar.nextYears')]

  // the arrows page by what is on screen: a month, a year, or a page of years.
  // with Shift held, the days view pages by a whole year.
  function page(dir: number, event?: MouseEvent): void {
    direction = dir
    const monthsBy = level === 'days' ? (event?.shiftKey ? 12 : 1) : level === 'months' ? 12 : 12 * yearsPerPage
    shown = addMonths(shown, dir * monthsBy)
  }

  function zoomOut(): void {
    level = level === 'days' ? 'months' : level === 'months' ? 'years' : 'days'
  }

  function pickMonth(month: number): void {
    shown = new Date(shown.getFullYear(), month, 1)
    level = 'days'
  }

  function pickYear(year: number): void {
    shown = new Date(year, shown.getMonth(), 1)
    level = 'months'
  }

  function inRange(d: Date): boolean {
    return rangeFrom !== null && rangeTo !== null && d >= rangeFrom && d < rangeTo
  }

  // arrow keys move the selection a day or a week, as in a native date picker.
  function onKeydown(event: KeyboardEvent): void {
    const rtl = document.documentElement.dir === 'rtl'
    const steps: Record<string, number> = {
      ArrowLeft: rtl ? 1 : -1,
      ArrowRight: rtl ? -1 : 1,
      ArrowUp: -7,
      ArrowDown: 7,
    }
    const step = steps[event.key]
    if (step === undefined) {
      return
    }
    event.preventDefault()
    const next = new Date(value)
    next.setDate(next.getDate() + step)
    dispatch('pick', next)
    requestAnimationFrame(() => {
      const root = (event.target as HTMLElement).closest('.mini')
      root?.querySelector<HTMLElement>(`[data-day="${dayKey(next)}"]`)?.focus()
    })
  }
</script>

<div class="mini">
  <div class="head">
    <button type="button" class="title" title={$t('calendar.zoomOut')} aria-live="polite" on:click={zoomOut}>
      {heading}
    </button>
    <div class="nav">
      <button type="button" class="step" aria-label={stepLabels[0]} title={stepLabels[0]} on:click={(e) => page(-1, e)}>
        <span class="flip"><Ph svg={caretLeft} size={13} /></span>
      </button>
      <button type="button" class="step" aria-label={stepLabels[1]} title={stepLabels[1]} on:click={(e) => page(1, e)}>
        <span class="flip"><Ph svg={caretRight} size={13} /></span>
      </button>
    </div>
  </div>
  {#if level === 'months'}
    {#key shown.getFullYear()}
      <div class="picker" in:fly={{ x: 10 * direction, duration: dur(motion.base) }}>
        {#each months as name, m (m)}
          <button
            type="button"
            class="cell"
            class:selected={m === value.getMonth() && shown.getFullYear() === value.getFullYear()}
            class:today={m === today.getMonth() && shown.getFullYear() === today.getFullYear()}
            on:click={() => pickMonth(m)}
          >
            {name}
          </button>
        {/each}
      </div>
    {/key}
  {:else if level === 'years'}
    {#key yearStart}
      <div class="picker" in:fly={{ x: 10 * direction, duration: dur(motion.base) }}>
        {#each years as year (year)}
          <button
            type="button"
            class="cell"
            class:selected={year === value.getFullYear()}
            class:today={year === today.getFullYear()}
            on:click={() => pickYear(year)}
          >
            {year}
          </button>
        {/each}
      </div>
    {/key}
  {:else}
  <div class="grid heads" aria-hidden="true">
    {#each heads as h, i (i)}
      <span class="wd">{h}</span>
    {/each}
  </div>
  {#key dayKey(shown)}
    <div class="grid days" role="grid" aria-label={title} in:fly={{ x: 10 * direction, duration: dur(motion.base) }}>
      {#each weeks as week, w (w)}
        <div class="week" role="row">
          {#each week as day (dayKey(day))}
            {@const selected = sameDay(day, value)}
            <button
              type="button"
              role="gridcell"
              class="day"
              class:other={day.getMonth() !== shown.getMonth()}
              class:today={sameDay(day, today)}
              class:selected
              class:range={inRange(day)}
              aria-selected={selected}
              aria-current={sameDay(day, today) ? 'date' : undefined}
              aria-label={day.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })}
              tabindex={selected ? 0 : -1}
              data-day={dayKey(day)}
              on:click={() => dispatch('pick', day)}
              on:keydown={onKeydown}
            >
              {day.getDate()}
              {#if busy.has(dayKey(day))}
                <span class="dot" aria-hidden="true"></span>
              {/if}
            </button>
          {/each}
        </div>
      {/each}
    </div>
  {/key}
  {/if}
</div>

<style>
  .mini {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    user-select: none;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-inline-start: var(--space-2);
  }

  .head {
    padding-inline-start: 0;
  }

  .title {
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    font: inherit;
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    font-variant-numeric: tabular-nums;
    color: var(--text-primary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .title:hover {
    background: var(--surface-hover);
  }

  /* the months of a year, or a page of years: three across, the same height
     as the six weeks they stand in for so the picker does not jump. */
  .picker {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--space-1);
    min-height: calc(20px + 6 * 26px + 5 * var(--space-1) + var(--space-2));
  }

  .cell {
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    font-variant-numeric: tabular-nums;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .cell:hover {
    background: var(--surface-hover);
  }

  .cell.today {
    color: var(--accent);
    font-weight: var(--fw-semibold);
  }

  .cell.selected {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .nav {
    display: flex;
    gap: var(--space-1);
  }

  .step {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .step:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  :global([dir='rtl']) .flip {
    display: inline-flex;
    transform: scaleX(-1);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
  }

  .wd {
    text-align: center;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    line-height: 20px;
  }

  .days {
    row-gap: var(--space-1);
  }

  .week {
    display: contents;
  }

  .day {
    position: relative;
    height: 26px;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    font-variant-numeric: tabular-nums;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .day:hover {
    background: var(--surface-hover);
  }

  .day.other {
    color: var(--text-tertiary);
  }

  .day.range {
    background: var(--surface-sunken);
    border-radius: 0;
  }

  .day.today {
    color: var(--accent);
    font-weight: var(--fw-semibold);
  }

  .day.selected {
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-control);
  }

  .dot {
    position: absolute;
    bottom: 3px;
    inset-inline-start: 50%;
    width: 3px;
    height: 3px;
    margin-inline-start: -1.5px;
    border-radius: 50%;
    background: currentColor;
    opacity: 0.55;
  }
</style>
