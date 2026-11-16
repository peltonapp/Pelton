<script lang="ts" context="module">
  // where each note was left, so switching tabs comes back to the same place.
  const places = new Map<string, { anchor: number; head: number; scroll: number }>()
</script>

<script lang="ts">
  // the editor of one note: CodeMirror with the live preview, completion and
  // editing keys of the notes space. it reads the note from disk, saves a
  // short while after the last change, and keeps the file safe from being
  // overwritten: the save says which version it was edited from, and when the
  // note changed on disk meanwhile a bar offers to keep this version or load
  // the one on disk. a clean editor simply reloads. line endings and every
  // byte outside what was typed stay as they were in the file.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { get } from 'svelte/store'
  import { Annotation, Compartment, EditorSelection, EditorState } from '@codemirror/state'
  import { EditorView, drawSelection, keymap } from '@codemirror/view'
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
  import Ph from '../common/Ph.svelte'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import { noteContext, previewMode, type NoteContext, type PreviewMode } from './editor/context'
  import { livePreview, refreshPreview } from './editor/livepreview'
  import { completionOf, acceptCompletion, closeCompletion, noteCompletion, type CompletionState } from './editor/complete'
  import { imagePaste, insertImages, noteKeymap } from './editor/keys'
  import { noteTheme } from './editor/theme'
  import { clearImageCache } from './editor/widgets'
  import { vaultRead, vaultSave, vaultImportFile } from '../../lib/api'
  import { onFilesDropped, onVaultChanged } from '../../lib/events'
  import { parseFrontMatter } from '../../lib/notemd'
  import { currentUIScale } from '../../theme/theme'
  import { errorMessage, toastError } from '../../stores/toast'
  import { noteLine } from '../../stores/notes'
  import { t } from '../../lib/i18n'

  /** The vault path of the note. */
  export let path: string
  /** How the note shows. */
  export let mode: PreviewMode = 'live'
  /** What the editor needs from the notes space, without the path. */
  export let context: Omit<NoteContext, 'path'>
  /** Bumped by the screen when the index changed, so links resolve again. */
  export let revision = 0

  const dispatch = createEventDispatcher<{ saved: { path: string }; gone: { path: string }; headings: void }>()
  const saveDelay = 700

  let host: HTMLDivElement
  let view: EditorView | null = null
  let baseHash = ''
  let baseText = ''
  let readOnly = false
  let loading = true
  let loadError = ''
  let conflict: { disk: string; diskHash: string } | null = null
  let gone = false
  let saving: Promise<void> | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  let completion: CompletionState | null = null
  let completionAt: { left: number; top: number } | null = null

  const modeCompartment = new Compartment()
  const contextCompartment = new Compartment()
  // marks a change that came from the file on disk, which needs no save.
  const fromDisk = Annotation.define<boolean>()

  $: fullContext = { ...context, path } as NoteContext

  function modeExtensions(m: PreviewMode) {
    const locked = m === 'reading' || readOnly
    return [previewMode.of(m), EditorState.readOnly.of(locked), EditorView.editable.of(!locked)]
  }

  // a file written with CRLF keeps CRLF; one with mixed endings keeps every
  // byte, carriage returns included, by splitting on LF alone.
  function lineSeparator(text: string): string {
    const crlf = (text.match(/\r\n/g) ?? []).length
    const lf = (text.match(/\n/g) ?? []).length
    return crlf > 0 && crlf === lf ? '\r\n' : '\n'
  }

  function makeState(text: string): EditorState {
    return EditorState.create({
      doc: text,
      extensions: [
        EditorState.lineSeparator.of(lineSeparator(text)),
        contextCompartment.of(noteContext.of(fullContext)),
        modeCompartment.of(modeExtensions(mode)),
        history(),
        drawSelection(),
        noteCompletion(),
        noteKeymap(),
        keymap.of([...defaultKeymap, ...historyKeymap]),
        EditorView.lineWrapping,
        EditorView.contentAttributes.of({ 'aria-label': $t('notes.editor.label'), spellcheck: 'true' }),
        livePreview(),
        imagePaste,
        noteTheme,
        EditorView.updateListener.of((u) => {
          if (u.docChanged && !u.transactions.every((tr) => tr.annotation(fromDisk))) {
            schedule()
          }
          if (u.docChanged || u.selectionSet || u.transactions.length) {
            refreshCompletion()
          }
          if (u.docChanged) {
            dispatch('headings')
          }
        }),
      ],
    })
  }

  function text(): string {
    return view ? view.state.sliceDoc() : ''
  }

  async function load(): Promise<void> {
    loading = true
    loadError = ''
    try {
      const file = await vaultRead(path)
      baseHash = file.hash
      baseText = file.content
      readOnly = file.readOnly
      await tick()
      view = new EditorView({ state: makeState(file.content), parent: host })
      restorePlace(file.content)
    } catch (err) {
      loadError = errorMessage(err)
    } finally {
      loading = false
    }
  }

  function restorePlace(content: string): void {
    if (!view) {
      return
    }
    const saved = places.get(path)
    const asked = get(noteLine)
    if (asked && asked.path === path) {
      noteLine.set(null)
      goToLine(asked.line)
      return
    }
    if (saved) {
      const len = view.state.doc.length
      view.dispatch({ selection: EditorSelection.single(Math.min(saved.anchor, len), Math.min(saved.head, len)) })
      requestAnimationFrame(() => view && (view.scrollDOM.scrollTop = saved.scroll))
      return
    }
    // a note opens below its properties, so they show as a block.
    const fm = parseFrontMatter(content)
    const doc = view.state.doc
    const at = fm && fm.lastLine + 2 <= doc.lines ? doc.line(fm.lastLine + 2).from : fm ? doc.length : 0
    view.dispatch({ selection: EditorSelection.cursor(at) })
  }

  /** Scrolls to a line, counting from 0, and puts the cursor there. */
  export function goToLine(n: number): void {
    if (!view) {
      return
    }
    const target = view.state.doc.line(Math.max(1, Math.min(n + 1, view.state.doc.lines)))
    view.dispatch({ selection: EditorSelection.cursor(target.from), effects: EditorView.scrollIntoView(target.from, { y: 'start', yMargin: 48 }) })
  }

  /** Puts the keyboard in the editor. */
  export function focus(): void {
    view?.focus()
  }

  /** Inserts text at the cursor, in place of the selection. */
  export function insertText(value: string): void {
    if (!view || view.state.readOnly) {
      return
    }
    // line breaks follow the note's own, so a CRLF file stays CRLF.
    view.dispatch(view.state.replaceSelection(view.state.toText(value.replace(/\r?\n/g, view.state.lineBreak))), { userEvent: 'input' })
    view.focus()
  }

  function schedule(): void {
    if (timer) {
      clearTimeout(timer)
    }
    timer = setTimeout(() => {
      timer = null
      void save()
    }, saveDelay)
  }

  /** Writes pending changes now. */
  export async function flush(): Promise<void> {
    if (timer) {
      clearTimeout(timer)
      timer = null
      await save()
    } else if (saving) {
      await saving
    }
  }

  async function save(force = false): Promise<void> {
    if (!view || readOnly || conflict || gone) {
      return
    }
    // the text is taken now: the editor may be gone by the time an earlier
    // save has finished.
    const content = text()
    if (saving) {
      await saving
    }
    if ((content === baseText && !force) || conflict || gone) {
      return
    }
    saving = (async () => {
      try {
        const res = await vaultSave(path, content, baseHash, force)
        if (res.conflict) {
          if (!res.diskHash && !res.disk) {
            gone = true
            return
          }
          conflict = { disk: res.disk, diskHash: res.diskHash }
          return
        }
        baseHash = res.hash
        baseText = content
        dispatch('saved', { path })
      } catch (err) {
        toastError(errorMessage(err))
      } finally {
        saving = null
      }
    })()
    await saving
  }

  function replaceDoc(content: string): void {
    if (!view) {
      return
    }
    // a fresh state, since the file may have changed its line endings too.
    const head = view.state.selection.main.head
    const scroll = view.scrollDOM.scrollTop
    view.setState(makeState(content))
    view.dispatch({ selection: EditorSelection.cursor(Math.min(head, view.state.doc.length)), annotations: fromDisk.of(true) })
    view.scrollDOM.scrollTop = scroll
    refreshCompletion()
    dispatch('headings')
  }

  // a change on disk: a clean editor takes it, a dirty one asks.
  async function diskChanged(): Promise<void> {
    if (!view || saving) {
      return
    }
    try {
      const file = await vaultRead(path)
      if (file.hash === baseHash) {
        return
      }
      if (text() === baseText && !timer) {
        baseHash = file.hash
        baseText = file.content
        replaceDoc(file.content)
        clearImageCache()
        return
      }
      conflict = { disk: file.content, diskHash: file.hash }
    } catch {
      gone = true
    }
  }

  async function keepMine(): Promise<void> {
    if (!conflict) {
      return
    }
    baseHash = conflict.diskHash
    conflict = null
    await save(true)
  }

  function loadTheirs(): void {
    if (!conflict) {
      return
    }
    baseHash = conflict.diskHash
    baseText = conflict.disk
    replaceDoc(conflict.disk)
    conflict = null
  }

  async function saveAgain(): Promise<void> {
    gone = false
    baseHash = ''
    await save(true)
  }

  function refreshCompletion(): void {
    completion = view ? completionOf(view.state) : null
    if (completion && view) {
      const c = view.coordsAtPos(completion.from)
      const scale = currentUIScale()
      completionAt = c ? { left: c.left / scale, top: c.bottom / scale + 4 } : null
    } else {
      completionAt = null
    }
  }

  let stopChanges: (() => void) | undefined
  let stopDrops: (() => void) | undefined

  onMount(() => {
    void load()
    stopChanges = onVaultChanged((e) => {
      if (e.removed?.includes(path)) {
        void diskChanged()
      } else if (e.changed?.includes(path) || e.reset) {
        void diskChanged()
      }
    })
    stopDrops = onFilesDropped((e) => {
      if (e.target !== 'notes-editor' || !view || view.state.readOnly) {
        return
      }
      void importDropped(e.paths)
    })
  })

  async function importDropped(paths: string[]): Promise<void> {
    if (!view) {
      return
    }
    const parts: string[] = []
    for (const p of paths) {
      try {
        const saved = await vaultImportFile(p)
        const name = saved.slice(saved.lastIndexOf('/') + 1)
        parts.push(`![[${name}]]`)
      } catch (err) {
        toastError(errorMessage(err))
      }
    }
    if (parts.length) {
      insertText(parts.join('\n'))
    }
  }

  onDestroy(() => {
    stopChanges?.()
    stopDrops?.()
    if (view) {
      const sel = view.state.selection.main
      places.set(path, { anchor: sel.anchor, head: sel.head, scroll: view.scrollDOM.scrollTop })
      if (timer) {
        clearTimeout(timer)
        timer = null
        // save takes the text before its first await, so the view can go.
        void save()
      }
      view.destroy()
      view = null
    }
  })

  $: if (view) {
    view.dispatch({ effects: modeCompartment.reconfigure(modeExtensions(mode)) })
  }

  $: if (view && fullContext) {
    view.dispatch({ effects: [contextCompartment.reconfigure(noteContext.of(fullContext)), refreshPreview.of(null)] })
  }

  $: if (view && revision) {
    view.dispatch({ effects: refreshPreview.of(null) })
  }

  // a line asked for from elsewhere: a backlink, the outline, a search hit.
  $: if (view && !loading && $noteLine && $noteLine.path === path) {
    const at = $noteLine.line
    noteLine.set(null)
    goToLine(at)
  }

  /** The note's current text. */
  export function currentText(): string {
    return text()
  }

  /** Saves image files into the vault and embeds them at the cursor. */
  export function addImages(files: File[]): void {
    if (view) {
      void insertImages(view, files, view.state.selection.main.head)
    }
  }
</script>

<div class="note-editor" data-file-drop-target="notes-editor">
  {#if conflict}
    <div class="bar" role="alert">
      <Ph svg={warningIcon} size={15} />
      <span class="bar-text">{$t('notes.conflict.body')}</span>
      <button type="button" class="cal-btn" on:click={loadTheirs}>{$t('notes.conflict.theirs')}</button>
      <button type="button" class="cal-btn primary" on:click={() => void keepMine()}>{$t('notes.conflict.mine')}</button>
    </div>
  {:else if gone}
    <div class="bar" role="alert">
      <Ph svg={warningIcon} size={15} />
      <span class="bar-text">{$t('notes.gone.body')}</span>
      <button type="button" class="cal-btn" on:click={() => dispatch('gone', { path })}>{$t('notes.gone.close')}</button>
      <button type="button" class="cal-btn primary" on:click={() => void saveAgain()}>{$t('notes.gone.save')}</button>
    </div>
  {:else if readOnly}
    <div class="bar quiet" role="status">
      <span class="bar-text">{$t('notes.readOnlyEncoding')}</span>
    </div>
  {/if}
  {#if loadError}
    <p class="load-error" role="alert">{loadError}</p>
  {/if}
  <div class="host" class:loading bind:this={host}></div>
</div>

{#if completion && completionAt && view}
  <div class="complete" role="listbox" aria-label={$t('notes.editor.suggestions')} style={`left:${completionAt.left}px;top:${completionAt.top}px`}>
    {#each completion.items as item, i (i)}
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <div
        class="item"
        role="option"
        aria-selected={i === completion.selected}
        tabindex="-1"
        on:mousedown|preventDefault={() => view && acceptCompletion(view, i)}
      >
        <span class="label" class:create={item.create}>{item.label}</span>
        {#if item.detail}<span class="detail">{item.detail}</span>{/if}
      </div>
    {/each}
  </div>
{/if}

<svelte:window on:blur={() => view && closeCompletion(view)} />

<style>
  .note-editor {
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .host {
    flex: 1;
    min-height: 0;
  }

  .host.loading {
    opacity: 0;
  }

  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--warning-bg);
    color: var(--text-primary);
    font-size: var(--fz-label);
  }

  .bar.quiet {
    background: var(--surface-sunken);
    color: var(--text-secondary);
  }

  .bar :global(.ph) {
    color: var(--warning);
  }

  .bar-text {
    flex: 1;
    min-width: 0;
  }

  .load-error {
    margin: var(--space-6);
    color: var(--danger);
    font-size: var(--fz-list);
  }

  .complete {
    position: fixed;
    z-index: 330;
    min-width: calc(var(--popover-width) * 0.8);
    max-width: var(--popover-width);
    max-height: var(--suggest-max-height);
    overflow-y: auto;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    font-family: var(--font-ui);
    font-size: var(--fz-list);
  }

  .item {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .item[aria-selected='true'] {
    background: var(--selection-bg);
  }

  .label {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .label.create {
    color: var(--text-secondary);
  }

  .detail {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
