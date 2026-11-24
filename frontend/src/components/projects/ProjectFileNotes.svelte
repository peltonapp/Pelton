<script lang="ts">
  // a project's notes as files in the notes folder: the notes whose front
  // matter puts them in the project, and under them the notes that only link
  // to it, its cards, mail or events. the chosen note opens in the Notes
  // editor right here; it can also open in Notes, leave the project (the file
  // stays) or go to the trash of the notes folder. a new note starts from the
  // "Project" template, and any note of the folder can join. in a narrow
  // window the list and the editor take turns.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { get } from 'svelte/store'
  import { fade } from 'svelte/transition'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import backIcon from '@phosphor-icons/core/assets/duotone/caret-left-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/arrow-square-out-duotone.svg?raw'
  import linkIcon from '@phosphor-icons/core/assets/duotone/link-simple-duotone.svg?raw'
  import unlinkIcon from '@phosphor-icons/core/assets/duotone/link-simple-break-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-pencil-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import NoteEditor from '../notes/NoteEditor.svelte'
  import NoteSwitcher from '../notes/NoteSwitcher.svelte'
  import type { FollowedLink } from '../notes/editor/context'
  import { addNoteToProject, trashProjectNote, vaultUndo } from '../../lib/api'
  import { onProjectsChanged, onVaultChanged } from '../../lib/events'
  import { noteEditorContext } from '../../lib/noteeditorcontext'
  import { resolveLink, resolveMarkdownLink } from '../../lib/noteresolve'
  import { projectNotes, type ProjectNote } from '../../lib/projectnotes'
  import { openPeltonUrl } from '../../lib/peltonopen'
  import { openURL } from '../../lib/browser'
  import { bodyFontStack } from '../../lib/fonts'
  import {
    closeNote,
    noteResolver,
    notesByPath,
    noteTags,
    openNote,
    projectNoteText,
    recentNotes,
    reloadVault,
    startNotes,
    vaultIndex,
    vaultNotes,
  } from '../../stores/notes'
  import { openSpace } from '../../stores/space'
  import { prefs } from '../../stores/prefs'
  import { askConfirm } from '../../stores/confirm'
  import { errorMessage, pushActions, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { Project } from '../../lib/types'

  /** The project whose notes these are. */
  export let project: Project
  /** A note to open, by path, as a card's link asks. */
  export let focusId: string | null = null

  const dispatch = createEventDispatcher<{ focused: void }>()
  const narrow = 640

  let notes: ProjectNote[] = []
  let linked: ProjectNote[] = []
  let loaded = false
  let loadedFor = 0
  let selected: string | null = null
  let width = 0
  let creating = false
  let newTitle = ''
  let picking = false
  let editor: NoteEditor | undefined
  let revision = 0
  let reloadTimer: ReturnType<typeof setTimeout> | null = null
  let stopNotes: (() => void) | undefined

  $: folded = width > 0 && width < narrow
  $: members = new Set(notes.map((n) => n.path))
  $: current = notes.find((n) => n.path === selected) ?? linked.find((n) => n.path === selected) ?? null
  $: member = current !== null && members.has(current.path)
  $: candidates = $vaultNotes.filter((n) => !members.has(n.path))
  $: sections = [
    { key: 'notes', label: $t('projects.tab.notes'), list: notes },
    { key: 'linked', label: $t('projects.notes.linked'), list: linked },
  ].filter((s) => s.list.length > 0)
  $: noteFont = bodyFontStack($prefs.bodyFont) ?? 'var(--font-ui)'
  $: noteWidth = $prefs.notesLineWidth === 'narrow' ? 'var(--reader-width-narrow)' : $prefs.notesLineWidth === 'medium' ? 'var(--reader-width-medium)' : '100%'
  $: context = noteEditorContext(
    { notes: $vaultNotes, files: $vaultIndex?.files ?? [], resolver: $noteResolver, tags: $noteTags, byPath: $notesByPath },
    { active: selected, headings: currentHeadings, follow: (link, newTab) => void follow(link, newTab), t: $t },
  )
  $: if (project.id !== loadedFor) {
    loadedFor = project.id
    loaded = false
    selected = null
    void load()
  }
  $: if (focusId && loaded) {
    const path = focusId
    dispatch('focused')
    void focus(path)
  }

  function currentHeadings(): { level: number; text: string }[] {
    return selected ? ($notesByPath.get(selected)?.headings ?? []) : []
  }

  async function load(): Promise<void> {
    const id = project.id
    try {
      const list = await projectNotes().list(id)
      if (id !== project.id) {
        return
      }
      notes = list.notes
      linked = list.linked
      if (selected !== null && !notes.some((n) => n.path === selected) && !linked.some((n) => n.path === selected)) {
        selected = null
      }
      if (selected === null && !folded && notes.length > 0) {
        selected = notes[0].path
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loaded = true
    }
  }

  function scheduleLoad(): void {
    if (reloadTimer) {
      clearTimeout(reloadTimer)
    }
    reloadTimer = setTimeout(() => {
      reloadTimer = null
      void load()
    }, 200)
  }

  onMount(() => {
    stopNotes = startNotes()
    if (!get(vaultIndex)) {
      void reloadVault()
    }
  })

  const offProjects = onProjectsChanged((e) => {
    if (e.projectIds.length === 0 || e.projectIds.includes(project.id)) {
      scheduleLoad()
    }
  })
  const offVault = onVaultChanged(() => {
    revision += 1
    scheduleLoad()
  })

  onDestroy(() => {
    offProjects()
    offVault()
    stopNotes?.()
    if (reloadTimer) {
      clearTimeout(reloadTimer)
    }
  })

  async function select(path: string): Promise<void> {
    if (path === selected) {
      return
    }
    await editor?.flush()
    selected = path
  }

  async function focus(path: string): Promise<void> {
    if (members.has(path) || linked.some((n) => n.path === path)) {
      await select(path)
      return
    }
    openNote(path, { newTab: true })
    openSpace('notes')
  }

  async function follow(link: FollowedLink, newTab: boolean): Promise<void> {
    if (link.kind === 'url') {
      if (!(await openPeltonUrl(link.target)) && /^(https?|mailto):/i.test(link.target)) {
        openURL(link.target)
      }
      return
    }
    if (link.kind === 'tag') {
      openSpace('notes')
      return
    }
    const from = selected ?? ''
    const target = link.kind === 'markdown' ? resolveMarkdownLink($noteResolver, link.target, from) : resolveLink($noteResolver, link.target, from)
    if (!target || !/\.md$/i.test(target)) {
      return
    }
    if (!newTab && (members.has(target) || linked.some((n) => n.path === target))) {
      await select(target)
      return
    }
    await openInNotes(target)
  }

  async function openInNotes(path: string): Promise<void> {
    await editor?.flush()
    openNote(path, { newTab: true })
    openSpace('notes')
  }

  async function create(): Promise<void> {
    const title = newTitle.trim() || $t('notes.untitled')
    creating = false
    newTitle = ''
    try {
      const body = await projectNoteText(project, title)
      const note = await projectNotes().create(project.id, title, body)
      await reloadVault()
      await load()
      if (note.path) {
        await select(note.path)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function startCreate(): Promise<void> {
    creating = true
    await tick()
    document.querySelector<HTMLInputElement>('.file-notes .new-title')?.focus()
  }

  async function addExisting(path: string): Promise<void> {
    picking = false
    try {
      await addNoteToProject(path, project.id)
      await load()
      await select(path)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function leave(note: ProjectNote): Promise<void> {
    await editor?.flush()
    try {
      await projectNotes().remove(note)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function trash(note: ProjectNote): Promise<void> {
    const ok = await askConfirm({
      title: $t('projects.notes.trashTitle').replace('{name}', note.title),
      body: $t('projects.notes.trashBody'),
      confirmLabel: $t('projects.notes.trash'),
      danger: true,
    })
    if (!ok) {
      return
    }
    await editor?.flush()
    try {
      const op = await trashProjectNote(note.path, project.id)
      closeNote(note.path)
      selected = null
      await load()
      pushActions('success', $t('notes.toast.deleted'), [{ label: $t('notes.undo'), run: () => void undoTrash(op.undoId) }], 8000)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function undoTrash(id: string): Promise<void> {
    try {
      await vaultUndo(id)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function updated(iso: string): string {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
  }

  async function back(): Promise<void> {
    await editor?.flush()
    selected = null
  }
</script>

<section class="notes file-notes" aria-label={$t('projects.tab.notes')} bind:clientWidth={width} style={`--note-font:${noteFont};--note-width:${noteWidth}`}>
  {#if !folded || selected === null}
    <div class="list" class:full={folded}>
      <div class="list-head">
        <span class="count">{$t('projects.notes.count').replace('{n}', String(notes.length))}</span>
        <span class="spacer"></span>
        <button type="button" class="cal-icon" aria-label={$t('projects.notes.add')} title={$t('projects.notes.add')} on:click={() => (picking = true)}>
          <Ph svg={linkIcon} size={15} />
        </button>
        <button type="button" class="cal-btn" on:click={() => void startCreate()}><Ph svg={plusIcon} size={12} />{$t('projects.notes.new')}</button>
      </div>
      {#if creating}
        <form class="new" on:submit|preventDefault={() => void create()}>
          <input
            class="cal-field new-title"
            bind:value={newTitle}
            placeholder={$t('projects.notes.titlePlaceholder')}
            aria-label={$t('projects.notes.title')}
            on:keydown={(e) => e.key === 'Escape' && ((creating = false), (newTitle = ''))}
          />
        </form>
      {/if}
      {#if !loaded}
        <div class="loading"><PieProgress label={$t('projects.loading')} /></div>
      {:else if sections.length === 0}
        <EmptyState
          icon={noteIcon}
          title={$t('projects.notes.emptyTitle')}
          detail={$t('projects.notes.filesEmpty')}
          actionLabel={$t('projects.notes.new')}
          onAction={() => void startCreate()}
          secondaryLabel={$t('projects.notes.add')}
          onSecondary={() => (picking = true)}
        />
      {:else}
        <div class="scroll">
          {#each sections as section (section.key)}
            {#if section.key === 'linked'}
              <h3 class="section">{section.label}</h3>
            {/if}
            <ul role="listbox" aria-label={section.label}>
              {#each section.list as note (note.path)}
                <li role="option" aria-selected={note.path === selected}>
                  <button type="button" class="note" class:on={note.path === selected} title={note.path} on:click={() => void select(note.path)}>
                    <span class="n-title">{note.title}</span>
                    <span class="n-meta">
                      <span class="n-date">{updated(note.updatedAt)}</span>
                      <span class="n-text">{note.snippet}</span>
                    </span>
                  </button>
                </li>
              {/each}
            </ul>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  {#if current && (!folded || selected !== null)}
    <div class="editor" in:fade={{ duration: dur(motion.fast) }}>
      <div class="ed-head">
        {#if folded}
          <button type="button" class="cal-icon" aria-label={$t('projects.notes.back')} on:click={() => void back()}><Ph svg={backIcon} size={15} /></button>
        {/if}
        <h3 class="ed-title" title={current.path}>{current.title}</h3>
        {#if !member}
          <span class="state">{$t('projects.notes.linksHere')}</span>
        {/if}
        <span class="spacer"></span>
        <button type="button" class="cal-icon" aria-label={$t('projects.notes.openInNotes')} title={$t('projects.notes.openInNotes')} on:click={() => current && void openInNotes(current.path)}>
          <Ph svg={openIcon} size={15} />
        </button>
        {#if member}
          <button type="button" class="cal-icon" aria-label={$t('projects.notes.leave')} title={$t('projects.notes.leave')} on:click={() => current && void leave(current)}>
            <Ph svg={unlinkIcon} size={15} />
          </button>
        {:else}
          <button type="button" class="cal-icon" aria-label={$t('projects.notes.join')} title={$t('projects.notes.join')} on:click={() => current && void addExisting(current.path)}>
            <Ph svg={linkIcon} size={15} />
          </button>
        {/if}
        <button type="button" class="cal-icon" aria-label={$t('projects.notes.trash')} title={$t('projects.notes.trash')} on:click={() => current && void trash(current)}>
          <Ph svg={trashIcon} size={15} />
        </button>
      </div>
      <div class="ed-body">
        {#key current.path}
          <NoteEditor bind:this={editor} path={current.path} mode={$prefs.notesEditorMode} {context} {revision} on:gone={() => void load()} />
        {/key}
      </div>
    </div>
  {:else if loaded && sections.length > 0 && !folded}
    <div class="editor pick"><p class="blank">{$t('projects.notes.pick')}</p></div>
  {/if}
</section>

{#if picking}
  <NoteSwitcher
    notes={candidates}
    recent={$recentNotes}
    allowCreate={false}
    label={$t('projects.notes.add')}
    placeholder={$t('notes.switcher.placeholder')}
    on:pick={(e) => void addExisting(e.detail.path)}
    on:close={() => (picking = false)}
  />
{/if}

<style>
  .notes {
    display: flex;
    height: 100%;
    min-width: 0;
  }

  .list {
    display: flex;
    flex-direction: column;
    flex: 0 0 var(--project-notes-width);
    min-width: 0;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .list.full {
    flex: 1;
    border: none;
  }

  .list-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .spacer {
    flex: 1;
  }

  .count {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .new {
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .new .cal-field {
    width: 100%;
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-5);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-2);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .section {
    margin: var(--space-4) var(--space-3) var(--space-2);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .note {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .note:hover {
    background: var(--surface-hover);
  }

  .note.on {
    background: var(--selection-bg);
  }

  .n-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .n-meta {
    display: flex;
    gap: var(--space-2);
    min-width: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .n-date {
    flex-shrink: 0;
  }

  .n-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .editor {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }

  .editor.pick {
    align-items: center;
    justify-content: center;
  }

  .ed-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .ed-title {
    min-width: 0;
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .state {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .ed-body {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .ed-body :global(.note-editor) {
    flex: 1;
    min-height: 0;
  }

  .blank {
    margin: var(--space-6);
    font-size: var(--fz-list);
    color: var(--text-tertiary);
  }
</style>
