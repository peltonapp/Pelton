<script lang="ts">
  // the Projects space of the new design: the list of projects, or one
  // project open on its board. the app's shortcuts act on it: compose makes a
  // card on an open board and a project otherwise, search goes to the top bar
  // search, delete takes the focused card off the board. it is code-split
  // like the other spaces and only loads when opened.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import ProjectsHome from './ProjectsHome.svelte'
  import ProjectView from './ProjectView.svelte'
  import ProjectDialog from './ProjectDialog.svelte'
  import { focusSpaceSearch, matchesQuery, registerSpaceSearch } from '../../lib/spacesearch'
  import { projectColor } from '../../lib/projectlogic'
  import { resolveWeekStart } from '../../lib/caldate'
  import { registerSpaceActions } from '../../stores/space'
  import { board, loadBoard, loadProjects, newProjectRequest, openCard, openProject, openProjectId, projects } from '../../stores/projects'
  import { calendarSettings, loadCalendarSettings, refreshCalendars } from '../../stores/calendar'
  import { loadTasks, startTasks } from '../../stores/tasks'
  import { prefs } from '../../stores/prefs'
  import { popupIsOpen } from '../../lib/popups'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Project } from '../../lib/types'

  const dispatch = createEventDispatcher<{ compose: { email: string; projectId: number } }>()

  let now = new Date()
  let editing: Project | null = null
  let creating = false
  let query = ''
  let home: ProjectsHome | undefined
  let view: ProjectView | undefined
  let unregister: (() => void) | undefined
  let stopSearch: (() => void) | undefined
  let stopTasks: (() => void) | undefined
  let clock: ReturnType<typeof setInterval> | undefined

  $: open = $projects.find((p) => p.id === $openProjectId) ?? null
  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: weekStart = resolveWeekStart($calendarSettings.weekStart, navigator.language)
  $: if ($newProjectRequest) {
    newProjectRequest.set(false)
    creating = true
  }
  $: if ($openProjectId !== null && !open && $projects.length > 0 && !$projects.some((p) => p.id === $openProjectId)) {
    openProjectId.set(null)
  }

  function whenFree(run: () => void): void {
    if (!popupIsOpen() && !editing && !creating) {
      run()
    }
  }

  onMount(() => {
    stopTasks = startTasks()
    void loadTasks()
    void loadProjects()
    void loadCalendarSettings()
    if ($openProjectId !== null && $board?.project.id !== $openProjectId) {
      void loadBoard($openProjectId)
    }
    clock = setInterval(() => (now = new Date()), 60000)
    unregister = registerSpaceActions(
      'projects',
      {
        compose: () =>
          whenFree(() => {
            if (open) {
              view?.newItem()
            } else {
              creating = true
            }
          }),
        search: () => {
          if (!focusSpaceSearch()) {
            if (open) {
              view?.focusFilter()
            } else {
              home?.focusFilter()
            }
          }
        },
        'delete-message': () => (open && view ? view.removeSelected() : false),
        sync: () => {
          void refreshCalendars().then(() => {
            void loadTasks()
            void loadProjects()
            if ($openProjectId !== null) {
              void loadBoard($openProjectId)
            }
          })
        },
      },
      { compose: 'projects.newItem', 'delete-message': 'projects.card.remove' },
    )
    stopSearch = registerSpaceSearch('projects', {
      placeholderKey: 'projects.searchPlaceholder',
      emptyKey: 'projects.noMatches',
      search: (q) => {
        const found = $projects
          .filter((p) => matchesQuery(q, [p.name, p.description, ...p.people.map((x) => `${x.name} ${x.email}`)]))
          .slice(0, 10)
          .map((p) => ({
            id: `p:${p.id}`,
            title: p.name,
            detail: p.description.split('\n')[0] ?? '',
            aside: p.status === 'archived' ? $t('projects.status.archived') : `${p.done}/${p.cards}`,
            color: projectColor(p),
          }))
        const cards = ($board?.cards ?? [])
          .filter((c) => matchesQuery(q, [c.task.title, c.task.notes, ...c.task.categories]))
          .slice(0, 10)
          .map((c) => ({
            id: `c:${c.projectId}:${c.task.uid}`,
            title: c.task.title || $t('tasks.untitled'),
            detail: $board?.project.name ?? '',
            aside: $board?.columns.find((col) => col.id === c.columnId)?.name ?? '',
          }))
        return [...cards, ...found]
      },
      open: (result) => {
        const [kind, id, uid] = result.id.split(':')
        if (kind === 'p') {
          openProject(Number(id))
        } else {
          openCard(Number(id), uid)
        }
      },
    })
  })

  onDestroy(() => {
    unregister?.()
    stopSearch?.()
    stopTasks?.()
    if (clock) {
      clearInterval(clock)
    }
  })
</script>

<section class="screen" aria-label={$t('space.projects')}>
  {#if open}
    <ProjectView
      bind:this={view}
      project={open}
      {now}
      {timeFormat}
      {weekStart}
      on:edit={(e) => (editing = e.detail)}
      on:compose={(e) => dispatch('compose', e.detail)}
    />
  {:else}
    <ProjectsHome bind:this={home} bind:query {now} on:create={() => (creating = true)} on:edit={(e) => (editing = e.detail)} />
  {/if}
</section>

{#if creating}
  <ProjectDialog {weekStart} on:close={() => (creating = false)} on:saved={(e) => openProject(e.detail.id)} />
{/if}
{#if editing}
  <ProjectDialog project={editing} {weekStart} on:close={() => (editing = null)} />
{/if}

<style>
  .screen {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    min-width: 0;
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: var(--font-ui);
    overflow: hidden;
  }
</style>
