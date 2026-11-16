<script lang="ts">
  // the Tasks space of the new design: the smart lists and task lists in a
  // sidebar, the chosen list in the middle with a quick entry line on top,
  // and the selected task's details in a pane beside it. panes fold in a
  // narrow window: the details first slide over the list, then the sidebar
  // becomes a drawer. mail dropped on the Tasks entry while this is showing
  // becomes tasks here, and the app's shortcuts act on it: compose makes a
  // task, search goes to the task search, delete removes the selected task.
  // it is code-split like the calendar and only loads when opened.
  import { onDestroy, onMount, tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import sidebarIcon from '@phosphor-icons/core/assets/duotone/sidebar-simple-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import Modal from '../common/Modal.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import TaskSidebar from './TaskSidebar.svelte'
  import TaskListPane from './TaskListPane.svelte'
  import TaskDetail from './TaskDetail.svelte'
  import {
    tasks,
    taskLists,
    taskSettings,
    taskSeed,
    taskFocus,
    taskForContact,
    taskConflict,
    taskClock,
    defaultListId,
    emptyDraft,
    draftOf,
    loadTasks,
    localZone,
    moveTaskDue,
    removeTask,
    resolveConflict,
    saveTask,
    saveTaskSettings,
    startTasks,
    toggleDone,
  } from '../../stores/tasks'
  import { calendarFocus, calendarSync, calendarSyncError, calendarSettings, loadCalendarSettings, refreshCalendars } from '../../stores/calendar'
  import { reorderTasks, findEventByUID } from '../../lib/api'
  import { createTasksFromMail } from '../../lib/taskmail'
  import { convertTaskToEvent } from '../../lib/taskevent'
  import { parseTaskPhrase } from '../../lib/taskphrase'
  import { loadChrono } from '../../lib/chronoload'
  import type { ChronoLike } from '../../lib/eventphrase'
  import { dayKey, parseEventTime, resolveWeekStart } from '../../lib/caldate'
  import { describeRRule } from '../../lib/recurrence'
  import { dueDate, dueHasTime, moveDue, taskRows, tasksInScope, type TaskScope } from '../../lib/tasklogic'
  import { focusSpaceSearch, matchesQuery, registerSpaceSearch } from '../../lib/spacesearch'
  import { registerSpaceActions, openSpace } from '../../stores/space'
  import { openContextMenu } from '../../stores/contextmenu'
  import { popupIsOpen } from '../../lib/popups'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { locale, t } from '../../lib/i18n'
  import { isEditableTarget } from '../../lib/editable'
  import type { TimeFormat } from '../../lib/format'
  import type { Task, TaskSort } from '../../lib/types'

  const narrowDetail = 1040
  const narrowSide = 720

  let width = 0
  let sideOpen = false
  let scope: TaskScope = { kind: 'smart', key: 'today' }
  let selectedId: number | null = null
  let detailOpen = false
  let filter = ''
  let filterInput: HTMLInputElement
  let listPane: TaskListPane
  let chrono: ChronoLike[] = []
  let entryText = ''
  let contactForNew: { name: string; email: string } | null = null

  let unregister: (() => void) | undefined
  let stopSearch: (() => void) | undefined
  let stopTasks: (() => void) | undefined

  $: foldDetail = width > 0 && width < narrowDetail
  $: foldSide = width > 0 && width < narrowSide
  $: if (!foldSide) {
    sideOpen = false
  }
  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: weekStart = resolveWeekStart($calendarSettings.weekStart, navigator.language)
  $: now = $taskClock
  $: sort = $taskSettings.sort
  $: scoped = tasksInScope($tasks, scope, now)
  $: visible = filter.trim() ? scoped.filter((x) => matchesTask(x, filter)) : scoped
  $: rows = taskRows(visible, sort, !(scope.kind === 'smart' && scope.key === 'completed'))
  $: selected = $tasks.find((x) => x.id === selectedId) ?? null
  $: subtasks = selected ? $tasks.filter((x) => x.listId === selected?.listId && x.parent === selected?.uid) : []
  $: currentList = scope.kind === 'list' ? $taskLists.find((l) => scope.kind === 'list' && l.id === scope.id) : undefined
  $: targetList = currentList && !currentList.readOnly ? currentList.id : defaultListId($taskLists, $taskSettings)
  $: canAdd = targetList !== 0 && !(scope.kind === 'smart' && scope.key === 'completed')
  $: canReorder = scope.kind === 'list' && sort === 'manual' && !filter.trim() && !currentList?.readOnly
  $: title = scope.kind === 'smart' ? $t(`tasks.smart.${scope.key}`) : currentList?.name || $t('tasks.defaultList')
  $: emptyText = filter.trim() ? $t('tasks.noMatches') : $t(scope.kind === 'smart' ? `tasks.empty.${scope.key}` : 'tasks.empty.list')
  $: preview = describeEntry(entryText, chrono, contactForNew)

  function matchesTask(task: Task, query: string): boolean {
    const list = $taskLists.find((l) => l.id === task.listId)
    return matchesQuery(query, [task.title, task.notes, list?.name || $t('tasks.defaultList'), ...task.categories, ...task.contacts.map((c) => `${c.name} ${c.email}`)])
  }

  onMount(() => {
    stopTasks = startTasks()
    void loadTasks()
    void loadCalendarSettings()
    void loadChrono($locale).then((c) => (chrono = c))
    unregister = registerSpaceActions('tasks', {
      compose: () => whenFree(newTask),
      search: () => {
        if (!focusSpaceSearch()) {
          filterInput?.focus()
        }
      },
      'delete-message': () => whenFree(() => void removeSelected()),
      sync: () => void sync(),
    })
    stopSearch = registerSpaceSearch('tasks', {
      placeholderKey: 'tasks.searchPlaceholder',
      emptyKey: 'tasks.noMatches',
      search: (query) =>
        $tasks
          .filter((x) => matchesTask(x, query))
          .sort((a, b) => Number(a.done) - Number(b.done))
          .slice(0, 20)
          .map((x) => {
            const list = $taskLists.find((l) => l.id === x.listId)
            const due = dueDate(x)
            return {
              id: String(x.id),
              title: x.title || $t('tasks.untitled'),
              detail: x.notes.split('\n')[0] ?? '',
              aside: due ? due.toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) : '',
              color: list?.color || undefined,
            }
          }),
      open: (result) => showTask(Number(result.id)),
    })
  })

  onDestroy(() => {
    unregister?.()
    stopSearch?.()
    stopTasks?.()
  })

  // a shortcut acts only while no dialog of the screen holds the keyboard.
  function whenFree(run: () => void): void {
    if (!$taskConflict && !popupIsOpen()) {
      run()
    }
  }

  function setScope(next: TaskScope): void {
    scope = next
    filter = ''
    if (foldSide) {
      sideOpen = false
    }
  }

  // --- selection --------------------------------------------------------------------

  function select(id: number): void {
    selectedId = id
    if (!foldDetail) {
      detailOpen = true
    }
  }

  async function openDetail(id: number): Promise<void> {
    selectedId = id
    detailOpen = true
    await tick()
    document.querySelector<HTMLInputElement>('.detail .title')?.focus()
  }

  // shows a task wherever it is: in the current scope when it is there, else
  // in its list.
  function showTask(id: number): void {
    const task = $tasks.find((x) => x.id === id)
    if (!task) {
      void loadTasks().then(() => {
        if ($tasks.some((x) => x.id === id)) {
          showTask(id)
        }
      })
      return
    }
    if (!tasksInScope([task], scope, now).length) {
      scope = { kind: 'list', id: task.listId }
    }
    filter = ''
    void openDetail(id)
  }

  $: if ($taskFocus !== null) {
    const id = $taskFocus
    taskFocus.set(null)
    showTask(id)
  }

  // --- making tasks -----------------------------------------------------------------

  function newTask(): void {
    if (!canAdd) {
      scope = { kind: 'smart', key: 'today' }
    }
    void tick().then(() => listPane?.focusEntry())
  }

  function describeEntry(text: string, parsers: ChronoLike[], contact: { name: string; email: string } | null): string {
    const parts: string[] = []
    if (text.trim() && parsers.length) {
      const r = parseTaskPhrase(text, { now: new Date(), lang: $locale, chrono: parsers, defaultDueMinutes: $taskSettings.defaultDueMinutes })
      if (r.due) {
        const d = parseEventTime(r.due, !dueHasTime(r.due))
        parts.push($t('tasks.preview.due').replace('{when}', dueHasTime(r.due) ? d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : d.toLocaleDateString(undefined, { dateStyle: 'medium' })))
      }
      if (r.rrule) {
        parts.push(describeRRule(r.rrule, r.due ? parseEventTime(r.due, !dueHasTime(r.due)) : new Date(), $t, navigator.language))
      }
    }
    if (contact) {
      parts.push($t('tasks.preview.for').replace('{name}', contact.name || contact.email))
    }
    return parts.join(', ')
  }

  async function quickAdd(text: string): Promise<void> {
    const parsers = chrono.length ? chrono : await loadChrono($locale)
    const r = parseTaskPhrase(text, { now: new Date(), lang: $locale, chrono: parsers, defaultDueMinutes: $taskSettings.defaultDueMinutes })
    const draft = { ...emptyDraft(targetList), title: r.title, due: r.due, rrule: r.rrule, timeZone: localZone() }
    if (!draft.due && scope.kind === 'smart' && scope.key === 'today') {
      draft.due = dayKey(new Date())
    }
    if (r.reminderMinutes !== null && draft.due) {
      draft.reminders = [{ minutesBefore: r.reminderMinutes, relatedEnd: true, absolute: '', action: '' }]
    }
    if (contactForNew) {
      draft.contacts = [contactForNew]
      contactForNew = null
    }
    const saved = await saveTask(draft)
    if (saved) {
      selectedId = saved.task.id
    }
  }

  async function addSubtask(parent: Task, text: string): Promise<void> {
    await saveTask({ ...emptyDraft(parent.listId), title: text, parent: parent.uid, timeZone: localZone() })
  }

  $: if ($taskSeed) {
    const ids = $taskSeed
    taskSeed.set(null)
    void createTasksFromMail(ids)
  }

  $: if ($taskForContact) {
    contactForNew = $taskForContact
    taskForContact.set(null)
    newTask()
  }

  // --- changing tasks ----------------------------------------------------------------

  async function removeSelected(): Promise<void> {
    const task = selected
    if (!task || task.readOnly) {
      return
    }
    const index = rows.findIndex((r) => r.task.id === task.id)
    const next = rows[index + 1]?.task ?? rows[index - 1]?.task ?? null
    if ((await removeTask(task)) !== null) {
      selectedId = next?.id ?? null
      detailOpen = detailOpen && next !== null
      await tick()
      if (next) {
        document.querySelector<HTMLElement>(`[data-task-row="${next.id}"]`)?.focus()
      }
    }
  }

  // space on a row: in a smart list the task leaves the list once it changes,
  // so the selection and the keyboard move on to the row after it.
  async function toggleSelected(task: Task): Promise<void> {
    const index = rows.findIndex((r) => r.task.id === task.id)
    const next = rows[index + 1]?.task ?? rows[index - 1]?.task ?? null
    await toggleDone(task)
    if (!rows.some((r) => r.task.id === task.id) && next) {
      selectedId = next.id
      await tick()
      document.querySelector<HTMLElement>(`[data-task-row="${next.id}"]`)?.focus()
    } else {
      document.querySelector<HTMLElement>(`[data-task-row="${task.id}"]`)?.focus()
    }
  }

  async function remove(task: Task): Promise<void> {
    if ((await removeTask(task)) !== null && selectedId === task.id) {
      selectedId = null
      detailOpen = false
    }
  }

  async function reorder(ids: number[]): Promise<void> {
    if (scope.kind !== 'list') {
      return
    }
    const listId = scope.id
    const inList = ids.filter((id) => $tasks.find((x) => x.id === id)?.listId === listId)
    tasks.update((list) => list.map((x) => (inList.includes(x.id) ? { ...x, sortOrder: (inList.indexOf(x.id) + 1) * 10 } : x)))
    try {
      await reorderTasks(inList)
    } catch (err) {
      toastError(errorMessage(err))
    }
    await loadTasks()
  }

  async function dropOn(task: Task, target: string): Promise<void> {
    const [kind, value] = target.split(':')
    if (kind === 'list') {
      const listId = Number(value)
      if (listId !== task.listId) {
        await saveTask({ ...draftOf(task), listId })
      }
      return
    }
    switch (value) {
      case 'today':
        await moveTaskDue(task, task.due ? moveDue(task.due, new Date()) : dayKey(new Date()))
        break
      case 'nodate':
        await moveTaskDue(task, '')
        break
      case 'completed':
        await toggleDone(task, true)
        break
    }
  }

  function rowMenu(task: Task, x: number, y: number): void {
    selectedId = task.id
    const entries = [
      { label: $t('tasks.menu.open'), action: () => void openDetail(task.id) },
      { label: task.done ? $t('tasks.menu.reopen') : $t('tasks.menu.done'), action: () => void toggleDone(task) },
    ]
    if (!task.readOnly) {
      entries.push(
        { label: $t('tasks.menu.today'), action: () => void dropOn(task, 'smart:today') },
        { label: $t('tasks.toEvent'), action: () => void convertTaskToEvent(task) },
      )
    }
    openContextMenu(x, y, [...entries, ...(task.readOnly ? [] : ['separator' as const, { label: $t('tasks.delete'), action: () => void remove(task), danger: true }])])
  }

  async function openEvent(uid: string): Promise<void> {
    try {
      const ref = await findEventByUID(uid)
      if (!ref.found) {
        toastError($t('tasks.eventGone'))
        return
      }
      calendarFocus.set(parseEventTime(ref.start, ref.allDay))
      openSpace('calendar')
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function sync(): Promise<void> {
    await refreshCalendars()
    await loadTasks()
  }

  $: sortItems = (['manual', 'due', 'priority', 'title', 'created'] as TaskSort[]).map((key) => ({ value: key, label: $t(`tasks.sort.${key}`) }))

  // --- keys -------------------------------------------------------------------------

  function onKeydown(event: KeyboardEvent): void {
    if (event.metaKey || event.ctrlKey || event.altKey || popupIsOpen() || $taskConflict) {
      return
    }
    if (event.key === 'Escape' && detailOpen && !isEditableTarget(event.target)) {
      event.preventDefault()
      detailOpen = false
      return
    }
    if (isEditableTarget(event.target) || (event.target as HTMLElement).closest('.detail')) {
      return
    }
    const run: Record<string, () => void> = {
      n: newTask,
      ' ': () => selected && !selected.readOnly && void toggleSelected(selected),
      Enter: () => selected && void openDetail(selected.id),
      Delete: () => void removeSelected(),
      Backspace: () => void removeSelected(),
    }
    const action = run[event.key]
    if (action) {
      event.preventDefault()
      event.stopPropagation()
      action()
    }
  }

  $: syncing = $calendarSync
  $: syncValue = syncing && syncing.total > 0 ? syncing.done / syncing.total : null
  $: rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl'
  $: showDetail = selected !== null && detailOpen
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<section class="screen" aria-label={$t('space.tasks')} bind:clientWidth={width} on:keydown={onKeydown}>
  {#if !foldSide || sideOpen}
    <div class="side" class:drawer={foldSide} transition:fly={{ x: foldSide ? (rtl ? 24 : -24) : 0, duration: dur(foldSide ? motion.base : 0) }}>
      <TaskSidebar {scope} on:scope={(e) => setScope(e.detail)} />
    </div>
  {/if}
  {#if foldSide && sideOpen}
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="scrim" transition:fade={{ duration: dur(motion.fast) }} on:click={() => (sideOpen = false)}></div>
  {/if}

  <div class="main">
    <header class="bar">
      {#if foldSide}
        <button type="button" class="cal-icon" aria-label={$t('tasks.sidebar')} aria-expanded={sideOpen} on:click={() => (sideOpen = !sideOpen)}>
          <Ph svg={sidebarIcon} size={16} />
        </button>
      {/if}
      <h2 class="title">{title}</h2>
      <span class="spacer"></span>
      <input
        class="cal-field filter"
        type="search"
        bind:this={filterInput}
        bind:value={filter}
        placeholder={$t('tasks.filter')}
        aria-label={$t('tasks.filter')}
        on:keydown={(e) => e.key === 'Escape' && filter && (e.stopPropagation(), (filter = ''))}
      />
      <Select class="sort" value={sort} items={sortItems} ariaLabel={$t('tasks.sortLabel')} on:change={(e) => void saveTaskSettings({ sort: e.detail as TaskSort })} />
      {#if $calendarSyncError && !syncing}
        <span class="sync-error" title={$calendarSyncError}><Ph svg={warningIcon} size={15} /></span>
      {/if}
      <button type="button" class="cal-icon" aria-label={$t('calendar.sync')} disabled={syncing !== null} on:click={() => void sync()}>
        {#if syncing}
          <PieProgress value={syncValue} label={$t('calendar.syncing')} />
        {:else}
          <Ph svg={syncIcon} size={16} />
        {/if}
      </button>
    </header>

    <TaskListPane
      bind:this={listPane}
      {rows}
      lists={$taskLists}
      {selectedId}
      showList={scope.kind === 'smart' && $taskLists.length > 1}
      {canReorder}
      {canAdd}
      {timeFormat}
      {now}
      {emptyText}
      {preview}
      entryPlaceholder={$t('tasks.entryPlaceholder')}
      on:select={(e) => select(e.detail)}
      on:open={(e) => void openDetail(e.detail)}
      on:toggle={(e) => void toggleDone(e.detail)}
      on:quickAdd={(e) => void quickAdd(e.detail)}
      on:entry={(e) => (entryText = e.detail)}
      on:reorder={(e) => void reorder(e.detail)}
      on:drop={(e) => void dropOn(e.detail.task, e.detail.target)}
      on:menu={(e) => rowMenu(e.detail.task, e.detail.x, e.detail.y)}
    />
  </div>

  {#if showDetail && selected}
    <div class="detail-pane" class:overlay={foldDetail} transition:fly={{ x: foldDetail ? (rtl ? -24 : 24) : 0, duration: dur(foldDetail ? motion.base : 0) }}>
      <TaskDetail
        task={selected}
        lists={$taskLists}
        {subtasks}
        {weekStart}
        {timeFormat}
        defaultDueMinutes={$taskSettings.defaultDueMinutes}
        on:close={() => (detailOpen = false)}
        on:remove={(e) => void remove(e.detail)}
        on:open={(e) => void openDetail(e.detail)}
        on:addSubtask={(e) => selected && void addSubtask(selected, e.detail)}
        on:toEvent={(e) => void convertTaskToEvent(e.detail)}
        on:openEvent={(e) => void openEvent(e.detail)}
      />
    </div>
  {/if}
</section>

{#if $taskConflict}
  <Modal title={$t('tasks.conflict.title')} size="small" on:close={() => taskConflict.set(null)}>
    <p class="conflict">{$t('tasks.conflict.body').replace('{title}', $taskConflict.server.title || $taskConflict.task.title || $t('tasks.untitled'))}</p>
    <svelte:fragment slot="footer">
      <div class="conflict-actions">
        <button type="button" class="cal-btn" on:click={() => void resolveConflict(false)}>{$t('tasks.conflict.theirs')}</button>
        <button type="button" class="cal-btn primary" on:click={() => void resolveConflict(true)}>{$t('tasks.conflict.mine')}</button>
      </div>
    </svelte:fragment>
  </Modal>
{/if}

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

  .filter {
    width: calc(var(--cal-sidebar-width) * 0.75);
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .sync-error {
    display: inline-flex;
    color: var(--warning);
  }

  .detail-pane {
    flex-shrink: 0;
    width: var(--task-detail-width);
    height: 100%;
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  .detail-pane.overlay {
    position: absolute;
    inset-block: 0;
    inset-inline-end: 0;
    z-index: 18;
    max-width: 100%;
    box-shadow: var(--shadow-overlay);
  }

  .conflict {
    margin: 0;
    font-size: var(--fz-list);
    color: var(--text-secondary);
  }

  .conflict-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    width: 100%;
  }
</style>
