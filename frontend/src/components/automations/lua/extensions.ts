// extensions.ts is the CodeMirror side of the Lua editor's aids: the
// completion list's state and keys, and the error marks in the gutter and on
// the line. The editor component draws the list and the docs line itself,
// since the root's css zoom puts CodeMirror's own tooltips in the wrong place.

import { EditorSelection, Prec, RangeSet, StateEffect, StateField, type EditorState, type Extension, type Transaction } from '@codemirror/state'
import { Decoration, EditorView, GutterMarker, gutter, keymap, type DecorationSet } from '@codemirror/view'
import { completionsAt, type Completion, type LuaContext } from './assist'

/** The open completion: where the query starts and ends, and what is offered. */
export interface CompletionState extends Completion {
  from: number
  to: number
  selected: number
}

const close = StateEffect.define<null>()
const move = StateEffect.define<number>()

function compute(state: EditorState, ctx: () => LuaContext | null): CompletionState | null {
  const sel = state.selection.main
  const context = ctx()
  if (!sel.empty || !context) {
    return null
  }
  const line = state.doc.lineAt(sel.head)
  const found = completionsAt(line.text.slice(0, sel.head - line.from), context)
  if (!found) {
    return null
  }
  return { ...found, from: sel.head - found.back, to: sel.head, selected: 0 }
}

function completionField(ctx: () => LuaContext | null) {
  return StateField.define<CompletionState | null>({
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
      if (tr.docChanged && (tr.isUserEvent('input.type') || (tr.isUserEvent('delete') && value))) {
        return compute(tr.state, ctx)
      }
      if (tr.selection || tr.docChanged) {
        return null
      }
      return value
    },
  })
}

type Field = ReturnType<typeof completionField>

/** The completion parts of the editor and the handles the component draws the list with. */
export interface LuaCompletion {
  extension: Extension
  /** The open completion of an editor, or null. */
  current(state: EditorState): CompletionState | null
  /** Puts the chosen suggestion in place of the typed query. */
  accept(view: EditorView, index?: number): boolean
  /** Closes the list. */
  close(view: EditorView): void
}

/** Completion over the context ctx returns when asked; none while it returns null. */
export function luaCompletion(ctx: () => LuaContext | null): LuaCompletion {
  const field: Field = completionField(ctx)
  const current = (state: EditorState) => state.field(field, false) ?? null
  const accept = (view: EditorView, index?: number): boolean => {
    const value = current(view.state)
    const item = value?.items[index ?? value.selected]
    if (!value || !item) {
      return false
    }
    view.dispatch({
      changes: { from: value.from, to: value.to, insert: item.insert },
      selection: EditorSelection.cursor(value.from + item.insert.length),
      effects: close.of(null),
      userEvent: 'input.complete',
    })
    return true
  }
  const keys = Prec.highest(
    keymap.of([
      { key: 'ArrowDown', run: (v) => (current(v.state) ? (v.dispatch({ effects: move.of(1) }), true) : false) },
      { key: 'ArrowUp', run: (v) => (current(v.state) ? (v.dispatch({ effects: move.of(-1) }), true) : false) },
      { key: 'Enter', run: (v) => accept(v) },
      { key: 'Tab', run: (v) => accept(v) },
      { key: 'Escape', run: (v) => (current(v.state) ? (v.dispatch({ effects: close.of(null) }), true) : false) },
    ]),
  )
  return {
    extension: [field, keys],
    current,
    accept,
    close: (view) => {
      if (current(view.state)) {
        view.dispatch({ effects: close.of(null) })
      }
    },
  }
}

/** A problem on a line of the source, counted from 1. */
export interface LineMark {
  line: number
  message: string
}

const setMarks = StateEffect.define<LineMark[]>()

class ErrorMarker extends GutterMarker {
  constructor(readonly message: string) {
    super()
  }

  eq(other: ErrorMarker): boolean {
    return other.message === this.message
  }

  toDOM(): Node {
    const el = document.createElement('span')
    el.className = 'cm-lua-error-dot'
    el.title = this.message
    el.setAttribute('aria-label', this.message)
    return el
  }
}

const errorLine = Decoration.line({ class: 'cm-lua-error-line' })

interface MarkState {
  lines: DecorationSet
  gutter: RangeSet<GutterMarker>
}

function build(state: EditorState, marks: LineMark[]): MarkState {
  const valid = marks.filter((m) => m.line >= 1 && m.line <= state.doc.lines)
  const at = (m: LineMark) => state.doc.line(m.line).from
  return {
    lines: Decoration.set(
      valid.map((m) => errorLine.range(at(m))),
      true,
    ),
    gutter: RangeSet.of(
      valid.map((m) => new ErrorMarker(m.message).range(at(m))),
      true,
    ),
  }
}

const markField = StateField.define<MarkState>({
  create: (state) => build(state, []),
  update(value, tr) {
    for (const e of tr.effects) {
      if (e.is(setMarks)) {
        return build(tr.state, e.value)
      }
    }
    if (tr.docChanged) {
      return { lines: value.lines.map(tr.changes), gutter: value.gutter.map(tr.changes) }
    }
    return value
  },
  provide: (f) => EditorView.decorations.from(f, (v) => v.lines),
})

/** The error marks: a dot in their own gutter and a tinted line. */
export const luaMarks: Extension = [
  markField,
  gutter({ class: 'cm-lua-marks', markers: (view) => view.state.field(markField).gutter }),
]

/** Replaces the error marks of an editor. */
export function showMarks(view: EditorView, marks: LineMark[]): void {
  view.dispatch({ effects: setMarks.of(marks) })
}

/** The message of the mark on a line, or ''. */
export function markOn(state: EditorState, line: number): string {
  const field = state.field(markField, false)
  if (!field) {
    return ''
  }
  const pos = state.doc.line(line).from
  let message = ''
  field.gutter.between(pos, pos, (_from, _to, m) => {
    message = (m as ErrorMarker).message
  })
  return message
}
