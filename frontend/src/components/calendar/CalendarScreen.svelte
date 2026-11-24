<script lang="ts">
  // the calendar space of the new design: a sidebar with a small month and the
  // calendars, and the month, week, day or agenda view beside it. clicking an
  // event shows its details in a popover; drawing on empty time or double
  // clicking a day opens a quick create; right clicking a day or slot offers a
  // new event or quick add there; the full editor does the rest. mail dropped
  // on the Calendar entry or on a day or slot becomes a new event, calendar
  // files dropped on it are imported, and the app's shortcuts act on it while
  // it is on screen. it is code-split like the contacts screen and only loads
  // when opened.
  import { onDestroy, onMount, tick } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import caretLeft from '@phosphor-icons/core/assets/regular/caret-left.svg?raw'
  import caretRight from '@phosphor-icons/core/assets/regular/caret-right.svg?raw'
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import sidebarIcon from '@phosphor-icons/core/assets/duotone/sidebar-simple-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import lightningIcon from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import Segmented from '../settings2/ui/Segmented.svelte'
  import CalendarSidebar from './CalendarSidebar.svelte'
  import MonthView from './MonthView.svelte'
  import TimeGrid from './TimeGrid.svelte'
  import AgendaView from './AgendaView.svelte'
  import YearView from './YearView.svelte'
  import EventPopover from './EventPopover.svelte'
  import QuickCreate from './QuickCreate.svelte'
  import QuickAdd from './QuickAdd.svelte'
  import EventEditor from './EventEditor.svelte'
  import CalendarImport from './CalendarImport.svelte'
  import ScopeDialog from './ScopeDialog.svelte'
  import ConflictDialog from './ConflictDialog.svelte'
  import PlanPane from './PlanPane.svelte'
  import FindTime from './FindTime.svelte'
  import BlockDialog from './BlockDialog.svelte'
  import planIcon from '@phosphor-icons/core/assets/duotone/calendar-check-duotone.svg?raw'
  import {
    blockCalendarId,
    blockOccurrence,
    blockSettings,
    changeBlock,
    dropProposals,
    duplicateRequest,
    loadBlocks,
    loadBlockSettings,
    loadTaskPlans,
    makeBlocks,
    moveBlock,
    moveProposal,
    planPaneOpen,
    proposalOccurrence,
    proposalRequest,
    proposals,
    proposeAll,
    removeBlocks,
    requestOf,
    taskPlans,
    timeBlocks,
    watchBlocks,
  } from './timeblocks'
  import { blockTimeForMessages, blockTimeForTask, planDay, settleTimeBlockLink } from '../../lib/api'
  import { revealMessage } from '../../stores/selection'
  import { sidebar } from '../../stores/accounts'
  import {
    calendars,
    occurrences,
    calendarSettings,
    calendarView,
    calendarFocus,
    calendarSeed,
    calendarQuickAdd,
    calendarImport,
    timeblockRequest,
    calendarSync,
    calendarSyncError,
    defaultCalendarId,
    eventConflict,
    loadCalendars,
    loadCalendarSettings,
    showRange,
    refreshCalendars,
    saveEvent,
    removeEvent,
    watchCalendar,
    type CellMenuAt,
    type MailDropAt,
  } from '../../stores/calendar'
  import { createEventDraftFromMessage, getEvent, listAccounts, listOccurrences, mailEventSource, openICSFile, readICSFiles } from '../../lib/api'
  import { focusSpaceSearch, registerSpaceSearch } from '../../lib/spacesearch'
  import { searchOccurrences } from '../../lib/calendarsearch'
  import { onFilesDropped } from '../../lib/events'
  import {
    addDays,
    atMinutes,
    dayKey,
    defaultSlot,
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
  import { calendarColor, formatViewTitle, formatWhen } from '../../lib/calformat'
  import { datesInText, type FoundDate } from '../../lib/eventphrase'
  import { loadChrono } from '../../lib/chronoload'
  import { mailLinkLines } from '../../lib/maillink'
  import { draftFromEvent, localZone, newDraft, occurrenceKey, withTimes } from './occurrence'
  import { popupIsOpen } from '../../lib/popups'
  import { tip } from '../../lib/tip'
  import { openSpace, registerSpaceActions } from '../../stores/space'
  import { tasks, taskLists, taskSettings, moveTaskDue, toggleDone } from '../../stores/tasks'
  import { taskOccurrences } from './taskocc'
  import { moveDue } from '../../lib/tasklogic'
  import { openTask } from '../../lib/taskmail'
  import { convertEventToTask } from '../../lib/taskevent'
  import { openContextMenu } from '../../stores/contextmenu'
  import { bindings, menuHint } from '../../stores/shortcuts'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, pushActions, toastError, toastInfo } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { locale, shortcutLabel, t } from '../../lib/i18n'
  import { isEditableTarget } from '../../lib/editable'
  import type { TimeFormat } from '../../lib/format'
  import type { CalendarView, EventDraft, EventScope, ICSFile, MailEventSource, Occurrence, Task, TimeBlock } from '../../lib/types'

  const narrowWidth = 820
  const undoLimit = 20
  const toastMs = 8000

  let width = 0
  let sideOpen = false
  let now = new Date()
  let clock: ReturnType<typeof setInterval> | undefined
  let unwatch: (() => void) | undefined
  let unwatchBlocks: (() => void) | undefined
  let unregister: (() => void) | undefined
  let stopDrop: (() => void) | undefined
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
  $: taskEntries = $taskSettings.showInCalendar ? taskOccurrences($tasks, $taskLists, range.from, range.to) : []
  $: gridView = view === 'day' || view === 'week'
  // in the time grid a block written to a calendar is drawn as the block, so
  // its busy event is left out there; the other views show the event.
  $: blockEventUids = new Set($timeBlocks.map((b) => b.eventUid).filter(Boolean))
  $: shownEvents = gridView && blockEventUids.size > 0 ? $occurrences.filter((o) => !blockEventUids.has(o.uid)) : $occurrences
  $: blockEntries = gridView ? [...$timeBlocks.map(blockOccurrence), ...$proposals.map(proposalOccurrence)] : []
  $: placed = [...shownEvents, ...taskEntries, ...blockEntries].map(placeOccurrence)
  $: colors = new Map($calendars.map((c) => [c.id, calendarColor(c)]))
  $: writable = $calendars.filter((c) => !c.readOnly)
  $: defaultCalendar = defaultCalendarId($calendars, $calendarSettings)
  $: busyDays = daysWithEvents(placed)
  $: visibleCalendars = new Set($calendars.filter((c) => c.visible).map((c) => c.id))
  $: yearPlaced = view === 'year' ? placed.filter((p) => !p.occ.task && visibleCalendars.has(p.occ.calendarId)) : []
  $: title = formatViewTitle(view, range.from, range.to, focus)
  $: rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl'
  $: quickAddKey = $bindings['calendar-quick-add'] ? shortcutLabel($bindings['calendar-quick-add']) : ''

  let loadedKey = ''
  function loadRange(key: string): void {
    if (key !== loadedKey) {
      loadedKey = key
      void showRange(toRFC3339(range.from), toRFC3339(range.to))
      void loadBlocks(toRFC3339(range.from), toRFC3339(range.to))
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
    unwatchBlocks = watchBlocks()
    void loadBlockSettings()
    void loadTaskPlans()
    unregister = registerSpaceActions('calendar', {
      'plan-day': () => whenFree(() => void planToday()),
      'block-time': () => whenFree(blockTimeShortcut),
      compose: () => whenFree(newEvent),
      search: () => {
        if (!focusSpaceSearch()) {
          whenFree(() => openQuickAdd())
        }
      },
      'calendar-quick-add': () => whenFree(() => openQuickAdd()),
      'calendar-import': () => whenFree(() => void pickImport(0)),
      'delete-message': () => whenFree(deleteSelected),
      sync: () => void refreshCalendars(),
      undo: () => runUndo(),
      'next-view': () => whenFree(() => go(1)),
      'prev-view': () => whenFree(() => go(-1)),
    })
    stopDrop = onFilesDropped((e) => {
      if (e.target === 'calendar') {
        void importPaths(e.paths)
      }
    })
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

  // --- the top bar search ---------------------------------------------------------

  const searchBackDays = 366
  const searchAheadDays = 2 * 366
  const searchCacheMs = 30_000
  let searchPool: { at: number; list: Occurrence[] } | null = null

  // every occurrence from a year back to two years ahead, kept for half a
  // minute so typing does not ask the store for each letter.
  async function searchablePool(): Promise<Occurrence[]> {
    const now = Date.now()
    if (searchPool && now - searchPool.at < searchCacheMs) {
      return searchPool.list
    }
    const day = 86_400_000
    const list = await listOccurrences(toRFC3339(new Date(now - searchBackDays * day)), toRFC3339(new Date(now + searchAheadDays * day)))
    searchPool = { at: now, list }
    return list
  }

  const searchHits = new Map<string, Occurrence>()

  const stopSearch = registerSpaceSearch('calendar', {
    placeholderKey: 'calendar.search.placeholder',
    emptyKey: 'calendar.search.none',
    search: async (query) => {
      const found = searchOccurrences(await searchablePool(), query, new Date())
      searchHits.clear()
      return found.map((occ) => {
        const id = occurrenceKey(occ)
        searchHits.set(id, occ)
        const start = parseEventTime(occ.start, occ.allDay)
        const end = parseEventTime(occ.end, occ.allDay)
        return {
          id,
          title: occ.summary || $t('calendar.untitled'),
          detail: formatWhen(start, end, occ.allDay, timeFormat, $locale),
          aside: occ.location,
          color: colors.get(occ.calendarId),
        }
      })
    },
    open: (result) => {
      const occ = searchHits.get(result.id)
      if (occ) {
        whenFree(() => void openFound(occ))
      }
    },
  })

  async function openFound(occ: Occurrence): Promise<void> {
    try {
      const ev = await getEvent(occ.eventId, occ.recurrenceId)
      closeFloating()
      showDay(parseEventTime(occ.start, occ.allDay))
      openEditor(draftFromEvent(ev))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  onDestroy(() => {
    stopSearch()
    unwatch?.()
    unwatchBlocks?.()
    unregister?.()
    stopDrop?.()
    clearInterval(clock)
  })

  // an app shortcut acts only while nothing else of the calendar holds the
  // keyboard: an open editor, quick add or dialog takes it first.
  function whenFree(run: () => void): void {
    if (!editing && !quickAdd && !scopeFor && !importing && !findFor && !blockEditing) {
      run()
    }
  }

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

  function openMonth(day: Date): void {
    closeFloating()
    direction = 0
    calendarFocus.set(day)
    calendarView.set('month')
  }

  function showDay(day: Date): void {
    if (day < range.from || day >= range.to) {
      direction = day > focus ? 1 : -1
      calendarFocus.set(day)
    }
  }

  $: viewOptions = [
    { key: 'day', label: $t('calendar.view.day') },
    { key: 'week', label: $t('calendar.view.week') },
    { key: 'month', label: $t('calendar.view.month') },
    { key: 'agenda', label: $t('calendar.view.agenda') },
    { key: 'year', label: $t('calendar.view.year') },
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
    if (occ.task) {
      openTask(occ.task.id)
      return
    }
    if (occ.block || occ.proposal) {
      entryMenu(occ, anchor)
      return
    }
    selected = { occ, anchor }
  }

  // a task's checkbox in a view.
  function toggleTask(occ: Occurrence): void {
    if (occ.task && !occ.task.readOnly) {
      void toggleDone(occ.task)
    }
  }

  // a task dragged to another day of the month keeps its time of day.
  function moveTaskToDay(occ: Occurrence, day: Date): void {
    if (occ.task && !occ.task.readOnly && dayKey(day) !== dayKey(placeOccurrence(occ).start)) {
      void moveTaskDue(occ.task, moveDue(occ.task.due, day))
    }
  }

  // an event in the popover turned into a task.
  async function eventToTask(occ: Occurrence): Promise<void> {
    selected = null
    await convertEventToTask(occ.eventId)
  }

  function canCreate(): boolean {
    if (writable.length === 0) {
      toastError($t('calendar.noWritable'))
      return false
    }
    return true
  }

  async function startQuick(start: Date, end: Date, allDay: boolean, anchor: HTMLElement | null = null): Promise<void> {
    selected = null
    if (!canCreate()) {
      return
    }
    quickPending = { start, end, allDay }
    await tick()
    const target = anchor ?? stage?.querySelector<HTMLElement>('[data-pending]') ?? stage
    quick = { start, end, allDay, anchor: target }
  }

  function startMonthCreate(day: Date, anchor: HTMLElement): void {
    const at = defaultSlot(day, new Date())
    void startQuick(at, new Date(at.getTime() + 3600000), false, anchor)
  }

  function quickDraft(summary: string, calendarId: number): EventDraft | null {
    if (!quick) {
      return null
    }
    return { ...newDraft(calendarId, quick.start, quick.end, quick.allDay, $calendarSettings.defaultReminderMinutes), summary }
  }

  async function saveQuick(summary: string, calendarId: number): Promise<void> {
    const draft = quickDraft(summary, calendarId)
    if (draft && (await createDrafts([draft]))) {
      closeFloating()
    }
  }

  function moreFromQuick(summary: string, calendarId: number): void {
    const draft = quickDraft(summary, calendarId)
    closeFloating()
    if (draft) {
      openEditor(draft)
    }
  }

  // --- the context menu of a day or slot ------------------------------------------

  function onCellMenu(cell: CellMenuAt): void {
    closeFloating()
    openContextMenu(cell.x, cell.y, [
      { label: $t('calendar.menu.create'), action: () => createAt(cell) },
      { label: $t('calendar.menu.quickAdd'), action: () => openQuickAdd(cell.at, cell.mode === 'time' ? minutesOfDay(cell.at) : null), hint: menuHint('calendar-quick-add') },
    ])
  }

  function createAt(cell: Pick<CellMenuAt, 'at' | 'mode'>): void {
    if (!canCreate()) {
      return
    }
    const reminder = $calendarSettings.defaultReminderMinutes
    if (cell.mode === 'allDay') {
      openEditor(newDraft(defaultCalendar, startOfDay(cell.at), addDays(startOfDay(cell.at), 1), true, reminder))
      return
    }
    const start = cell.mode === 'time' ? cell.at : defaultSlot(cell.at, new Date())
    openEditor(newDraft(defaultCalendar, start, new Date(start.getTime() + 3600000), false, reminder))
  }

  // --- quick add ------------------------------------------------------------------

  let quickAdd: { base: Date; minutes: number | null } | null = null

  $: if ($calendarQuickAdd) {
    calendarQuickAdd.set(false)
    openQuickAdd()
  }

  function openQuickAdd(base: Date = focus, minutes: number | null = null): void {
    closeFloating()
    if (canCreate()) {
      quickAdd = { base, minutes }
    }
  }

  async function createFromQuickAdd(drafts: EventDraft[]): Promise<void> {
    if (await createDrafts(drafts)) {
      quickAdd = null
    }
  }

  function editFromQuickAdd(draft: EventDraft): void {
    quickAdd = null
    openEditor(draft)
  }

  // --- undo -----------------------------------------------------------------------

  let undoStack: (() => Promise<void>)[] = []

  function pushUndo(run: () => Promise<void>): void {
    undoStack = [...undoStack.slice(-(undoLimit - 1)), run]
  }

  function runUndo(): boolean {
    const last = undoStack[undoStack.length - 1]
    if (!last) {
      return false
    }
    undoStack = undoStack.slice(0, -1)
    void last()
    return true
  }

  // creates events, then says so with Undo and Open. it reports whether any
  // was created.
  async function createDrafts(list: EventDraft[]): Promise<boolean> {
    const ids: number[] = []
    for (const draft of list) {
      const result = await saveEvent(draft)
      if (result) {
        ids.push(result.event.id)
      }
    }
    if (ids.length === 0) {
      return false
    }
    showDay(parseEventTime(list[0].start, list[0].allDay))
    let undone = false
    const undo = async (): Promise<void> => {
      if (!undone) {
        undone = true
        for (const id of ids) {
          await removeEvent(id)
        }
      }
    }
    pushUndo(undo)
    const message = ids.length > 1 ? $t('quickAdd.createdMany').replace('{n}', String(ids.length)) : $t('quickAdd.created')
    pushActions(
      'success',
      message,
      [
        { label: $t('quickAdd.undo'), run: () => void undo() },
        { label: $t('quickAdd.open'), run: () => void openSaved(ids[0]) },
      ],
      toastMs,
    )
    return true
  }

  async function openSaved(id: number): Promise<void> {
    try {
      const ev = await getEvent(id, '')
      showDay(parseEventTime(ev.start, ev.allDay))
      openEditor(draftFromEvent(ev))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // --- the editor -----------------------------------------------------------------

  let editing: EventDraft | null = null
  let editorFound: FoundDate[] = []
  let editorNote = ''
  let saving = false
  let scopeFor: { kind: 'edit' | 'move' | 'delete'; run: (scope: EventScope) => Promise<void> } | null = null

  function openEditor(draft: EventDraft, found: FoundDate[] = [], note = ''): void {
    editorFound = found
    editorNote = note
    editing = draft
  }

  function closeEditor(): void {
    editing = null
    editorFound = []
    editorNote = ''
  }

  function newEvent(): void {
    closeFloating()
    if (!canCreate()) {
      return
    }
    const start = defaultSlot(focus, new Date())
    openEditor(newDraft(defaultCalendar, start, new Date(start.getTime() + 3600000), false, $calendarSettings.defaultReminderMinutes))
  }

  function editEvent(ev: Parameters<typeof draftFromEvent>[0]): void {
    selected = null
    openEditor(draftFromEvent(ev))
  }

  async function commit(draft: EventDraft): Promise<boolean> {
    saving = true
    const result = await saveEvent(draft)
    saving = false
    if (result && draft.id === 0) {
      const id = result.event.id
      pushUndo(async () => void (await removeEvent(id)))
    }
    return result !== null || $eventConflict !== null
  }

  function onEditorSave(draft: EventDraft): void {
    if (draft.id !== 0 && draft.recurrenceId) {
      scopeFor = {
        kind: 'edit',
        run: async (scope) => {
          if (await commit({ ...draft, scope })) {
            closeEditor()
          }
        },
      }
      return
    }
    void commit(draft).then((done) => {
      if (done) {
        closeEditor()
      }
    })
  }

  function onEditorRemove(): void {
    const draft = editing
    if (!draft) {
      return
    }
    void removeWithScope(draft.id, draft.recurrenceId, draft.recurrenceId !== '').then((done) => done && closeEditor())
  }

  // deletes an event, asking first which occurrences for a repeating one. a
  // plain event can be brought back with undo.
  function removeWithScope(id: number, recurrenceId: string, recurring: boolean): Promise<boolean> {
    if (recurring) {
      return new Promise((resolve) => {
        scopeFor = {
          kind: 'delete',
          run: async (scope) => resolve(await removeEvent(id, scope, recurrenceId)),
        }
      })
    }
    return (async () => {
      let before: EventDraft | null = null
      try {
        before = draftFromEvent(await getEvent(id, ''))
      } catch {
        before = null
      }
      const done = await removeEvent(id)
      if (done && before) {
        const restore = { ...before, id: 0 }
        pushUndo(async () => void (await saveEvent(restore)))
      }
      return done
    })()
  }

  async function removeFromPopover(occ: Occurrence, scope: EventScope | ''): Promise<void> {
    selected = null
    if (scope) {
      await removeEvent(occ.eventId, scope, occ.recurrenceId)
    } else {
      await removeWithScope(occ.eventId, occ.recurrenceId, false)
    }
  }

  function deleteSelected(): void {
    const occ = selected?.occ
    if (!occ || occ.readOnly) {
      return
    }
    selected = null
    void removeWithScope(occ.eventId, occ.recurrenceId, occ.recurring)
  }

  // a drag in the time grid: the occurrence moves at once, then the change is
  // saved, asking first which occurrences it is for when the event repeats.
  function onChange(occ: Occurrence, start: Date, end: Date): void {
    if (occ.block) {
      void moveBlockWithUndo(occ.block, start, end)
      return
    }
    if (occ.proposal) {
      moveProposal(occ.proposal.key, start, end)
      refocus(occurrenceKey(occ))
      return
    }
    if (occ.task) {
      if (!occ.task.readOnly) {
        void moveTaskDue(occ.task, occ.allDay ? dayKey(start) : start.toISOString())
      }
      return
    }
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
        const old = draftFromEvent(ev)
        const result = await saveEvent({ ...withTimes(old, start, end), scope })
        if (!result) {
          occurrences.set(before)
        } else if (!occ.recurring) {
          pushUndo(async () => {
            const fresh = draftFromEvent(await getEvent(occ.eventId, ''))
            await saveEvent({ ...fresh, start: old.start, end: old.end })
          })
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

  // where a drop with no date of its own goes: the slot it landed on, or the
  // day on screen at the next half hour.
  function slotOf(seed: MailDropAt): { start: Date; end: Date; allDay: boolean } {
    if (seed.at && seed.mode === 'allDay') {
      return { start: startOfDay(seed.at), end: addDays(startOfDay(seed.at), 1), allDay: true }
    }
    const start = seed.at && seed.mode === 'time' ? seed.at : defaultSlot(seed.at ?? focus, new Date())
    const end = seed.mode === 'time' && seed.end && seed.end > start ? seed.end : new Date(start.getTime() + 3600000)
    return { start, end, allDay: false }
  }

  async function foundIn(mail: MailEventSource): Promise<FoundDate[]> {
    const chrono = await loadChrono($locale)
    const sent = mail.date ? new Date(mail.date) : new Date()
    return datesInText(`${mail.subject}\n${mail.text}`, { ref: sent, now: new Date(), lang: $locale, chrono })
  }

  // a plain drop: the event takes its date from the mail, or from the slot it
  // was dropped on, which the user chose.
  function mailSlot(seed: MailDropAt, found: FoundDate[]): { start: Date; end: Date; allDay: boolean; location: string; guessed: boolean } {
    const first = found[0]
    if (seed.at && seed.mode === 'day' && first && !first.allDay) {
      const start = atMinutes(seed.at, minutesOfDay(first.start))
      return { start, end: new Date(start.getTime() + (first.end.getTime() - first.start.getTime())), allDay: false, location: first.location, guessed: false }
    }
    if (seed.at) {
      return { ...slotOf(seed), location: first?.location ?? '', guessed: false }
    }
    if (first) {
      return { start: first.start, end: first.end, allDay: first.allDay, location: first.location, guessed: false }
    }
    return { ...slotOf(seed), location: '', guessed: true }
  }

  async function takeSeed(seed: MailDropAt): Promise<void> {
    calendarSeed.set(null)
    closeFloating()
    if (seed.alt && seed.messageIds.length > 0) {
      await blockForMail(seed)
      return
    }
    if (!canCreate() || seed.messageIds.length === 0) {
      return
    }
    try {
      const reminder = $calendarSettings.defaultReminderMinutes
      if (seed.shift) {
        const mails = await Promise.all(seed.messageIds.map(mailEventSource))
        const slot = slotOf(seed)
        const draft = newDraft(defaultCalendar, slot.start, slot.end, slot.allDay, reminder)
        draft.summary = mails.length === 1 ? mails[0].subject : ''
        draft.description = mails.map((m) => mailLinkLines(m, $t)).join('\n\n')
        showDay(slot.start)
        openEditor(draft)
        return
      }
      if (seed.messageIds.length > 1) {
        const drafts: EventDraft[] = []
        for (const id of seed.messageIds) {
          const mail = await mailEventSource(id)
          const at = mailSlot(seed, await foundIn(mail))
          drafts.push({ ...newDraft(defaultCalendar, at.start, at.end, at.allDay, reminder), summary: mail.subject, location: at.location, description: mailLinkLines(mail, $t) })
        }
        await createDrafts(drafts)
        return
      }
      const [prefill, mail] = await Promise.all([createEventDraftFromMessage(seed.messageIds[0]), mailEventSource(seed.messageIds[0])])
      const found = await foundIn(mail)
      const at = mailSlot(seed, found)
      const calendarId = writable.some((c) => c.id === prefill.calendarId) ? prefill.calendarId : defaultCalendar
      const draft = withTimes(
        { ...prefill, calendarId, allDay: at.allDay, location: at.location || prefill.location, timeZone: at.allDay ? '' : localZone(), scope: '', recurrenceId: '', force: false },
        at.start,
        at.end,
      )
      showDay(at.start)
      openEditor(draft, found, at.guessed ? $t('calendar.noDateInMail') : '')
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // --- time blocking ---------------------------------------------------------------------

  let focusKey = ''
  let findFor: { task: Task; minutes: number } | null = null
  let blockEditing: TimeBlock | null = null
  let planning = false
  let unplaced: string[] = []
  let short: string[] = []
  let announcement = ''

  $: showPlan = gridView && $planPaneOpen && !narrow
  $: planKey = $bindings['plan-day'] ? shortcutLabel($bindings['plan-day']) : ''
  $: blockCalendar = blockCalendarId($blockSettings, $calendars, defaultCalendar)

  $: if ($timeblockRequest) {
    const ask = $timeblockRequest
    timeblockRequest.set(null)
    if (ask.kind === 'plan') {
      void planToday()
    } else {
      const task = $tasks.find((x) => x.id === ask.taskId)
      if (task) {
        findTime(task)
      }
    }
  }

  // puts the keyboard focus on an entry once the grid has drawn it again.
  function refocus(key: string): void {
    focusKey = ''
    void tick().then(() => (focusKey = key))
  }

  // says something to assistive technology without moving the focus.
  function announce(text: string): void {
    announcement = ''
    void tick().then(() => (announcement = text))
  }

  // how long a new block for a task is: what its estimate still needs, else
  // the whole estimate or the default length.
  function blockMinutesFor(task: Task): number {
    const planned = $taskPlans.get(task.uid)?.plannedMinutes ?? 0
    if (task.estimate > planned) {
      return task.estimate - planned
    }
    return task.estimate || $blockSettings.defaultMinutes
  }

  // says that blocks were made, with Undo, which the undo key reaches too.
  function saidMade(made: TimeBlock[], message: string, afterUndo: () => void = () => undefined): void {
    const ids = made.map((b) => b.id)
    let undone = false
    const undo = async (): Promise<void> => {
      if (!undone) {
        undone = true
        await removeBlocks(ids)
        afterUndo()
        void loadTaskPlans()
        announce($t('timeblock.announce.undone'))
      }
    }
    pushUndo(undo)
    pushActions('success', message, [{ label: $t('quickAdd.undo'), run: () => void undo() }], toastMs)
    announce(message)
    void loadTaskPlans()
  }

  function madeText(made: TimeBlock[]): string {
    if (made.length > 1) {
      return $t('timeblock.toast.madeMany').replace('{n}', String(made.length))
    }
    return $t('timeblock.toast.made').replace('{when}', formatWhen(new Date(made[0].start), new Date(made[0].end), false, timeFormat, $locale))
  }

  async function blockTaskAt(task: Task, start: Date, end: Date): Promise<void> {
    const made = await makeBlocks([
      { id: 0, title: '', start: toRFC3339(start), end: toRFC3339(end), taskUid: task.uid, messageId: 0, done: false, calendarId: blockCalendar, eventTitle: '' },
    ])
    if (made.length > 0) {
      showDay(start)
      refocus(occurrenceKey(blockOccurrence(made[0])))
      saidMade(made, madeText(made))
    }
  }

  async function blockNext(task: Task): Promise<void> {
    try {
      const made = await blockTimeForTask(task.id, blockMinutesFor(task))
      timeBlocks.update((list) => [...list, made])
      showDay(new Date(made.start))
      refocus(occurrenceKey(blockOccurrence(made)))
      saidMade([made], madeText([made]))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function findTime(task: Task): void {
    closeFloating()
    findFor = { task, minutes: blockMinutesFor(task) }
  }

  async function pickSlot(start: Date, end: Date): Promise<void> {
    const task = findFor?.task
    findFor = null
    if (task) {
      await blockTaskAt(task, start, end)
    }
  }

  // mail dropped with Alt held: a block to deal with each mail, from the slot
  // it was dropped on, or in the next free time.
  async function blockForMail(seed: MailDropAt): Promise<void> {
    let at = ''
    if (seed.at) {
      at = toRFC3339(seed.mode === 'time' ? seed.at : defaultSlot(seed.at, new Date()))
    }
    try {
      const made = await blockTimeForMessages(seed.messageIds, at)
      if (made.length > 0) {
        timeBlocks.update((list) => [...list, ...made])
        showDay(new Date(made[0].start))
        refocus(occurrenceKey(blockOccurrence(made[0])))
        saidMade(made, madeText(made))
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function moveBlockWithUndo(b: TimeBlock, start: Date, end: Date): Promise<void> {
    const saved = await moveBlock(b, start, end)
    if (saved) {
      refocus(occurrenceKey(blockOccurrence(saved)))
      pushUndo(async () => void (await changeBlock(saved, { start: b.start, end: b.end })))
    }
  }

  async function deleteBlock(b: TimeBlock): Promise<void> {
    if (!(await removeBlocks([b.id]))) {
      return
    }
    void loadTaskPlans()
    const again = { ...requestOf(b), id: 0, messageId: b.messageId }
    let undone = false
    const undo = async (): Promise<void> => {
      if (!undone) {
        undone = true
        await makeBlocks([again])
        void loadTaskPlans()
      }
    }
    pushUndo(undo)
    pushActions('success', $t('timeblock.deleted'), [{ label: $t('quickAdd.undo'), run: () => void undo() }], toastMs)
    announce($t('timeblock.deleted'))
  }

  async function duplicateBlock(b: TimeBlock): Promise<void> {
    const made = await makeBlocks([{ ...duplicateRequest(b), messageId: b.messageId }])
    if (made.length > 0) {
      refocus(occurrenceKey(blockOccurrence(made[0])))
      saidMade(made, madeText(made))
    }
  }

  async function saveBlock(b: TimeBlock, patch: Parameters<typeof changeBlock>[1]): Promise<void> {
    blockEditing = null
    const saved = await changeBlock(b, patch)
    if (saved) {
      void loadTaskPlans()
      refocus(occurrenceKey(blockOccurrence(saved)))
    }
  }

  async function settle(b: TimeBlock, action: 'follow' | 'rewrite' | 'forget'): Promise<void> {
    blockEditing = null
    try {
      const saved = await settleTimeBlockLink(b.id, action)
      timeBlocks.update((list) => list.map((x) => (x.id === saved.id ? saved : x)))
      refocus(occurrenceKey(blockOccurrence(saved)))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function showMail(b: TimeBlock): void {
    openSpace('mail')
    revealMessage(b.messageId, b.accountId, b.folderId, $sidebar.data ?? null)
  }

  // the menu of a block or a proposal, which is also what clicking one opens.
  function entryMenu(occ: Occurrence, anchor: HTMLElement): void {
    const r = anchor.getBoundingClientRect()
    const x = r.left + Math.min(r.width / 2, 48)
    const y = r.top + Math.min(r.height, 28)
    if (occ.proposal) {
      const key = occ.proposal.key
      openContextMenu(x, y, [
        { label: $t('timeblock.review.accept'), action: () => void acceptProposals([key]) },
        { label: $t('timeblock.review.dismiss'), action: () => dismissProposals([key]) },
      ])
      return
    }
    const b = occ.block
    if (!b) {
      return
    }
    const task = $tasks.find((x) => x.id === b.taskId)
    const entries: Parameters<typeof openContextMenu>[2] = []
    if (task) {
      entries.push({ label: $t('timeblock.openTask'), action: () => openTask(task.id) })
    }
    if (b.messageId && !b.masked) {
      entries.push({ label: $t('timeblock.menu.showMail'), action: () => showMail(b) })
    }
    if (entries.length > 0) {
      entries.push('separator')
    }
    entries.push(
      { label: $t(b.done ? 'timeblock.menu.undone' : 'timeblock.menu.done'), action: () => void changeBlock(b, { done: !b.done }) },
      { label: $t('timeblock.menu.duplicate'), hint: shortcutLabel('mod+d'), action: () => void duplicateBlock(b) },
      { label: $t('timeblock.menu.details'), action: () => (blockEditing = b) },
    )
    if (b.linkState === 'out_of_step') {
      entries.push({ label: $t('timeblock.settle.follow'), action: () => void settle(b, 'follow') })
    }
    if (b.linkState) {
      entries.push(
        { label: $t('timeblock.settle.rewrite'), action: () => void settle(b, 'rewrite') },
        { label: $t('timeblock.settle.forget'), action: () => void settle(b, 'forget') },
      )
    }
    entries.push('separator', { label: $t('timeblock.delete'), danger: true, hint: shortcutLabel('delete'), action: () => void deleteBlock(b) })
    openContextMenu(x, y, entries)
  }

  function onBlockAction(occ: Occurrence, action: 'delete' | 'duplicate' | 'menu', anchor: HTMLElement): void {
    if (action === 'menu') {
      entryMenu(occ, anchor)
    } else if (occ.proposal && action === 'delete') {
      dismissProposals([occ.proposal.key])
    } else if (occ.block && action === 'delete') {
      void deleteBlock(occ.block)
    } else if (occ.block && action === 'duplicate') {
      void duplicateBlock(occ.block)
    }
  }

  // --- plan my day ------------------------------------------------------------

  async function planToday(): Promise<void> {
    if (planning) {
      return
    }
    planning = true
    closeFloating()
    const today = new Date()
    if (!gridView) {
      calendarView.set('day')
    }
    showDay(today)
    planPaneOpen.set(true)
    try {
      const plan = await planDay(dayKey(today))
      proposeAll(plan.proposals)
      unplaced = plan.unplaced
      short = plan.short
      if (plan.proposals.length > 0) {
        announce($t('timeblock.announce.proposed').replace('{n}', String(plan.proposals.length)))
        refocus(occurrenceKey(proposalOccurrence($proposals[0])))
      } else {
        announce($t(plan.tasks === 0 ? 'timeblock.announce.nothing' : 'timeblock.announce.noTime'))
        if (plan.tasks === 0) {
          toastInfo($t('timeblock.announce.nothing'))
        }
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      planning = false
    }
  }

  function clearNotesWhenDone(): void {
    if ($proposals.length === 0) {
      unplaced = []
      short = []
    }
  }

  async function acceptProposals(keys: string[]): Promise<void> {
    const chosen = $proposals.filter((p) => keys.includes(p.key))
    if (chosen.length === 0) {
      return
    }
    const made = await makeBlocks(chosen.map((p) => proposalRequest(p, blockCalendar)))
    if (made.length === 0) {
      return
    }
    dropProposals(keys)
    clearNotesWhenDone()
    const message = made.length > 1 ? $t('timeblock.accepted.many').replace('{n}', String(made.length)) : $t('timeblock.accepted.one')
    saidMade(made, message, () => proposals.update((list) => [...list, ...chosen]))
    if ($proposals.length > 0) {
      refocus(occurrenceKey(proposalOccurrence($proposals[0])))
    } else {
      refocus(occurrenceKey(blockOccurrence(made[0])))
    }
  }

  function dismissProposals(keys: string[]): void {
    dropProposals(keys)
    clearNotesWhenDone()
    announce($t(keys.length > 1 ? 'timeblock.announce.dismissedMany' : 'timeblock.announce.dismissed').replace('{n}', String(keys.length)))
  }

  // the block time key in the calendar opens the plan pane and puts the focus
  // on its first task's "Find time".
  function blockTimeShortcut(): void {
    if (!gridView) {
      calendarView.set('week')
    }
    planPaneOpen.set(true)
    void tick().then(() => document.querySelector<HTMLElement>('.screen aside .tasks button')?.focus())
  }

  // --- importing calendar files -------------------------------------------------------

  let importing: { files: ICSFile[]; calendarId: number } | null = null

  $: if ($calendarImport) {
    const paths = $calendarImport
    calendarImport.set(null)
    void importPaths(paths)
  }

  async function importPaths(paths: string[]): Promise<void> {
    try {
      startImport(await readICSFiles(paths), 0)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function pickImport(calendarId: number): Promise<void> {
    try {
      const file = await openICSFile()
      if (file.name) {
        startImport([file], calendarId)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function startImport(files: ICSFile[], calendarId: number): void {
    if (files.length === 0 || !canCreate()) {
      return
    }
    closeFloating()
    importing = { files, calendarId: writable.some((c) => c.id === calendarId) ? calendarId : defaultCalendar }
  }

  // --- keys -------------------------------------------------------------------------

  function onKeydown(event: KeyboardEvent): void {
    if (
      event.metaKey ||
      event.ctrlKey ||
      event.altKey ||
      isEditableTarget(event.target) ||
      popupIsOpen() ||
      editing ||
      scopeFor ||
      quickAdd ||
      importing ||
      findFor ||
      blockEditing
    ) {
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
      y: () => setView('year'),
      n: newEvent,
      [back]: () => go(-1),
      [forward]: () => go(1),
    }
    const run = actions[event.key]
    const target = event.target as HTMLElement
    const ownsKey = target.closest('.mini') || (target.closest('.year') && event.key.startsWith('Arrow'))
    if (run && !ownsKey) {
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
<section class="screen" aria-label={$t('space.calendar')} data-file-drop-target="calendar" bind:clientWidth={width} on:keydown={onKeydown}>
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
        on:import={(e) => pickImport(e.detail)}
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
      <button type="button" class="cal-btn quick-add" aria-label={$t('quickAdd.title')} use:tip={{ text: $t('quickAdd.title'), shortcut: quickAddKey }} on:click={() => openQuickAdd()}>
        <Ph svg={lightningIcon} size={15} />
        {#if !narrow}<span>{$t('quickAdd.button')}</span>{/if}
      </button>
      <button
        type="button"
        class="cal-icon"
        aria-label={$t('calendar.sync')}
        use:tip={{ text: $t('calendar.sync') }}
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
      {#if gridView && !narrow}
        <button
          type="button"
          class="cal-icon"
          aria-label={$t('timeblock.pane.toggle')}
          aria-pressed={$planPaneOpen}
          use:tip={{ text: $t('timeblock.pane.toggle') }}
          on:click={() => planPaneOpen.update((v) => !v)}
        >
          <Ph svg={planIcon} size={16} />
        </button>
      {/if}
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
              on:menu={(e) => onCellMenu(e.detail)}
              on:toggleTask={(e) => toggleTask(e.detail)}
              on:moveTask={(e) => moveTaskToDay(e.detail.occ, e.detail.day)}
            />
          {:else if view === 'year'}
            <YearView
              year={focus.getFullYear()}
              {focus}
              {now}
              {weekStart}
              placed={yearPlaced}
              {colors}
              {timeFormat}
              on:day={(e) => openDay(e.detail)}
              on:month={(e) => openMonth(e.detail)}
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
              on:menu={(e) => onCellMenu(e.detail)}
              on:toggleTask={(e) => toggleTask(e.detail)}
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
              {focusKey}
              busy={floating}
              on:open={(e) => openOccurrence(e.detail.occ, e.detail.anchor)}
              on:create={(e) => startQuick(e.detail.start, e.detail.end, e.detail.allDay)}
              on:change={(e) => onChange(e.detail.occ, e.detail.start, e.detail.end)}
              on:day={(e) => openDay(e.detail)}
              on:mailDrop={(e) => onMailDrop(e.detail)}
              on:menu={(e) => onCellMenu(e.detail)}
              on:toggleTask={(e) => toggleTask(e.detail)}
              on:taskDrop={(e) => void blockTaskAt(e.detail.task, e.detail.start, e.detail.end)}
              on:blockAction={(e) => onBlockAction(e.detail.occ, e.detail.action, e.detail.anchor)}
              on:quickAdd={() => openQuickAdd()}
            />
          {/if}
        </div>
      {/key}
    </div>
  </div>

  {#if showPlan}
    <PlanPane
      tasks={$tasks}
      plans={$taskPlans}
      proposals={$proposals}
      {unplaced}
      {short}
      defaultMinutes={$blockSettings.defaultMinutes}
      {timeFormat}
      {planKey}
      {planning}
      {now}
      on:findTime={(e) => findTime(e.detail)}
      on:blockNext={(e) => void blockNext(e.detail)}
      on:openTask={(e) => openTask(e.detail.id)}
      on:plan={() => void planToday()}
      on:accept={(e) => void acceptProposals(e.detail)}
      on:dismiss={(e) => dismissProposals(e.detail)}
    />
  {/if}

  <div class="sr" aria-live="polite" aria-atomic="true">{announcement}</div>

  <div class="drop-veil" aria-hidden="true">{$t('calendar.dropFiles')}</div>
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
      on:toTask={(e) => void eventToTask(e.detail)}
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

{#if quickAdd}
  <QuickAdd
    base={quickAdd.base}
    baseMinutes={quickAdd.minutes}
    calendars={writable}
    calendarId={defaultCalendar}
    {timeFormat}
    defaultReminder={$calendarSettings.defaultReminderMinutes}
    on:close={() => (quickAdd = null)}
    on:create={(e) => createFromQuickAdd(e.detail)}
    on:edit={(e) => editFromQuickAdd(e.detail)}
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
    found={editorFound}
    note={editorNote}
    on:close={closeEditor}
    on:save={(e) => onEditorSave(e.detail)}
    on:remove={onEditorRemove}
  />
{/if}

{#if importing}
  <CalendarImport files={importing.files} calendars={writable} calendarId={importing.calendarId} on:close={() => (importing = null)} />
{/if}

{#if scopeFor}
  <ScopeDialog
    title={$t(`calendar.scope.title.${scopeFor.kind}`)}
    on:close={() => (scopeFor = null)}
    on:choose={(e) => chooseScope(e.detail)}
  />
{/if}

{#if findFor}
  <FindTime task={findFor.task} minutes={findFor.minutes} {timeFormat} on:close={() => (findFor = null)} on:pick={(e) => void pickSlot(e.detail.start, e.detail.end)} />
{/if}

{#if blockEditing}
  {@const editingBlock = blockEditing}
  <BlockDialog
    block={editingBlock}
    calendars={$calendars}
    settings={$blockSettings}
    {timeFormat}
    on:close={() => (blockEditing = null)}
    on:save={(e) => void saveBlock(editingBlock, e.detail)}
    on:remove={() => {
      blockEditing = null
      void deleteBlock(editingBlock)
    }}
    on:settle={(e) => void settle(editingBlock, e.detail)}
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

  .quick-add {
    color: var(--text-secondary);
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

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  /* files held over the calendar: a dashed outline and a line saying they
     will be imported. */
  .drop-veil {
    position: absolute;
    inset: var(--space-3);
    z-index: 30;
    display: flex;
    align-items: center;
    justify-content: center;
    border: var(--hairline) dashed var(--accent);
    border-radius: var(--radius-card);
    background: color-mix(in srgb, var(--surface-overlay) 88%, transparent);
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--accent);
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .screen:global(.file-drop-target-active) > .drop-veil {
    opacity: 1;
  }
</style>
