<script lang="ts">
  // one card on a board: its task's checkbox and title, its tags, then a line
  // of when it is due, how urgent it is, what is attached and who it is for.
  // the board moves it by pointer and keyboard; mail dropped on it is
  // attached to it.
  import { createEventDispatcher } from 'svelte'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import eventIcon from '@phosphor-icons/core/assets/duotone/calendar-check-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import linkIcon from '@phosphor-icons/core/assets/duotone/link-simple-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Avatar from '../common/Avatar.svelte'
  import TaskCheck from '../tasks/TaskCheck.svelte'
  import { dueLabel, duePassed, priorityLevel } from '../../lib/projectlogic'
  import { maildrop } from '../../stores/maildrag'
  import { fileMail } from '../../stores/projects'
  import { toggleDone } from '../../stores/tasks'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { ProjectCard } from '../../lib/types'

  /** The card shown. */
  export let card: ProjectCard
  /** The current minute, for deadlines. */
  export let now: Date
  export let timeFormat: TimeFormat = 'auto'
  /** The card holds the board's keyboard focus stop. */
  export let focusable = false
  /** The card is open in the panel beside the board. */
  export let selected = false
  /** The card is the one being dragged; it stays as a faint outline. */
  export let lifted = false
  /** The project color, for the checkbox. */
  export let color = ''

  const dispatch = createEventDispatcher<{ open: void; menu: { x: number; y: number } }>()

  $: task = card.task
  $: due = task.due ? dueLabel(task.due, now, $t, timeFormat) : ''
  $: overdue = !task.done && duePassed(task.due, now)
  $: level = priorityLevel(task.priority)
  $: people = task.contacts
  $: title = task.title || $t('tasks.untitled')

  $: drop = {
    onDrop: (items: { id: number }[]) => void fileMail(card.projectId, items.map((m) => m.id), card.task.uid),
    hint: () => $t('projects.drop.card'),
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-tabindex a11y-no-noninteractive-element-interactions a11y-click-events-have-key-events -->
<article
  class="card"
  class:selected
  class:lifted
  class:done={task.done}
  tabindex={focusable ? 0 : -1}
  data-card-uid={task.uid}
  aria-label={title}
  aria-current={selected ? 'true' : undefined}
  use:maildrop={drop}
  on:click={() => dispatch('open')}
  on:contextmenu|preventDefault={(e) => dispatch('menu', { x: e.clientX, y: e.clientY })}
>
  <div class="top">
    <TaskCheck
      checked={task.done}
      label={$t('tasks.toggle').replace('{title}', title)}
      {color}
      disabled={task.readOnly}
      on:toggle={(e) => void toggleDone(task, e.detail)}
    />
    <span class="title">{title}</span>
  </div>
  {#if task.categories.length > 0}
    <div class="tags">
      {#each task.categories.slice(0, 3) as tag (tag)}
        <span class="tag">{tag}</span>
      {/each}
    </div>
  {/if}
  {#if due || level !== 'none' || card.mail || card.events || card.notes || people.length || task.rrule || card.sourceKind}
    <div class="meta">
      {#if due}
        <span class="due" class:overdue title={$t('tasks.due')}><Ph svg={clockIcon} size={12} />{due}</span>
      {/if}
      {#if task.rrule}
        <span class="mark" title={$t('tasks.repeats')}><Ph svg={repeatIcon} size={12} /></span>
      {/if}
      {#if level !== 'none'}
        <span class="prio {level}" title={$t(`projects.priority.${level}`)} aria-label={$t(`projects.priority.${level}`)}><Ph svg={flagIcon} size={12} /></span>
      {/if}
      {#if card.mail}
        <span class="mark" title={$t('projects.card.mail').replace('{n}', String(card.mail))}><Ph svg={mailIcon} size={12} />{card.mail}</span>
      {/if}
      {#if card.events}
        <span class="mark" title={$t('projects.card.events').replace('{n}', String(card.events))}><Ph svg={eventIcon} size={12} />{card.events}</span>
      {/if}
      {#if card.notes}
        <span class="mark" title={$t('projects.card.notes').replace('{n}', String(card.notes))}><Ph svg={noteIcon} size={12} />{card.notes}</span>
      {/if}
      {#if card.sourceKind}
        <span class="mark" title={card.sourceRef}><Ph svg={linkIcon} size={12} /></span>
      {/if}
      <span class="spacer"></span>
      {#if people.length > 0}
        <span class="people">
          {#each people.slice(0, 3) as p (p.email || p.name)}
            <span class="face" title={p.name || p.email}><Avatar name={p.name} email={p.email} size={18} /></span>
          {/each}
          {#if people.length > 3}
            <span class="more">+{people.length - 3}</span>
          {/if}
        </span>
      {/if}
    </div>
  {/if}
</article>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    cursor: var(--cursor-action);
    user-select: none;
    -webkit-user-select: none;
    touch-action: none;
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out);
  }

  .card:hover {
    border-color: var(--border-strong);
  }

  .card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .card.selected {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  /* the card being carried leaves a faint outline where it was. */
  .card.lifted {
    opacity: 0.35;
    border-style: dashed;
  }

  .top {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
  }

  .top :global(.check) {
    margin-top: 1px;
  }

  .title {
    flex: 1;
    min-width: 0;
    font-size: var(--fz-list);
    line-height: 1.35;
    color: var(--text-primary);
    overflow-wrap: anywhere;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .done .title {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
    padding-inline-start: calc(var(--task-check) + var(--space-2));
  }

  .tag {
    max-width: 100%;
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    line-height: 1.6;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    padding-inline-start: calc(var(--task-check) + var(--space-2));
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .due,
  .mark,
  .prio {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .due.overdue {
    color: var(--danger);
  }

  .prio.high {
    color: var(--danger);
  }

  .prio.medium {
    color: var(--warning);
  }

  .spacer {
    flex: 1;
  }

  .people {
    display: inline-flex;
    align-items: center;
  }

  .face {
    display: inline-flex;
    border-radius: 50%;
    box-shadow: 0 0 0 1.5px var(--surface-raised);
  }

  .face + .face {
    margin-inline-start: calc(-1 * var(--space-2));
  }

  .more {
    margin-inline-start: var(--space-1);
  }
</style>
