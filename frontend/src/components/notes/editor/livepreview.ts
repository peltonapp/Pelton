// livepreview.ts renders a note's Markdown in place, the way Obsidian's live
// preview does: on every line the cursor is not on, the syntax hides and the
// text shows as what it means (headings, emphasis, links, checkboxes, tables,
// images, embeds, callouts, the properties block). The line being edited
// shows its source. Reading mode renders every line and allows no edits;
// source mode shows the plain text.

import { EditorState, StateEffect, StateField, type Range } from '@codemirror/state'
import { Decoration, EditorView, type DecorationSet } from '@codemirror/view'
import { classifyLines, codeTokens, inlineTokens, isImagePath, type LineInfo } from '../../../lib/notemd'
import { noteContext, previewMode } from './context'
import { CheckboxWidget, ImageWidget, MarkerWidget, NoteEmbedWidget, PropertiesWidget, TableWidget, followElement } from './widgets'

/** Asks the preview to draw again, after links may resolve differently. */
export const refreshPreview = StateEffect.define<null>()

const hide = Decoration.replace({})

function lineClass(cls: string): Decoration {
  return Decoration.line({ class: cls })
}

function mark(cls: string, attrs?: Record<string, string>): Decoration {
  return Decoration.mark({ class: cls, attributes: attrs })
}

// the lines the selection touches, which show their source.
function activeLines(state: EditorState): Set<number> {
  const out = new Set<number>()
  for (const r of state.selection.ranges) {
    const a = state.doc.lineAt(r.from).number
    const b = state.doc.lineAt(r.to).number
    for (let n = a; n <= b; n++) {
      out.add(n)
    }
  }
  return out
}

const lineInfoField = StateField.define<LineInfo[]>({
  create: (state) => classifyLines(state.doc.toString().split('\n')),
  update: (value, tr) => (tr.docChanged ? classifyLines(tr.state.doc.toString().split('\n')) : value),
})

function build(state: EditorState): DecorationSet {
  const mode = state.facet(previewMode)
  const info = state.field(lineInfoField)
  const ctx = state.facet(noteContext)
  const doc = state.doc
  const ranges: Range<Decoration>[] = []
  const active = mode === 'reading' ? new Set<number>() : activeLines(state)

  if (mode === 'source') {
    for (let n = 1; n <= doc.lines; n++) {
      const k = info[n - 1]
      if (k?.type === 'heading') {
        ranges.push(lineClass(`cm-nt-src-h`).range(doc.line(n).from))
      } else if (k?.type === 'code' || k?.type === 'fence' || k?.type === 'frontmatter') {
        ranges.push(lineClass('cm-nt-src-code').range(doc.line(n).from))
      }
    }
    return Decoration.set(ranges, true)
  }

  // each fenced block's first and last line, by the index of its opening line.
  const codeBlocks = new Map<number, [number, number]>()
  info.forEach((k, i) => {
    if (k.type === 'fence' || k.type === 'code') {
      const r = codeBlocks.get(k.block)
      if (r) {
        r[1] = i + 1
      } else {
        codeBlocks.set(k.block, [i + 1, i + 1])
      }
    }
  })

  const blockActive = (first: number, last: number): boolean => {
    for (let n = first; n <= last; n++) {
      if (active.has(n)) {
        return true
      }
    }
    return false
  }

  let n = 1
  while (n <= doc.lines) {
    const line = doc.line(n)
    const k = info[n - 1] ?? { type: 'text' }
    const isActive = active.has(n)

    if (k.type === 'frontmatter' && k.first) {
      let last = n
      while (last < doc.lines && !(info[last - 1]?.type === 'frontmatter' && (info[last - 1] as { last: boolean }).last)) {
        last++
      }
      if (!blockActive(n, last)) {
        const end = doc.line(last)
        ranges.push(Decoration.replace({ widget: new PropertiesWidget(doc.sliceString(line.from, end.to), line.from, ctx.t('notes.editor.properties')), block: true }).range(line.from, end.to))
      } else {
        for (let i = n; i <= last; i++) {
          ranges.push(lineClass('cm-nt-fm').range(doc.line(i).from))
        }
      }
      n = last + 1
      continue
    }

    if (k.type === 'table' && (n === 1 || info[n - 2]?.type !== 'table' || (info[n - 2] as { block: number }).block !== k.block)) {
      let last = n
      while (last < doc.lines && info[last]?.type === 'table' && (info[last] as { block: number }).block === k.block) {
        last++
      }
      if (last > n && !blockActive(n, last)) {
        const end = doc.line(last)
        ranges.push(Decoration.replace({ widget: new TableWidget(doc.sliceString(line.from, end.to), line.from), block: true }).range(line.from, end.to))
      } else {
        for (let i = n; i <= last; i++) {
          ranges.push(lineClass('cm-nt-table-src').range(doc.line(i).from))
        }
      }
      n = last + 1
      continue
    }

    const text = line.text
    let inlineFrom = 0
    switch (k.type) {
      case 'fence':
      case 'code': {
        const [first, last] = codeBlocks.get(k.block) ?? [n, n]
        const shown = blockActive(first, last)
        if (k.type === 'fence') {
          ranges.push(lineClass(`cm-nt-code ${k.open ? 'cm-nt-code-first' : 'cm-nt-code-last'}`).range(line.from))
          if (!isActive && !shown && text.length) {
            ranges.push(Decoration.replace({ widget: new MarkerWidget('cm-nt-code-lang', k.open ? k.lang : '') }).range(line.from, line.to))
          } else {
            ranges.push(mark('cm-nt-syntax').range(line.from, line.to))
          }
        } else {
          ranges.push(lineClass('cm-nt-code').range(line.from))
          for (const tok of codeTokens(text, k.lang)) {
            ranges.push(mark(`cm-nt-tok-${tok.kind}`).range(line.from + tok.from, line.from + tok.to))
          }
        }
        n++
        continue
      }
      case 'heading':
        ranges.push(lineClass(`cm-nt-h cm-nt-h${k.level}`).range(line.from))
        if (!isActive) {
          ranges.push(hide.range(line.from, line.from + k.markEnd))
        } else {
          ranges.push(mark('cm-nt-syntax').range(line.from, line.from + k.markEnd))
        }
        inlineFrom = k.markEnd
        break
      case 'hr':
        ranges.push(lineClass('cm-nt-hr-line').range(line.from))
        if (!isActive) {
          ranges.push(Decoration.replace({ widget: new MarkerWidget('cm-nt-hr') }).range(line.from, line.to))
        }
        n++
        continue
      case 'quote': {
        const cls = k.callout ? `cm-nt-quote cm-nt-callout cm-nt-callout-${k.callout.kind}${k.callout.head ? ' cm-nt-callout-head' : ''}` : 'cm-nt-quote'
        ranges.push(lineClass(cls).range(line.from))
        if (!isActive) {
          ranges.push(hide.range(line.from, line.from + k.markEnd))
          if (k.callout?.head) {
            const m = /^\[![^\]]+\][+-]?[ \t]*/.exec(text.slice(k.markEnd))
            if (m) {
              const label = k.callout.title.trim() ? '' : k.callout.kind.charAt(0).toUpperCase() + k.callout.kind.slice(1)
              ranges.push(Decoration.replace({ widget: new MarkerWidget('cm-nt-callout-label', label) }).range(line.from + k.markEnd, line.from + k.markEnd + m[0].length))
              inlineFrom = k.markEnd + m[0].length
              break
            }
          }
        } else {
          ranges.push(mark('cm-nt-syntax').range(line.from, line.from + k.markEnd))
        }
        inlineFrom = k.markEnd
        break
      }
      case 'list': {
        const indent = text.slice(0, k.markFrom)
        const depth = (indent.match(/\t/g)?.length ?? 0) + Math.floor(indent.replace(/\t/g, '').length / 2)
        ranges.push(lineClass(`cm-nt-li cm-nt-li-${Math.min(depth, 6)}`).range(line.from))
        if (k.task) {
          const box = line.from + k.task.from
          if (!isActive) {
            ranges.push(hide.range(line.from + k.markFrom, box))
            ranges.push(Decoration.replace({ widget: new CheckboxWidget(k.task.checked, box) }).range(box, box + 3))
          } else {
            ranges.push(mark('cm-nt-syntax').range(line.from + k.markFrom, box + 3))
          }
          if (k.task.checked && box + 3 < line.to) {
            ranges.push(mark('cm-nt-done').range(box + 3, line.to))
          }
          inlineFrom = k.task.from + 3
        } else {
          if (!isActive && !k.ordered) {
            ranges.push(Decoration.replace({ widget: new MarkerWidget('cm-nt-bullet', '•') }).range(line.from + k.markFrom, line.from + k.markFrom + 1))
          } else {
            ranges.push(mark(k.ordered ? 'cm-nt-num' : 'cm-nt-syntax').range(line.from + k.markFrom, line.from + k.markEnd))
          }
          inlineFrom = k.markEnd
        }
        break
      }
    }

    addInline(ranges, state, line.from, text, inlineFrom, isActive)
    n++
  }
  return Decoration.set(ranges, true)
}

function addInline(ranges: Range<Decoration>[], state: EditorState, base: number, text: string, from: number, isActive: boolean): void {
  const ctx = state.facet(noteContext)
  for (const tok of inlineTokens(text, from)) {
    const a = base + tok.from
    const b = base + tok.to
    switch (tok.kind) {
      case 'embed':
      case 'image': {
        if (isActive) {
          ranges.push(mark('cm-nt-syntax').range(a, b))
          continue
        }
        const target = tok.target ?? ''
        const remote = tok.kind === 'image' && /^https?:\/\//i.test(target)
        const width = Number(/^\d+$/.test((tok.label ?? '').split('x')[0]) ? (tok.label ?? '').split('x')[0] : 0)
        if (tok.kind === 'embed' && !isImagePath(target)) {
          const resolved = ctx.resolve(target, false)
          if (!resolved || !isImagePath(resolved)) {
            ranges.push(Decoration.replace({ widget: new NoteEmbedWidget(target, tok.subpath ?? '', a) }).range(a, b))
            continue
          }
        }
        const embedTarget = tok.kind === 'image' && !remote ? ctx.resolve(target, true) || target : target
        ranges.push(Decoration.replace({ widget: new ImageWidget(embedTarget, tok.label ?? '', remote, width, a) }).range(a, b))
        continue
      }
      case 'wiki': {
        const resolved = tok.target ? ctx.resolve(tok.target, false) : ctx.path
        const cls = resolved ? 'cm-nt-link' : 'cm-nt-link cm-nt-unresolved'
        ranges.push(mark(cls, { 'data-nt-kind': 'wiki', 'data-nt-target': tok.target ?? '', 'data-nt-subpath': tok.subpath ?? '' }).range(a, b))
        break
      }
      case 'link':
      case 'url': {
        const target = tok.target ?? ''
        const external = /^[a-z][a-z0-9+.-]*:/i.test(target)
        const pelton = /^pelton:/i.test(target)
        const cls = pelton ? 'cm-nt-link cm-nt-pelton' : external ? 'cm-nt-link cm-nt-external' : ctx.resolve(target, true) ? 'cm-nt-link' : 'cm-nt-link cm-nt-unresolved'
        ranges.push(mark(cls, { 'data-nt-kind': external ? 'url' : 'markdown', 'data-nt-target': target, 'data-nt-subpath': tok.subpath ?? '' }).range(a, b))
        break
      }
      case 'tag':
        ranges.push(mark('cm-nt-tag', { 'data-nt-kind': 'tag', 'data-nt-target': tok.target ?? '', 'data-nt-subpath': '' }).range(a, b))
        continue
      case 'comment':
        if (!isActive) {
          ranges.push(hide.range(a, b))
          continue
        }
        ranges.push(mark('cm-nt-comment').range(a, b))
        continue
      default:
        ranges.push(mark(`cm-nt-${tok.kind}`).range(a, b))
    }
    for (const [x, y] of tok.marks) {
      if (y > x) {
        ranges.push((isActive ? mark('cm-nt-syntax') : hide).range(base + x, base + y))
      }
    }
  }
}

const decorations = StateField.define<DecorationSet>({
  create: build,
  update(value, tr) {
    if (tr.docChanged || tr.selection || tr.reconfigured || tr.effects.some((e) => e.is(refreshPreview))) {
      return build(tr.state)
    }
    return value
  },
  provide: (f) => EditorView.decorations.from(f),
})

// clicking a rendered link follows it; on the line being edited it takes a
// held modifier, so the cursor can still be put inside the link's text.
const clicks = EditorView.domEventHandlers({
  mousedown(event, view) {
    if (event.button !== 0) {
      return false
    }
    const el = event.target as HTMLElement
    if (!el.closest('[data-nt-kind]')) {
      return false
    }
    const pos = view.posAtDOM(el)
    const line = view.state.doc.lineAt(pos).number
    const editing = view.state.facet(previewMode) !== 'reading' && activeLines(view.state).has(line)
    if (editing && !(event.metaKey || event.ctrlKey)) {
      return false
    }
    return followElement(view, el, event)
  },
})

/** The live preview: line classification, decorations and link clicks. */
export function livePreview() {
  return [lineInfoField, decorations, clicks]
}
