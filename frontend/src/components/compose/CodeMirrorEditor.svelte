<script lang="ts">
  // the compose body editor, built on CodeMirror 6. it replaces the plain
  // textarea so vim mode is a real, robust implementation (including visual block)
  // via @replit/codemirror-vim, instead of a hand-rolled emulation. it keeps the
  // markdown toolbar working by exposing selection-formatting methods the parent
  // calls. line wrapping is on and the theme is driven by the app's css tokens.
  import { onMount, onDestroy, createEventDispatcher } from 'svelte'
  import { EditorView, keymap, placeholder as cmPlaceholder, drawSelection } from '@codemirror/view'
  import { EditorState, Compartment } from '@codemirror/state'
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
  import { vim } from '@replit/codemirror-vim'

  export let content = ''
  export let placeholder = ''
  export let vimEnabled = false
  // mono uses a monospace font (for the plaintext editor); markdown keeps the ui font.
  export let mono = false
  export let grabFocus = false

  const dispatch = createEventDispatcher<{ change: string }>()

  let el: HTMLDivElement
  let view: EditorView | null = null
  const vimCompartment = new Compartment()

  const theme = EditorView.theme(
    {
      '&': { height: '100%', backgroundColor: 'transparent', color: 'var(--text-primary)' },
      '&.cm-focused': { outline: 'none' },
      // --compose-font is the mail body font setting, set by Compose on the
      // editor container (#64); unset, each mode keeps its built-in font. the
      // markdown editor stays on the ui font: it edits source, and the
      // preview pane is what mirrors the recipient's view.
      '.cm-scroller': {
        fontFamily: mono ? 'var(--compose-font, var(--font-mono, ui-monospace, monospace))' : 'var(--font-ui)',
        fontSize: 'var(--fz-body)',
        lineHeight: '1.55',
        overflow: 'auto',
      },
      '.cm-content': { padding: 'var(--space-3) var(--space-4)', caretColor: 'var(--accent)' },
      '.cm-cursor': { borderLeftColor: 'var(--accent)' },
      '.cm-fat-cursor': { background: 'var(--accent)', color: 'var(--surface-base)' },
      '.cm-placeholder': { color: 'var(--text-tertiary)' },
      '.cm-selectionBackground': { backgroundColor: 'var(--selection-bg-strong)' },
      '&.cm-focused .cm-selectionBackground': { backgroundColor: 'var(--selection-bg-strong)' },
      '.cm-content ::selection': { backgroundColor: 'var(--selection-bg-strong)' },
      '.cm-panels': { backgroundColor: 'var(--surface-sunken)', color: 'var(--text-tertiary)' },
      '.cm-vim-panel': {
        padding: '2px var(--space-3)',
        fontFamily: 'var(--font-mono, ui-monospace, monospace)',
        fontSize: 'var(--fz-meta)',
        color: 'var(--text-secondary)',
      },
    },
    { dark: false },
  )

  function makeState(doc: string): EditorState {
    return EditorState.create({
      doc,
      extensions: [
        // vim must precede the default keymap so it can intercept keys.
        vimCompartment.of(vimEnabled ? vim({ status: true }) : []),
        // drawSelection renders selection (including vim visual mode) as its own
        // layer instead of relying on native ::selection, which codemirror-vim's
        // fake cursor/selection handling does not reliably trigger.
        drawSelection(),
        history(),
        keymap.of([...defaultKeymap, ...historyKeymap]),
        EditorView.lineWrapping,
        cmPlaceholder(placeholder),
        theme,
        EditorView.updateListener.of((u) => {
          if (u.docChanged) {
            dispatch('change', u.state.doc.toString())
          }
        }),
      ],
    })
  }

  onMount(() => {
    view = new EditorView({ state: makeState(content), parent: el })
    if (grabFocus) {
      view.focus()
    }
  })
  onDestroy(() => {
    view?.destroy()
    view = null
  })

  // external content changes (signature insert, reopened draft) sync in without
  // fighting local typing: only when the value truly differs from the doc.
  $: if (view && content !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: content } })
  }

  // toggle vim live without recreating the editor.
  $: if (view) {
    view.dispatch({ effects: vimCompartment.reconfigure(vimEnabled ? vim({ status: true }) : []) })
  }

  export function focusEditor(): void {
    view?.focus()
  }

  // wrapSelection surrounds the selection (or a placeholder) with token, used by
  // the markdown toolbar for bold/italic/code.
  export function wrapSelection(token: string, ph: string): void {
    if (!view) return
    const sel = view.state.selection.main
    const selected = view.state.sliceDoc(sel.from, sel.to)
    const inner = selected || ph
    const insert = token + inner + token
    view.dispatch({
      changes: { from: sel.from, to: sel.to, insert },
      selection: { anchor: sel.from + token.length, head: sel.from + token.length + inner.length },
    })
    view.focus()
  }

  // linePrefix prepends prefix to every line the selection touches (headings,
  // lists, quotes).
  export function linePrefix(prefix: string): void {
    if (!view) return
    const sel = view.state.selection.main
    const startLine = view.state.doc.lineAt(sel.from)
    const endLine = view.state.doc.lineAt(sel.to)
    const changes = []
    for (let ln = startLine.number; ln <= endLine.number; ln++) {
      changes.push({ from: view.state.doc.line(ln).from, insert: prefix })
    }
    view.dispatch({ changes })
    view.focus()
  }

  // insertLink inserts a markdown link around the selection (or "link").
  export function insertLink(): void {
    if (!view) return
    const sel = view.state.selection.main
    const text = view.state.sliceDoc(sel.from, sel.to) || 'link'
    const insert = `[${text}](https://)`
    view.dispatch({
      changes: { from: sel.from, to: sel.to, insert },
      selection: { anchor: sel.from + insert.length },
    })
    view.focus()
  }
</script>

<div class="cm-host" bind:this={el}></div>

<style>
  .cm-host {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .cm-host :global(.cm-editor) {
    height: 100%;
  }
</style>
