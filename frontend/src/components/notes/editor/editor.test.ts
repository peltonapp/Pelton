import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorSelection, EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { noteContext, previewMode, type NoteContext, type PreviewMode } from './context'
import { livePreview } from './livepreview'
import { acceptCompletion, completionOf, noteCompletion } from './complete'
import { continueList, indentList, noteKeymap, outdentList, toggleTask, toggleWrap } from './keys'

const views: EditorView[] = []

afterEach(() => {
  for (const v of views.splice(0)) {
    v.destroy()
  }
})

function context(over: Partial<NoteContext> = {}): NoteContext {
  return {
    path: 'Note.md',
    resolve: (target) => (target === 'Other' ? 'Other.md' : ''),
    fileData: () => Promise.resolve('data:image/png;base64,'),
    readNote: () => Promise.resolve('# Other\ntext'),
    follow: vi.fn(),
    completeNotes: (q) => [{ label: 'Other', detail: '', insert: `Other${q ? '' : ''}` }],
    completeHeadings: () => [{ label: 'Part', detail: '##', insert: 'Part' }],
    completeTags: (q) => [{ label: `#${q}more`, detail: '1', insert: `${q}more` }],
    saveAttachment: () => Promise.resolve('Attachments/x.png'),
    t: (k) => k,
    ...over,
  }
}

function view(doc: string, cursor = 0, mode: PreviewMode = 'live', ctx: NoteContext = context(), separator = '\n'): EditorView {
  const v = new EditorView({
    state: EditorState.create({
      doc,
      selection: EditorSelection.cursor(cursor),
      extensions: [EditorState.lineSeparator.of(separator), noteContext.of(ctx), previewMode.of(mode), livePreview(), noteCompletion(), noteKeymap()],
    }),
    parent: document.body,
  })
  views.push(v)
  return v
}

function lineText(v: EditorView, cls: string): string {
  return v.contentDOM.querySelector(`.${cls}`)?.textContent ?? ''
}

describe('live preview', () => {
  it('hides syntax on lines the cursor is not on', () => {
    const v = view('first\n# Heading\nsome **bold** and [[Other|alias]]', 0)
    expect(lineText(v, 'cm-nt-h1')).toBe('Heading')
    const text = v.contentDOM.textContent ?? ''
    expect(text).toContain('some bold and alias')
    expect(text).not.toContain('**')
    expect(v.contentDOM.querySelector('.cm-nt-strong')?.textContent).toBe('bold')
    const link = v.contentDOM.querySelector<HTMLElement>('[data-nt-kind="wiki"]')
    expect(link?.dataset.ntTarget).toBe('Other')
  })

  it('shows the source of the line being edited', () => {
    const v = view('# Heading\nnext', 2)
    expect(lineText(v, 'cm-nt-h1')).toBe('# Heading')
  })

  it('marks links to notes that do not exist', () => {
    const v = view('x\n[[Missing]] [[Other]]', 0)
    const links = [...v.contentDOM.querySelectorAll('.cm-nt-link')]
    expect(links.map((l) => l.classList.contains('cm-nt-unresolved'))).toEqual([true, false])
  })

  it('draws front matter as properties until the cursor goes in', () => {
    const doc = '---\ntags: [a, b]\n---\nbody'
    const v = view(doc, doc.length)
    expect(v.contentDOM.querySelector('.cm-nt-props')).not.toBeNull()
    expect([...v.contentDOM.querySelectorAll('.cm-nt-chip')].map((c) => c.textContent)).toEqual(['a', 'b'])
    v.dispatch({ selection: { anchor: 5 } })
    expect(v.contentDOM.querySelector('.cm-nt-props')).toBeNull()
    expect(v.contentDOM.querySelectorAll('.cm-nt-fm').length).toBe(3)
  })

  it('ticks a task from its checkbox without touching anything else', () => {
    const doc = 'top\n- [ ] call Ann\n- [x] done'
    const v = view(doc, 0)
    const boxes = v.contentDOM.querySelectorAll<HTMLInputElement>('input.cm-nt-check')
    expect([...boxes].map((b) => b.checked)).toEqual([false, true])
    boxes[0].click()
    expect(v.state.doc.toString()).toBe('top\n- [x] call Ann\n- [x] done')
  })

  it('renders a table when the cursor is outside it', () => {
    const v = view('x\n| a | b |\n|---|---|\n| 1 | **2** |', 0)
    const cells = [...v.contentDOM.querySelectorAll('.cm-nt-table td, .cm-nt-table th')].map((c) => c.textContent)
    expect(cells).toEqual(['a', 'b', '1', '2'])
  })

  it('does not fetch a remote image until asked', () => {
    const fileData = vi.fn(() => Promise.resolve(''))
    const v = view('x\n![pic](https://example.com/a.png)', 0, 'live', context({ fileData }))
    expect(v.contentDOM.querySelector('.cm-nt-image img')).toBeNull()
    expect(v.contentDOM.querySelector('.cm-nt-remote')?.textContent).toBe('notes.editor.loadRemote')
    expect(fileData).not.toHaveBeenCalled()
  })

  it('reads vault images through the backend', () => {
    const fileData = vi.fn(() => Promise.resolve('data:image/png;base64,AA=='))
    view('x\n![[pic.png]]', 0, 'live', context({ fileData, resolve: (t) => (t === 'pic.png' ? 'img/pic.png' : '') }))
    expect(fileData).toHaveBeenCalledWith('img/pic.png')
  })

  it('renders everything in reading mode and shows plain text in source mode', () => {
    const reading = view('# Head\n**b**', 0, 'reading')
    expect(lineText(reading, 'cm-nt-h1')).toBe('Head')
    const source = view('# Head\n**b**', 12, 'source')
    expect(source.contentDOM.textContent).toContain('**b**')
    expect(source.contentDOM.querySelector('.cm-nt-strong')).toBeNull()
  })

  it('follows a rendered link on click', () => {
    const follow = vi.fn()
    const v = view('x\n[[Other#Part]]', 0, 'live', context({ follow }))
    const link = v.contentDOM.querySelector<HTMLElement>('[data-nt-kind="wiki"]')!
    link.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
    expect(follow).toHaveBeenCalledWith({ kind: 'wiki', target: 'Other', subpath: 'Part' }, false)
  })
})

describe('completion', () => {
  function type(v: EditorView, text: string): void {
    const at = v.state.selection.main.head
    v.dispatch({ changes: { from: at, insert: text }, selection: { anchor: at + text.length }, userEvent: 'input.type' })
  }

  it('offers notes after [[ and closes the link on accept', () => {
    const v = view('', 0)
    type(v, 'See [[Ot')
    expect(completionOf(v.state)?.items.map((i) => i.label)).toEqual(['Other'])
    acceptCompletion(v)
    expect(v.state.doc.toString()).toBe('See [[Other]]')
    expect(completionOf(v.state)).toBeNull()
  })

  it('offers headings after [[Note# and tags after #', () => {
    const v = view('', 0)
    type(v, '[[Other#P')
    expect(completionOf(v.state)?.kind).toBe('heading')
    acceptCompletion(v)
    expect(v.state.doc.toString()).toBe('[[Other#Part]]')
    type(v, ' #pro')
    expect(completionOf(v.state)?.kind).toBe('tag')
    acceptCompletion(v)
    expect(v.state.doc.toString()).toBe('[[Other#Part]] #promore ')
  })
})

describe('editing keys', () => {
  it('continues lists and ends them on an empty item', () => {
    const v = view('- one', 5)
    continueList(v)
    expect(v.state.doc.toString()).toBe('- one\n- ')
    continueList(v)
    expect(v.state.doc.toString()).toBe('- one\n')
    const n = view('1. [ ] a', 8)
    continueList(n)
    expect(n.state.doc.toString()).toBe('1. [ ] a\n2. [ ] ')
  })

  it('keeps the file line endings when continuing a list', () => {
    const v = view('- one\r\n- two', 0, 'live', context(), '\r\n')
    v.dispatch({ selection: { anchor: v.state.doc.length } })
    continueList(v)
    expect(v.state.sliceDoc()).toBe('- one\r\n- two\r\n- ')
  })

  it('wraps and unwraps the selection', () => {
    const v = view('make this bold', 0)
    v.dispatch({ selection: EditorSelection.range(5, 9) })
    toggleWrap('**')(v)
    expect(v.state.doc.toString()).toBe('make **this** bold')
    toggleWrap('**')(v)
    expect(v.state.doc.toString()).toBe('make this bold')
  })

  it('indents, outdents and ticks tasks', () => {
    const v = view('- a', 1)
    indentList(v)
    expect(v.state.doc.toString()).toBe('\t- a')
    outdentList(v)
    expect(v.state.doc.toString()).toBe('- a')
    toggleTask(v)
    expect(v.state.doc.toString()).toBe('- [ ] a')
    toggleTask(v)
    expect(v.state.doc.toString()).toBe('- [x] a')
    const plain = view('text', 0)
    toggleTask(plain)
    expect(plain.state.doc.toString()).toBe('- [ ] text')
  })
})
