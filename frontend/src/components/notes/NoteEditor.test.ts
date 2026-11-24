import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { EditorView } from '@codemirror/view'

const api = vi.hoisted(() => ({
  vaultRead: vi.fn(),
  vaultSave: vi.fn(),
  vaultImportFile: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import NoteEditor from './NoteEditor.svelte'
import type { NoteContext } from './editor/context'

const context: Omit<NoteContext, 'path'> = {
  resolve: () => '',
  fileData: () => Promise.reject(new Error('none')),
  readNote: () => Promise.reject(new Error('none')),
  follow: () => {},
  completeNotes: () => [],
  completeHeadings: () => [],
  completeTags: () => [],
  saveAttachment: () => Promise.reject(new Error('none')),
  t: (k) => k,
}

function renderEditor(): void {
  render(NoteEditor, { props: { path: 'n.md', context } })
}

function editorView(): EditorView {
  const el = document.querySelector('.cm-editor') as HTMLElement
  const v = EditorView.findFromDOM(el)
  if (!v) {
    throw new Error('no editor')
  }
  return v
}

beforeEach(() => {
  api.vaultRead.mockReset()
  api.vaultSave.mockReset()
})

describe('NoteEditor', () => {
  it('saves only what was typed, keeping CRLF line endings and front matter', async () => {
    const original = '---\r\ntags: [a]\r\n---\r\n# Title\r\nbody\r\n'
    api.vaultRead.mockResolvedValue({ path: 'n.md', content: original, hash: 'h1', mtime: 1, readOnly: false })
    api.vaultSave.mockResolvedValue({ hash: 'h2', mtime: 2, conflict: false, disk: '', diskHash: '' })
    renderEditor()
    await waitFor(() => expect(document.querySelector('.cm-editor')).not.toBeNull())
    await waitFor(() => expect(editorView().state.doc.length).toBeGreaterThan(0))
    const v = editorView()
    const end = v.state.doc.length
    v.dispatch({ changes: { from: end, insert: 'more' }, userEvent: 'input.type' })
    await waitFor(() => expect(api.vaultSave).toHaveBeenCalledTimes(1), { timeout: 3000 })
    expect(api.vaultSave).toHaveBeenCalledWith('n.md', `${original}more`, 'h1', false)
  })

  it('offers to keep mine or load theirs when the file changed on disk', async () => {
    api.vaultRead.mockResolvedValue({ path: 'n.md', content: 'one', hash: 'h1', mtime: 1, readOnly: false })
    api.vaultSave.mockResolvedValueOnce({ hash: '', mtime: 0, conflict: true, disk: 'theirs', diskHash: 'h9' })
    api.vaultSave.mockResolvedValueOnce({ hash: 'h10', mtime: 3, conflict: false, disk: '', diskHash: '' })
    renderEditor()
    await waitFor(() => expect(editorView().state.doc.toString()).toBe('one'))
    editorView().dispatch({ changes: { from: 3, insert: ' mine' }, userEvent: 'input.type' })
    const keep = await screen.findByRole('button', { name: 'Keep mine' }, { timeout: 3000 })
    expect(screen.getByRole('alert')).toHaveTextContent('changed on disk')
    await userEvent.click(keep)
    await waitFor(() => expect(api.vaultSave).toHaveBeenLastCalledWith('n.md', 'one mine', 'h9', true))
  })

  it('loads the version on disk when asked', async () => {
    api.vaultRead.mockResolvedValue({ path: 'n.md', content: 'one', hash: 'h1', mtime: 1, readOnly: false })
    api.vaultSave.mockResolvedValueOnce({ hash: '', mtime: 0, conflict: true, disk: 'theirs\r\nline', diskHash: 'h9' })
    renderEditor()
    await waitFor(() => expect(editorView().state.doc.toString()).toBe('one'))
    editorView().dispatch({ changes: { from: 3, insert: '!' }, userEvent: 'input.type' })
    await userEvent.click(await screen.findByRole('button', { name: 'Load from disk' }, { timeout: 3000 }))
    expect(editorView().state.sliceDoc()).toBe('theirs\r\nline')
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('never writes a file that is not UTF-8', async () => {
    api.vaultRead.mockResolvedValue({ path: 'n.md', content: 'bad', hash: 'h1', mtime: 1, readOnly: true })
    renderEditor()
    await screen.findByRole('status')
    expect(editorView().state.readOnly).toBe(true)
    expect(api.vaultSave).not.toHaveBeenCalled()
  })
})
