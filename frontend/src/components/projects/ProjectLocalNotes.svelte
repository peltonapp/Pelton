<script lang="ts">
  // a project's notes while no notes folder is open: they are kept in the
  // database, listed beside a plain Markdown editor with a preview. edits save
  // on their own a moment after typing stops and when the note is left. in a
  // narrow window the list and the editor take turns.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import { fade } from 'svelte/transition'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import backIcon from '@phosphor-icons/core/assets/duotone/caret-left-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-pencil-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import MarkdownPreview from '../compose/MarkdownPreview.svelte'
  import { noteExcerpt, noteTitle, projectNotes, type ProjectNote } from '../../lib/projectnotes'
  import { onProjectsChanged } from '../../lib/events'
  import { askConfirm } from '../../stores/confirm'
  import { errorMessage, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { Project } from '../../lib/types'

  /** The project whose notes these are. */
  export let project: Project
  /** A note to open, by id, as a card's link asks. */
  export let focusId: string | null = null

  const dispatch = createEventDispatcher<{ focused: void }>()
  const narrow = 640
  const saveDelay = 600

  let notes: ProjectNote[] = []
  let loaded = false
  let loadedFor = 0
  let openId: string | null = null
  let title = ''
  let body = ''
  let preview = false
  let saving = false
  let width = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  let bodyEl: HTMLTextAreaElement

  $: folded = width > 0 && width < narrow
  $: current = notes.find((n) => n.id === openId) ?? null
  $: dirty = current !== null && (title !== current.title || body !== current.body)
  $: if (project.id !== loadedFor) {
    loadedFor = project.id
    loaded = false
    openId = null
    void load()
  }
  $: if (focusId && loaded) {
    const id = focusId
    dispatch('focused')
    if (notes.some((n) => n.id === id)) {
      void open(id)
    }
  }

  async function load(): Promise<void> {
    const id = project.id
    try {
      const next = await projectNotes().list(id)
      if (id !== project.id) {
        return
      }
      notes = next.notes
      if (openId === null && !folded && notes.length > 0) {
        select(notes[0])
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loaded = true
    }
  }

  const off = onProjectsChanged((e) => {
    if ((e.projectIds.length === 0 || e.projectIds.includes(project.id)) && !dirty) {
      void load()
    }
  })

  onDestroy(() => {
    off()
    void flush()
  })

  function select(note: ProjectNote): void {
    openId = note.id
    title = note.title
    body = note.body
    preview = false
  }

  async function open(id: string): Promise<void> {
    await flush()
    const note = notes.find((n) => n.id === id)
    if (note) {
      select(note)
    }
  }

  function edited(): void {
    if (timer) {
      clearTimeout(timer)
    }
    timer = setTimeout(() => void flush(), saveDelay)
  }

  async function flush(): Promise<void> {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    const note = current
    if (!note || (title === note.title && body === note.body)) {
      return
    }
    saving = true
    try {
      const saved = await projectNotes().save({ ...note, title, body })
      notes = [saved, ...notes.filter((n) => n.id !== saved.id)]
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      saving = false
    }
  }

  async function create(): Promise<void> {
    await flush()
    try {
      const note = await projectNotes().create(project.id, '', '')
      notes = [note, ...notes]
      select(note)
      await tick()
      document.querySelector<HTMLInputElement>('.notes .note-title')?.focus()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function remove(note: ProjectNote): Promise<void> {
    const ok = await askConfirm({
      title: $t('projects.notes.deleteTitle').replace('{name}', noteTitle(note, $t('projects.notes.untitled'))),
      body: $t('projects.notes.deleteBody'),
      confirmLabel: $t('projects.notes.delete'),
      danger: true,
    })
    if (!ok) {
      return
    }
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    const at = notes.findIndex((n) => n.id === note.id)
    try {
      await projectNotes().remove(note)
      notes = notes.filter((n) => n.id !== note.id)
      const next = notes[Math.min(at, notes.length - 1)]
      if (next && !folded) {
        select(next)
      } else {
        openId = null
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function updated(iso: string): string {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
  }

  async function back(): Promise<void> {
    await flush()
    openId = null
  }
</script>

<section class="notes" aria-label={$t('projects.tab.notes')} bind:clientWidth={width}>
  {#if !folded || openId === null}
    <div class="list" class:full={folded}>
      <div class="list-head">
        <span class="count">{$t('projects.notes.count').replace('{n}', String(notes.length))}</span>
        <button type="button" class="cal-btn" on:click={() => void create()}><Ph svg={plusIcon} size={12} />{$t('projects.notes.new')}</button>
      </div>
      {#if !loaded}
        <div class="loading"><PieProgress label={$t('projects.loading')} /></div>
      {:else if notes.length === 0}
        <EmptyState title={$t('projects.notes.emptyTitle')} detail={$t('projects.notes.emptyDetail')}>
          <Ph svg={noteIcon} size={26} />
        </EmptyState>
      {:else}
        <ul role="listbox" aria-label={$t('projects.tab.notes')}>
          {#each notes as note (note.id)}
            <li role="option" aria-selected={note.id === openId}>
              <button type="button" class="note" class:on={note.id === openId} on:click={() => void open(note.id)}>
                <span class="n-title">{note.id === openId && title.trim() ? title : noteTitle(note, $t('projects.notes.untitled'))}</span>
                <span class="n-meta">
                  <span class="n-date">{updated(note.updatedAt)}</span>
                  <span class="n-text">{noteExcerpt(note.id === openId ? body : note.body)}</span>
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}

  {#if current && (!folded || openId !== null)}
    <div class="editor" in:fade={{ duration: dur(motion.fast) }}>
      <div class="ed-head">
        {#if folded}
          <button type="button" class="cal-icon" aria-label={$t('projects.notes.back')} on:click={() => void back()}><Ph svg={backIcon} size={15} /></button>
        {/if}
        <input
          class="note-title"
          bind:value={title}
          placeholder={$t('projects.notes.titlePlaceholder')}
          aria-label={$t('projects.notes.title')}
          on:input={edited}
          on:blur={() => void flush()}
          on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), bodyEl?.focus())}
        />
        {#if saving}
          <PieProgress label={$t('projects.notes.saving')} />
        {:else if dirty}
          <span class="state">{$t('projects.notes.unsaved')}</span>
        {/if}
        <div class="seg" role="radiogroup" aria-label={$t('projects.notes.mode')}>
          <button type="button" role="radio" aria-checked={!preview} class:on={!preview} on:click={() => (preview = false)}>{$t('projects.notes.write')}</button>
          <button type="button" role="radio" aria-checked={preview} class:on={preview} on:click={() => void flush().then(() => (preview = true))}>{$t('projects.notes.preview')}</button>
        </div>
        <button type="button" class="cal-icon" aria-label={$t('projects.notes.delete')} title={$t('projects.notes.delete')} on:click={() => current && void remove(current)}>
          <Ph svg={trashIcon} size={15} />
        </button>
      </div>
      {#if preview}
        <div class="preview">
          {#if body.trim()}
            <MarkdownPreview markdown={body} font="" />
          {:else}
            <p class="blank">{$t('projects.notes.nothing')}</p>
          {/if}
        </div>
      {:else}
        <textarea
          class="body"
          bind:this={bodyEl}
          bind:value={body}
          placeholder={$t('projects.notes.bodyPlaceholder')}
          aria-label={$t('projects.notes.body')}
          spellcheck="true"
          on:input={edited}
          on:blur={() => void flush()}
        ></textarea>
      {/if}
    </div>
  {:else if loaded && notes.length > 0 && !folded}
    <div class="editor pick"><p class="blank">{$t('projects.notes.pick')}</p></div>
  {/if}
</section>

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
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .count {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-5);
  }

  ul {
    flex: 1;
    margin: 0;
    padding: var(--space-2);
    list-style: none;
    overflow-y: auto;
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

  .note-title {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    outline: none;
  }

  .note-title::placeholder {
    color: var(--text-tertiary);
  }

  .state {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .seg {
    display: inline-flex;
    padding: 1px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
  }

  .seg button {
    padding: 0 var(--space-3);
    height: calc(var(--control-height) - 8px);
    border: none;
    border-radius: calc(var(--radius-control) - 1px);
    background: transparent;
    color: var(--text-tertiary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .seg button.on {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .body {
    flex: 1;
    min-height: 0;
    padding: var(--space-4);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-family: var(--font-mono);
    font-size: var(--fz-list);
    line-height: 1.6;
    resize: none;
    outline: none;
  }

  .body::placeholder {
    color: var(--text-tertiary);
  }

  .preview {
    display: flex;
    flex: 1;
    min-height: 0;
  }

  .preview :global(iframe) {
    flex: 1;
  }

  .blank {
    margin: var(--space-6);
    font-size: var(--fz-list);
    color: var(--text-tertiary);
  }
</style>
