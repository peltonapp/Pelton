<script lang="ts">
  // the month view: the weeks of the month as rows of days. all-day and
  // multi-day events run as filled bars across the days they cover; timed ones
  // are a line with a dot, their time and their title. a day with more than
  // fits says how many more, and that opens the day's list. double-clicking a
  // day starts an event on it, and mail dropped on a day starts one from the
  // message.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { openScriptMenu } from '../../lib/scriptmenu'
  import { maildrop, type MailDropOptions } from '../../stores/maildrag'
  import { mailDropHint } from './maildrop'
  import { dayKey, eventsOnDay, isLaneEvent, layoutRow, parseDayKey, rowOverflow, sameDay, startOfDay, type Placed, type RowSpan } from '../../lib/caldate'
  import TaskCheck from '../tasks/TaskCheck.svelte'
  import { weekdayName } from '../../lib/recurrence'
  import { formatLongDay, formatTime } from '../../lib/calformat'
  import { occurrenceKey } from './occurrence'
  import type { CellMenuAt, MailDropAt } from '../../stores/calendar'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence } from '../../lib/types'

  /** The weeks shown, seven days each. */
  export let weeks: Date[][]
  /** The month the view is on; days outside it are dimmed. */
  export let month: number
  export let placed: Placed<Occurrence>[]
  export let colors: Map<number, string>
  export let now: Date
  export let weekStart: number
  export let timeFormat: TimeFormat = 'auto'
  /** The day the calendar is on. */
  export let focus: Date
  export let selectedKey: string = ''

  const dispatch = createEventDispatcher<{
    open: { occ: Occurrence; anchor: HTMLElement }
    pick: Date
    day: Date
    create: { day: Date; anchor: HTMLElement }
    mailDrop: MailDropAt
    menu: CellMenuAt
    toggleTask: Occurrence
    moveTask: { occ: Occurrence; day: Date }
  }>()

  let grid: HTMLDivElement
  let lanes = 3
  let observer: ResizeObserver | undefined

  // how many lines of events fit under a day's number, from the measured row.
  function measure(): void {
    const row = grid?.querySelector<HTMLElement>('.week')
    const head = grid?.querySelector<HTMLElement>('.num')
    if (!row || !head) {
      return
    }
    const style = getComputedStyle(row)
    const lane = parseFloat(style.getPropertyValue('--cal-lane-height')) || 20
    const gap = parseFloat(style.getPropertyValue('--space-1')) || 2
    const room = row.clientHeight - head.offsetHeight - gap * 4
    lanes = Math.max(1, Math.floor(room / (lane + gap)))
  }

  onMount(() => {
    measure()
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(measure)
      observer.observe(grid)
    }
  })

  onDestroy(() => observer?.disconnect())

  $: heads = Array.from({ length: 7 }, (_, i) => (weekStart + i) % 7)
  $: rows = weeks.map((week) => {
    const spans = layoutRow(placed, week)
    return { week, spans, ...rowOverflow(spans, 7, lanes) }
  })
  $: today = startOfDay(now)

  function colorOf(occ: Occurrence): string {
    return colors.get(occ.calendarId) ?? occ.calendarColor ?? 'var(--cal-1)'
  }

  function isBar(s: RowSpan<Occurrence>): boolean {
    return isLaneEvent(s.placed) || s.span > 1
  }

  // the list a "more" link opens.
  let moreDay: Date | null = null
  let moreAnchor: HTMLElement | null = null

  function openMore(day: Date, event: MouseEvent): void {
    moreDay = day
    moreAnchor = event.currentTarget as HTMLElement
  }

  function openFromList(occ: Occurrence): void {
    const anchor = moreAnchor
    moreDay = null
    if (anchor) {
      dispatch('open', { occ, anchor })
    }
  }

  function dropOn(day: Date): MailDropOptions {
    return {
      onDrop: (items, info) => {
        if (items.length > 0) {
          dispatch('mailDrop', { messageIds: items.map((m) => m.id), at: startOfDay(day), mode: 'day', shift: info.shift })
        }
      },
      hint: (items, shift) => mailDropHint(items.length, shift, true),
    }
  }

  // dragging a task to another day: the items stop catching the pointer while
  // it moves, so the day under it can be found.
  let taskDrag: { occ: Occurrence; x: number; y: number; moved: boolean } | null = null
  let overDay: HTMLElement | null = null
  let justDragged = false

  function onTaskDown(event: PointerEvent, occ: Occurrence): void {
    if (event.button !== 0 || !occ.task || occ.task.readOnly) {
      return
    }
    taskDrag = { occ, x: event.clientX, y: event.clientY, moved: false }
    window.addEventListener('pointermove', onTaskMove)
    window.addEventListener('pointerup', onTaskUp)
  }

  function setOverDay(el: HTMLElement | null): void {
    if (el !== overDay) {
      overDay?.classList.remove('task-over')
      el?.classList.add('task-over')
      overDay = el
    }
  }

  function onTaskMove(event: PointerEvent): void {
    if (!taskDrag) {
      return
    }
    if (!taskDrag.moved && Math.hypot(event.clientX - taskDrag.x, event.clientY - taskDrag.y) > 4) {
      taskDrag = { ...taskDrag, moved: true }
    }
    if (taskDrag.moved) {
      setOverDay((document.elementFromPoint(event.clientX, event.clientY) as HTMLElement | null)?.closest<HTMLElement>('.cell[data-day]') ?? null)
    }
  }

  function onTaskUp(): void {
    window.removeEventListener('pointermove', onTaskMove)
    window.removeEventListener('pointerup', onTaskUp)
    const drag = taskDrag
    const target = overDay?.dataset.day
    setOverDay(null)
    taskDrag = null
    if (drag?.moved) {
      justDragged = true
      setTimeout(() => (justDragged = false), 0)
      if (target) {
        dispatch('moveTask', { occ: drag.occ, day: parseDayKey(target) })
      }
    }
  }

  onDestroy(() => {
    window.removeEventListener('pointermove', onTaskMove)
    window.removeEventListener('pointerup', onTaskUp)
  })

  function onCellMenu(event: MouseEvent, day: Date): void {
    if ((event.target as HTMLElement).closest('.item, .more')) {
      return
    }
    event.preventDefault()
    dispatch('menu', { at: startOfDay(day), mode: 'day', x: event.clientX, y: event.clientY })
  }
</script>

<div class="month" class:task-dragging={taskDrag?.moved} bind:this={grid}>
  <div class="heads" aria-hidden="true">
    {#each heads as d (d)}
      <span class:weekend={d === 0 || d === 6}>{weekdayName(d, navigator.language, 'short')}</span>
    {/each}
  </div>
  <div class="weeks" style={`--rows:${weeks.length}`}>
    {#each rows as row (dayKey(row.week[0]))}
      <div class="week" style={`--lanes:${row.shown + 1}`}>
        {#each row.week as day, i (dayKey(day))}
          <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
          <div
            class="cell"
            data-day={dayKey(day)}
            class:other={day.getMonth() !== month}
            class:today={sameDay(day, today)}
            class:focus={sameDay(day, focus)}
            class:weekend={day.getDay() === 0 || day.getDay() === 6}
            style={`grid-column:${i + 1}`}
            use:maildrop={dropOn(day)}
            on:click={() => dispatch('pick', day)}
            on:dblclick={(e) => dispatch('create', { day, anchor: e.currentTarget })}
            on:contextmenu={(e) => onCellMenu(e, day)}
          >
            <button
              type="button"
              class="num"
              aria-label={formatLongDay(day)}
              aria-current={sameDay(day, today) ? 'date' : undefined}
              on:click|stopPropagation={() => dispatch('day', day)}
            >
              {day.getDate() === 1 ? day.toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) : day.getDate()}
            </button>
          </div>
        {/each}
        <div class="items">
          {#each row.spans.filter((s) => s.lane < row.shown) as s (occurrenceKey(s.placed.occ))}
            {@const occ = s.placed.occ}
            {#if occ.task}
              <div
                class="item task"
                class:done={occ.task.done}
                class:dragging={taskDrag?.occ === occ && taskDrag.moved}
                style={`grid-column:${s.col + 1} / span 1;grid-row:${s.lane + 1};--ev:${colorOf(occ)}`}
              >
                <TaskCheck
                  checked={occ.task.done}
                  label={$t(occ.task.done ? 'tasks.inCalendarOpen' : 'tasks.inCalendarDone').replace('{title}', occ.summary || $t('tasks.untitled'))}
                  color={colorOf(occ)}
                  disabled={occ.task.readOnly}
                  on:toggle={() => dispatch('toggleTask', occ)}
                />
                <button type="button" class="task-title" on:pointerdown={(e) => onTaskDown(e, occ)} on:click={(e) => !justDragged && dispatch('open', { occ, anchor: e.currentTarget })}>
                  {#if !occ.allDay}<span class="time">{formatTime(s.placed.start, timeFormat)}</span>{/if}
                  <span class="title">{occ.summary || $t('tasks.untitled')}</span>
                </button>
              </div>
            {:else}
            <button
              type="button"
              class="item"
              class:bar={isBar(s)}
              class:before={s.before}
              class:after={s.after}
              class:selected={selectedKey === occurrenceKey(occ)}
              class:cancelled={occ.status === 'CANCELLED'}
              style={`grid-column:${s.col + 1} / span ${s.span};grid-row:${s.lane + 1};--ev:${colorOf(occ)}`}
              on:click={(e) => dispatch('open', { occ, anchor: e.currentTarget })}
              on:contextmenu={(e) => !occ.task && openScriptMenu(e, 'calendar.event', { id: String(occ.eventId) })}
            >
              {#if !isBar(s)}
                <span class="dot" aria-hidden="true"></span>
                <span class="time">{formatTime(s.placed.start, timeFormat)}</span>
              {/if}
              <span class="title">{occ.summary || $t('calendar.untitled')}</span>
            </button>
            {/if}
          {/each}
          {#each row.hidden as n, i (i)}
            {#if n > 0}
              <button
                type="button"
                class="more"
                style={`grid-column:${i + 1};grid-row:${row.shown + 1}`}
                on:click={(e) => openMore(row.week[i], e)}
              >
                {$t('calendar.more').replace('{n}', String(n))}
              </button>
            {/if}
          {/each}
        </div>
      </div>
    {/each}
  </div>
</div>

{#if moreDay && moreAnchor}
  <AnchoredPopover anchor={moreAnchor} align="start" label={formatLongDay(moreDay)} on:close={() => (moreDay = null)}>
    <div class="day-list">
      <h4>{formatLongDay(moreDay)}</h4>
      {#each eventsOnDay(placed, moreDay) as p (occurrenceKey(p.occ))}
        <button type="button" class="item" class:bar={p.occ.allDay} style={`--ev:${colorOf(p.occ)}`} on:click={() => openFromList(p.occ)}>
          {#if !p.occ.allDay}
            <span class="dot" aria-hidden="true"></span>
            <span class="time">{formatTime(p.start, timeFormat)}</span>
          {/if}
          <span class="title">{p.occ.summary || $t('calendar.untitled')}</span>
        </button>
      {/each}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .month {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    user-select: none;
  }

  .heads {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .heads span {
    padding: var(--space-3) var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .weeks {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-rows: repeat(var(--rows), minmax(0, 1fr));
  }

  .week {
    position: relative;
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    grid-template-rows: minmax(0, 1fr);
    min-height: 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .cell {
    grid-row: 1;
    display: flex;
    align-items: flex-start;
    padding: var(--space-2);
    border-inline-start: var(--hairline) solid var(--border-subtle);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .cell:first-child {
    border-inline-start: none;
  }

  .cell.weekend {
    background: color-mix(in srgb, var(--surface-sunken) 50%, transparent);
  }

  .cell.other .num {
    color: var(--text-tertiary);
  }

  .cell.focus {
    background: var(--selection-bg);
  }

  .num {
    min-width: calc(var(--fz-list) * 1.9);
    height: calc(var(--fz-list) * 1.9);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
    font-variant-numeric: tabular-nums;
    cursor: var(--cursor-action);
  }

  .num:hover {
    background: var(--surface-hover);
  }

  .cell.today .num {
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: var(--fw-semibold);
  }

  .items {
    position: absolute;
    inset: calc(var(--fz-list) * 1.9 + var(--space-3)) 0 var(--space-1);
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    grid-template-rows: repeat(var(--lanes), var(--cal-lane-height));
    row-gap: var(--space-1);
    pointer-events: none;
  }

  .item,
  .more {
    pointer-events: auto;
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    height: var(--cal-lane-height);
    margin-inline: var(--space-1);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .item:hover,
  .item.selected {
    background: var(--surface-hover);
  }

  .item.bar {
    border-inline-start: var(--cal-edge) solid var(--ev);
    background: color-mix(in srgb, var(--ev) 22%, var(--surface-raised));
  }

  .item.bar:hover,
  .item.bar.selected {
    background: color-mix(in srgb, var(--ev) 34%, var(--surface-raised));
  }

  .item.before {
    margin-inline-start: 0;
    border-start-start-radius: 0;
    border-end-start-radius: 0;
  }

  .item.after {
    margin-inline-end: 0;
    border-start-end-radius: 0;
    border-end-end-radius: 0;
  }

  .item.cancelled .title {
    text-decoration: line-through;
    opacity: 0.7;
  }

  .item:focus-visible,
  .more:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  /* a task: no fill, its checkbox in the list color and a quieter title, so
     it reads as something to do rather than somewhere to be. */
  .item.task {
    gap: var(--space-2);
    color: var(--text-secondary);
    cursor: default;
  }

  .item.task.done .title {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .item.task.dragging {
    opacity: 0.4;
  }

  .task-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0;
    border: none;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
    touch-action: none;
  }

  .task-title:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .month.task-dragging .item {
    pointer-events: none;
  }

  .cell:global(.task-over) {
    background: var(--selection-bg-strong);
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    background: var(--ev);
  }

  .time {
    flex-shrink: 0;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .bar .title {
    font-weight: var(--fw-medium);
  }

  .more {
    justify-self: start;
    height: var(--cal-lane-height);
    margin-inline: var(--space-1);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }

  .more:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .day-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: calc(var(--cal-popover-width) - var(--space-6) * 2);
    padding: var(--space-3);
    overflow-y: auto;
  }

  .day-list h4 {
    margin: 0 0 var(--space-2);
    padding-inline: var(--space-2);
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
  }

  .day-list .item {
    margin: 0;
    flex-shrink: 0;
  }
</style>
