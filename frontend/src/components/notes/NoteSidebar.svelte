<script lang="ts">
  // the left pane of the notes space: the notes folder with its actions, the
  // daily notes calendar, starred and recent notes, the folder tree, the tags
  // and the projects notes belong to. a folder, tag or project chosen here
  // fills the note list. notes and folders dragged onto a folder move there,
  // links following.
  import { createEventDispatcher } from 'svelte'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import folderPlusIcon from '@phosphor-icons/core/assets/duotone/folder-simple-plus-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-simple-duotone.svg?raw'
  import folderOpenIcon from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
  import caretRight from '@phosphor-icons/core/assets/regular/caret-right.svg?raw'
  import caretDown from '@phosphor-icons/core/assets/regular/caret-down.svg?raw'
  import hashIcon from '@phosphor-icons/core/assets/duotone/hash-duotone.svg?raw'
  import starIcon from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import recentIcon from '@phosphor-icons/core/assets/duotone/clock-counter-clockwise-duotone.svg?raw'
  import notesIcon from '@phosphor-icons/core/assets/duotone/notebook-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import sunIcon from '@phosphor-icons/core/assets/duotone/sun-duotone.svg?raw'
  import graphIcon from '@phosphor-icons/core/assets/duotone/graph-duotone.svg?raw'
  import templateIcon from '@phosphor-icons/core/assets/duotone/files-duotone.svg?raw'
  import kanbanIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import MiniMonth from '../calendar/MiniMonth.svelte'
  import { folderCounts, folderRows, type NoteScope } from '../../lib/notelist'
  import { noteTitle } from '../../lib/notemd'
  import { projectOfNote } from '../../lib/noteprojects'
  import { projectColor } from '../../lib/projectlogic'
  import { dailyNoteDate } from '../../lib/notetemplate'
  import { dayKey } from '../../lib/caldate'
  import { vaultIndex, vaultNotes, noteTags, starredNotes, recentNotes, notesByPath } from '../../stores/notes'
  import { projects } from '../../stores/projects'
  import { prefs } from '../../stores/prefs'
  import type { Project, VaultNote } from '../../lib/types'
  import { t } from '../../lib/i18n'

  /** What the note list shows. */
  export let scope: NoteScope
  /** The day the calendar is on. */
  export let day: Date = new Date()
  /** The note open in the editor. */
  export let active: string | null = null
  /** The graph view is showing. */
  export let graphOpen = false
  /** First day of the week, 0 for Sunday. */
  export let weekStart = 1

  const dispatch = createEventDispatcher<{
    scope: NoteScope
    open: string
    newNote: string
    newFolder: string
    today: void
    day: Date
    template: void
    graph: void
    folderMenu: { path: string; x: number; y: number }
    noteMenu: { path: string; x: number; y: number }
    drop: { from: string; folder: string }
  }>()

  let expanded = new Set<string>()
  let showRecent = true
  let showTags = true
  let showProjects = true
  let showDaily = true
  let dragOver: string | null = null

  $: rows = folderRows($vaultIndex?.folders ?? [], expanded)
  $: counts = folderCounts($vaultNotes)
  $: busy = dailyDays($vaultNotes.map((n) => n.path), $prefs.notesDailyFolder, $prefs.notesDailyFormat)
  $: starred = $starredNotes.filter((p) => $notesByPath.has(p))
  $: recent = $recentNotes.filter((p) => $notesByPath.has(p)).slice(0, 6)
  $: currentFolder = scope.kind === 'folder' ? scope.path : ''
  $: folderName = ($vaultIndex?.folder ?? '').split(/[\\/]/).filter(Boolean).pop() ?? $t('space.notes')

  $: projectRows = notesPerProject($vaultNotes, $projects)

  function notesPerProject(notes: readonly VaultNote[], list: readonly Project[]): { project: Project; count: number }[] {
    const counts = new Map<number, number>()
    for (const n of notes) {
      const p = projectOfNote(n.props, list)
      if (p) {
        counts.set(p.id, (counts.get(p.id) ?? 0) + 1)
      }
    }
    return list.filter((p) => counts.has(p.id)).map((project) => ({ project, count: counts.get(project.id) ?? 0 }))
  }

  function dailyDays(paths: string[], folder: string, format: string): Set<string> {
    const out = new Set<string>()
    for (const p of paths) {
      const d = dailyNoteDate(p, folder, format)
      if (d) {
        out.add(dayKey(d))
      }
    }
    return out
  }

  function toggle(path: string): void {
    const next = new Set(expanded)
    if (next.has(path)) {
      next.delete(path)
    } else {
      next.add(path)
    }
    expanded = next
  }

  // right opens a folder and left closes it, as in a tree.
  function onFolderKey(event: KeyboardEvent, path: string, hasChildren: boolean): void {
    if (!hasChildren) {
      return
    }
    const rtl = getComputedStyle(document.documentElement).direction === 'rtl'
    const open = rtl ? 'ArrowLeft' : 'ArrowRight'
    const close = rtl ? 'ArrowRight' : 'ArrowLeft'
    if ((event.key === open && !expanded.has(path)) || (event.key === close && expanded.has(path))) {
      event.preventDefault()
      toggle(path)
    }
  }

  // a folder opens in the tree when the list shows it, so it can be found.
  $: if (scope.kind === 'folder' && scope.path) {
    const parts = scope.path.split('/')
    let changed = false
    const next = new Set(expanded)
    for (let i = 1; i < parts.length; i++) {
      const p = parts.slice(0, i).join('/')
      if (!next.has(p)) {
        next.add(p)
        changed = true
      }
    }
    if (changed) {
      expanded = next
    }
  }

  function isScope(s: NoteScope): boolean {
    return JSON.stringify(s) === JSON.stringify(scope)
  }

  function onDragStart(e: DragEvent, path: string): void {
    e.dataTransfer?.setData('application/x-pelton-note', path)
    e.dataTransfer?.setData('text/plain', path)
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
    }
  }

  function onDragOver(e: DragEvent, folder: string): void {
    if (e.dataTransfer?.types.includes('application/x-pelton-note')) {
      e.preventDefault()
      dragOver = folder
    }
  }

  function onDrop(e: DragEvent, folder: string): void {
    const from = e.dataTransfer?.getData('application/x-pelton-note')
    dragOver = null
    if (!from) {
      return
    }
    e.preventDefault()
    dispatch('drop', { from, folder })
  }
</script>

<nav class="sidebar" aria-label={$t('notes.sidebar')}>
  <header class="head">
    <span class="vault" title={$vaultIndex?.folder ?? ''}>
      <Ph svg={notesIcon} size={16} />
      <span class="vault-name">{folderName}</span>
    </span>
    <button type="button" class="cal-icon" aria-label={$t('notes.newNote')} title={$t('notes.newNote')} on:click={() => dispatch('newNote', currentFolder)}>
      <Ph svg={plusIcon} size={16} />
    </button>
    <button type="button" class="cal-icon" aria-label={$t('notes.newFolder')} title={$t('notes.newFolder')} on:click={() => dispatch('newFolder', currentFolder)}>
      <Ph svg={folderPlusIcon} size={16} />
    </button>
  </header>

  <div class="actions">
    <button type="button" class="action" on:click={() => dispatch('today')}>
      <Ph svg={sunIcon} size={15} />
      <span>{$t('notes.today')}</span>
    </button>
    <button type="button" class="action" on:click={() => dispatch('template')}>
      <Ph svg={templateIcon} size={15} />
      <span>{$t('notes.fromTemplate')}</span>
    </button>
    <button type="button" class="action" class:on={graphOpen} aria-pressed={graphOpen} on:click={() => dispatch('graph')}>
      <Ph svg={graphIcon} size={15} />
      <span>{$t('notes.graph')}</span>
    </button>
  </div>

  <div class="scroll">
    <section class="group">
      <button type="button" class="group-head" aria-expanded={showDaily} on:click={() => (showDaily = !showDaily)}>
        <Ph svg={showDaily ? caretDown : caretRight} size={12} />
        <span>{$t('notes.daily')}</span>
      </button>
      {#if showDaily}
        <div class="month">
          <MiniMonth value={day} {weekStart} {busy} on:pick={(e) => dispatch('day', e.detail)} />
        </div>
      {/if}
    </section>

    <button type="button" class="row top" class:selected={isScope({ kind: 'all' })} on:click={() => dispatch('scope', { kind: 'all' })}>
      <Ph svg={noteIcon} size={15} />
      <span class="name">{$t('notes.allNotes')}</span>
      <span class="count">{$vaultNotes.length}</span>
    </button>
    <button type="button" class="row top" class:selected={isScope({ kind: 'starred' })} on:click={() => dispatch('scope', { kind: 'starred' })}>
      <Ph svg={starIcon} size={15} />
      <span class="name">{$t('notes.starred')}</span>
      {#if starred.length}<span class="count">{starred.length}</span>{/if}
    </button>

    {#if starred.length}
      <ul class="notes" aria-label={$t('notes.starred')}>
        {#each starred as p (p)}
          <li>
            <button
              type="button"
              class="row note"
              class:current={p === active}
              draggable="true"
              on:dragstart={(e) => onDragStart(e, p)}
              on:click={() => dispatch('open', p)}
              on:contextmenu|preventDefault={(e) => dispatch('noteMenu', { path: p, x: e.clientX, y: e.clientY })}
            >
              <span class="name">{noteTitle(p)}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}

    {#if recent.length}
      <section class="group">
        <button type="button" class="group-head" aria-expanded={showRecent} on:click={() => (showRecent = !showRecent)}>
          <Ph svg={showRecent ? caretDown : caretRight} size={12} />
          <span>{$t('notes.recent')}</span>
        </button>
        {#if showRecent}
          <ul class="notes">
            {#each recent as p (p)}
              <li>
                <button type="button" class="row note" class:current={p === active} on:click={() => dispatch('open', p)}>
                  <Ph svg={recentIcon} size={13} />
                  <span class="name">{noteTitle(p)}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    <section class="group">
      <div class="group-head static">
        <span>{$t('notes.folders')}</span>
      </div>
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div
        class="drop-root"
        class:over={dragOver === ''}
        on:dragover={(e) => onDragOver(e, '')}
        on:dragleave={() => (dragOver = null)}
        on:drop={(e) => onDrop(e, '')}
      >
        <ul class="tree" aria-label={$t('notes.folders')}>
          {#each rows as row (row.path)}
            <li>
              <div
                class="row folder"
                class:selected={scope.kind === 'folder' && scope.path === row.path}
                class:over={dragOver === row.path}
                style={`--depth:${row.depth}`}
                role="presentation"
                on:dragover={(e) => onDragOver(e, row.path)}
                on:dragleave={() => dragOver === row.path && (dragOver = null)}
                on:drop|stopPropagation={(e) => onDrop(e, row.path)}
              >
                <button type="button" class="twisty" tabindex="-1" aria-hidden="true" class:hidden={!row.hasChildren} on:click={() => toggle(row.path)}>
                  <Ph svg={expanded.has(row.path) ? caretDown : caretRight} size={11} />
                </button>
                <button
                  type="button"
                  class="folder-btn"
                  draggable="true"
                  aria-current={currentFolder === row.path ? 'true' : undefined}
                  aria-expanded={row.hasChildren ? expanded.has(row.path) : undefined}
                  on:keydown={(e) => onFolderKey(e, row.path, row.hasChildren)}
                  on:dragstart={(e) => onDragStart(e, row.path)}
                  on:click={() => dispatch('scope', { kind: 'folder', path: row.path })}
                  on:dblclick={() => toggle(row.path)}
                  on:contextmenu|preventDefault={(e) => dispatch('folderMenu', { path: row.path, x: e.clientX, y: e.clientY })}
                >
                  <Ph svg={expanded.has(row.path) && row.hasChildren ? folderOpenIcon : folderIcon} size={15} />
                  <span class="name">{row.name}</span>
                  <span class="count">{counts.get(row.path) ?? 0}</span>
                </button>
              </div>
            </li>
          {/each}
        </ul>
        {#if rows.length === 0}
          <EmptyState
            size="compact"
            title={$t('notes.noFolders')}
            actionLabel={$t('notes.newFolder')}
            onAction={() => dispatch('newFolder', '')}
          />
        {/if}
      </div>
    </section>

    {#if $noteTags.length}
      <section class="group">
        <button type="button" class="group-head" aria-expanded={showTags} on:click={() => (showTags = !showTags)}>
          <Ph svg={showTags ? caretDown : caretRight} size={12} />
          <span>{$t('notes.tags')}</span>
        </button>
        {#if showTags}
          <ul class="tags">
            {#each $noteTags as tag (tag.tag)}
              <li>
                <button
                  type="button"
                  class="row tag"
                  class:selected={scope.kind === 'tag' && scope.tag.toLowerCase() === tag.tag.toLowerCase()}
                  style={`--depth:${tag.tag.split('/').length - 1}`}
                  on:click={() => dispatch('scope', { kind: 'tag', tag: tag.tag })}
                >
                  <Ph svg={hashIcon} size={13} />
                  <span class="name">{tag.tag.split('/').pop()}</span>
                  <span class="count">{tag.count}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    {#if projectRows.length}
      <section class="group">
        <button type="button" class="group-head" aria-expanded={showProjects} on:click={() => (showProjects = !showProjects)}>
          <Ph svg={showProjects ? caretDown : caretRight} size={12} />
          <span>{$t('notes.projects')}</span>
        </button>
        {#if showProjects}
          <ul class="tags" aria-label={$t('notes.projects')}>
            {#each projectRows as row (row.project.id)}
              <li>
                <button
                  type="button"
                  class="row tag project"
                  class:selected={scope.kind === 'project' && scope.uid === row.project.uid}
                  style={`--depth:0;--project-color:${projectColor(row.project)}`}
                  on:click={() => dispatch('scope', { kind: 'project', uid: row.project.uid, name: row.project.name })}
                >
                  <Ph svg={kanbanIcon} size={13} />
                  <span class="name">{row.project.name}</span>
                  <span class="count">{row.count}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}
  </div>
</nav>

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    width: var(--cal-sidebar-width);
    height: 100%;
    min-height: 0;
    font-size: var(--fz-list);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-3) var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .vault {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    min-width: 0;
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .vault-name,
  .name {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .actions {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .action {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    height: calc(var(--control-height) - 4px);
    padding: 0 var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .action:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .action.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-2) var(--space-3) var(--space-5);
  }

  .group {
    margin-top: var(--space-3);
  }

  .group-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2) var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font: inherit;
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    cursor: var(--cursor-action);
  }

  .group-head.static {
    cursor: default;
    padding-inline-start: var(--space-2);
  }

  .month {
    padding: var(--space-1) 0 var(--space-2);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: calc(var(--control-height) - 6px);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .row:hover,
  .folder-btn:hover {
    background: var(--surface-hover);
  }

  .row.selected,
  .row.selected .folder-btn {
    background: var(--selection-bg);
  }

  .row.over {
    outline: var(--hairline) solid var(--accent);
    background: var(--selection-bg);
  }

  .row.note {
    padding-inline-start: calc(var(--space-6) + var(--space-1));
    color: var(--text-secondary);
  }

  .row.note.current {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .row.note :global(.ph) {
    color: var(--text-tertiary);
  }

  .row :global(.ph) {
    color: var(--text-secondary);
  }

  .row.folder {
    padding: 0;
    padding-inline-start: calc(var(--depth) * var(--space-4));
    gap: 0;
  }

  .row.tag {
    padding-inline-start: calc(var(--space-2) + var(--depth) * var(--space-4));
  }

  .row.project :global(.ph) {
    color: var(--project-color);
  }

  .twisty {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--space-5);
    height: var(--space-5);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    flex-shrink: 0;
  }

  .twisty.hidden {
    visibility: hidden;
  }

  .folder-btn {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    min-width: 0;
    min-height: calc(var(--control-height) - 6px);
    padding: 0 var(--space-2) 0 var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .count {
    margin-inline-start: auto;
    padding-inline-start: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .drop-root {
    border-radius: var(--radius-control);
    min-height: var(--control-height);
  }

  .drop-root.over {
    outline: var(--hairline) dashed var(--accent);
  }
</style>
