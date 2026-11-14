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

  function page(dir: number): void {
    direction = dir
    shown = addMonths(shown, dir)
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
    <span class="title">{title}</span>
    <div class="nav">
      <button type="button" class="step" aria-label={$t('calendar.previousMonth')} on:click={() => page(-1)}>
        <span class="flip"><Ph svg={caretLeft} size={13} /></span>
      </button>
      <button type="button" class="step" aria-label={$t('calendar.nextMonth')} on:click={() => page(1)}>
        <span class="flip"><Ph svg={caretRight} size={13} /></span>
      </button>
    </div>
  </div>
  <div class="grid heads" aria-hidden="true">
    {#each heads as h, i (i)}
      <span class="wd">{h}</span>
    {/each}
  </div>
  {#key dayKey(shown)}
    <div class="grid days" role="grid" aria-label={title} in:fly={{ x: 10 * direction, duration: dur(motion.base) }}>
      {#each weeks as week, w (w)}
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
      {/each}
    </div>
  {/key}
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

  .title {
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
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
