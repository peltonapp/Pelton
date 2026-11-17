<script lang="ts">
  // the Lua source editor: CodeMirror with line numbers in the mono font, read
  // only for the Lua a block script compiles to. there is no Lua highlighting,
  // since CodeMirror's Lua mode lives in a package the app does not ship.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import { EditorView, keymap, lineNumbers, highlightActiveLine, drawSelection } from '@codemirror/view'
  import { EditorState, Compartment } from '@codemirror/state'
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'

  /** The source. */
  export let content = ''
  /** Shows the source without letting it be edited. */
  export let readonly = false
  /** Accessible name of the editor. */
  export let label = ''

  const dispatch = createEventDispatcher<{ change: string }>()
  const editable = new Compartment()

  let el: HTMLDivElement
  let view: EditorView | null = null

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
  })

  function editableExt(ro: boolean) {
    return [EditorState.readOnly.of(ro), EditorView.editable.of(!ro)]
  }

  onMount(() => {
    view = new EditorView({
      parent: el,
      state: EditorState.create({
        doc: content,
        extensions: [
          lineNumbers(),
          highlightActiveLine(),
          drawSelection(),
          history(),
          keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
          EditorState.tabSize.of(2),
          editable.of(editableExt(readonly)),
          EditorView.contentAttributes.of({ 'aria-label': label }),
          theme,
          EditorView.updateListener.of((u) => {
            if (u.docChanged) {
              dispatch('change', u.state.doc.toString())
            }
          }),
        ],
      }),
    })
  })

  onDestroy(() => {
    view?.destroy()
    view = null
  })

  $: if (view && content !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: content } })
  }

  $: if (view) {
    view.dispatch({ effects: editable.reconfigure(editableExt(readonly)) })
  }
</script>

<div class="lua" class:readonly bind:this={el}></div>

<style>
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
</style>
