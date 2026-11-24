<script lang="ts">
  // the middle pane of the notes space: the notes of the folder or tag the
  // sidebar chose, with their opening words and when they changed. typing in
  // the filter searches the text of every note in the scope. rows can be
  // dragged onto a folder in the sidebar.
  import { createEventDispatcher, tick } from 'svelte'
  import sidebarIcon from '@phosphor-icons/core/assets/duotone/sidebar-simple-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import starIcon from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import { noteSorts, type NoteSort } from '../../lib/notelist'
  import { noteFolder } from '../../lib/notemd'
  import { formatTime } from '../../lib/calformat'
  import type { TimeFormat } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { starredNotes } from '../../stores/notes'
  import { t } from '../../lib/i18n'
  import type { VaultNote } from '../../lib/types'

  /** The notes to list, in order. */
  export let notes: VaultNote[]
  /** The heading over the list. */
  export let title: string
  /** The note open in the editor. */
  export let active: string | null = null
  /** The text of the filter field. */
  export let query = ''
  /** The order. */
  export let sort: NoteSort = 'modified'
  /** Lines that matched a text search, by path. */
  export let contexts: Map<string, string> = new Map()
  /** A text search is running. */
  export let searching = false
  /** Shows the button that opens the sidebar drawer. */
  export let showSidebarButton = false
  /** Whether the sidebar drawer is open. */
  export let sidebarOpen = false

  const dispatch = createEventDispatcher<{
    open: { path: string; newTab: boolean }
    menu: { path: string; x: number; y: number }
    query: string
    sort: NoteSort
    newNote: void
    sidebar: void
  }>()

  let filterInput: HTMLInputElement
  let listEl: HTMLUListElement

  $: sortItems = noteSorts.map((s) => ({ value: s, label: $t(`notes.sort.${s}`) }))
  $: timeFormat = $prefs.timeFormat as TimeFormat

  function when(ms: number): string {
    const d = new Date(ms)
    const today = new Date()
    if (d.toDateString() === today.toDateString()) {
      return formatTime(d, timeFormat)
    }
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: d.getFullYear() === today.getFullYear() ? undefined : 'numeric' })
  }

  /** Puts the keyboard in the filter field. */
  export function focusFilter(): void {
    filterInput?.focus()
    filterInput?.select()
  }

  async function move(delta: number): Promise<void> {
    if (!notes.length) {
      return
    }
    const i = notes.findIndex((n) => n.path === active)
    const next = notes[Math.max(0, Math.min(notes.length - 1, i < 0 ? 0 : i + delta))]
    dispatch('open', { path: next.path, newTab: false })
    await tick()
    listEl?.querySelector<HTMLElement>(`[data-note-row="${CSS.escape(next.path)}"]`)?.focus()
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      void move(1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      void move(-1)
    }
  }

  function onDragStart(e: DragEvent, path: string): void {
    e.dataTransfer?.setData('application/x-pelton-note', path)
    e.dataTransfer?.setData('text/plain', `[[${path.replace(/\.md$/i, '')}]]`)
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'copyMove'
    }
  }
</script>

<section class="list" aria-label={title}>
  <header class="bar">
    {#if showSidebarButton}
      <button type="button" class="cal-icon" aria-label={$t('notes.sidebar')} aria-expanded={sidebarOpen} on:click={() => dispatch('sidebar')}>
        <Ph svg={sidebarIcon} size={16} />
      </button>
    {/if}
    <h2 class="title">{title}</h2>
    <span class="spacer"></span>
    <button type="button" class="cal-icon" aria-label={$t('notes.newNote')} title={$t('notes.newNote')} on:click={() => dispatch('newNote')}>
      <Ph svg={plusIcon} size={16} />
    </button>
  </header>
  <div class="tools">
    <input
      class="cal-field filter"
      type="search"
      bind:this={filterInput}
      value={query}
      placeholder={$t('notes.filter')}
      aria-label={$t('notes.filter')}
      on:input={(e) => dispatch('query', e.currentTarget.value)}
      on:keydown={(e) => {
        if (e.key === 'Escape' && query) {
          e.stopPropagation()
          dispatch('query', '')
        } else if (e.key === 'ArrowDown') {
          e.preventDefault()
          void move(0)
        }
      }}
    />
    {#if searching}
      <PieProgress label={$t('notes.searching')} />
    {/if}
    <Select class="sort" value={sort} items={sortItems} ariaLabel={$t('notes.sortLabel')} on:change={(e) => dispatch('sort', e.detail as NoteSort)} />
  </div>

  {#if notes.length === 0}
    {#if query.trim()}
      <EmptyState
        kind="filtered"
        icon={searchIcon}
        title={$t('notes.noMatches')}
        detail={$t('notes.noMatchesDetail')}
        actionLabel={$t('common.clearSearch')}
        onAction={() => dispatch('query', '')}
      />
    {:else}
      <EmptyState
        icon={noteIcon}
        title={$t('notes.emptyList')}
        detail={$t('notes.emptyListDetail')}
        actionLabel={$t('notes.newNote')}
        onAction={() => dispatch('newNote')}
        shortcut="notes-new"
      />
    {/if}
  {:else}
    <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
    <ul class="rows" bind:this={listEl} role="listbox" aria-label={title} on:keydown={onKeydown}>
      {#each notes as note (note.path)}
        <li role="presentation">
          <button
            type="button"
            class="row"
            class:active={note.path === active}
            role="option"
            aria-selected={note.path === active}
            data-note-row={note.path}
            tabindex={note.path === active || (!active && note === notes[0]) ? 0 : -1}
            draggable="true"
            on:dragstart={(e) => onDragStart(e, note.path)}
            on:click={(e) => dispatch('open', { path: note.path, newTab: e.metaKey || e.ctrlKey })}
            on:auxclick={(e) => e.button === 1 && dispatch('open', { path: note.path, newTab: true })}
            on:contextmenu|preventDefault={(e) => dispatch('menu', { path: note.path, x: e.clientX, y: e.clientY })}
          >
            <span class="line1">
              <span class="name">{note.title}</span>
              {#if $starredNotes.includes(note.path)}
                <span class="star" aria-label={$t('notes.starred')}><Ph svg={starIcon} size={12} /></span>
              {/if}
              <span class="date">{when(note.mtime)}</span>
            </span>
            <span class="snippet">{contexts.get(note.path) ?? note.snippet}</span>
            {#if noteFolder(note.path)}
              <span class="folder">{noteFolder(note.path)}</span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .list {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
    min-height: 0;
    background: var(--surface-raised);
  }

  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-3) var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    margin: 0;
    min-width: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .spacer {
    flex: 1;
  }

  .tools {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .filter {
    flex: 1;
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .rows {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    list-style: none;
    margin: 0;
    padding: var(--space-1) 0;
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: 100%;
    padding: var(--row-pad-y) var(--row-pad-x);
    border: none;
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row.active {
    background: var(--selection-bg);
  }

  .row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .line1 {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    min-width: 0;
  }

  .name {
    flex: 1;
    min-width: 0;
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .star {
    display: inline-flex;
    color: var(--warning);
  }

  .date {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .snippet {
    font-size: var(--fz-label);
    color: var(--text-secondary);
    line-height: var(--list-line-height);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .folder {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

</style>
