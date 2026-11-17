<script lang="ts">
  // an open project: a header with its name and tabs, then the tab's page.
  // it opens on the board. the header's menu edits, archives or deletes the
  // project; the back arrow returns to the list of projects.
  import { createEventDispatcher, tick } from 'svelte'
  import backIcon from '@phosphor-icons/core/assets/duotone/arrow-left-duotone.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/regular/dots-three.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import ProjectBoard from './ProjectBoard.svelte'
  import ProjectOverview from './ProjectOverview.svelte'
  import ProjectMail from './ProjectMail.svelte'
  import ProjectNotes from './ProjectNotes.svelte'
  import ProjectPeople from './ProjectPeople.svelte'
  import ProjectIcon from './ProjectIcon.svelte'
  import { progressOf, projectColor } from '../../lib/projectlogic'
  import { askConfirm } from '../../stores/confirm'
  import { openContextMenu } from '../../stores/contextmenu'
  import { board, cardFocus, closeProject, projectTab, projectTabs, removeProject, setArchived, type ProjectTab } from '../../stores/projects'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Project } from '../../lib/types'

  /** The project open. */
  export let project: Project
  /** The current minute. */
  export let now: Date
  export let timeFormat: TimeFormat = 'auto'
  export let weekStart = 1

  const dispatch = createEventDispatcher<{ edit: Project; compose: { email: string; projectId: number } }>()

  let boardView: ProjectBoard | undefined
  let noteFocus: string | null = null
  let tabEls: Record<string, HTMLButtonElement> = {}

  $: color = projectColor(project)
  $: loadedBoard = $board && $board.project.id === project.id ? $board : null
  $: counts = {
    board: project.cards - project.done,
    overview: 0,
    mail: project.mail,
    notes: project.notes,
    people: project.people.length,
  } as Record<ProjectTab, number>

  /** Opens the line to add a card on the board. */
  export function newItem(): void {
    projectTab.set('board')
    void tick().then(() => boardView?.startAdd())
  }

  /** Takes the open or focused card off the board. */
  export function removeSelected(): boolean {
    if ($projectTab !== 'board' || !boardView) {
      return false
    }
    boardView.removeSelected()
    return true
  }

  /** Puts the cursor in the board's filter. */
  export function focusFilter(): boolean {
    if ($projectTab !== 'board' || !boardView) {
      return false
    }
    boardView.focusFilter()
    return true
  }

  function onTabKey(event: KeyboardEvent): void {
    const at = projectTabs.indexOf($projectTab)
    let next = -1
    if (event.key === 'ArrowRight') {
      next = (at + 1) % projectTabs.length
    } else if (event.key === 'ArrowLeft') {
      next = (at - 1 + projectTabs.length) % projectTabs.length
    } else if (event.key === 'Home') {
      next = 0
    } else if (event.key === 'End') {
      next = projectTabs.length - 1
    }
    if (next >= 0) {
      event.preventDefault()
      projectTab.set(projectTabs[next])
      tabEls[projectTabs[next]]?.focus()
    }
  }

  async function confirmDelete(): Promise<void> {
    const ok = await askConfirm({
      title: $t('projects.delete.title').replace('{name}', project.name),
      body: $t('projects.delete.body'),
      confirmLabel: $t('projects.delete.confirm'),
      danger: true,
    })
    if (ok) {
      await removeProject(project)
    }
  }

  function menu(event: MouseEvent): void {
    const r = (event.currentTarget as HTMLElement).getBoundingClientRect()
    const active = project.status === 'active'
    openContextMenu(r.left, r.bottom + 4, [
      { label: $t('projects.edit'), action: () => dispatch('edit', project) },
      { label: active ? $t('projects.archive') : $t('projects.restore'), action: () => void setArchived(project, active) },
      'separator',
      { label: $t('projects.delete.menu'), danger: true, action: () => void confirmDelete() },
    ])
  }

  function showCard(uid: string): void {
    cardFocus.set(uid)
    projectTab.set('board')
  }
</script>

<section class="view" aria-label={project.name} style={`--project:${color}`}>
  <header class="head">
    <button type="button" class="cal-icon" aria-label={$t('projects.back')} title={$t('projects.back')} on:click={closeProject}>
      <Ph svg={backIcon} size={16} />
    </button>
    <span class="mark" aria-hidden="true"><ProjectIcon icon={project.icon} iconNodes={project.iconNodes} size={16} /></span>
    <div class="titles">
      <h2 class="name">{project.name}</h2>
      {#if project.status === 'archived'}
        <span class="archived">{$t('projects.status.archived')}</span>
      {/if}
    </div>
    <span class="progress" title={$t('projects.doneOf').replace('{done}', String(project.done)).replace('{total}', String(project.cards))}>
      <PieProgress value={progressOf(project)} size={14} label={$t('projects.progress')} />
      <span>{Math.round(progressOf(project) * 100)}%</span>
    </span>
    <span class="spacer"></span>
    <button type="button" class="cal-btn" on:click={() => dispatch('edit', project)}>{$t('projects.edit')}</button>
    <button type="button" class="cal-icon" aria-label={$t('projects.menu').replace('{name}', project.name)} aria-haspopup="menu" on:click={menu}>
      <Ph svg={moreIcon} size={16} />
    </button>
  </header>

  <!-- svelte-ignore a11y-interactive-supports-focus -->
  <div class="tabs" role="tablist" tabindex="-1" aria-label={$t('projects.tabs')} on:keydown={onTabKey}>
    {#each projectTabs as tab (tab)}
      <button
        type="button"
        role="tab"
        id={`project-tab-${tab}`}
        aria-selected={$projectTab === tab}
        aria-controls="project-panel"
        tabindex={$projectTab === tab ? 0 : -1}
        class:on={$projectTab === tab}
        bind:this={tabEls[tab]}
        on:click={() => projectTab.set(tab)}
      >
        {$t(`projects.tab.${tab}`)}
        {#if counts[tab] > 0}
          <span class="badge">{counts[tab]}</span>
        {/if}
      </button>
    {/each}
  </div>

  <div class="panel" id="project-panel" role="tabpanel" aria-labelledby={`project-tab-${$projectTab}`}>
    {#if $projectTab === 'board'}
      {#if loadedBoard}
        <ProjectBoard
          bind:this={boardView}
          project={loadedBoard.project}
          columns={loadedBoard.columns}
          cards={loadedBoard.cards}
          {now}
          {timeFormat}
          {weekStart}
          on:openNote={(e) => {
            noteFocus = e.detail
            projectTab.set('notes')
          }}
        />
      {:else}
        <div class="loading"><PieProgress label={$t('projects.loading')} /></div>
      {/if}
    {:else if $projectTab === 'overview'}
      <ProjectOverview
        {project}
        columns={loadedBoard?.columns ?? []}
        cards={loadedBoard?.cards ?? []}
        {now}
        {timeFormat}
        on:tab={(e) => projectTab.set(e.detail)}
        on:card={(e) => showCard(e.detail)}
        on:edit={() => dispatch('edit', project)}
      />
    {:else if $projectTab === 'mail'}
      <ProjectMail {project} />
    {:else if $projectTab === 'notes'}
      <ProjectNotes {project} focusId={noteFocus} on:focused={() => (noteFocus = null)} />
    {:else}
      <ProjectPeople {project} on:compose={(e) => dispatch('compose', { email: e.detail, projectId: project.id })} />
    {/if}
  </div>
</section>

<style>
  .view {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4) var(--space-2);
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

  .titles {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    min-width: 0;
  }

  .name {
    margin: 0;
    min-width: calc(var(--space-6) * 3);
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .archived {
    flex-shrink: 0;
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .progress {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .spacer {
    flex: 1;
  }

  .tabs {
    display: flex;
    gap: var(--space-1);
    padding: 0 var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .tabs button {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    height: calc(var(--control-height) + 2px);
    padding: 0 var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out);
  }

  .tabs button::after {
    content: '';
    position: absolute;
    inset-inline: var(--space-2);
    bottom: -1px;
    height: 2px;
    border-radius: 1px;
    background: var(--accent);
    transform: scaleX(0);
    transition: transform var(--motion-base) var(--ease-out);
  }

  .tabs button:hover {
    color: var(--text-primary);
  }

  .tabs button.on {
    color: var(--text-primary);
  }

  .tabs button.on::after {
    transform: scaleX(1);
  }

  .tabs button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    border-radius: var(--radius-control);
  }

  .badge {
    padding: 0 var(--space-1);
    min-width: 16px;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    text-align: center;
  }

  .panel {
    flex: 1;
    min-height: 0;
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-6);
  }
</style>
