<script lang="ts">
  // where a project stands at a glance: its status and dates, how far along
  // it is and how the cards spread over the columns, the deadlines coming up,
  // the events filed under it, its latest mail and its people.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import Avatar from '../common/Avatar.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { listProjectEvents, listProjectThreads, setEventProject } from '../../lib/api'
  import { onMailNew, onProjectsChanged } from '../../lib/events'
  import { dueLabel, duePassed, nextDeadlines, progressOf, projectColor } from '../../lib/projectlogic'
  import { openMessageInMail, showEvent, showPerson } from '../../lib/projectlinks'
  import { formatWhen } from '../../lib/calformat'
  import { parseDayKey, parseEventTime } from '../../lib/caldate'
  import { displayName, formatListDate, type TimeFormat } from '../../lib/format'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Project, ProjectCard, ProjectColumn, ProjectEvent, ThreadSummary } from '../../lib/types'

  /** The project shown. */
  export let project: Project
  /** Its board, for the deadlines and the spread over the columns. */
  export let columns: ProjectColumn[] = []
  export let cards: ProjectCard[] = []
  /** The current minute. */
  export let now: Date
  export let timeFormat: TimeFormat = 'auto'

  const dispatch = createEventDispatcher<{ tab: 'board' | 'mail' | 'people'; card: string; edit: void }>()

  let events: ProjectEvent[] = []
  let threads: ThreadSummary[] = []
  let loading = true
  let loadedFor = 0

  $: color = projectColor(project)
  $: progress = progressOf(project)
  $: deadlines = nextDeadlines(cards, 6)
  $: upcoming = events.filter((e) => e.found && !e.past).slice(0, 6)
  $: gone = events.filter((e) => !e.found)
  $: recent = threads.slice(0, 5)
  $: spread = columns.map((c) => ({ column: c, count: cards.filter((x) => x.columnId === c.id).length }))
  $: spreadTotal = spread.reduce((n, s) => n + s.count, 0)
  $: if (project.id !== loadedFor) {
    loadedFor = project.id
    loading = true
    void load()
  }

  async function load(): Promise<void> {
    const id = project.id
    try {
      const [ev, th] = await Promise.all([listProjectEvents(id), listProjectThreads(id)])
      if (id === project.id) {
        events = ev
        threads = th
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loading = false
    }
  }

  const offs = [
    onProjectsChanged((e) => {
      if (e.projectIds.length === 0 || e.projectIds.includes(project.id)) {
        void load()
      }
    }),
    onMailNew(() => void load()),
  ]
  onDestroy(() => offs.forEach((off) => off()))

  async function unlinkEvent(e: ProjectEvent): Promise<void> {
    events = events.filter((x) => x.uid !== e.uid)
    try {
      await setEventProject(e.uid, '', 0)
    } catch (err) {
      toastError(errorMessage(err))
      void load()
    }
  }

  function eventWhen(e: ProjectEvent): string {
    return formatWhen(parseEventTime(e.start, e.allDay), parseEventTime(e.end, e.allDay), e.allDay, timeFormat)
  }

  function day(key: string): string {
    return parseDayKey(key).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
  }
</script>

<div class="overview" style={`--project:${color}`}>
  <section class="panel status">
    <h3>{$t('projects.overview.status')}</h3>
    <div class="status-line">
      <span class="pill" class:archived={project.status === 'archived'}>{project.status === 'archived' ? $t('projects.status.archived') : $t('projects.status.active')}</span>
      {#if project.startDate || project.dueDate}
        <span class="dates">
          <Ph svg={calendarIcon} size={13} />
          {#if project.startDate}{day(project.startDate)}{/if}
          {#if project.startDate && project.dueDate}<span aria-hidden="true">-</span>{/if}
          {#if project.dueDate}<span class:overdue={duePassed(project.dueDate, now) && project.status === 'active'}>{day(project.dueDate)}</span>{/if}
        </span>
      {/if}
    </div>
    {#if project.description}
      <p class="desc">{project.description}</p>
    {:else}
      <button type="button" class="link" on:click={() => dispatch('edit')}>{$t('projects.overview.addDescription')}</button>
    {/if}
    <div class="progress-block">
      <span class="big">{Math.round(progress * 100)}%</span>
      <div class="bar" role="progressbar" aria-label={$t('projects.progress')} aria-valuemin={0} aria-valuemax={project.cards} aria-valuenow={project.done}>
        <span style={`width:${progress * 100}%`}></span>
      </div>
      <span class="sub">
        {$t('projects.doneOf').replace('{done}', String(project.done)).replace('{total}', String(project.cards))}
        {#if project.overdue > 0}<span class="overdue"> &middot; {$t('projects.overview.overdue').replace('{n}', String(project.overdue))}</span>{/if}
      </span>
    </div>
    {#if spreadTotal > 0}
      <div class="spread" aria-label={$t('projects.overview.spread')}>
        <div class="stack">
          {#each spread as s (s.column.id)}
            {#if s.count > 0}
              <span class="seg" class:done={s.column.done} style={`flex:${s.count}`} title={`${s.column.name}: ${s.count}`}></span>
            {/if}
          {/each}
        </div>
        <ul class="legend">
          {#each spread as s (s.column.id)}
            <li><span class="swatch" class:done={s.column.done} aria-hidden="true"></span>{s.column.name} <span class="n">{s.count}</span></li>
          {/each}
        </ul>
      </div>
    {/if}
  </section>

  <section class="panel">
    <header class="ph">
      <h3>{$t('projects.overview.deadlines')}</h3>
      <button type="button" class="link" on:click={() => dispatch('tab', 'board')}>{$t('projects.overview.toBoard')}</button>
    </header>
    {#if deadlines.length === 0}
      <p class="empty">{$t('projects.overview.noDeadlines')}</p>
    {:else}
      <ul class="rows">
        {#each deadlines as c (c.task.uid)}
          <li>
            <button type="button" class="row" on:click={() => dispatch('card', c.task.uid)}>
              <span class="text">{c.task.title || $t('tasks.untitled')}</span>
              <span class="aside" class:overdue={duePassed(c.task.due, now)}><Ph svg={clockIcon} size={12} />{dueLabel(c.task.due, now, $t, timeFormat)}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section class="panel">
    <header class="ph">
      <h3>{$t('projects.overview.events')}</h3>
      {#if loading}<PieProgress label={$t('projects.loading')} />{/if}
    </header>
    {#if !loading && upcoming.length === 0}
      <p class="empty">{events.length > 0 ? $t('projects.overview.noUpcoming') : $t('projects.overview.noEvents')}</p>
    {/if}
    <ul class="rows">
      {#each upcoming as e (e.uid)}
        <li class="with-tool">
          <button type="button" class="row" on:click={() => void showEvent(e.uid)}>
            <span class="dot" style={`background:${e.calendarColor || 'var(--cal-1)'}`} aria-hidden="true"></span>
            <span class="text">{e.summary || e.label || $t('calendar.untitled')}</span>
            <span class="aside">{eventWhen(e)}</span>
          </button>
          {#if !e.taskUid}
            <button type="button" class="cal-icon" aria-label={$t('projects.overview.unlinkEvent').replace('{name}', e.summary)} on:click={() => void unlinkEvent(e)}><Ph svg={closeIcon} size={11} /></button>
          {/if}
        </li>
      {/each}
      {#each gone as e (e.uid)}
        <li class="with-tool">
          <span class="row gone"><span class="text">{e.label || $t('calendar.untitled')}</span><span class="aside">{$t('projects.overview.eventGone')}</span></span>
          {#if !e.taskUid}
            <button type="button" class="cal-icon" aria-label={$t('projects.overview.unlinkEvent').replace('{name}', e.label)} on:click={() => void unlinkEvent(e)}><Ph svg={closeIcon} size={11} /></button>
          {/if}
        </li>
      {/each}
    </ul>
    <p class="hint">{$t('projects.overview.eventsHint')}</p>
  </section>

  <section class="panel">
    <header class="ph">
      <h3>{$t('projects.overview.mail')}</h3>
      {#if threads.length > 0}
        <button type="button" class="link" on:click={() => dispatch('tab', 'mail')}>{$t('projects.overview.allMail').replace('{n}', String(threads.length))}</button>
      {/if}
    </header>
    {#if !loading && recent.length === 0}
      <p class="empty">{$t('projects.overview.noMail')}</p>
    {/if}
    <ul class="rows">
      {#each recent as th (th.threadId)}
        <li>
          <button type="button" class="row mail" class:unread={th.unreadCount > 0} on:click={() => openMessageInMail(th.head)}>
            <span class="from">{displayName(th.head.fromName, th.head.fromAddress)}</span>
            <span class="text">{th.head.subject || $t('messageList.noSubject')}</span>
            <span class="aside">{formatListDate(th.head.date, timeFormat)}</span>
          </button>
        </li>
      {/each}
    </ul>
  </section>

  <section class="panel">
    <header class="ph">
      <h3>{$t('projects.overview.people')}</h3>
      <button type="button" class="link" on:click={() => dispatch('tab', 'people')}>{$t('projects.overview.managePeople')}</button>
    </header>
    {#if project.people.length === 0}
      <p class="empty">{$t('projects.overview.noPeople')}</p>
    {:else}
      <ul class="faces">
        {#each project.people as p (p.email)}
          <li>
            <button type="button" class="face" title={p.email} on:click={() => void showPerson(p.email, p.name)}>
              <Avatar name={p.name} email={p.email} size={24} />
              <span>{p.name || p.email}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</div>

<style>
  .overview {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(calc(var(--cal-popover-width) * 1.05), 100%), 1fr));
    align-content: start;
    gap: var(--space-4);
    height: 100%;
    padding: var(--space-5);
    overflow-y: auto;
  }

  .panel {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-width: 0;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .status {
    grid-row: span 2;
  }

  h3 {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  .ph {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .status-line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }

  .pill {
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--success-bg);
    color: var(--success);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    line-height: 1.7;
  }

  .pill.archived {
    background: var(--surface-sunken);
    color: var(--text-tertiary);
  }

  .dates {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .desc {
    margin: 0;
    font-size: var(--fz-list);
    line-height: 1.5;
    color: var(--text-primary);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .link {
    align-self: flex-start;
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .link:hover {
    text-decoration: underline;
  }

  .progress-block {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    column-gap: var(--space-3);
    row-gap: var(--space-1);
  }

  .big {
    grid-row: span 2;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    font-variant-numeric: tabular-nums;
    color: var(--text-primary);
  }

  .bar {
    height: 4px;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .bar span {
    display: block;
    height: 100%;
    background: var(--project);
    transition: width var(--motion-slow) var(--ease-out);
  }

  .sub {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .overdue {
    color: var(--danger);
  }

  .spread {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .stack {
    display: flex;
    gap: 2px;
    height: 6px;
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .seg {
    background: color-mix(in srgb, var(--project) 45%, var(--surface-sunken));
  }

  .seg.done,
  .swatch.done {
    background: var(--project);
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-4);
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .legend li {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }

  .swatch {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 2px;
    background: color-mix(in srgb, var(--project) 45%, var(--surface-sunken));
  }

  .n {
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .empty,
  .hint {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .hint {
    font-size: var(--fz-meta);
  }

  .rows {
    display: flex;
    flex-direction: column;
    margin: 0 calc(-1 * var(--space-2));
    padding: 0;
    list-style: none;
  }

  .with-tool {
    display: flex;
    align-items: center;
  }

  .with-tool .cal-icon {
    opacity: 0;
  }

  .with-tool:hover .cal-icon,
  .with-tool:focus-within .cal-icon {
    opacity: 1;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    width: 100%;
    min-width: 0;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  button.row:hover {
    background: var(--surface-hover);
  }

  .row.gone {
    color: var(--text-tertiary);
    cursor: default;
  }

  .text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .from {
    flex-shrink: 0;
    max-width: 40%;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mail.unread .from,
  .mail.unread .text {
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .aside {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .aside.overdue {
    color: var(--danger);
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
  }

  .faces {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .face {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    max-width: 100%;
    padding: var(--space-1) var(--space-3) var(--space-1) var(--space-1);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .face:hover {
    background: var(--surface-hover);
  }

  .face span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
