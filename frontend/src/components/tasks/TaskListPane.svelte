<script lang="ts">
  // the task list itself: the quick entry line on top, which reads natural
  // language, and the tasks below as compact rows with subtasks indented under
  // their parent. a row is dragged by its grip: onto another row to reorder,
  // onto a list or smart list in the sidebar to move it there.
  import { createEventDispatcher, tick } from 'svelte'
  import { flip } from 'svelte/animate'
  import { fade } from 'svelte/transition'
  import gripIcon from '@phosphor-icons/core/assets/regular/dots-six-vertical.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import envelopeIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import usersIcon from '@phosphor-icons/core/assets/duotone/users-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import TaskCheck from './TaskCheck.svelte'
  import { calendarColor } from '../../lib/calformat'
  import { isOverdue, type TaskRow } from '../../lib/tasklogic'
  import { dueLabel } from '../../lib/projectlogic'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Task, TaskList } from '../../lib/types'

  /** The rows to show, already ordered and nested. */
  export let rows: TaskRow[]
  export let lists: TaskList[]
  export let selectedId: number | null
  /** Shows each task's list, for the smart lists that mix lists. */
  export let showList: boolean = false
  /** Rows can be put in order by dragging. */
  export let canReorder: boolean = false
  /** Quick entry can make tasks here. */
  export let canAdd: boolean = true
  export let timeFormat: TimeFormat = 'auto'
  export let now: Date
  /** What the empty list says. */
  export let emptyText: string
  /** Placeholder of the quick entry line. */
  export let entryPlaceholder: string
  /** Describes what the line being typed will make, under it. */
  export let preview: string = ''

  const dispatch = createEventDispatcher<{
    select: number
    open: number
    toggle: Task
    quickAdd: string
    entry: string
    reorder: number[]
    drop: { task: Task; target: string }
    menu: { task: Task; x: number; y: number }
  }>()

  let entry = ''
  let entryInput: HTMLInputElement

  /** Puts the cursor in the quick entry line. */
  export function focusEntry(): void {
    entryInput?.focus()
  }

  $: dispatch('entry', entry)

  function submitEntry(): void {
    const text = entry.trim()
    if (text) {
      dispatch('quickAdd', text)
      entry = ''
    }
  }

  $: listById = new Map(lists.map((l) => [l.id, l]))

  function listName(task: Task): string {
    const list = listById.get(task.listId)
    return list?.name || $t('tasks.defaultList')
  }

  // --- dragging -----------------------------------------------------------------------

  let drag: { task: Task; x: number; y: number; startX: number; startY: number; moved: boolean } | null = null
  let overRow: { id: number; after: boolean } | null = null
  let overTarget: HTMLElement | null = null
  let scale = 1

  function onGripDown(event: PointerEvent, task: Task): void {
    if (event.button !== 0 || task.readOnly) {
      return
    }
    event.preventDefault()
    scale = Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
    drag = { task, x: event.clientX, y: event.clientY, startX: event.clientX, startY: event.clientY, moved: false }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('keydown', onDragKey, true)
  }

  function setTarget(el: HTMLElement | null): void {
    if (el !== overTarget) {
      overTarget?.classList.remove('task-drop-over')
      el?.classList.add('task-drop-over')
      overTarget = el
    }
  }

  function onMove(event: PointerEvent): void {
    if (!drag) {
      return
    }
    const moved = drag.moved || Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) > 4
    drag = { ...drag, x: event.clientX, y: event.clientY, moved }
    const under = document.elementFromPoint(event.clientX, event.clientY) as HTMLElement | null
    const row = under?.closest<HTMLElement>('[data-task-row]')
    const target = under?.closest<HTMLElement>('[data-task-drop]') ?? null
    setTarget(target)
    if (row && canReorder && !target) {
      const rect = row.getBoundingClientRect()
      overRow = { id: Number(row.dataset.taskRow), after: event.clientY > rect.top + rect.height / 2 }
    } else {
      overRow = null
    }
  }

  function endDrag(): void {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('keydown', onDragKey, true)
    setTarget(null)
    drag = null
    overRow = null
  }

  function onDragKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      endDrag()
    }
  }

  function onUp(): void {
    const current = drag
    const row = overRow
    const target = overTarget?.dataset.taskDrop ?? ''
    endDrag()
    if (!current?.moved) {
      return
    }
    if (target) {
      dispatch('drop', { task: current.task, target })
      return
    }
    if (row && row.id !== current.task.id) {
      const ids = rows.map((r) => r.task.id).filter((id) => id !== current.task.id)
      const at = ids.indexOf(row.id) + (row.after ? 1 : 0)
      ids.splice(at, 0, current.task.id)
      dispatch('reorder', ids)
    }
  }

  // --- keys --------------------------------------------------------------------------

  async function focusRow(id: number): Promise<void> {
    await tick()
    document.querySelector<HTMLElement>(`[data-task-row="${id}"]`)?.focus()
  }

  function onRowKey(event: KeyboardEvent, index: number): void {
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
    if (step !== 0) {
      event.preventDefault()
      const next = rows[Math.max(0, Math.min(rows.length - 1, index + step))]
      if (next) {
        dispatch('select', next.task.id)
        void focusRow(next.task.id)
      }
    }
  }

  function onEntryKey(event: KeyboardEvent): void {
    if (event.key === 'ArrowDown' && rows.length > 0) {
      event.preventDefault()
      dispatch('select', rows[0].task.id)
      void focusRow(rows[0].task.id)
    } else if (event.key === 'Escape' && entry) {
      event.preventDefault()
      event.stopPropagation()
      entry = ''
    }
  }
</script>

<div class="pane">
  {#if canAdd}
    <form class="entry" on:submit|preventDefault={submitEntry}>
      <Ph svg={plusIcon} size={14} />
      <input
        bind:this={entryInput}
        bind:value={entry}
        class="entry-field"
        placeholder={entryPlaceholder}
        aria-label={$t('tasks.quickEntry')}
        on:keydown={onEntryKey}
      />
    </form>
    {#if preview && entry.trim()}
      <p class="preview" transition:fade={{ duration: dur(motion.fast) }}>{preview}</p>
    {/if}
  {/if}

  {#if rows.length === 0}
    <p class="empty">{emptyText}</p>
  {:else}
    <ul class="rows" role="listbox" aria-label={$t('tasks.listLabel')}>
      {#each rows as row, i (row.task.id)}
        {@const task = row.task}
        {@const overdue = isOverdue(task, now)}
        <li
          class="row"
          class:selected={selectedId === task.id}
          class:done={task.done}
          class:dragging={drag?.moved && drag.task.id === task.id}
          class:drop-before={overRow?.id === task.id && !overRow.after}
          class:drop-after={overRow?.id === task.id && overRow.after}
          style={`--depth:${row.depth};--cal:${calendarColor(listById.get(task.listId) ?? { id: task.listId, color: task.listColor })}`}
          role="option"
          aria-selected={selectedId === task.id}
          tabindex={selectedId === task.id || (selectedId === null && i === 0) ? 0 : -1}
          data-task-row={task.id}
          animate:flip={{ duration: dur(motion.base) }}
          on:click={() => dispatch('select', task.id)}
          on:dblclick={() => dispatch('open', task.id)}
          on:keydown={(e) => onRowKey(e, i)}
          on:contextmenu|preventDefault={(e) => dispatch('menu', { task, x: e.clientX, y: e.clientY })}
        >
          {#if canReorder || lists.length > 1}
            <span class="grip" aria-hidden="true" on:pointerdown={(e) => onGripDown(e, task)}><Ph svg={gripIcon} size={14} /></span>
          {/if}
          <TaskCheck
            checked={task.done}
            label={$t('tasks.toggle').replace('{title}', task.title || $t('tasks.untitled'))}
            color={calendarColor(listById.get(task.listId) ?? { id: task.listId, color: task.listColor })}
            disabled={task.readOnly}
            on:toggle={() => dispatch('toggle', task)}
          />
          <span class="text">
            <span class="title">{task.title || $t('tasks.untitled')}</span>
            <span class="meta">
              {#if task.due}
                <span class="due" class:overdue>{dueLabel(task.due, now, $t, timeFormat)}</span>
              {/if}
              {#if task.rrule}
                <span class="mark" title={$t('tasks.repeats')}><Ph svg={repeatIcon} size={12} /></span>
              {/if}
              {#if task.reminders.length > 0}
                <span class="mark" title={$t('tasks.hasReminder')}><Ph svg={bellIcon} size={12} /></span>
              {/if}
              {#if row.children > 0}
                <span class="sub">{$t('tasks.subtaskCount').replace('{n}', String(row.children))}</span>
              {/if}
              {#if showList}
                <span class="list"><span class="dot" aria-hidden="true"></span>{listName(task)}</span>
              {/if}
            </span>
          </span>
          <span class="badges">
            {#if task.mailLinks.length > 0}
              <span class="mark" title={$t('tasks.fromMailNote')}><Ph svg={envelopeIcon} size={13} /></span>
            {/if}
            {#if task.contacts.length > 0}
              <span class="mark" title={task.contacts.map((c) => c.name || c.email).join(', ')}><Ph svg={usersIcon} size={13} /></span>
            {/if}
            {#if task.priority > 0 && task.priority < 5}
              <span class="mark high" title={$t('tasks.priority.high')}><Ph svg={flagIcon} size={13} /></span>
            {/if}
            {#if task.pending}
              <span class="pending" title={$t('tasks.pendingNote')} aria-hidden="true"></span>
            {/if}
          </span>
        </li>
      {/each}
    </ul>
  {/if}
</div>

{#if drag?.moved}
  <div class="ghost" style={`left:${drag.x / scale}px;top:${drag.y / scale}px`} aria-hidden="true">
    {drag.task.title || $t('tasks.untitled')}
  </div>
{/if}

<style>
  .pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .entry {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: var(--space-3) var(--space-4) var(--space-2);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .entry:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  .entry-field {
    flex: 1;
    min-width: 0;
    height: var(--control-height);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
  }

  .entry-field:focus {
    outline: none;
  }

  .entry-field::placeholder {
    color: var(--text-tertiary);
  }

  .preview {
    margin: 0 var(--space-4) var(--space-2);
    padding-inline: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .empty {
    margin: var(--space-6) var(--space-4);
    font-size: var(--fz-list);
    color: var(--text-tertiary);
    text-align: center;
  }

  .rows {
    flex: 1;
    min-height: 0;
    margin: 0;
    padding: 0 0 var(--space-4);
    list-style: none;
    overflow-y: auto;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) + var(--space-3));
    padding-block: var(--row-pad-y, var(--space-2));
    padding-inline: calc(var(--space-4) + var(--depth) * var(--task-indent)) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row.selected {
    background: var(--selection-bg);
  }

  .row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .row.dragging {
    opacity: 0.4;
  }

  .row.drop-before::before,
  .row.drop-after::after {
    content: '';
    position: absolute;
    inset-inline: var(--space-4);
    height: calc(var(--hairline) * 2);
    background: var(--accent);
  }

  .row.drop-before::before {
    top: calc(-1 * var(--hairline));
  }

  .row.drop-after::after {
    bottom: calc(-1 * var(--hairline));
  }

  .grip {
    display: inline-flex;
    margin-inline-start: calc(-1 * var(--space-4));
    color: var(--text-tertiary);
    opacity: 0;
    cursor: grab;
    touch-action: none;
  }

  .row:hover .grip {
    opacity: 1;
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
  }

  .title {
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    transition: color var(--motion-base) var(--ease-out);
  }

  .row.done .title {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .meta {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    min-width: 0;
  }

  .meta:empty {
    display: none;
  }

  .due {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .due.overdue {
    color: var(--danger);
  }

  .list {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
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
    background: var(--cal);
  }

  .mark {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .mark.high {
    color: var(--danger);
  }

  .badges {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .pending {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: var(--warning);
  }

  .ghost {
    position: fixed;
    z-index: 60;
    max-width: calc(var(--task-detail-width) - var(--space-6));
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fz-list);
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    pointer-events: none;
    transform: translate(var(--space-3), var(--space-3));
  }
</style>
