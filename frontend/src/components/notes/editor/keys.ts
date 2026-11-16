// keys.ts holds the note editor's editing keys: the usual formatting
// shortcuts, Enter that continues a list, a task or a quote, Tab that indents
// a list item, and a key that ticks a task. Pasted images are saved into the
// vault's attachments folder and embedded where they were pasted.

import { EditorSelection, type ChangeSpec, type EditorState } from '@codemirror/state'
import { EditorView, keymap, type Command } from '@codemirror/view'
import { nextListPrefix } from '../../../lib/notemd'
import { noteContext } from './context'

/** Wraps the selection (or the word at the cursor) in mark, or unwraps it when it is wrapped already. */
export function toggleWrap(mark: string): Command {
  return (view) => {
    if (view.state.readOnly) {
      return false
    }
    const tr = view.state.changeByRange((range) => {
      let { from, to } = range
      if (from === to) {
        const word = view.state.wordAt(from)
        if (word) {
          from = word.from
          to = word.to
        }
      }
      const before = view.state.sliceDoc(from - mark.length, from)
      const after = view.state.sliceDoc(to, to + mark.length)
      if (before === mark && after === mark) {
        return {
          changes: [
            { from: from - mark.length, to: from },
            { from: to, to: to + mark.length },
          ],
          range: EditorSelection.range(from - mark.length, to - mark.length),
        }
      }
      const inner = view.state.sliceDoc(from, to)
      if (inner.startsWith(mark) && inner.endsWith(mark) && inner.length >= mark.length * 2) {
        return {
          changes: { from, to, insert: inner.slice(mark.length, inner.length - mark.length) },
          range: EditorSelection.range(from, to - mark.length * 2),
        }
      }
      return {
        changes: [
          { from, insert: mark },
          { from: to, insert: mark },
        ],
        range: from === to ? EditorSelection.cursor(from + mark.length) : EditorSelection.range(from + mark.length, to + mark.length),
      }
    })
    view.dispatch(tr, { userEvent: 'input.format' })
    return true
  }
}

/** Enter: continues a list item, task or quote, or ends the list on an empty item. */
export const continueList: Command = (view) => {
  const { state } = view
  if (state.readOnly || state.selection.ranges.length > 1) {
    return false
  }
  const sel = state.selection.main
  const line = state.doc.lineAt(sel.head)
  const next = nextListPrefix(line.text)
  if (!next || sel.head < line.from + next.markerLength) {
    return false
  }
  if (next.empty) {
    view.dispatch({ changes: { from: line.from, to: line.to, insert: '' }, selection: EditorSelection.cursor(line.from), userEvent: 'delete' })
    return true
  }
  // a CRLF note counts its line break as one position, like any other.
  const insert = state.toText(state.lineBreak + next.prefix)
  view.dispatch({
    changes: { from: sel.from, to: sel.to, insert },
    selection: EditorSelection.cursor(sel.from + insert.length),
    scrollIntoView: true,
    userEvent: 'input',
  })
  return true
}

function listLines(state: EditorState): { from: number; text: string }[] {
  const out: { from: number; text: string }[] = []
  const seen = new Set<number>()
  for (const r of state.selection.ranges) {
    for (let n = state.doc.lineAt(r.from).number; n <= state.doc.lineAt(r.to).number; n++) {
      if (!seen.has(n)) {
        seen.add(n)
        const l = state.doc.line(n)
        out.push({ from: l.from, text: l.text })
      }
    }
  }
  return out
}

/** Tab: indents the selected list items by a tab, or inserts one elsewhere. */
export const indentList: Command = (view) => {
  if (view.state.readOnly) {
    return false
  }
  const lines = listLines(view.state)
  const multi = lines.length > 1 || !view.state.selection.main.empty
  if (!multi && !nextListPrefix(lines[0]?.text ?? '')) {
    view.dispatch(view.state.replaceSelection('\t'), { userEvent: 'input' })
    return true
  }
  const changes: ChangeSpec[] = lines.map((l) => ({ from: l.from, insert: '\t' }))
  view.dispatch({ changes, userEvent: 'input.indent' })
  return true
}

/** Shift-Tab: takes one level of indentation off the selected lines. */
export const outdentList: Command = (view) => {
  if (view.state.readOnly) {
    return false
  }
  const changes: ChangeSpec[] = []
  for (const l of listLines(view.state)) {
    const m = /^(\t| {1,4})/.exec(l.text)
    if (m) {
      changes.push({ from: l.from, to: l.from + m[0].length })
    }
  }
  if (changes.length) {
    view.dispatch({ changes, userEvent: 'delete.dedent' })
  }
  return true
}

/** Ticks or unticks the task on each selected line; a plain line becomes a task. */
export const toggleTask: Command = (view) => {
  if (view.state.readOnly) {
    return false
  }
  const changes: ChangeSpec[] = []
  for (const l of listLines(view.state)) {
    const task = /^(\s*(?:[-*+]|\d{1,9}[.)])[ \t]+)\[([ xX])\]/.exec(l.text)
    if (task) {
      const at = l.from + task[1].length + 1
      changes.push({ from: at, to: at + 1, insert: task[2] === ' ' ? 'x' : ' ' })
      continue
    }
    const item = /^(\s*(?:[-*+]|\d{1,9}[.)])[ \t]+)/.exec(l.text)
    if (item) {
      changes.push({ from: l.from + item[1].length, insert: '[ ] ' })
      continue
    }
    const indent = /^\s*/.exec(l.text)![0].length
    changes.push({ from: l.from + indent, insert: '- [ ] ' })
  }
  view.dispatch({ changes, userEvent: 'input.toggle' })
  return true
}

/** The editing keys of the note editor. */
export function noteKeymap() {
  return keymap.of([
    { key: 'Enter', run: continueList },
    { key: 'Tab', run: indentList, shift: outdentList },
    { key: 'Mod-b', run: toggleWrap('**') },
    { key: 'Mod-i', run: toggleWrap('*') },
    { key: 'Mod-Shift-s', run: toggleWrap('~~') },
    { key: 'Mod-`', run: toggleWrap('`') },
    { key: 'Mod-Shift-=', run: toggleWrap('==') },
    { key: 'Mod-Enter', run: toggleTask },
  ])
}

function toBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let bin = ''
  const step = 0x8000
  for (let i = 0; i < bytes.length; i += step) {
    bin += String.fromCharCode(...bytes.subarray(i, i + step))
  }
  return btoa(bin)
}

function extension(type: string): string {
  const sub = type.split('/')[1] ?? 'png'
  return sub === 'jpeg' ? 'jpg' : sub === 'svg+xml' ? 'svg' : sub
}

/** Saves image files into the vault and embeds them at pos. */
export async function insertImages(view: EditorView, files: File[], pos: number): Promise<void> {
  const ctx = view.state.facet(noteContext)
  const stamp = new Date()
  const parts: string[] = []
  for (const [i, file] of files.entries()) {
    const pad = (n: number): string => String(n).padStart(2, '0')
    const name =
      file.name && file.name !== 'image.png'
        ? file.name
        : `Pasted image ${stamp.getFullYear()}${pad(stamp.getMonth() + 1)}${pad(stamp.getDate())}${pad(stamp.getHours())}${pad(stamp.getMinutes())}${pad(stamp.getSeconds())}${i ? `-${i}` : ''}.${extension(file.type)}`
    const saved = await ctx.saveAttachment(name, toBase64(await file.arrayBuffer()))
    parts.push(`![[${saved.slice(saved.lastIndexOf('/') + 1)}]]`)
  }
  if (parts.length) {
    const insert = view.state.toText(parts.join(view.state.lineBreak))
    view.dispatch({ changes: { from: pos, insert }, selection: EditorSelection.cursor(pos + insert.length), userEvent: 'input.paste' })
  }
}

/** Pasting or dropping images saves them into the vault instead of inserting nothing. */
export const imagePaste = EditorView.domEventHandlers({
  paste(event, view) {
    const files = [...(event.clipboardData?.files ?? [])].filter((f) => f.type.startsWith('image/'))
    if (!files.length || view.state.readOnly) {
      return false
    }
    event.preventDefault()
    void insertImages(view, files, view.state.selection.main.head)
    return true
  },
  drop(event, view) {
    const files = [...(event.dataTransfer?.files ?? [])].filter((f) => f.type.startsWith('image/'))
    if (!files.length || view.state.readOnly) {
      return false
    }
    event.preventDefault()
    const pos = view.posAtCoords({ x: event.clientX, y: event.clientY }) ?? view.state.selection.main.head
    void insertImages(view, files, pos)
    return true
  },
})
