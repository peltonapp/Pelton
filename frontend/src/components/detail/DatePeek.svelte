<script lang="ts">
  // the preview a date in a mail opens: the day it names, with the time drawn
  // into a few hours of the calendar around it, the all-day events, the tasks
  // due that day, and whether the time is free. a date without a time shows
  // the day's agenda instead. its foot offers to make an event or a task from
  // the mail at that time, or to open the day in the calendar.
  //
  // it hangs below (or above) the date at a fixed place outside the mail's
  // frame, at the end of <body>, so nothing in the reader clips it.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { fade } from 'svelte/transition'
  import calendarPlusIcon from '@phosphor-icons/core/assets/duotone/calendar-plus-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-square-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/arrow-square-out-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import DaySlice from '../calendar/DaySlice.svelte'
  import TaskCheck from '../tasks/TaskCheck.svelte'
  import { portal } from '../../lib/portal'
  import { popPopup, pushPopup } from '../../lib/popups'
  import { listOccurrences } from '../../lib/api'
  import { addDays, placeOccurrence, startOfDay, toRFC3339, type Placed } from '../../lib/caldate'
  import { calendarColor, formatLongDay, formatTime, formatTimeRange } from '../../lib/calformat'
  import { peekDay, type Slot } from '../../lib/daypeek'
  import { tasksOnDay, dueHasTime, dueDate } from '../../lib/tasklogic'
  import { draftEventFromMail, openDayInCalendar, taskFromMailAt } from '../../lib/mailcalendar'
  import { onCalendarChanged } from '../../lib/events'
  import { calendarSettings, calendarSettingsLoaded, loadCalendarSettings } from '../../stores/calendar'
  import { tasks, toggleDone } from '../../stores/tasks'
  import { prefs } from '../../stores/prefs'
  import { dur, motion } from '../../lib/motion'
  import { locale, t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence } from '../../lib/types'

  /** The time the mail names. */
  export let slot: Slot
  /** The date's box, in the layout space a fixed element is placed in. */
  export let anchor: { left: number; top: number; right: number; bottom: number }
  /** Opened by a click or Enter: it stays until dismissed and takes the focus. */
  export let pinned: boolean = false
  /** The mail the date is in. */
  export let messageId: number

  // close carries whether the focus goes back to the date.
  const dispatch = createEventDispatcher<{ close: boolean; hold: void; release: void }>()

  const gap = 6
  const margin = 8

  let card: HTMLDivElement
  let left = 0
  let top = 0
  let placed = false
  let loaded = false
  let occurrences: Placed<Occurrence>[] = []
  let seq = 0

  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: day = startOfDay(slot.start)
  $: void load(day)
  $: peek = peekDay(occurrences, slot, $calendarSettings.workDayStart, $calendarSettings.workDayEnd)
  $: dueTasks = tasksOnDay($tasks, day)
  $: free = peek.clashes.length === 0
  $: when = slot.allDay ? formatLongDay(slot.start, $locale) : `${formatLongDay(slot.start, $locale)}, ${formatTimeRange(slot.start, slot.end, timeFormat)}`

  async function load(d: Date): Promise<void> {
    const mine = ++seq
    try {
      const list = await listOccurrences(toRFC3339(d), toRFC3339(addDays(d, 1)))
      if (mine === seq) {
        occurrences = list.map(placeOccurrence)
      }
    } catch {
      if (mine === seq) {
        occurrences = []
      }
    } finally {
      if (mine === seq) {
        loaded = true
        void place()
      }
    }
  }

  async function place(): Promise<void> {
    await tick()
    if (!card) {
      return
    }
    const scale = Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
    const vw = window.innerWidth / scale
    const vh = window.innerHeight / scale
    const w = card.offsetWidth
    const h = card.offsetHeight
    const below = vh - anchor.bottom - gap - margin
    const above = anchor.top - gap - margin
    const up = h > below && above > below
    top = up ? anchor.top - gap - h : anchor.bottom + gap
    top = Math.min(Math.max(top, margin), Math.max(margin, vh - h - margin))
    left = Math.min(Math.max(anchor.left, margin), Math.max(margin, vw - w - margin))
    placed = true
  }

  $: if (anchor && card) {
    void place()
  }

  function colorOf(occ: Occurrence): string {
    return calendarColor({ id: occ.calendarId, color: occ.calendarColor })
  }

  function timeOf(p: Placed<Occurrence>): string {
    return formatTimeRange(p.start, p.end, timeFormat)
  }

  function run(action: () => unknown): void {
    dispatch('close', false)
    void action()
  }

  let firstAction: HTMLButtonElement

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      dispatch('close', true)
    }
  }

  function onPointerDown(event: PointerEvent): void {
    if (pinned && card && !card.contains(event.target as Node)) {
      dispatch('close', false)
    }
  }

  // a press inside the mail's frame never reaches this document; the window
  // losing focus to it is what says it happened.
  function onBlur(): void {
    if (pinned) {
      setTimeout(() => {
        if (document.activeElement?.tagName === 'IFRAME') {
          dispatch('close', false)
        }
      }, 0)
    }
  }

  // the date moves away from under it when the reader scrolls.
  function onScroll(event: Event): void {
    if (card && !card.contains(event.target as Node)) {
      dispatch('close', false)
    }
  }

  let offChanged: (() => void) | null = null

  onMount(() => {
    if (!$calendarSettingsLoaded) {
      void loadCalendarSettings()
    }
    offChanged = onCalendarChanged(() => void load(day))
    window.addEventListener('pointerdown', onPointerDown, true)
    window.addEventListener('blur', onBlur)
    window.addEventListener('scroll', onScroll, true)
  })

  // pinned, it holds Escape for itself and takes the focus.
  let held = false
  $: if (pinned && !held) {
    held = true
    pushPopup()
    void tick().then(() => firstAction?.focus())
  }

  onDestroy(() => {
    offChanged?.()
    window.removeEventListener('pointerdown', onPointerDown, true)
    window.removeEventListener('blur', onBlur)
    window.removeEventListener('scroll', onScroll, true)
    if (held) {
      popPopup()
    }
  })
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div
  class="peek"
  class:placed
  bind:this={card}
  use:portal
  role="dialog"
  tabindex="-1"
  aria-label={$t('dateHint.label').replace('{when}', when)}
  style={`left:${left}px;top:${top}px`}
  on:pointerenter={() => dispatch('hold')}
  on:pointerleave={() => dispatch('release')}
  on:keydown={onKeydown}
  transition:fade={{ duration: dur(motion.fast) }}
>
  <header>
    <div class="when">
      <span class="day">{formatLongDay(slot.start, $locale)}</span>
      {#if !slot.allDay}
        <span class="time">{formatTimeRange(slot.start, slot.end, timeFormat)}</span>
      {/if}
    </div>
    {#if loaded && !slot.allDay}
      <span class="state" class:clash={!free}>
        {free ? $t('dateHint.free') : peek.clashes.length === 1 ? $t('dateHint.clashOne').replace('{title}', peek.clashes[0].occ.summary || $t('calendar.untitled')) : $t('dateHint.clashMany').replace('{n}', String(peek.clashes.length))}
      </span>
    {/if}
  </header>

  {#if peek.allDay.length > 0}
    <ul class="lane" aria-label={$t('calendar.allDay')}>
      {#each peek.allDay as p (p.occ.eventId + '|' + p.occ.recurrenceId)}
        <li style={`--ev:${colorOf(p.occ)}`}>{p.occ.summary || $t('calendar.untitled')}</li>
      {/each}
    </ul>
  {/if}

  {#if slot.allDay}
    {#if peek.timed.length > 0}
      <ul class="agenda">
        {#each peek.timed as p (p.occ.eventId + '|' + p.occ.recurrenceId)}
          <li style={`--ev:${colorOf(p.occ)}`}>
            <span class="at">{formatTime(p.start, timeFormat)}</span>
            <span class="what">{p.occ.summary || $t('calendar.untitled')}</span>
          </li>
        {/each}
      </ul>
    {:else if loaded && peek.allDay.length === 0}
      <p class="empty">{$t('dateHint.emptyDay')}</p>
    {/if}
  {:else}
    <DaySlice {day} timed={peek.timed} from={peek.from} to={peek.to} slot={{ start: slot.start, end: slot.end }} clash={!free} {timeFormat} />
    {#if peek.before || peek.after}
      <dl class="around">
        {#if peek.before}
          <dt>{$t('dateHint.before')}</dt>
          <dd><span class="what">{peek.before.occ.summary || $t('calendar.untitled')}</span> <span class="at">{timeOf(peek.before)}</span></dd>
        {/if}
        {#if peek.after}
          <dt>{$t('dateHint.after')}</dt>
          <dd><span class="what">{peek.after.occ.summary || $t('calendar.untitled')}</span> <span class="at">{timeOf(peek.after)}</span></dd>
        {/if}
      </dl>
    {/if}
  {/if}

  {#if dueTasks.length > 0}
    <ul class="tasks" aria-label={$t('dateHint.tasks')}>
      {#each dueTasks as task (task.id)}
        <li>
          <TaskCheck
            checked={task.done}
            color={task.listColor}
            label={$t(task.done ? 'tasks.inCalendarOpen' : 'tasks.inCalendarDone').replace('{title}', task.title || $t('tasks.untitled'))}
            disabled={task.readOnly}
            on:toggle={() => void toggleDone(task)}
          />
          <span class="what" class:done={task.done}>{task.title || $t('tasks.untitled')}</span>
          {#if dueHasTime(task.due)}
            <span class="at">{formatTime(dueDate(task) ?? slot.start, timeFormat)}</span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  <footer>
    <button type="button" class="cal-btn" bind:this={firstAction} on:click={() => run(() => draftEventFromMail(messageId, slot))}>
      <Ph svg={calendarPlusIcon} size={14} />
      <span>{$t('dateHint.createEvent')}</span>
    </button>
    <button type="button" class="cal-btn" on:click={() => run(() => taskFromMailAt([messageId], slot))}>
      <Ph svg={checkIcon} size={14} />
      <span>{$t('dateHint.createTask')}</span>
    </button>
    <button type="button" class="cal-btn" on:click={() => run(() => openDayInCalendar(slot.start))}>
      <Ph svg={openIcon} size={14} />
      <span>{$t('dateHint.openDay')}</span>
    </button>
  </footer>
</div>

<style>
  .peek {
    position: fixed;
    z-index: 400;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    width: var(--date-peek-width);
    max-width: calc(100% - 2 * var(--space-3));
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    visibility: hidden;
  }

  .peek.placed {
    visibility: visible;
  }

  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .when {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .day {
    font-weight: var(--fw-semibold);
  }

  .time,
  .at {
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .state {
    flex-shrink: 0;
    max-width: 55%;
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--success-bg);
    color: var(--success);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    line-height: var(--chip-height);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .state.clash {
    background: var(--danger-bg);
    color: var(--danger);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .lane li {
    padding: 0 var(--space-2);
    border-inline-start: var(--cal-edge) solid var(--ev);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--ev) 16%, var(--surface-raised));
    font-size: var(--fz-meta);
    line-height: var(--chip-height);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .agenda li,
  .tasks li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .agenda li {
    padding-inline-start: var(--space-2);
    border-inline-start: var(--cal-edge) solid var(--ev);
  }

  .agenda .at {
    flex-shrink: 0;
    min-width: calc(var(--cal-gutter) * 0.8);
  }

  .what {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .what.done {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .tasks .at {
    margin-inline-start: auto;
    flex-shrink: 0;
  }

  .tasks {
    padding-top: var(--space-2);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .around {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-1) var(--space-2);
    margin: 0;
    font-size: var(--fz-meta);
  }

  .around dt {
    color: var(--text-tertiary);
  }

  .around dd {
    margin: 0;
    display: flex;
    gap: var(--space-1);
    min-width: 0;
  }

  .empty {
    margin: 0;
    color: var(--text-tertiary);
  }

  footer {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
    padding-top: var(--space-2);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  footer .cal-btn {
    flex: 1 1 auto;
    padding: 0 var(--space-2);
  }
</style>
