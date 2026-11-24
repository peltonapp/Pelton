// complete.ts suggests while typing a link or a tag: notes after "[[", the
// headings of a note after "[[Note#", and tags after "#". It keeps the state
// and the keys; the editor has no completion package to lean on, so the list
// itself is drawn by the editor component.

import { EditorSelection, Prec, StateEffect, StateField, type EditorState, type Transaction } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { noteContext, type CompletionItem } from './context'

/** What is being completed and where the typed query sits in the document. */
export interface CompletionState {
  kind: 'note' | 'heading' | 'tag'
  from: number
  to: number
  items: CompletionItem[]
  selected: number
}

const close = StateEffect.define<null>()
const move = StateEffect.define<number>()

/** Finds what the text before the cursor asks to complete, if anything. */
export function completionQuery(before: string): { kind: CompletionState['kind']; query: string; note?: string } | null {
  const heading = /\[\[([^[\]|#]*)#([^[\]|#]*)$/.exec(before)
  if (heading) {
    return { kind: 'heading', query: heading[2], note: heading[1] }
  }
  const note = /\[\[([^[\]|#]*)$/.exec(before)
  if (note) {
    return { kind: 'note', query: note[1] }
  }
  const tag = /(?:^|[\s(,])#([\p{L}\p{N}_/-]+)$/u.exec(before)
  if (tag && !/^\d+$/.test(tag[1])) {
    return { kind: 'tag', query: tag[1] }
  }
  return null
}

function compute(state: EditorState): CompletionState | null {
  const sel = state.selection.main
  if (!sel.empty) {
    return null
  }
  const line = state.doc.lineAt(sel.head)
  const before = line.text.slice(0, sel.head - line.from)
  const q = completionQuery(before)
  if (!q) {
    return null
  }
  const ctx = state.facet(noteContext)
  const items = q.kind === 'note' ? ctx.completeNotes(q.query) : q.kind === 'heading' ? ctx.completeHeadings(q.note ?? '', q.query) : ctx.completeTags(q.query)
  if (!items.length) {
    return null
  }
  return { kind: q.kind, from: sel.head - q.query.length, to: sel.head, items, selected: 0 }
}

const completion = StateField.define<CompletionState | null>({
  create: () => null,
  update(value, tr: Transaction) {
    for (const e of tr.effects) {
      if (e.is(close)) {
        return null
      }
      if (e.is(move) && value) {
        const n = value.items.length
        return { ...value, selected: (value.selected + e.value + n) % n }
      }
    }
    if (tr.docChanged && tr.isUserEvent('input.type')) {
      return compute(tr.state)
    }
    if (tr.docChanged && tr.isUserEvent('delete') && value) {
      return compute(tr.state)
    }
    if (tr.selection || tr.docChanged) {
      return null
    }
    return value
  },
})

/**
 * The open completion of an editor, or null. The editor component draws the
 * list itself, since the root's css zoom puts the editor's own tooltips in
 * the wrong place.
 */
export function completionOf(state: EditorState): CompletionState | null {
  return state.field(completion, false) ?? null
}

/** Closes the completion list. */
export function closeCompletion(view: EditorView): void {
  if (view.state.field(completion, false)) {
    view.dispatch({ effects: close.of(null) })
  }
}

/** Puts the chosen completion in place of the typed query. */
export function acceptCompletion(view: EditorView, index?: number): boolean {
  const value = view.state.field(completion, false)
  if (!value) {
    return false
  }
  const item = value.items[index ?? value.selected]
  if (!item) {
    return false
  }
  const after = view.state.sliceDoc(value.to, value.to + 2)
  let insert = item.insert
  let end = value.to
  if (value.kind === 'note' || value.kind === 'heading') {
    if (after === ']]') {
      end += 2
    }
    insert += ']]'
  } else if (!/^\s/.test(view.state.sliceDoc(value.to, value.to + 1))) {
    insert += ' '
  }
  view.dispatch({
    changes: { from: value.from, to: end, insert },
    selection: EditorSelection.cursor(value.from + insert.length),
    effects: close.of(null),
    userEvent: 'input.complete',
  })
  return true
}

const keys = Prec.highest(
  keymap.of([
    { key: 'ArrowDown', run: (v) => (v.state.field(completion, false) ? (v.dispatch({ effects: move.of(1) }), true) : false) },
    { key: 'ArrowUp', run: (v) => (v.state.field(completion, false) ? (v.dispatch({ effects: move.of(-1) }), true) : false) },
    { key: 'Enter', run: (v) => acceptCompletion(v) },
    { key: 'Tab', run: (v) => acceptCompletion(v) },
    { key: 'Escape', run: (v) => (v.state.field(completion, false) ? (v.dispatch({ effects: close.of(null) }), true) : false) },
  ]),
)

// typing the second "[" of a link closes it, so the completion has the
// brackets to complete into.
const closeBrackets = EditorView.inputHandler.of((view, from, to, text) => {
  if (text !== '[' || from !== to) {
    return false
  }
  const before = view.state.sliceDoc(from - 1, from)
  const after = view.state.sliceDoc(from, from + 1)
  if (before !== '[' || after === ']') {
    return false
  }
  view.dispatch({ changes: { from, insert: '[]]' }, selection: EditorSelection.cursor(from + 1), userEvent: 'input.type' })
  return true
})

/** Link, heading and tag completion. */
export function noteCompletion() {
  return [completion, keys, closeBrackets]
}
