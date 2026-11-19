<script lang="ts">
  // the Lua source editor: CodeMirror with line numbers in the mono font and
  // Lua colouring, read only for the Lua a block script compiles to. with
  // assist set it also completes globals, module functions, event fields,
  // settings and libraries from the registry, explains the name at the caret
  // on a line below, and marks the lines errors point at.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import { EditorView, keymap, lineNumbers, highlightActiveLine, drawSelection } from '@codemirror/view'
  import { EditorSelection, EditorState, Compartment } from '@codemirror/state'
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
  import riskIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { luaHighlight } from './lua/highlight'
  import { docAt, type LuaContext, type LuaDoc } from './lua/assist'
  import { luaCompletion, luaMarks, markOn, showMarks, type CompletionState, type LineMark } from './lua/extensions'
  import { catalog } from '../../stores/scripts'
  import { currentUIScale } from '../../theme/theme'
  import { t } from '../../lib/i18n'

  /** The source. */
  export let content = ''
  /** Shows the source without letting it be edited. */
  export let readonly = false
  /** Accessible name of the editor. */
  export let label = ''
  /** What completion and the docs line draw on; null turns both off. */
  export let assist: LuaContext | null = null
  /** Lines to mark as wrong, counted from 1. */
  export let marks: LineMark[] = []

  const dispatch = createEventDispatcher<{ change: string }>()
  const editable = new Compartment()
  const colours = new Compartment()

  let el: HTMLDivElement
  let view: EditorView | null = null
  let completion: CompletionState | null = null
  let completionAt: { left: number; top: number } | null = null
  let doc: LuaDoc | null = null
  let problem = ''

  const complete = luaCompletion(() => (readonly ? null : assist))

  const theme = EditorView.theme({
    '&': { height: '100%', backgroundColor: 'transparent', color: 'var(--text-primary)' },
    '&.cm-focused': { outline: 'none' },
    '.cm-scroller': { fontFamily: 'var(--font-mono)', fontSize: 'var(--fz-body)', lineHeight: '1.5', overflow: 'auto' },
    '.cm-content': { padding: 'var(--space-2) 0', caretColor: 'var(--accent)' },
    '.cm-cursor': { borderLeftColor: 'var(--accent)' },
    '.cm-gutters': {
      backgroundColor: 'var(--surface-sunken)',
      color: 'var(--text-tertiary)',
      border: 'none',
      borderInlineEnd: 'var(--hairline) solid var(--border-subtle)',
      minWidth: 'var(--automation-gutter)',
    },
    '.cm-activeLine': { backgroundColor: 'var(--surface-hover)' },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'var(--selection-bg-strong)' },
    '.cm-lua-keyword': { color: 'var(--link)', fontWeight: 'var(--fw-semibold)' },
    '.cm-lua-string': { color: 'var(--success)' },
    '.cm-lua-number': { color: 'var(--warning)' },
    '.cm-lua-comment': { color: 'var(--text-tertiary)', fontStyle: 'italic' },
    '.cm-lua-builtin': { color: 'var(--text-secondary)', fontWeight: 'var(--fw-medium)' },
    '.cm-lua-module': { color: 'var(--accent)' },
    '.cm-lua-error-line': { backgroundColor: 'var(--danger-bg)' },
    '.cm-lua-marks': { minWidth: 'var(--space-3)' },
    '.cm-lua-error-dot': {
      display: 'inline-block',
      width: 'var(--space-2)',
      height: 'var(--space-2)',
      borderRadius: '50%',
      backgroundColor: 'var(--danger)',
    },
  })

  function editableExt(ro: boolean) {
    return [EditorState.readOnly.of(ro), EditorView.editable.of(!ro)]
  }

  $: modules = new Set(($catalog?.modules ?? []).map((m) => m.id))

  function refresh(): void {
    if (!view || !assist || readonly) {
      completion = null
      doc = null
      problem = ''
      return
    }
    completion = complete.current(view.state)
    completionAt = null
    if (completion) {
      const c = view.coordsAtPos(completion.from)
      const scale = currentUIScale()
      completionAt = c ? { left: c.left / scale, top: c.bottom / scale + 4 } : null
    }
    const head = view.state.selection.main.head
    const line = view.state.doc.lineAt(head)
    problem = markOn(view.state, line.number)
    doc = docAt(line.text, head - line.from, assist)
  }

  onMount(() => {
    view = new EditorView({
      parent: el,
      state: EditorState.create({
        doc: content,
        extensions: [
          lineNumbers(),
          luaMarks,
          highlightActiveLine(),
          drawSelection(),
          history(),
          complete.extension,
          keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
          EditorState.tabSize.of(2),
          editable.of(editableExt(readonly)),
          colours.of(luaHighlight(modules)),
          EditorView.contentAttributes.of({ 'aria-label': label }),
          theme,
          EditorView.updateListener.of((u) => {
            if (u.docChanged) {
              dispatch('change', u.state.doc.toString())
            }
            if (u.docChanged || u.selectionSet || u.transactions.some((tr) => tr.effects.length > 0)) {
              refresh()
            }
          }),
        ],
      }),
    })
    showMarks(view, marks)
  })

  onDestroy(() => {
    view?.destroy()
    view = null
  })

  /** Puts text at the caret, in place of the selection, and focuses the editor. */
  export function insert(text: string): void {
    if (!view || readonly) {
      return
    }
    const sel = view.state.selection.main
    view.dispatch({
      changes: { from: sel.from, to: sel.to, insert: text },
      selection: EditorSelection.cursor(sel.from + text.length),
      userEvent: 'input',
      scrollIntoView: true,
    })
    view.focus()
  }

  $: if (view && content !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: content } })
  }

  $: if (view) {
    view.dispatch({ effects: editable.reconfigure(editableExt(readonly)) })
  }

  $: if (view) {
    view.dispatch({ effects: colours.reconfigure(luaHighlight(modules)) })
  }

  $: if (view) {
    showMarks(view, marks)
  }
</script>

<div class="lua-wrap">
  <div class="lua" class:readonly bind:this={el}></div>
  {#if assist && !readonly && (problem || doc)}
    <div class="docs" role="status" aria-live="polite">
      {#if problem}
        <span class="problem">{problem}</span>
      {:else if doc}
        <code class="sig">{doc.title}</code>
        {#if doc.description}<span class="desc">{doc.description}</span>{/if}
        {#each doc.permissions as p (p)}<span class="perm risk-{doc.risk || 'low'}">{p}</span>{/each}
        {#if doc.risky}<span class="perm risk-high" title={$t('scripts.lua.asksFirst')}><Ph svg={riskIcon} size={12} />{$t('scripts.lua.asksFirst')}</span>{/if}
        {#if doc.effect}<span class="note">{$t('scripts.lua.effect')}</span>{/if}
      {/if}
    </div>
  {/if}
</div>

{#if completion && completionAt && view}
  <div class="complete" role="listbox" aria-label={$t('scripts.lua.suggestions')} style={`left:${completionAt.left}px;top:${completionAt.top}px`}>
    {#each completion.items as item, i (i)}
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <div class="item" role="option" aria-selected={i === completion.selected} tabindex="-1" on:mousedown|preventDefault={() => view && complete.accept(view, i)}>
        <span class="label kind-{item.kind}">{item.label}</span>
        {#if item.detail}<span class="detail">{item.detail}</span>{/if}
      </div>
    {/each}
  </div>
{/if}

<svelte:window on:blur={() => view && complete.close(view)} />

<style>
  .lua-wrap {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .lua {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .lua.readonly {
    background: var(--surface-sunken);
  }

  .lua :global(.cm-editor) {
    height: 100%;
  }

  .docs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--chip-height);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .sig {
    font-family: var(--font-mono);
    color: var(--text-primary);
  }

  .problem {
    color: var(--danger);
    font-family: var(--font-mono);
  }

  .perm {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0 var(--space-1);
    border: var(--hairline) solid currentColor;
    border-radius: var(--radius-control);
    line-height: var(--chip-height);
    color: var(--text-tertiary);
  }

  .perm.risk-medium {
    color: var(--warning);
  }

  .perm.risk-high {
    color: var(--danger);
  }

  .note {
    color: var(--text-tertiary);
  }

  .complete {
    position: fixed;
    z-index: 330;
    min-width: calc(var(--popover-width) * 0.8);
    max-width: calc(var(--popover-width) * 1.6);
    max-height: var(--suggest-max-height);
    overflow-y: auto;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fz-list);
  }

  .item {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .item[aria-selected='true'] {
    background: var(--selection-bg);
  }

  .label {
    font-family: var(--font-mono);
    color: var(--text-primary);
  }

  .label.kind-module,
  .label.kind-function {
    color: var(--accent);
  }

  .label.kind-keyword {
    color: var(--link);
  }

  .detail {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
