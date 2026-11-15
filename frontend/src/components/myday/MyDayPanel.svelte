<script lang="ts">
  // My Day: today's agenda and tasks in a column beside the mail. The agenda
  // is the calendar's own time grid for one day, with its now line and its
  // all-day lane; below it the overdue tasks and those due today, each with
  // its checkbox. A switch looks at tomorrow instead.
  //
  // Mail dragged from the list onto a time becomes an event there linked to
  // the mail, or with Shift held opens the editor for one; dropped on the
  // tasks it becomes a task due that day. The two fields at the foot add an
  // event as a line of text, the way the calendar's quick add reads it, and a
  // task the way the task list's entry does.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import { fly } from 'svelte/transition'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import lightningIcon from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-circle-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import Segmented from '../settings2/ui/Segmented.svelte'
  import TimeGrid from '../calendar/TimeGrid.svelte'
  import TaskCheck from '../tasks/TaskCheck.svelte'
  import { listOccurrences, getEvent } from '../../lib/api'
  import { addDays, dayKey, minutesOfDay, placeOccurrence, startOfDay, toRFC3339, type Placed } from '../../lib/caldate'
  import { calendarColor, formatLongDay, formatTime, formatWhen } from '../../lib/calformat'
  import { parsePhrase, type ChronoLike } from '../../lib/eventphrase'
  import { parseTaskPhrase } from '../../lib/taskphrase'
  import { loadChrono } from '../../lib/chronoload'
  import { dueDate, dueHasTime } from '../../lib/tasklogic'
  import { myDayTasks } from '../../lib/myday'
  import { draftEventFromMail, eventFromMailAt, openDayInCalendar, taskFromMailAt } from '../../lib/mailcalendar'
  import { openTask } from '../../lib/taskmail'
  import { onCalendarChanged } from '../../lib/events'
  import { maildrop, type MailDropOptions } from '../../stores/maildrag'
  import {
    calendars,
    calendarSettings,
    calendarSettingsLoaded,
    defaultCalendarId,
    loadCalendars,
    loadCalendarSettings,
    removeEvent,
    saveEvent,
    type MailDropAt,
  } from '../../stores/calendar'
  import { defaultListId, emptyDraft, localZone, saveTask, taskLists, taskSettings, tasks, toggleDone, loadTasks } from '../../stores/tasks'
  import { prefs } from '../../stores/prefs'
  import { shortcutTitle } from '../../stores/shortcuts'
  import { errorMessage, pushActions, toastError } from '../../stores/toast'
  import { draftFromEvent, newDraft, withTimes } from '../calendar/occurrence'
  import { tip } from '../../lib/tip'
  import { dur, motion } from '../../lib/motion'
  import { locale, t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence } from '../../lib/types'

  /** Fixed for tests; the clock otherwise. */
  export let clock: () => Date = () => new Date()

  const dispatch = createEventDispatcher<{ close: void }>()

  let now = clock()
  let which: 'today' | 'tomorrow' = 'today'
  let occurrences: Placed<Occurrence>[] = []
  let seq = 0

  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: day = which === 'today' ? startOfDay(now) : addDays(startOfDay(now), 1)
  $: dayId = dayKey(day)
  $: void load(day)
  $: colors = new Map($calendars.map((c) => [c.id, calendarColor(c)]))
  $: dayTasks = myDayTasks($tasks, day, now)
  $: dayOptions = [
    { key: 'today', label: $t('myday.today') },
    { key: 'tomorrow', label: $t('myday.tomorrow') },
  ]

  async function load(d: Date): Promise<void> {
    const mine = ++seq
    try {
      const list = await listOccurrences(toRFC3339(d), toRFC3339(addDays(d, 1)))
      if (mine === seq) {
        occurrences = list.map(placeOccurrence)
      }
    } catch (err) {
      if (mine === seq) {
        occurrences = []
        toastError(errorMessage(err))
      }
    }
  }

  // --- the agenda ---------------------------------------------------------

  // a time dragged out on the grid waits for the event field to name it.
  let slot: { start: Date; end: Date; allDay: boolean } | null = null
  let eventInput: HTMLInputElement

  function onCreate(span: { start: Date; end: Date; allDay: boolean }): void {
    slot = span
    eventInput?.focus()
  }

  async function onMove(occ: Occurrence, start: Date, end: Date): Promise<void> {
    if (occ.readOnly || occ.task) {
      return
    }
    try {
      const ev = await getEvent(occ.eventId, occ.recurrenceId)
      if (await saveEvent({ ...withTimes(draftFromEvent(ev), start, end), scope: occ.recurring ? 'this' : '' })) {
        await load(day)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onMailDrop(drop: MailDropAt): Promise<void> {
    if (!drop.at || drop.messageIds.length === 0) {
      return
    }
    const allDay = drop.mode === 'allDay'
    const start = allDay ? startOfDay(drop.at) : drop.at
    const at = { start, end: allDay ? addDays(start, 1) : new Date(start.getTime() + 3600000), allDay }
    if (drop.shift) {
      await draftEventFromMail(drop.messageIds[0], at)
      return
    }
    if ((await eventFromMailAt(drop.messageIds, at)).length > 0) {
      await load(day)
    }
  }

  // --- the tasks ----------------------------------------------------------

  $: taskDrop = {
    onDrop: (items) => void taskFromMailAt(items.map((m) => m.id), { start: day, end: addDays(day, 1), allDay: true }),
    hint: (items) => (items.length > 1 ? $t('myday.drop.tasks').replace('{n}', String(items.length)) : $t('myday.drop.task')),
  } satisfies MailDropOptions

  function dueLabel(due: string): string {
    const d = dueDate({ due })
    if (!d) {
      return ''
    }
    if (dayKey(d) === dayId) {
      return dueHasTime(due) ? formatTime(d, timeFormat) : ''
    }
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
  }

  // --- quick add ----------------------------------------------------------

  let chrono: ChronoLike[] = []
  let eventText = ''
  let taskText = ''

  function readyChrono(): void {
    if (chrono.length === 0) {
      void loadChrono($locale)
        .then((list) => (chrono = list))
        .catch(() => (chrono = []))
    }
  }

  $: eventPhrase =
    eventText.trim() && chrono.length
      ? parsePhrase(eventText.trim(), { now, base: slot?.start ?? day, baseMinutes: slot && !slot.allDay ? minutesOfDay(slot.start) : null, lang: $locale, chrono })
      : null
  $: eventPreview = eventPhrase ? formatWhen(eventPhrase.start, slot && !eventPhrase.dated ? slot.end : eventPhrase.end, eventPhrase.allDay, timeFormat) : slot ? formatWhen(slot.start, slot.end, slot.allDay, timeFormat) : ''
  $: taskPhrase = taskText.trim() && chrono.length ? parseTaskPhrase(taskText.trim(), { now, lang: $locale, chrono, defaultDueMinutes: $taskSettings.defaultDueMinutes }) : null
  $: taskPreview = taskPhrase?.due ? $t('tasks.preview.due').replace('{when}', dueDate({ due: taskPhrase.due })?.toLocaleString(undefined, dueHasTime(taskPhrase.due) ? { dateStyle: 'medium', timeStyle: 'short' } : { dateStyle: 'medium' }) ?? '') : ''

  async function addEvent(): Promise<void> {
    const p = eventPhrase
    if (!p || !p.title) {
      return
    }
    if ($calendars.length === 0) {
      await loadCalendars()
    }
    const calendarId = defaultCalendarId($calendars, $calendarSettings)
    if (calendarId === 0) {
      toastError($t('calendar.noWritable'))
      return
    }
    const end = slot && !p.dated ? slot.end : p.end
    const reminder = p.reminderMinutes ?? $calendarSettings.defaultReminderMinutes
    const result = await saveEvent({ ...newDraft(calendarId, p.start, end, p.allDay, reminder), summary: p.title, location: p.location, rrule: p.rrule })
    if (!result) {
      return
    }
    eventText = ''
    slot = null
    await load(day)
    const id = result.event.id
    pushActions(
      'success',
      $t('quickAdd.created'),
      [
        { label: $t('quickAdd.undo'), run: () => void removeEvent(id).then(() => load(day)) },
        { label: $t('quickAdd.open'), run: () => openDayInCalendar(p.start) },
      ],
      8000,
    )
  }

  async function addTask(): Promise<void> {
    const p = taskPhrase
    if (!p || !p.title) {
      return
    }
    if ($taskLists.length === 0) {
      await loadTasks()
    }
    const listId = defaultListId($taskLists, $taskSettings)
    if (listId === 0) {
      toastError($t('tasks.noWritable'))
      return
    }
    const draft = { ...emptyDraft(listId), title: p.title, due: p.due || dayKey(day), rrule: p.rrule, timeZone: localZone() }
    if (p.reminderMinutes !== null) {
      draft.reminders = [{ minutesBefore: p.reminderMinutes, relatedEnd: true, absolute: '', action: '' }]
    }
    if (await saveTask(draft)) {
      taskText = ''
    }
  }

  function onEventKey(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      event.preventDefault()
      void addEvent()
    } else if (event.key === 'Escape' && (eventText || slot)) {
      event.preventDefault()
      event.stopPropagation()
      eventText = ''
      slot = null
    }
  }

  function onTaskKey(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      event.preventDefault()
      void addTask()
    } else if (event.key === 'Escape' && taskText) {
      event.preventDefault()
      event.stopPropagation()
      taskText = ''
    }
  }

  let off: (() => void) | null = null
  let ticker: ReturnType<typeof setInterval> | undefined

  onMount(() => {
    off = onCalendarChanged(() => void load(day))
    ticker = setInterval(() => {
      const next = clock()
      if (dayKey(next) !== dayKey(now)) {
        slot = null
      }
      now = next
    }, 30000)
    if ($calendars.length === 0) {
      void loadCalendars()
    }
    if (!$calendarSettingsLoaded) {
      void loadCalendarSettings()
    }
  })

  onDestroy(() => {
    off?.()
    clearInterval(ticker)
  })
</script>

<aside class="myday" aria-label={$t('myday.title')} transition:fly={{ x: 24, duration: dur(motion.base) }}>
  <header>
    <div class="title">
      <h2>{$t('myday.title')}</h2>
      <span class="date">{formatLongDay(day, undefined, now)}</span>
    </div>
    <Segmented value={which} options={dayOptions} label={$t('myday.which')} on:change={(e) => { which = e.detail === 'tomorrow' ? 'tomorrow' : 'today'; slot = null }} />
    <button type="button" class="cal-icon" aria-label={$t('myday.close')} use:tip={{ text: $shortcutTitle($t('myday.close'), 'toggle-my-day') }} on:click={() => dispatch('close')}>
      <Ph svg={xIcon} size={14} />
    </button>
  </header>

  <section class="agenda" aria-label={$t('myday.agenda')}>
    {#key dayId}
      <TimeGrid
        days={[day]}
        placed={occurrences}
        {colors}
        {now}
        {timeFormat}
        workStart={$calendarSettings.workDayStart}
        workEnd={$calendarSettings.workDayEnd}
        pending={slot}
        on:create={(e) => onCreate(e.detail)}
        on:change={(e) => void onMove(e.detail.occ, e.detail.start, e.detail.end)}
        on:open={(e) => openDayInCalendar(placeOccurrence(e.detail.occ).start)}
        on:day={(e) => openDayInCalendar(e.detail)}
        on:mailDrop={(e) => void onMailDrop(e.detail)}
      />
    {/key}
  </section>

  <section class="tasks" aria-label={$t('myday.tasks')} use:maildrop={taskDrop}>
    <h3>{$t('myday.tasks')}</h3>
    {#if dayTasks.length === 0}
      <p class="empty">{$t(which === 'today' ? 'myday.noTasksToday' : 'myday.noTasksTomorrow')}</p>
    {:else}
      <ul>
        {#each dayTasks as { task, overdue } (task.id)}
          <li class:done={task.done}>
            <TaskCheck
              checked={task.done}
              color={task.listColor}
              label={$t(task.done ? 'tasks.inCalendarOpen' : 'tasks.inCalendarDone').replace('{title}', task.title || $t('tasks.untitled'))}
              disabled={task.readOnly}
              on:toggle={() => void toggleDone(task)}
            />
            <button type="button" class="task-title" on:click={() => openTask(task.id)}>{task.title || $t('tasks.untitled')}</button>
            {#if task.due}
              <span class="due" class:overdue>{dueLabel(task.due)}</span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <footer>
    <label class="add">
      <Ph svg={lightningIcon} size={14} />
      <input
        class="cal-field"
        bind:this={eventInput}
        bind:value={eventText}
        placeholder={slot ? $t('myday.eventAt').replace('{when}', formatWhen(slot.start, slot.end, slot.allDay, timeFormat)) : $t('myday.addEvent')}
        aria-label={$t('myday.addEvent')}
        on:focus={readyChrono}
        on:keydown={onEventKey}
      />
    </label>
    {#if eventPreview && (eventText || slot)}
      <p class="preview" aria-live="polite">{eventPreview}</p>
    {/if}
    <label class="add">
      <Ph svg={plusIcon} size={14} />
      <input class="cal-field" bind:value={taskText} placeholder={$t('myday.addTask')} aria-label={$t('myday.addTask')} on:focus={readyChrono} on:keydown={onTaskKey} />
    </label>
    {#if taskPreview}
      <p class="preview" aria-live="polite">{taskPreview}</p>
    {/if}
  </footer>
</aside>

<style>
  .myday {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    margin-inline-start: var(--pane-gap);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--pane-radius);
    background: var(--surface-raised);
    overflow: hidden;
    --cal-gutter: calc(var(--space-6) * 1.5);
  }

  header {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-3) var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  h2 {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .date {
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .agenda {
    flex: 1 1 60%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .tasks {
    flex: 0 1 auto;
    max-height: 40%;
    overflow-y: auto;
    padding: var(--space-2) var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .tasks:global(.maildrop-over) {
    background: var(--selection-bg);
  }

  h3 {
    margin: 0 0 var(--space-1);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    min-height: var(--chip-height);
  }

  .task-title {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: var(--cursor-action);
  }

  .task-title:hover {
    text-decoration: underline;
  }

  li.done .task-title {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .due {
    flex-shrink: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
  }

  .due.overdue {
    color: var(--danger);
  }

  .empty {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  footer {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2) var(--space-3) var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .add {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-tertiary);
  }

  .add input {
    flex: 1;
    min-width: 0;
  }

  .preview {
    margin: 0;
    padding-inline-start: calc(var(--space-2) + var(--space-4));
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }

  :global(:root[data-panes='flat']) .myday {
    margin-inline-start: 0;
    border-width: var(--hairline) 0 var(--hairline) var(--hairline);
    border-radius: 0;
  }

  :global(:root[data-reduce-motion]) .tasks {
    transition: none;
  }
</style>
