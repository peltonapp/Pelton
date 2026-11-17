<script lang="ts">
  // a project's board: its columns side by side, each with its cards and a
  // line to add one at the bottom. the board scrolls sideways rather than
  // squeezing columns in a narrow window.
  //
  // cards move by pointer: picked up, a card leans like dragged mail and
  // follows the pointer while the cards of the column under it part to show
  // where it will land; let go, it settles into that gap. by keyboard the
  // arrows move between cards and Alt with an arrow carries the focused card.
  // a card opens beside the board with its task's details and what is
  // attached to it. a filter and a sort narrow and order the columns; while
  // they do, a card dropped in a column keeps the column's own order.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import { flip } from 'svelte/animate'
  import { fade, fly } from 'svelte/transition'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/regular/dots-three.svg?raw'
  import gripIcon from '@phosphor-icons/core/assets/duotone/dots-six-vertical-duotone.svg?raw'
  import doneIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import columnsIcon from '@phosphor-icons/core/assets/duotone/columns-plus-right-duotone.svg?raw'
  import taskIcon from '@phosphor-icons/core/assets/duotone/list-checks-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import BoardCard from './BoardCard.svelte'
  import CardPanel from './CardPanel.svelte'
  import ColumnDialog from './ColumnDialog.svelte'
  import {
    boardLayout,
    boardSorts,
    columnCards,
    dropIndex,
    dueFilters,
    emptyFilter,
    filterActive,
    focusTarget,
    keyboardMove,
    projectColor,
    wipState,
    type BoardFilter,
    type BoardKey,
    type BoardSort,
  } from '../../lib/projectlogic'
  import { addProjectColumn, reorderProjectColumns } from '../../lib/api'
  import { reorder as reorderAction, reorderHandleAttr, type ReorderDetail } from '../../lib/reorder'
  import { parseTaskPhrase } from '../../lib/taskphrase'
  import { loadChrono } from '../../lib/chronoload'
  import { convertTaskToEvent } from '../../lib/taskevent'
  import { openTask } from '../../lib/taskmail'
  import { portal } from '../../lib/portal'
  import { currentUIScale } from '../../theme/theme'
  import { addCard, addTask, board, cardFocus, loadBoard, moveBoardCard, openCardUid, removeCard } from '../../stores/projects'
  import { emptyDraft, localZone, removeTask, saveTask, taskLists, taskSettings, tasks, toggleDone } from '../../stores/tasks'
  import { openContextMenu } from '../../stores/contextmenu'
  import { menuHint } from '../../stores/shortcuts'
  import { errorMessage, toastError } from '../../stores/toast'
  import { popupIsOpen } from '../../lib/popups'
  import { dur, motion } from '../../lib/motion'
  import { locale, t } from '../../lib/i18n'
  import { isEditableTarget } from '../../lib/editable'
  import type { TimeFormat } from '../../lib/format'
  import type { Project, ProjectCard, ProjectColumn } from '../../lib/types'

  /** The project whose board this is. */
  export let project: Project
  /** Its columns in order. */
  export let columns: ProjectColumn[]
  /** Its cards. */
  export let cards: ProjectCard[]
  /** The current minute, for deadlines. */
  export let now: Date
  export let timeFormat: TimeFormat = 'auto'
  export let weekStart = 1

  const dispatch = createEventDispatcher<{ openNote: string }>()

  const narrowPanel = 900
  const dragStart = 5
  const edgeScroll = 48

  let width = 0
  let filter: BoardFilter = { ...emptyFilter }
  let sort: BoardSort = 'manual'
  let selectedUid: string | null = null
  let focusUid: string | null = null
  let addingIn: number | null = null
  let addText = ''
  let addInput: HTMLInputElement
  let addingColumn = false
  let newColumn = ''
  let editing: ProjectColumn | null = null
  let picking = false
  let pickButton: HTMLButtonElement
  let pickQuery = ''
  let announce = ''
  let columnsEl: HTMLElement
  let filterInput: HTMLInputElement
  let chrono: Awaited<ReturnType<typeof loadChrono>> = []

  $: color = projectColor(project)
  $: layout = boardLayout(columns, cards, filter, sort, now)
  $: columnIds = columns.map((c) => c.id)
  $: manual = sort === 'manual'
  $: narrowing = filterActive(filter)
  $: selected = cards.find((c) => c.task.uid === selectedUid) ?? null
  $: openCardUid.set(selected?.task.uid ?? null)
  $: overlay = width > 0 && width < narrowPanel
  $: rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl'
  $: dropGone(cards)

  // a card that left the board, deleted or moved elsewhere, closes its panel.
  function dropGone(list: ProjectCard[]): void {
    if (selectedUid && !list.some((c) => c.task.uid === selectedUid)) {
      selectedUid = null
    }
  }
  $: firstUid = columnIds.map((id) => layout.get(id)?.[0]?.task.uid).find(Boolean) ?? null
  $: if (!focusUid || !cards.some((c) => c.task.uid === focusUid)) {
    focusUid = firstUid
  }
  $: people = [
    ...new Map(
      [...project.people, ...cards.flatMap((c) => c.task.contacts)]
        .filter((p) => p.email)
        .map((p) => [p.email.toLowerCase(), p.name || p.email] as const),
    ).entries(),
  ].sort((a, b) => a[1].localeCompare(b[1]))
  $: personItems = [{ value: '', label: $t('projects.board.anyone') }, ...people.map(([email, name]) => ({ value: email, label: name }))]
  $: dueItems = dueFilters.map((d) => ({ value: d, label: $t(`projects.board.due.${d}`) }))
  $: sortItems = boardSorts.map((s) => ({ value: s, label: $t(`projects.board.sort.${s}`) }))
  $: onBoard = new Set(cards.map((c) => c.task.uid))
  $: candidates = $tasks
    .filter((x) => !x.done && !x.parent && !onBoard.has(x.uid))
    .filter((x) => !pickQuery.trim() || x.title.toLocaleLowerCase().includes(pickQuery.trim().toLocaleLowerCase()))
    .slice(0, 40)

  $: if ($cardFocus && cards.some((c) => c.task.uid === $cardFocus)) {
    const uid = $cardFocus
    cardFocus.set(null)
    void openCard(uid)
  }

  // --- opening and focus -----------------------------------------------------

  async function openCard(uid: string): Promise<void> {
    selectedUid = uid
    focusUid = uid
    await tick()
    cardEl(uid)?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }

  function cardEl(uid: string): HTMLElement | null {
    return columnsEl?.querySelector<HTMLElement>(`[data-card-uid="${CSS.escape(uid)}"]`) ?? null
  }

  async function focusCard(uid: string): Promise<void> {
    focusUid = uid
    await tick()
    const el = cardEl(uid)
    el?.focus()
    el?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }

  /** Opens the line to add a card, in the focused card's column or the first. */
  export function startAdd(): void {
    const col = cards.find((c) => c.task.uid === focusUid)?.columnId ?? columns.find((c) => !c.done)?.id ?? columns[0]?.id
    if (col !== undefined) {
      void openAdd(col)
    }
  }

  /** Takes the open card, or the focused one, off the board. */
  export function removeSelected(): void {
    const card = cards.find((c) => c.task.uid === (selectedUid ?? focusUid))
    if (card) {
      void removeCard(card)
    }
  }

  /** Puts the cursor in the board's filter. */
  export function focusFilter(): void {
    filterInput?.focus()
  }

  // --- adding ------------------------------------------------------------------

  async function openAdd(columnId: number): Promise<void> {
    addingIn = columnId
    addText = ''
    if (!chrono.length) {
      void loadChrono($locale).then((c) => (chrono = c))
    }
    await tick()
    addInput?.focus()
  }

  async function submitAdd(): Promise<void> {
    const text = addText.trim()
    if (!text || addingIn === null) {
      return
    }
    const columnId = addingIn
    addText = ''
    const parsed = parseTaskPhrase(text, { now: new Date(), lang: $locale, chrono, defaultDueMinutes: $taskSettings.defaultDueMinutes })
    const card = await addCard({
      projectId: project.id,
      columnId,
      title: parsed.title,
      notes: '',
      due: parsed.due,
      timeZone: localZone(),
      priority: 0,
      index: -1,
    })
    if (card) {
      focusUid = card.task.uid
      announce = $t('projects.board.added').replace('{title}', card.task.title)
    }
  }

  function onAddKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      addingIn = null
    }
  }

  async function submitColumn(): Promise<void> {
    const name = newColumn.trim()
    if (!name) {
      addingColumn = false
      return
    }
    newColumn = ''
    try {
      await addProjectColumn(project.id, name)
      await loadBoard(project.id)
      await tick()
      columnsEl?.scrollTo({ left: columnsEl.scrollWidth, behavior: dur(motion.base) ? 'smooth' : 'auto' })
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onColumnsReorder(event: CustomEvent<ReorderDetail>): Promise<void> {
    const ids = event.detail.ids.map(Number)
    board.update((b) => (b ? { ...b, columns: ids.map((id) => b.columns.find((c) => c.id === id)).filter((c): c is ProjectColumn => !!c) } : b))
    try {
      await reorderProjectColumns(project.id, ids)
    } catch (err) {
      toastError(errorMessage(err))
      await loadBoard(project.id)
    }
  }

  async function addExisting(uid: string): Promise<void> {
    picking = false
    const card = await addTask(project.id, uid)
    if (card) {
      void openCard(card.task.uid)
    }
  }

  // --- menus -------------------------------------------------------------------

  function cardMenu(card: ProjectCard, x: number, y: number): void {
    const task = card.task
    openContextMenu(x, y, [
      { label: $t('projects.card.open'), action: () => void openCard(task.uid) },
      ...(task.readOnly ? [] : [{ label: task.done ? $t('tasks.menu.reopen') : $t('tasks.menu.done'), action: () => void toggleDone(task) }]),
      ...columns
        .filter((c) => c.id !== card.columnId)
        .map((c) => ({ label: $t('projects.card.moveTo').replace('{name}', c.name), action: () => void moveTo(card, c.id, Number.MAX_SAFE_INTEGER) })),
      'separator' as const,
      { label: $t('projects.card.openInTasks'), action: () => openTask(task.id) },
      { label: $t('projects.card.remove'), action: () => void removeCard(card), hint: menuHint('delete-message') },
      ...(task.readOnly ? [] : [{ label: $t('tasks.delete'), danger: true, action: () => void removeTask(task) }]),
    ])
  }

  async function moveTo(card: ProjectCard, columnId: number, index: number): Promise<void> {
    await moveBoardCard(card.task.uid, columnId, index)
    const col = columns.find((c) => c.id === columnId)
    announce = $t('projects.board.moved')
      .replace('{title}', card.task.title || $t('tasks.untitled'))
      .replace('{column}', col?.name ?? '')
      .replace('{n}', String(Math.min(index, columnCards(cards, columnId).length - 1) + 1))
  }

  // --- keyboard ----------------------------------------------------------------

  const arrows = ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight']

  function onKeydown(event: KeyboardEvent): void {
    if (isEditableTarget(event.target) || popupIsOpen() || event.metaKey || event.ctrlKey) {
      return
    }
    const el = (event.target as HTMLElement).closest<HTMLElement>('[data-card-uid]')
    const uid = el?.dataset.cardUid
    if (!uid) {
      return
    }
    const card = cards.find((c) => c.task.uid === uid)
    if (!card) {
      return
    }
    if (event.altKey && arrows.includes(event.key)) {
      event.preventDefault()
      event.stopPropagation()
      const vertical = event.key === 'ArrowUp' || event.key === 'ArrowDown'
      if (vertical && (!manual || narrowing)) {
        announce = $t('projects.board.orderLocked')
        return
      }
      const target = keyboardMove(columnIds, cards, uid, event.key as 'ArrowUp', rtl)
      if (target) {
        void moveTo(card, target.columnId, manual ? target.index : Number.MAX_SAFE_INTEGER).then(() => focusCard(uid))
      }
      return
    }
    if (event.altKey || event.shiftKey) {
      return
    }
    if ([...arrows, 'Home', 'End'].includes(event.key)) {
      event.preventDefault()
      event.stopPropagation()
      const next = focusTarget(columnIds, layout, uid, event.key as BoardKey, rtl)
      if (next) {
        void focusCard(next)
      }
      return
    }
    const run: Record<string, () => void> = {
      Enter: () => void openCard(uid),
      ' ': () => !card.task.readOnly && void toggleDone(card.task),
      n: () => void openAdd(card.columnId),
      ...(selectedUid ? { Escape: () => (selectedUid = null) } : {}),
    }
    const action = run[event.key]
    if (action) {
      event.preventDefault()
      event.stopPropagation()
      action()
    }
  }

  // --- dragging ----------------------------------------------------------------

  interface Drag {
    uid: string
    card: ProjectCard
    startX: number
    startY: number
    x: number
    y: number
    offX: number
    offY: number
    width: number
    height: number
    started: boolean
    columnId: number | null
    index: number
    settle: { x: number; y: number } | null
  }

  let drag: Drag | null = null
  let scrollFrame = 0
  let settleTimer: ReturnType<typeof setTimeout> | null = null

  type Slot = { key: string; card: ProjectCard | null }

  $: display = slotsFor(layout, drag)

  function slotsFor(view: Map<number, ProjectCard[]>, d: Drag | null): Map<number, Slot[]> {
    const out = new Map<number, Slot[]>()
    for (const [col, list] of view) {
      const slots: Slot[] = list.filter((c) => !(d?.started && c.task.uid === d.uid)).map((c) => ({ key: c.task.uid, card: c }))
      if (d?.started && d.columnId === col) {
        slots.splice(Math.min(d.index, slots.length), 0, { key: '__drop', card: null })
      }
      out.set(col, slots)
    }
    return out
  }

  function onPointerDown(event: PointerEvent): void {
    if (event.button !== 0 || event.pointerType === 'touch' || drag) {
      return
    }
    const target = event.target as HTMLElement
    if (target.closest('button, input, textarea, a, [role="checkbox"], [data-reorder-handle]')) {
      return
    }
    const el = target.closest<HTMLElement>('[data-card-uid]')
    const uid = el?.dataset.cardUid
    const card = cards.find((c) => c.task.uid === uid)
    if (!el || !uid || !card) {
      return
    }
    const rect = el.getBoundingClientRect()
    drag = {
      uid,
      card,
      startX: event.clientX,
      startY: event.clientY,
      x: event.clientX,
      y: event.clientY,
      offX: event.clientX - rect.left,
      offY: event.clientY - rect.top,
      width: rect.width,
      height: el.offsetHeight,
      started: false,
      columnId: card.columnId,
      index: 0,
      settle: null,
    }
    window.addEventListener('pointermove', onPointerMove, true)
    window.addEventListener('pointerup', onPointerUp, true)
    window.addEventListener('keydown', onDragKey, true)
  }

  function onPointerMove(event: PointerEvent): void {
    if (!drag || drag.settle) {
      return
    }
    const x = event.clientX
    const y = event.clientY
    if (!drag.started) {
      if (Math.hypot(x - drag.startX, y - drag.startY) < dragStart) {
        return
      }
      drag.started = true
      document.body.classList.add('card-dragging')
      window.getSelection()?.removeAllRanges()
      scrollFrame = requestAnimationFrame(autoScroll)
    }
    drag.x = x
    drag.y = y
    place(x, y)
    drag = drag
  }

  // finds the column under the pointer, the nearest one when it is between
  // columns, and where in it the card would land.
  function place(x: number, y: number): void {
    if (!drag || !columnsEl) {
      return
    }
    const sections = [...columnsEl.querySelectorAll<HTMLElement>('[data-column-id]')]
    let best: HTMLElement | null = null
    let distance = Number.POSITIVE_INFINITY
    for (const s of sections) {
      const r = s.getBoundingClientRect()
      const d = x < r.left ? r.left - x : x > r.right ? x - r.right : 0
      if (d < distance) {
        distance = d
        best = s
      }
    }
    if (!best) {
      return
    }
    const columnId = Number(best.dataset.columnId)
    const middles = [...best.querySelectorAll<HTMLElement>('[data-card-uid]')]
      .filter((el) => el.dataset.cardUid !== drag?.uid && !el.closest('.float'))
      .map((el) => {
        const r = el.getBoundingClientRect()
        return r.top + r.height / 2
      })
    drag.columnId = columnId
    drag.index = manual && !narrowing ? dropIndex(middles, y) : middles.length
  }

  function autoScroll(): void {
    if (!drag?.started || drag.settle || !columnsEl) {
      return
    }
    const r = columnsEl.getBoundingClientRect()
    let dx = 0
    if (drag.x < r.left + edgeScroll) {
      dx = -Math.ceil((r.left + edgeScroll - drag.x) / 4)
    } else if (drag.x > r.right - edgeScroll) {
      dx = Math.ceil((drag.x - (r.right - edgeScroll)) / 4)
    }
    if (dx) {
      columnsEl.scrollLeft += dx
    }
    const list = drag.columnId !== null ? columnsEl.querySelector<HTMLElement>(`[data-column-id="${drag.columnId}"] .cards`) : null
    if (list) {
      const lr = list.getBoundingClientRect()
      if (drag.y < lr.top + edgeScroll) {
        list.scrollTop -= Math.ceil((lr.top + edgeScroll - drag.y) / 4)
      } else if (drag.y > lr.bottom - edgeScroll) {
        list.scrollTop += Math.ceil((drag.y - (lr.bottom - edgeScroll)) / 4)
      }
    }
    if (dx) {
      place(drag.x, drag.y)
      drag = drag
    }
    scrollFrame = requestAnimationFrame(autoScroll)
  }

  // the index in the column's own order that a drop at a place in the shown,
  // possibly filtered list stands for.
  function manualIndex(columnId: number, shownIndex: number, uid: string): number {
    if (!manual) {
      return Number.MAX_SAFE_INTEGER
    }
    const shown = (layout.get(columnId) ?? []).filter((c) => c.task.uid !== uid)
    const full = columnCards(cards, columnId).filter((c) => c.task.uid !== uid)
    if (shownIndex < shown.length) {
      return full.findIndex((c) => c.task.uid === shown[shownIndex].task.uid)
    }
    if (shown.length > 0) {
      return full.findIndex((c) => c.task.uid === shown[shown.length - 1].task.uid) + 1
    }
    return full.length
  }

  function stopListening(): void {
    window.removeEventListener('pointermove', onPointerMove, true)
    window.removeEventListener('pointerup', onPointerUp, true)
    window.removeEventListener('keydown', onDragKey, true)
    cancelAnimationFrame(scrollFrame)
    document.body.classList.remove('card-dragging')
  }

  function onPointerUp(): void {
    const d = drag
    stopListening()
    if (!d || !d.started) {
      drag = null
      return
    }
    window.addEventListener('click', swallowClick, { capture: true, once: true })
    setTimeout(() => window.removeEventListener('click', swallowClick, true), 0)
    const slot = columnsEl?.querySelector<HTMLElement>('[data-drop-slot]')
    const columnId = d.columnId
    if (columnId === null) {
      drag = null
      return
    }
    const index = manualIndex(columnId, d.index, d.uid)
    const commit = (): void => {
      settleTimer = null
      drag = null
      void moveTo(d.card, columnId, index).then(() => focusCard(d.uid))
    }
    const time = dur(motion.base)
    if (slot && time > 0) {
      const r = slot.getBoundingClientRect()
      d.settle = { x: r.left, y: r.top }
      drag = d
      settleTimer = setTimeout(commit, time)
    } else {
      commit()
    }
  }

  function onDragKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      stopListening()
      drag = null
    }
  }

  function swallowClick(event: MouseEvent): void {
    event.stopPropagation()
    event.preventDefault()
  }

  onDestroy(() => {
    stopListening()
    if (settleTimer) {
      clearTimeout(settleTimer)
    }
    openCardUid.set(null)
  })

  $: scale = drag?.started ? currentUIScale() : 1
  $: floatLeft = drag ? (drag.settle ? drag.settle.x : drag.x - drag.offX) / scale : 0
  $: floatTop = drag ? (drag.settle ? drag.settle.y : drag.y - drag.offY) / scale : 0

  // --- the open card -----------------------------------------------------------

  function openSubtask(id: number): void {
    const task = $tasks.find((x) => x.id === id)
    const card = task ? cards.find((c) => c.task.uid === task.uid) : undefined
    if (card) {
      void openCard(card.task.uid)
    } else {
      openTask(id)
    }
  }

  async function addSubtask(card: ProjectCard, text: string): Promise<void> {
    await saveTask({ ...emptyDraft(card.task.listId), title: text, parent: card.task.uid, timeZone: localZone() })
  }

  async function deleteTask(card: ProjectCard): Promise<void> {
    if ((await removeTask(card.task)) !== null) {
      selectedUid = null
      void loadBoard(project.id)
    }
  }
</script>

<div class="wrap" bind:clientWidth={width}>
  <div class="main">
    <div class="tools">
      <input
        class="cal-field filter"
        type="search"
        bind:this={filterInput}
        bind:value={filter.text}
        placeholder={$t('projects.board.filter')}
        aria-label={$t('projects.board.filter')}
        on:keydown={(e) => e.key === 'Escape' && filter.text && (e.stopPropagation(), (filter.text = ''))}
      />
      <Select class="ctl" value={filter.due} items={dueItems} ariaLabel={$t('projects.board.dueLabel')} on:change={(e) => (filter = { ...filter, due: e.detail === 'overdue' || e.detail === 'week' || e.detail === 'none' ? e.detail : 'any' })} />
      {#if people.length > 0}
        <Select class="ctl" value={filter.person} items={personItems} ariaLabel={$t('projects.board.personLabel')} on:change={(e) => (filter = { ...filter, person: e.detail })} />
      {/if}
      <button type="button" class="cal-btn" class:on={filter.highPriority} aria-pressed={filter.highPriority} on:click={() => (filter = { ...filter, highPriority: !filter.highPriority })}>
        <Ph svg={flagIcon} size={13} />{$t('projects.board.high')}
      </button>
      {#if narrowing}
        <button type="button" class="link" transition:fade={{ duration: dur(motion.fast) }} on:click={() => (filter = { ...emptyFilter })}>{$t('projects.board.clear')}</button>
      {/if}
      <span class="spacer"></span>
      <Select class="ctl" value={sort} items={sortItems} ariaLabel={$t('projects.board.sortLabel')} on:change={(e) => (sort = boardSorts.find((s) => s === e.detail) ?? 'manual')} />
      <button type="button" class="cal-btn" bind:this={pickButton} aria-haspopup="dialog" aria-expanded={picking} on:click={() => ((pickQuery = ''), (picking = !picking))}>
        <Ph svg={taskIcon} size={13} />{$t('projects.board.addTask')}
      </button>
    </div>

    {#if columns.length === 0}
      <EmptyState
        icon={columnsIcon}
        title={$t('projects.board.noColumns')}
        detail={$t('projects.board.noColumnsDetail')}
        actionLabel={$t('projects.board.addColumn')}
        onAction={() => (addingColumn = true)}
      />
    {/if}

    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div class="columns" class:hidden={columns.length === 0 && !addingColumn} bind:this={columnsEl} on:pointerdown={onPointerDown} on:keydown={onKeydown}>
      <div class="column-row" use:reorderAction={{ axis: 'x' }} on:reorder={(e) => void onColumnsReorder(e)}>
        {#each columns as col (col.id)}
          {@const slots = display.get(col.id) ?? []}
          {@const total = cards.filter((c) => c.columnId === col.id).length}
          {@const wip = wipState(col, total)}
          <section class="column" class:done-col={col.done} data-column-id={col.id} data-reorder-id={col.id} aria-label={col.name} animate:flip={{ duration: dur(motion.base) }}>
            <header class="col-head">
              <span class="grip" title={$t('projects.board.dragColumn')} aria-hidden="true" {...{ [reorderHandleAttr]: '' }}><Ph svg={gripIcon} size={14} /></span>
              {#if col.done}
                <span class="done-mark" title={$t('projects.column.doneLabel')}><Ph svg={doneIcon} size={14} /></span>
              {/if}
              <h3 class="col-name" title={col.name}>{col.name}</h3>
              <span class="count {wip}" title={col.wipLimit ? $t('projects.column.limitShort').replace('{n}', String(col.wipLimit)) : ''}>
                {#if narrowing}{(layout.get(col.id) ?? []).length}/{/if}{total}{#if col.wipLimit} &middot; {col.wipLimit}{/if}
              </span>
              <span class="spacer"></span>
              <button type="button" class="cal-icon" aria-label={$t('projects.column.addCardTo').replace('{name}', col.name)} on:click={() => void openAdd(col.id)}>
                <Ph svg={plusIcon} size={13} />
              </button>
              <button type="button" class="cal-icon" aria-label={$t('projects.column.settingsOf').replace('{name}', col.name)} aria-haspopup="dialog" on:click={() => (editing = col)}>
                <Ph svg={moreIcon} size={14} />
              </button>
            </header>
            <ul class="cards" class:drop-target={drag?.started && drag.columnId === col.id} role="list" aria-label={col.name}>
              {#each slots as slot (slot.key)}
                <li animate:flip={{ duration: dur(motion.base) }} class:slot={!slot.card}>
                  {#if slot.card}
                    <BoardCard
                      card={slot.card}
                      {now}
                      {timeFormat}
                      {color}
                      focusable={focusUid === slot.card.task.uid}
                      selected={selectedUid === slot.card.task.uid}
                      on:open={() => slot.card && void openCard(slot.card.task.uid)}
                      on:menu={(e) => slot.card && cardMenu(slot.card, e.detail.x, e.detail.y)}
                    />
                  {:else}
                    <div class="drop" data-drop-slot style={`min-height:${drag?.height ?? 0}px`} in:fade={{ duration: dur(motion.fast) }}></div>
                  {/if}
                </li>
              {/each}
              {#if slots.length === 0}
                <li class="empty" aria-hidden="true">{narrowing ? $t('projects.board.noneMatch') : $t('projects.board.emptyColumn')}</li>
              {/if}
            </ul>
            <footer class="col-foot">
              {#if addingIn === col.id}
                <form class="quick" on:submit|preventDefault={() => void submitAdd()} in:fly={{ y: 4, duration: dur(motion.fast) }}>
                  <input
                    class="cal-field"
                    bind:this={addInput}
                    bind:value={addText}
                    placeholder={$t('projects.board.addPlaceholder')}
                    aria-label={$t('projects.column.addCardTo').replace('{name}', col.name)}
                    on:keydown={onAddKey}
                    on:blur={() => !addText.trim() && (addingIn = null)}
                  />
                </form>
              {:else}
                <button type="button" class="add-card" on:click={() => void openAdd(col.id)}>
                  <Ph svg={plusIcon} size={12} />{$t('projects.board.addCard')}
                </button>
              {/if}
            </footer>
          </section>
        {/each}
      </div>
      {#if columns.length > 0 || addingColumn}
        <div class="new-column">
          {#if addingColumn}
            <form on:submit|preventDefault={() => void submitColumn()}>
              <!-- svelte-ignore a11y-autofocus -->
              <input
                class="cal-field"
                bind:value={newColumn}
                placeholder={$t('projects.board.columnPlaceholder')}
                aria-label={$t('projects.board.addColumn')}
                autofocus
                on:keydown={(e) => e.key === 'Escape' && (e.stopPropagation(), (addingColumn = false))}
                on:blur={() => void submitColumn()}
              />
            </form>
          {:else}
            <button type="button" class="add-column" on:click={() => (addingColumn = true)}>
              <Ph svg={plusIcon} size={13} />{$t('projects.board.addColumn')}
            </button>
          {/if}
        </div>
      {/if}
    </div>
  </div>

  {#if selected}
    <aside class="panel" class:overlay transition:fly={{ x: overlay ? (rtl ? -24 : 24) : 0, duration: dur(overlay ? motion.base : 0) }}>
      <CardPanel
        card={selected}
        lists={$taskLists}
        {weekStart}
        {timeFormat}
        defaultDueMinutes={$taskSettings.defaultDueMinutes}
        on:close={() => (selectedUid = null)}
        on:removeCard={(e) => {
          selectedUid = null
          void removeCard(e.detail)
        }}
        on:deleteTask={(e) => void deleteTask(e.detail)}
        on:openNote={(e) => dispatch('openNote', e.detail)}
        on:openSubtask={(e) => openSubtask(e.detail)}
        on:addSubtask={(e) => selected && void addSubtask(selected, e.detail)}
        on:toEvent={(e) => void convertTaskToEvent(e.detail.task)}
      />
    </aside>
  {/if}
</div>

{#if drag?.started}
  <div
    class="float"
    class:settling={drag.settle !== null}
    style={`left:${floatLeft}px;top:${floatTop}px;width:${drag.width / scale}px`}
    aria-hidden="true"
    use:portal
  >
    <BoardCard card={drag.card} {now} {timeFormat} {color} />
  </div>
{/if}

{#if picking && pickButton}
  <AnchoredPopover anchor={pickButton} align="end" label={$t('projects.board.addTask')} on:close={() => (picking = false)}>
    <div class="picker">
      <!-- svelte-ignore a11y-autofocus -->
      <input class="cal-field" bind:value={pickQuery} placeholder={$t('projects.board.pickTask')} aria-label={$t('projects.board.pickTask')} autofocus />
      <div class="options">
        {#each candidates as task (task.id)}
          <button type="button" class="cal-item" on:click={() => void addExisting(task.uid)}>
            <span class="dot" style={`background:${task.listColor || 'var(--accent)'}`} aria-hidden="true"></span>
            <span class="text">{task.title || $t('tasks.untitled')}</span>
          </button>
        {:else}
          <EmptyState size="compact" title={$t('projects.board.noTasks')} />
        {/each}
      </div>
    </div>
  </AnchoredPopover>
{/if}

{#if editing}
  <ColumnDialog
    column={editing}
    {columns}
    count={cards.filter((c) => c.columnId === editing?.id).length}
    on:close={() => (editing = null)}
    on:changed={() => void loadBoard(project.id)}
  />
{/if}

<div class="sr" aria-live="polite">{announce}</div>

<style>
  .wrap {
    position: relative;
    display: flex;
    height: 100%;
    min-width: 0;
  }

  .main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }

  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .filter {
    width: calc(var(--cal-sidebar-width) * 0.75);
    min-width: calc(var(--cal-sidebar-width) * 0.45);
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .tools :global(.ctl) {
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .spacer {
    flex: 1;
  }

  .link {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .columns {
    display: flex;
    align-items: stretch;
    gap: var(--space-3);
    flex: 1;
    min-height: 0;
    padding: var(--space-4);
    overflow-x: auto;
    overflow-y: hidden;
  }

  .columns.hidden {
    display: none;
  }

  .column-row {
    display: flex;
    align-items: stretch;
    gap: var(--space-3);
  }

  .column {
    display: flex;
    flex-direction: column;
    flex: 0 0 var(--project-column-width);
    width: var(--project-column-width);
    min-height: 0;
    max-height: 100%;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
  }

  .col-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-1);
  }

  .grip {
    display: inline-flex;
    color: var(--text-tertiary);
    cursor: grab;
    opacity: 0;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .column:hover .grip {
    opacity: 1;
  }

  .done-mark {
    display: inline-flex;
    color: var(--success);
  }

  .col-name {
    min-width: 0;
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    flex-shrink: 0;
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .count.full {
    color: var(--warning);
    background: var(--warning-bg);
  }

  .count.over {
    color: var(--danger);
    background: var(--danger-bg);
  }

  .col-head .cal-icon {
    width: calc(var(--control-height) - 8px);
    height: calc(var(--control-height) - 8px);
    opacity: 0.7;
  }

  .col-head .cal-icon:hover {
    opacity: 1;
  }

  .cards {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    flex: 1;
    min-height: calc(var(--control-height) * 2);
    margin: 0;
    padding: var(--space-1) var(--space-2) var(--space-2);
    list-style: none;
    overflow-y: auto;
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .cards.drop-target {
    background: color-mix(in srgb, var(--accent) 4%, transparent);
  }

  .drop {
    border: var(--hairline) dashed var(--accent);
    border-radius: var(--radius-card);
    background: var(--selection-bg);
  }

  .empty {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: calc(var(--control-height) * 2);
    border: var(--hairline) dashed var(--border-default);
    border-radius: var(--radius-card);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .col-foot {
    padding: 0 var(--space-2) var(--space-2);
  }

  .quick .cal-field {
    width: 100%;
    font-size: var(--fz-label);
  }

  .add-card,
  .add-column {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    height: calc(var(--control-height) - 2px);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .add-card:hover,
  .add-column:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .new-column {
    flex: 0 0 var(--project-column-width);
  }

  .add-column {
    border: var(--hairline) dashed var(--border-default);
    height: var(--control-height);
    justify-content: center;
  }

  .new-column .cal-field {
    width: 100%;
  }

  .panel {
    flex-shrink: 0;
    width: var(--project-detail-width);
    height: 100%;
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  .panel.overlay {
    position: absolute;
    inset-block: 0;
    inset-inline-end: 0;
    z-index: 18;
    max-width: 100%;
    box-shadow: var(--shadow-overlay);
  }

  /* the card in hand: it lifts off at a slight lean, like dragged mail, and
     squares up as it settles into its gap. */
  .float {
    position: fixed;
    z-index: 950;
    pointer-events: none;
    transform: rotate(var(--project-drag-tilt)) scale(1.02);
    animation: lift var(--motion-base) var(--ease-out);
  }

  .float :global(.card) {
    border-color: var(--border-strong);
    box-shadow: var(--shadow-overlay);
  }

  .float.settling {
    transform: none;
    transition:
      left var(--motion-base) var(--ease-out),
      top var(--motion-base) var(--ease-out),
      transform var(--motion-base) var(--ease-out);
  }

  @keyframes lift {
    from {
      transform: rotate(0deg) scale(1);
    }
  }

  :global(body.card-dragging),
  :global(body.card-dragging *) {
    cursor: grabbing !important;
  }

  .picker {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    width: var(--cal-popover-width);
    padding: var(--space-3);
  }

  .options {
    display: flex;
    flex-direction: column;
    max-height: var(--snippet-list-height);
    overflow-y: auto;
  }

  .options .text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
  }


  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
</style>
