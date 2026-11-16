<script lang="ts">
  // the list of projects: a grid of tiles or a list of rows, each with how far
  // along it is, its next deadline and its people. a filter narrows them and
  // a toggle shows the archived ones instead. rows drag into a new order by
  // their handle; mail dropped on a project is filed under it. a project's
  // menu edits, archives or deletes it.
  import { createEventDispatcher } from 'svelte'
  import { flip } from 'svelte/animate'
  import { fade } from 'svelte/transition'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/regular/dots-three.svg?raw'
  import gridIcon from '@phosphor-icons/core/assets/duotone/squares-four-duotone.svg?raw'
  import listIcon from '@phosphor-icons/core/assets/duotone/rows-duotone.svg?raw'
  import kanbanIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
  import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import Avatar from '../common/Avatar.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import DragGrip from '../sidebar/DragGrip.svelte'
  import { projectIcon } from '../../lib/projecticons'
  import { dueLabel, duePassed, progressOf, projectColor, visibleProjects } from '../../lib/projectlogic'
  import { reorder as reorderAction, type ReorderDetail } from '../../lib/reorder'
  import { maildrop } from '../../stores/maildrag'
  import { openContextMenu } from '../../stores/contextmenu'
  import { askConfirm } from '../../stores/confirm'
  import { prefs, setProjectsView } from '../../stores/prefs'
  import { fileMail, openProject, projects, projectsLoaded, removeProject, reorder, setArchived } from '../../stores/projects'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { Project } from '../../lib/types'

  /** The current minute, for deadlines. */
  export let now: Date = new Date()
  /** The filter text, bound so the space's search can fill it. */
  export let query = ''

  const dispatch = createEventDispatcher<{ create: void; edit: Project }>()

  let archived = false
  let filterInput: HTMLInputElement

  $: view = $prefs.projectsView
  $: shown = visibleProjects($projects, query, archived)
  $: archivedCount = $projects.filter((p) => p.status === 'archived').length
  $: canReorder = !archived && !query.trim()
  $: if (archived && archivedCount === 0) {
    archived = false
  }

  /** Puts the cursor in the filter. */
  export function focusFilter(): void {
    filterInput?.focus()
    filterInput?.select()
  }

  function onReorder(event: CustomEvent<ReorderDetail>): void {
    const moved = event.detail.ids.map(Number)
    const rest = $projects.filter((p) => !moved.includes(p.id)).map((p) => p.id)
    void reorder([...moved, ...rest])
  }

  function move(p: Project, by: number): void {
    const ids = $projects.map((x) => x.id)
    const at = ids.indexOf(p.id)
    const to = Math.min(Math.max(at + by, 0), ids.length - 1)
    if (at === to) {
      return
    }
    ids.splice(at, 1)
    ids.splice(to, 0, p.id)
    void reorder(ids)
  }

  async function confirmDelete(p: Project): Promise<void> {
    const ok = await askConfirm({
      title: $t('projects.delete.title').replace('{name}', p.name),
      body: $t('projects.delete.body'),
      confirmLabel: $t('projects.delete.confirm'),
      danger: true,
    })
    if (ok) {
      await removeProject(p)
    }
  }

  function menu(p: Project, x: number, y: number): void {
    const active = p.status === 'active'
    openContextMenu(x, y, [
      { label: $t('projects.open'), action: () => openProject(p.id) },
      { label: $t('projects.edit'), action: () => dispatch('edit', p) },
      ...(active && canReorder
        ? [
            { label: $t('projects.moveEarlier'), action: () => move(p, -1) },
            { label: $t('projects.moveLater'), action: () => move(p, 1) },
          ]
        : []),
      'separator' as const,
      { label: active ? $t('projects.archive') : $t('projects.restore'), action: () => void setArchived(p, active) },
      { label: $t('projects.delete.menu'), danger: true, action: () => void confirmDelete(p) },
    ])
  }

  function openMenu(event: MouseEvent, p: Project): void {
    const r = (event.currentTarget as HTMLElement).getBoundingClientRect()
    menu(p, r.left, r.bottom + 4)
  }

  function dropFor(p: Project) {
    return {
      onDrop: (items: { id: number }[]) => void fileMail(p.id, items.map((m) => m.id)),
      hint: () => $t('projects.drop.into').replace('{name}', p.name),
    }
  }

  function percent(p: Project): string {
    return `${Math.round(progressOf(p) * 100)}%`
  }
</script>

<section class="home" aria-label={$t('space.projects')}>
  <header class="bar">
    <h2 class="title">{archived ? $t('projects.archivedTitle') : $t('space.projects')}</h2>
    <span class="spacer"></span>
    <input
      class="cal-field filter"
      type="search"
      bind:this={filterInput}
      bind:value={query}
      placeholder={$t('projects.filter')}
      aria-label={$t('projects.filter')}
      on:keydown={(e) => e.key === 'Escape' && query && (e.stopPropagation(), (query = ''))}
    />
    {#if archivedCount > 0}
      <button type="button" class="cal-btn" class:on={archived} aria-pressed={archived} on:click={() => (archived = !archived)}>
        {$t('projects.showArchived').replace('{n}', String(archivedCount))}
      </button>
    {/if}
    <div class="seg" role="radiogroup" aria-label={$t('projects.viewLabel')}>
      <button type="button" role="radio" aria-checked={view === 'grid'} class:on={view === 'grid'} aria-label={$t('projects.view.grid')} title={$t('projects.view.grid')} on:click={() => setProjectsView('grid')}>
        <Ph svg={gridIcon} size={15} />
      </button>
      <button type="button" role="radio" aria-checked={view === 'list'} class:on={view === 'list'} aria-label={$t('projects.view.list')} title={$t('projects.view.list')} on:click={() => setProjectsView('list')}>
        <Ph svg={listIcon} size={15} />
      </button>
    </div>
    <button type="button" class="cal-btn primary" on:click={() => dispatch('create')}>
      <Ph svg={plusIcon} size={13} />{$t('projects.new')}
    </button>
  </header>

  <div class="scroll">
    {#if $projectsLoaded && $projects.length === 0}
      <EmptyState title={$t('projects.empty.title')} detail={$t('projects.empty.detail')}>
        <Ph svg={kanbanIcon} size={30} />
        <button slot="action" type="button" class="cal-btn primary" on:click={() => dispatch('create')}>
          <Ph svg={plusIcon} size={13} />{$t('projects.empty.action')}
        </button>
      </EmptyState>
    {:else if $projectsLoaded && shown.length === 0}
      <p class="none" in:fade={{ duration: dur(motion.fast) }}>{$t('projects.noMatches')}</p>
    {:else if view === 'grid'}
      <ul class="grid" aria-label={$t('space.projects')}>
        {#each shown as p (p.id)}
          <li animate:flip={{ duration: dur(motion.base) }}>
            <article class="tile" class:archived={p.status === 'archived'} style={`--project:${projectColor(p)}`} use:maildrop={dropFor(p)}>
              <button type="button" class="open" aria-label={$t('projects.openNamed').replace('{name}', p.name)} on:click={() => openProject(p.id)} on:contextmenu|preventDefault={(e) => menu(p, e.clientX, e.clientY)}></button>
              <div class="head">
                <span class="mark" aria-hidden="true"><Ph svg={projectIcon(p.icon)} size={16} /></span>
                <h3 class="name">{p.name}</h3>
                <button type="button" class="cal-icon more" aria-label={$t('projects.menu').replace('{name}', p.name)} aria-haspopup="menu" on:click|stopPropagation={(e) => openMenu(e, p)}>
                  <Ph svg={moreIcon} size={15} />
                </button>
              </div>
              {#if p.description}
                <p class="desc">{p.description}</p>
              {/if}
              <div class="progress" role="progressbar" aria-label={$t('projects.progress')} aria-valuemin={0} aria-valuemax={p.cards} aria-valuenow={p.done}>
                <span class="fill" style={`width:${percent(p)}`}></span>
              </div>
              <div class="meta">
                <span class="count">{$t('projects.doneOf').replace('{done}', String(p.done)).replace('{total}', String(p.cards))}</span>
                {#if p.nextDue}
                  <span class="due" class:overdue={duePassed(p.nextDue, now)} title={$t('projects.nextDue')}>
                    <Ph svg={clockIcon} size={12} />{dueLabel(p.nextDue, now, $t)}
                  </span>
                {/if}
                {#if p.mail > 0}
                  <span class="mail" title={$t('projects.mailCount').replace('{n}', String(p.mail))}><Ph svg={mailIcon} size={12} />{p.mail}</span>
                {/if}
                <span class="spacer"></span>
                {#if p.people.length > 0}
                  <span class="people" aria-label={$t('projects.peopleCount').replace('{n}', String(p.people.length))}>
                    {#each p.people.slice(0, 4) as person (person.email)}
                      <span class="face" title={person.name || person.email}><Avatar name={person.name} email={person.email} size={20} /></span>
                    {/each}
                    {#if p.people.length > 4}
                      <span class="more-people">+{p.people.length - 4}</span>
                    {/if}
                  </span>
                {/if}
              </div>
            </article>
          </li>
        {/each}
      </ul>
    {:else}
      <div class="list" role="list" aria-label={$t('space.projects')} use:reorderAction on:reorder={onReorder}>
        {#each shown as p (p.id)}
          <div class="row" class:archived={p.status === 'archived'} role="listitem" data-reorder-id={p.id} style={`--project:${projectColor(p)}`} use:maildrop={dropFor(p)} animate:flip={{ duration: dur(motion.base) }}>
            {#if canReorder}
              <DragGrip />
            {/if}
            <button type="button" class="row-open" on:click={() => openProject(p.id)} on:contextmenu|preventDefault={(e) => menu(p, e.clientX, e.clientY)}>
              <span class="mark" aria-hidden="true"><Ph svg={projectIcon(p.icon)} size={15} /></span>
              <span class="row-name">{p.name}</span>
              {#if p.description}
                <span class="row-desc">{p.description}</span>
              {/if}
            </button>
            <span class="row-progress" title={percent(p)}>
              <span class="progress small"><span class="fill" style={`width:${percent(p)}`}></span></span>
              <span class="count">{p.done}/{p.cards}</span>
            </span>
            <span class="row-due due" class:overdue={p.nextDue && duePassed(p.nextDue, now)}>{p.nextDue ? dueLabel(p.nextDue, now, $t) : ''}</span>
            <span class="people">
              {#each p.people.slice(0, 3) as person (person.email)}
                <span class="face" title={person.name || person.email}><Avatar name={person.name} email={person.email} size={18} /></span>
              {/each}
            </span>
            <button type="button" class="cal-icon" aria-label={$t('projects.menu').replace('{name}', p.name)} aria-haspopup="menu" on:click={(e) => openMenu(e, p)}>
              <Ph svg={moreIcon} size={15} />
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</section>

<style>
  .home {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
    container-type: inline-size;
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
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
  }

  .filter {
    width: calc(var(--cal-sidebar-width) * 0.8);
    min-width: calc(var(--cal-sidebar-width) * 0.5);
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .seg {
    display: inline-flex;
    padding: 1px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
  }

  .seg button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) - 6px);
    height: calc(var(--control-height) - 6px);
    border: none;
    border-radius: calc(var(--radius-control) - 1px);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .seg button.on {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-5);
  }

  .none {
    margin: var(--space-6) 0;
    text-align: center;
    color: var(--text-tertiary);
    font-size: var(--fz-list);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(var(--project-tile-width), 100%), 1fr));
    gap: var(--space-4);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .tile {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .tile:hover {
    border-color: var(--border-strong);
  }

  .tile:active {
    transform: scale(0.995);
  }

  .tile.archived,
  .row.archived {
    opacity: 0.72;
  }

  /* the whole tile opens the project; the controls in it sit above. */
  .open {
    position: absolute;
    inset: 0;
    z-index: 0;
    border: none;
    border-radius: inherit;
    background: transparent;
    cursor: var(--cursor-action);
  }

  .open:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .head,
  .meta {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    pointer-events: none;
  }

  .head :global(button),
  .meta :global(.face) {
    pointer-events: auto;
  }

  .mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: calc(var(--control-height) - 4px);
    height: calc(var(--control-height) - 4px);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--project) 15%, transparent);
    color: var(--project);
  }

  .name {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .more {
    opacity: 0;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .tile:hover .more,
  .tile:focus-within .more {
    opacity: 1;
  }

  .desc {
    position: relative;
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-secondary);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    pointer-events: none;
  }

  .progress {
    position: relative;
    height: 3px;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    overflow: hidden;
    pointer-events: none;
  }

  .progress.small {
    width: calc(var(--space-6) * 2);
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--project);
    transition: width var(--motion-slow) var(--ease-out);
  }

  .meta {
    margin-top: auto;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .count {
    font-variant-numeric: tabular-nums;
  }

  .due,
  .mail {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    white-space: nowrap;
  }

  .due.overdue {
    color: var(--danger);
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

  .more-people {
    margin-inline-start: var(--space-1);
    font-variant-numeric: tabular-nums;
  }

  .list {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) + var(--space-3));
    padding: 0 var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .row:last-child {
    border-bottom: none;
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row-open {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    padding: var(--row-pad-y) 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .row-open:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    border-radius: var(--radius-control);
  }

  .row-name {
    flex-shrink: 0;
    max-width: 50%;
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-desc {
    min-width: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-progress {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .row-due {
    min-width: calc(var(--space-6) * 2.5);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    text-align: end;
  }

  @container (max-width: 560px) {
    .row-desc,
    .row-due {
      display: none;
    }
  }
</style>
