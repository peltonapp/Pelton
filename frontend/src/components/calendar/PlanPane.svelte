<script lang="ts">
  // the plan pane beside the day and week views: the open tasks, earliest due
  // first, each with how much of its time is planned. a task is dragged onto
  // the grid to block time for it; "Find time" and the task's menu do the same
  // from the keyboard. "Plan my day" lays today's due tasks out as proposals,
  // which are reviewed here and in the grid before anything is stored.
  import { createEventDispatcher } from 'svelte'
  import dotsIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import gripIcon from '@phosphor-icons/core/assets/regular/dots-six-vertical.svg?raw'
  import planIcon from '@phosphor-icons/core/assets/duotone/calendar-check-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import { armTaskDrag } from './taskdrag'
  import { formatMinutes, type Proposal } from './timeblocks'
  import { dueDate, isOverdue } from '../../lib/tasklogic'
  import { formatShortDay, formatTimeRange } from '../../lib/calformat'
  import { openContextMenu } from '../../stores/contextmenu'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Task, TaskPlan } from '../../lib/types'

  /** Every task; the pane lists the open ones. */
  export let tasks: Task[]
  /** How much each task has planned, by UID. */
  export let plans: Map<string, TaskPlan>
  /** The proposals of the day plan under review. */
  export let proposals: Proposal[]
  /** The titles a day plan could not fit, or fit only in part. */
  export let unplaced: string[] = []
  export let short: string[] = []
  /** How long a block lasts for a task without an estimate. */
  export let defaultMinutes: number
  export let timeFormat: TimeFormat = 'auto'
  /** The shortcut of "Plan my day", rendered, or empty. */
  export let planKey: string = ''
  /** A day plan is being made. */
  export let planning: boolean = false
  /** The current time. */
  export let now: Date

  const dispatch = createEventDispatcher<{
    findTime: Task
    blockNext: Task
    openTask: Task
    plan: void
    accept: string[]
    dismiss: string[]
  }>()

  const listLimit = 60

  $: open = tasks
    .filter((task) => !task.done)
    .sort((a, b) => {
      const da = dueDate(a)?.getTime() ?? Infinity
      const db = dueDate(b)?.getTime() ?? Infinity
      if (da !== db) {
        return da - db
      }
      return (a.priority || 10) - (b.priority || 10)
    })
    .slice(0, listLimit)

  /** How long a block for a task is: what its estimate still needs, else the whole estimate or the default. */
  function blockMinutes(task: Task): number {
    const planned = plans.get(task.uid)?.plannedMinutes ?? 0
    if (task.estimate > planned) {
      return task.estimate - planned
    }
    return task.estimate || defaultMinutes
  }

  function planText(task: Task): string {
    const planned = plans.get(task.uid)?.plannedMinutes ?? 0
    if (task.estimate > 0) {
      return $t('timeblock.pane.plannedOf').replace('{planned}', formatMinutes(planned, $t)).replace('{estimate}', formatMinutes(task.estimate, $t))
    }
    return planned > 0 ? $t('timeblock.pane.planned').replace('{planned}', formatMinutes(planned, $t)) : $t('timeblock.pane.notPlanned')
  }

  function dueText(task: Task): string {
    const due = dueDate(task)
    if (!due) {
      return ''
    }
    return $t(isOverdue(task, now) ? 'timeblock.pane.overdue' : 'timeblock.pane.due').replace('{day}', formatShortDay(due))
  }

  function menuFor(task: Task, anchor: HTMLElement): void {
    const r = anchor.getBoundingClientRect()
    openContextMenu(r.left, r.bottom, [
      { label: $t('timeblock.findTime'), action: () => dispatch('findTime', task) },
      { label: $t('timeblock.blockNext'), action: () => dispatch('blockNext', task) },
      'separator',
      { label: $t('timeblock.openTask'), action: () => dispatch('openTask', task) },
    ])
  }

  function proposalText(p: Proposal): string {
    const range = formatTimeRange(new Date(p.start), new Date(p.end), timeFormat)
    const part = p.parts > 1 ? ` ${$t('calendar.block.part').replace('{part}', String(p.part)).replace('{parts}', String(p.parts))}` : ''
    return `${range}${part}`
  }
</script>

<aside class="pane" aria-label={$t('timeblock.pane.title')}>
  <header class="head">
    <h3>{$t('timeblock.pane.title')}</h3>
    <button
      type="button"
      class="cal-btn primary"
      disabled={planning}
      title={planKey ? `${$t('timeblock.planDay')} (${planKey})` : $t('timeblock.planDay')}
      on:click={() => dispatch('plan')}
    >
      <Ph svg={planIcon} size={14} />
      <span>{$t('timeblock.planDay')}</span>
    </button>
  </header>

  {#if proposals.length > 0 || unplaced.length > 0 || short.length > 0}
    <section class="review" aria-label={$t('timeblock.review.title')}>
      <h4>{$t('timeblock.review.title')}</h4>
      {#if proposals.length > 0}
        <ul class="proposals">
          {#each proposals as p (p.key)}
            <li>
              <div class="what">
                <span class="name">{p.title || $t('calendar.block.untitled')}</span>
                <span class="meta">{proposalText(p)}</span>
              </div>
              <button
                type="button"
                class="cal-btn small"
                aria-label={$t('timeblock.review.acceptOne').replace('{title}', p.title || $t('calendar.block.untitled')).replace('{time}', proposalText(p))}
                on:click={() => dispatch('accept', [p.key])}
              >
                {$t('timeblock.review.accept')}
              </button>
              <button
                type="button"
                class="cal-btn small"
                aria-label={$t('timeblock.review.dismissOne').replace('{title}', p.title || $t('calendar.block.untitled')).replace('{time}', proposalText(p))}
                on:click={() => dispatch('dismiss', [p.key])}
              >
                {$t('timeblock.review.dismiss')}
              </button>
            </li>
          {/each}
        </ul>
        <div class="all">
          <button type="button" class="cal-btn primary" on:click={() => dispatch('accept', proposals.map((p) => p.key))}>{$t('timeblock.review.acceptAll')}</button>
          <button type="button" class="cal-btn" on:click={() => dispatch('dismiss', proposals.map((p) => p.key))}>{$t('timeblock.review.dismissAll')}</button>
        </div>
        <p class="note">{$t('timeblock.review.hint')}</p>
      {/if}
      {#if short.length > 0}
        <p class="note">{$t('timeblock.review.short').replace('{tasks}', short.join(', '))}</p>
      {/if}
      {#if unplaced.length > 0}
        <p class="note">{$t('timeblock.review.unplaced').replace('{tasks}', unplaced.join(', '))}</p>
      {/if}
    </section>
  {/if}

  {#if open.length === 0}
    <p class="empty">{$t('timeblock.pane.empty')}</p>
  {:else}
    <p class="note">{$t('timeblock.pane.hint')}</p>
    <ul class="tasks">
      {#each open as task (task.id)}
        <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
        <li class:overdue={isOverdue(task, now)} on:pointerdown={(e) => !(e.target instanceof HTMLButtonElement) && armTaskDrag(e, task, blockMinutes(task))}>
          <span class="grip" aria-hidden="true"><Ph svg={gripIcon} size={12} /></span>
          <div class="what">
            <span class="name">{task.title || $t('tasks.untitled')}</span>
            <span class="meta">
              {#if dueText(task)}<span>{dueText(task)}</span>{/if}
              <span>{planText(task)}</span>
            </span>
          </div>
          <button
            type="button"
            class="cal-btn small"
            aria-label={$t('timeblock.findTimeFor').replace('{title}', task.title || $t('tasks.untitled'))}
            on:click={() => dispatch('findTime', task)}
          >
            {$t('timeblock.findTime')}
          </button>
          <button
            type="button"
            class="cal-icon"
            aria-label={$t('timeblock.taskMenu').replace('{title}', task.title || $t('tasks.untitled'))}
            on:click={(e) => menuFor(task, e.currentTarget)}
          >
            <Ph svg={dotsIcon} size={14} />
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</aside>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: var(--cal-plan-width);
    flex-shrink: 0;
    min-width: 0;
    height: 100%;
    padding: var(--space-3);
    border-inline-start: var(--hairline) solid var(--border-default);
    background: var(--surface-base);
    overflow-y: auto;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  h3,
  h4 {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  .review {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) dashed var(--accent);
    border-radius: var(--radius-card);
  }

  ul {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
    border-radius: var(--radius-control);
  }

  .tasks li {
    cursor: grab;
  }

  .tasks li:hover {
    background: var(--surface-hover);
  }

  .grip {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .what {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .overdue .meta span:first-child {
    color: var(--danger);
  }

  .all {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .small {
    height: auto;
    padding: var(--space-1) var(--space-3);
    font-size: var(--fz-meta);
  }

  .note,
  .empty {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  :global(body.task-dragging) {
    cursor: grabbing;
  }
</style>
