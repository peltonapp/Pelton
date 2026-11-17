<script lang="ts">
  // the "Open tasks" section of a contact: the open tasks linked to any of the
  // person's addresses, each with its checkbox and a way into Tasks, and a
  // button that starts a new task for them. it reads the tasks store, so it
  // can sit in whichever contact pane shows a person.
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import TaskCheck from './TaskCheck.svelte'
  import { taskForContact, taskLists, tasks, toggleDone } from '../../stores/tasks'
  import { openSpace } from '../../stores/space'
  import { openTask } from '../../lib/taskmail'
  import { tasksForContact, dueDate } from '../../lib/tasklogic'
  import { calendarColor } from '../../lib/calformat'
  import { t } from '../../lib/i18n'
  import type { Task } from '../../lib/types'

  /** The person's name, for a new task. */
  export let name: string
  /** The person's addresses; tasks linked to any of them are listed. */
  export let emails: string[]

  $: open = dedupe(emails.flatMap((e) => tasksForContact($tasks, e))).filter((x) => !x.done)

  function dedupe(list: Task[]): Task[] {
    return [...new Map(list.map((x) => [x.id, x])).values()].sort((a, b) => (dueDate(a)?.getTime() ?? Infinity) - (dueDate(b)?.getTime() ?? Infinity))
  }

  function colorOf(task: Task): string {
    const list = $taskLists.find((l) => l.id === task.listId)
    return list ? calendarColor(list) : ''
  }

  function newTask(): void {
    taskForContact.set({ name, email: emails[0] ?? '' })
    openSpace('tasks')
  }
</script>

<section class="tasks" aria-label={$t('tasks.forContact')}>
  <header>
    <h4>{$t('tasks.forContact')}</h4>
    <button type="button" class="cal-item" on:click={newTask}>
      <Ph svg={plusIcon} size={12} />
      <span>{$t('tasks.newTask')}</span>
    </button>
  </header>
  {#if open.length === 0}
    <p class="none">{$t('tasks.forContactNone')}</p>
  {:else}
    <ul>
      {#each open as task (task.id)}
        <li>
          <TaskCheck checked={task.done} label={$t('tasks.toggle').replace('{title}', task.title || $t('tasks.untitled'))} color={colorOf(task)} disabled={task.readOnly} on:toggle={() => void toggleDone(task)} />
          <button type="button" class="title" on:click={() => openTask(task.id)}>{task.title || $t('tasks.untitled')}</button>
          {#if task.due}
            <span class="due">{dueDate(task)?.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}</span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .tasks {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-4);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  h4 {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  header .cal-item {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .none {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--control-height);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  li:last-child {
    border-bottom: none;
  }

  .title {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
    text-align: start;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: var(--cursor-action);
  }

  .title:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .due {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }
</style>
