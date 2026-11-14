<script lang="ts">
  // the calendar space of the new design: a sidebar with a small month and the
  // calendars, and the month, week, day or agenda view beside it. clicking an
  // event shows its details in a popover; drawing on empty time or double
  // clicking a day opens a quick create; the full editor does the rest. mail
  // dropped on the Calendar entry or on a day or slot here becomes a new event.
  // it is code-split like the contacts screen and only loads when opened.
  import { onDestroy, onMount, tick } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import caretLeft from '@phosphor-icons/core/assets/regular/caret-left.svg?raw'
  import caretRight from '@phosphor-icons/core/assets/regular/caret-right.svg?raw'
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import sidebarIcon from '@phosphor-icons/core/assets/duotone/sidebar-simple-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import './controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import Segmented from '../settings2/ui/Segmented.svelte'
  import CalendarSidebar from './CalendarSidebar.svelte'
  import MonthView from './MonthView.svelte'
  import TimeGrid from './TimeGrid.svelte'
  import AgendaView from './AgendaView.svelte'
  import EventPopover from './EventPopover.svelte'
  import QuickCreate from './QuickCreate.svelte'
  import EventEditor from './EventEditor.svelte'
  import ScopeDialog from './ScopeDialog.svelte'
  import ConflictDialog from './ConflictDialog.svelte'
  import {
    calendars,
    occurrences,
    calendarSettings,
    calendarView,
    calendarFocus,
    calendarSeed,
    calendarSync,
    calendarSyncError,
    eventConflict,
    loadCalendars,
    loadCalendarSettings,
    showRange,
    refreshCalendars,
    saveEvent,
    removeEvent,
    watchCalendar,
    type MailDropAt,
  } from '../../stores/calendar'
  import { createEventDraftFromMessage, getEvent, listAccounts } from '../../lib/api'
  import {
    addDays,
    atMinutes,
    dayKey,
    minutesOfDay,
    monthGrid,
    parseEventTime,
    placeOccurrence,
    resolveWeekStart,
    startOfDay,
    stepFocus,
    toRFC3339,
    viewRange,
    weekDays,
  } from '../../lib/caldate'
  import { calendarColor, formatViewTitle } from '../../lib/calformat'
  import { draftFromEvent, localZone, newDraft, occurrenceKey, withTimes } from './occurrence'
  import { popupIsOpen } from '../../lib/popups'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { CalendarView, EventDraft, EventScope, Occurrence } from '../../lib/types'

  const narrowWidth = 820

  let width = 0
  let sideOpen = false
  let now = new Date()
  let clock: ReturnType<typeof setInterval> | undefined
  let unwatch: (() => void) | undefined
  let identity: { name: string; email: string } | null = null
  let stage: HTMLDivElement

  $: narrow = width > 0 && width < narrowWidth
  $: if (!narrow) {
    sideOpen = false
  }
  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: weekStart = resolveWeekStart($calendarSettings.weekStart, navigator.language)
  $: view = $calendarView
  $: focus = $calendarFocus
  $: range = viewRange(view, focus, weekStart)
  $: rangeKey = `${toRFC3339(range.from)}|${toRFC3339(range.to)}`
  $: loadRange(rangeKey)
  $: placed = $occurrences.map(placeOccurrence)
  $: colors = new Map($calendars.map((c) => [c.id, calendarColor(c)]))
  $: writable = $calendars.filter((c) => !c.readOnly)
  $: defaultCalendar = (writable.find((c) => c.id === $calendarSettings.defaultCalendarId) ?? writable.find((c) => c.default) ?? writable[0])?.id ?? 0
  $: busyDays = daysWithEvents(placed)
  $: title = formatViewTitle(view, range.from, range.to, focus)
  $: rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl'

  let loadedKey = ''
  function loadRange(key: string): void {
    if (key !== loadedKey) {
      loadedKey = key
      void showRange(toRFC3339(range.from), toRFC3339(range.to))
    }
  }

  function daysWithEvents(list: ReturnType<typeof placeOccurrence>[]): Set<string> {
    const out = new Set<string>()
    for (const p of list) {
      for (let d = startOfDay(p.start); d < p.end || dayKey(d) === dayKey(p.start); d = addDays(d, 1)) {
        out.add(dayKey(d))
        if (out.size > 400) {
          break
        }
      }
    }
    return out
  }

  onMount(async () => {
    unwatch = watchCalendar()
    clock = setInterval(() => (now = new Date()), 30000)
    await Promise.all([loadCalendarSettings(), loadCalendars()])
    try {
      const own = (await listAccounts()).find((a) => !a.local)
      if (own) {
        identity = { name: own.displayName, email: own.email }
      }
    } catch {
      identity = null
    }
  })

  onDestroy(() => {
    unwatch?.()
    clearInterval(clock)
  })

  // --- moving around ------------------------------------------------------------

  // which way the page slides: forward pages come in from the end side.
  let direction = 0

  function go(dir: number): void {
    closeFloating()
    direction = dir
    calendarFocus.set(stepFocus(view, focus, dir))
  }

  function goToday(): void {
    closeFloating()
    const today = new Date()
    direction = today > focus ? 1 : today < focus ? -1 : 0
    calendarFocus.set(today)
  }

  function setView(next: CalendarView): void {
    if (next === view) {
      return
    }
    closeFloating()
    direction = 0
    calendarView.set(next)
  }

  function pickDay(day: Date): void {
    direction = day > focus ? 1 : -1
    calendarFocus.set(day)
    if (narrow) {
      sideOpen = false
    }
  }

  function openDay(day: Date): void {
    closeFloating()
    direction = 0
    calendarFocus.set(day)
    calendarView.set('day')
  }

  $: viewOptions = [
    { key: 'day', label: $t('calendar.view.day') },
    { key: 'week', label: $t('calendar.view.week') },
    { key: 'month', label: $t('calendar.view.month') },
    { key: 'agenda', label: $t('calendar.view.agenda') },
  ]

  // --- popovers -----------------------------------------------------------------

  let selected: { occ: Occurrence; anchor: HTMLElement } | null = null
  let quick: { start: Date; end: Date; allDay: boolean; anchor: HTMLElement } | null = null
  let quickPending: { start: Date; end: Date; allDay: boolean } | null = null

  $: selectedKey = selected ? occurrenceKey(selected.occ) : ''
  $: floating = selected !== null || quick !== null

  function closeFloating(): void {
    selected = null
    quick = null
    quickPending = null
  }

  function openOccurrence(occ: Occurrence, anchor: HTMLElement): void {
    quick = null
    quickPending = null
    selected = { occ, anchor }
  }

  async function startQuick(start: Date, end: Date, allDay: boolean, anchor: HTMLElement | null = null): Promise<void> {
    selected = null
    if (writable.length === 0) {
      toastError($t('calendar.noWritable'))
      return
    }
    quickPending = { start, end, allDay }
    await tick()
    const target = anchor ?? stage?.querySelector<HTMLElement>('[data-pending]') ?? stage
    quick = { start, end, allDay, anchor: target }
  }

  function startMonthCreate(day: Date, anchor: HTMLElement): void {
    const at = atMinutes(day, defaultStartMinutes(day))
    void startQuick(at, new Date(at.getTime() + 3600000), false, anchor)
  }

  function defaultStartMinutes(day: Date): number {
    const current = new Date()
    if (dayKey(day) === dayKey(current)) {
      return Math.min(23 * 60, (current.getHours() + 1) * 60)
    }
    return $calendarSettings.workDayStart
  }

  async function saveQuick(summary: string, calendarId: number): Promise<void> {
    if (!quick) {
      return
    }
    const draft = { ...newDraft(calendarId, quick.start, quick.end, quick.allDay, $calendarSettings.defaultReminderMinutes), summary }
    const result = await saveEvent(draft)
    if (result) {
      closeFloating()
    }
  }

  function moreFromQuick(summary: string, calendarId: number): void {
    if (!quick) {
      return
    }
    const draft = { ...newDraft(calendarId, quick.start, quick.end, quick.allDay, $calendarSettings.defaultReminderMinutes), summary }
    closeFloating()
    editing = draft
  }

  // --- the editor -----------------------------------------------------------------

  let editing: EventDraft | null = null
  let saving = false
  let scopeFor: { kind: 'edit' | 'move' | 'delete'; run: (scope: EventScope) => Promise<void> } | null = null

  function newEvent(): void {
    closeFloating()
    if (writable.length === 0) {
      toastError($t('calendar.noWritable'))
      return
    }
    const start = atMinutes(focus, defaultStartMinutes(focus))
    editing = newDraft(defaultCalendar, start, new Date(start.getTime() + 3600000), false, $calendarSettings.defaultReminderMinutes)
  }

  function editEvent(ev: Parameters<typeof draftFromEvent>[0]): void {
    selected = null
    editing = draftFromEvent(ev)
  }

  async function commit(draft: EventDraft): Promise<boolean> {
    saving = true
    const result = await saveEvent(draft)
    saving = false
    return result !== null || $eventConflict !== null
  }

  function onEditorSave(draft: EventDraft): void {
    if (draft.id !== 0 && draft.recurrenceId) {
      scopeFor = {
        kind: 'edit',
        run: async (scope) => {
          if (await commit({ ...draft, scope })) {
            editing = null
          }
        },
      }
      return
    }
    void commit(draft).then((done) => {
      if (done) {
        editing = null
      }
    })
  }

  function onEditorRemove(): void {
    const draft = editing
    if (!draft) {
      return
    }
    if (draft.recurrenceId) {
      scopeFor = {
        kind: 'delete',
        run: async (scope) => {
          if (await removeEvent(draft.id, scope, draft.recurrenceId)) {
            editing = null
          }
        },
      }
      return
    }
    void removeEvent(draft.id).then((done) => {
      if (done) {
        editing = null
      }
    })
  }

  async function removeFromPopover(occ: Occurrence, scope: EventScope | ''): Promise<void> {
    selected = null
    await removeEvent(occ.eventId, scope, occ.recurrenceId)
  }

  // a drag in the time grid: the occurrence moves at once, then the change is
  // saved, asking first which occurrences it is for when the event repeats.
  function onChange(occ: Occurrence, start: Date, end: Date): void {
    const apply = async (scope: EventScope | ''): Promise<void> => {
      const key = occurrenceKey(occ)
      const before = $occurrences
      occurrences.update((list) =>
        list.map((o) =>
          occurrenceKey(o) === key
            ? { ...o, start: o.allDay ? dayKey(start) : toRFC3339(start), end: o.allDay ? dayKey(end) : toRFC3339(end) }
            : o,
        ),
      )
      try {
        const ev = await getEvent(occ.eventId, occ.recurrenceId)
        const result = await saveEvent({ ...withTimes(draftFromEvent(ev), start, end), scope })
        if (!result) {
          occurrences.set(before)
        }
      } catch (err) {
        occurrences.set(before)
        toastError(errorMessage(err))
      }
    }
    if (occ.recurring) {
      scopeFor = { kind: 'move', run: apply }
    } else {
      void apply('')
    }
  }

  async function chooseScope(scope: EventScope): Promise<void> {
    const pending = scopeFor
    scopeFor = null
    await pending?.run(scope)
  }

  // --- mail dropped on the calendar -------------------------------------------------

  $: if ($calendarSeed) {
    void takeSeed($calendarSeed)
  }

  function onMailDrop(drop: MailDropAt): void {
    calendarSeed.set(drop)
  }

  async function takeSeed(seed: NonNullable<typeof $calendarSeed>): Promise<void> {
    calendarSeed.set(null)
    closeFloating()
    try {
      const draft = await createEventDraftFromMessage(seed.messageId)
      const suggested = parseEventTime(draft.start, false)
      const length = Math.max(15 * 60000, parseEventTime(draft.end, false).getTime() - suggested.getTime())
      let start = suggested
      let allDay = false
      if (seed.at && seed.mode === 'time') {
        start = seed.at
      } else if (seed.at && seed.mode === 'day') {
        start = atMinutes(seed.at, minutesOfDay(suggested))
      } else if (seed.at && seed.mode === 'allDay') {
        start = startOfDay(seed.at)
        allDay = true
      }
      const end = allDay ? addDays(start, 1) : new Date(start.getTime() + length)
      const calendarId = writable.some((c) => c.id === draft.calendarId) ? draft.calendarId : defaultCalendar
      editing = withTimes({ ...draft, calendarId, allDay, timeZone: allDay ? '' : localZone(), scope: '', recurrenceId: '', force: false }, start, end)
      if (dayKey(start) !== dayKey(focus)) {
        direction = start > focus ? 1 : -1
        calendarFocus.set(start)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // --- keys -------------------------------------------------------------------------

  function editableTarget(target: EventTarget | null): boolean {
    const el = target as HTMLElement | null
    return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.metaKey || event.ctrlKey || event.altKey || editableTarget(event.target) || popupIsOpen() || editing || scopeFor) {
      return
    }
    const back = rtl ? 'ArrowRight' : 'ArrowLeft'
    const forward = rtl ? 'ArrowLeft' : 'ArrowRight'
    const actions: Record<string, () => void> = {
      t: goToday,
      d: () => setView('day'),
      w: () => setView('week'),
      m: () => setView('month'),
      a: () => setView('agenda'),
      n: newEvent,
      [back]: () => go(-1),
      [forward]: () => go(1),
    }
    const run = actions[event.key]
    if (run && !(event.target as HTMLElement).closest('.mini')) {
      event.preventDefault()
      event.stopPropagation()
      run()
    }
  }

  $: syncing = $calendarSync
  $: syncValue = syncing && syncing.total > 0 ? syncing.done / syncing.total : null
  $: flyX = (direction === 0 ? 0 : direction * 18) * (rtl ? -1 : 1)
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<section class="screen" aria-label={$t('space.calendar')} bind:clientWidth={width} on:keydown={onKeydown}>
  {#if !narrow || sideOpen}
    <div class="side" class:drawer={narrow} transition:fly={{ x: narrow ? (rtl ? 24 : -24) : 0, duration: dur(narrow ? motion.base : 0) }}>
      <CalendarSidebar
        {focus}
        {weekStart}
        rangeFrom={range.from}
        rangeTo={range.to}
        busy={busyDays}
        on:pick={(e) => pickDay(e.detail)}
        on:create={newEvent}
      />
    </div>
  {/if}
  {#if narrow && sideOpen}
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="scrim" transition:fade={{ duration: dur(motion.fast) }} on:click={() => (sideOpen = false)}></div>
  {/if}

  <div class="main">
    <header class="bar">
      {#if narrow}
        <button type="button" class="cal-icon" aria-label={$t('calendar.sidebar')} aria-expanded={sideOpen} on:click={() => (sideOpen = !sideOpen)}>
          <Ph svg={sidebarIcon} size={16} />
        </button>
      {/if}
      <button type="button" class="cal-btn" on:click={goToday}>{$t('calendar.today')}</button>
      <div class="nav">
        <button type="button" class="cal-icon" aria-label={$t('calendar.previous')} on:click={() => go(-1)}>
          <span class="cal-flip"><Ph svg={caretLeft} size={14} /></span>
        </button>
        <button type="button" class="cal-icon" aria-label={$t('calendar.next')} on:click={() => go(1)}>
          <span class="cal-flip"><Ph svg={caretRight} size={14} /></span>
        </button>
      </div>
      {#key title}
        <h2 class="title" in:fade={{ duration: dur(motion.fast) }}>{title}</h2>
      {/key}
      <span class="spacer"></span>
      {#if $calendarSyncError && !syncing}
        <span class="sync-error" title={$calendarSyncError}><Ph svg={warningIcon} size={15} /></span>
      {/if}
      <button
        type="button"
        class="cal-icon"
        aria-label={$t('calendar.sync')}
        title={$t('calendar.sync')}
        disabled={syncing !== null}
        on:click={() => void refreshCalendars()}
      >
        {#if syncing}
          <PieProgress value={syncValue} label={$t('calendar.syncing')} />
        {:else}
          <Ph svg={syncIcon} size={16} />
        {/if}
      </button>
      <Segmented value={view} options={viewOptions} label={$t('calendar.viewLabel')} on:change={(e) => setView(e.detail as CalendarView)} />
    </header>

    <div class="stage" bind:this={stage}>
      {#key `${view}|${rangeKey}`}
        <div class="page" in:fly={{ x: flyX, y: direction === 0 ? 6 : 0, duration: dur(motion.slow), opacity: 0.2 }}>
          {#if view === 'month'}
            <MonthView
              weeks={monthGrid(focus, weekStart)}
              month={focus.getMonth()}
              {placed}
              {colors}
              {now}
              {weekStart}
              {timeFormat}
              {focus}
              {selectedKey}
              on:open={(e) => openOccurrence(e.detail.occ, e.detail.anchor)}
              on:pick={(e) => calendarFocus.set(e.detail)}
              on:day={(e) => openDay(e.detail)}
              on:create={(e) => startMonthCreate(e.detail.day, e.detail.anchor)}
              on:mailDrop={(e) => onMailDrop(e.detail)}
            />
          {:else if view === 'agenda'}
            <AgendaView
              from={range.from}
              to={range.to}
              {placed}
              {colors}
              {now}
              {timeFormat}
              {selectedKey}
              on:open={(e) => openOccurrence(e.detail.occ, e.detail.anchor)}
              on:day={(e) => openDay(e.detail)}
            />
          {:else}
            <TimeGrid
              days={view === 'week' ? weekDays(focus, weekStart) : [startOfDay(focus)]}
              {placed}
              {colors}
              {now}
              {timeFormat}
              workStart={$calendarSettings.workDayStart}
              workEnd={$calendarSettings.workDayEnd}
              zoneDisplay={$calendarSettings.timeZoneDisplay}
              secondaryZone={$calendarSettings.secondaryTimeZone}
              pending={quickPending}
              {selectedKey}
              busy={floating}
              on:open={(e) => openOccurrence(e.detail.occ, e.detail.anchor)}
              on:create={(e) => startQuick(e.detail.start, e.detail.end, e.detail.allDay)}
              on:change={(e) => onChange(e.detail.occ, e.detail.start, e.detail.end)}
              on:day={(e) => openDay(e.detail)}
              on:mailDrop={(e) => onMailDrop(e.detail)}
            />
          {/if}
        </div>
      {/key}
    </div>
  </div>
</section>

{#if selected}
  {#key selectedKey}
    <EventPopover
      occ={selected.occ}
      anchor={selected.anchor}
      calendarName={$calendars.find((c) => c.id === selected?.occ.calendarId)?.name || $t('calendar.defaultName')}
      color={colors.get(selected.occ.calendarId) ?? 'var(--cal-1)'}
      {timeFormat}
      on:close={() => (selected = null)}
      on:edit={(e) => editEvent(e.detail)}
      on:remove={(e) => removeFromPopover(e.detail.occ, e.detail.scope)}
    />
  {/key}
{/if}

{#if quick}
  <QuickCreate
    anchor={quick.anchor}
    start={quick.start}
    end={quick.end}
    allDay={quick.allDay}
    calendars={writable}
    calendarId={defaultCalendar}
    {timeFormat}
    on:close={closeFloating}
    on:save={(e) => saveQuick(e.detail.summary, e.detail.calendarId)}
    on:more={(e) => moreFromQuick(e.detail.summary, e.detail.calendarId)}
  />
{/if}

{#if editing}
  <EventEditor
    draft={editing}
    calendars={writable}
    {weekStart}
    {timeFormat}
    {identity}
    {saving}
    on:close={() => (editing = null)}
    on:save={(e) => onEditorSave(e.detail)}
    on:remove={onEditorRemove}
  />
{/if}

{#if scopeFor}
  <ScopeDialog
    title={$t(`calendar.scope.title.${scopeFor.kind}`)}
    on:close={() => (scopeFor = null)}
    on:choose={(e) => chooseScope(e.detail)}
  />
{/if}

<ConflictDialog {timeFormat} />

<style>
  .screen {
    position: absolute;
    inset: 0;
    display: flex;
    min-width: 0;
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: var(--font-ui);
    overflow: hidden;
  }

  .side {
    flex-shrink: 0;
    height: 100%;
    background: var(--surface-base);
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .side.drawer {
    position: absolute;
    inset-block: 0;
    inset-inline-start: 0;
    z-index: 20;
    box-shadow: var(--shadow-overlay);
  }

  .scrim {
    position: absolute;
    inset: 0;
    z-index: 19;
    background: var(--scrim);
  }

  .main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .nav {
    display: flex;
    gap: var(--space-1);
  }

  .title {
    margin: 0;
    min-width: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .spacer {
    flex: 1;
  }

  .sync-error {
    display: inline-flex;
    color: var(--warning);
  }

  .stage {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  .page {
    position: absolute;
    inset: 0;
  }
</style>
