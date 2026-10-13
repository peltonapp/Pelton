import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { EditorView } from '@codemirror/view'
import type { Editor } from '@tiptap/core'

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 7 }),
}))

import Compose from './Compose.svelte'
import { composeSessions, getSession, openCompose, updateCompose } from '../../stores/compose'
import { signatures } from '../../stores/signatures'
import type { EditorMode } from '../../lib/types'

let unsubscribe: (() => void) | undefined

afterEach(() => {
  unsubscribe?.()
  composeSessions.set([])
  signatures.set([])
})

function openPane(mode: EditorMode, body: string, signaturesApplied = true): number {
  const id = openCompose(1, mode)
  updateCompose(id, { body, signaturesApplied })
  const { rerender } = render(Compose, { session: getSession(id)! })
  unsubscribe = composeSessions.subscribe((sessions) => {
    const session = sessions.find((s) => s.id === id)
    if (session) void rerender({ session })
  })
  return id
}

describe('composer mode switching', () => {
  it('renders Markdown on entering Rich and restores Markdown on returning', async () => {
    const id = openPane('markdown', '**Hello**\n\n> Original message')
    await userEvent.click(screen.getByRole('tab', { name: 'Rich' }))
    await vi.waitFor(() => expect(getSession(id)!.body).toContain('<strong>Hello</strong>'))
    await userEvent.click(screen.getByRole('tab', { name: 'Markdown' }))
    expect(getSession(id)!.body).toContain('**Hello**')
    expect(getSession(id)!.body).toContain('> Original message')
    expect(getSession(id)!.body).not.toMatch(/<\/?(?:p|blockquote|strong)>/)
  })

  it('returns plain replies from Rich with line breaks, decoded entities and quote markers', async () => {
    const id = openPane('plaintext', 'Hello & welcome\nSecond line\n\n> Original\n>> Older')
    await userEvent.click(screen.getByRole('tab', { name: 'Rich' }))
    await vi.waitFor(() => expect(getSession(id)!.body).toContain('<blockquote>'))
    await userEvent.click(screen.getByRole('tab', { name: 'Plain' }))
    expect(getSession(id)!.body).toContain('Hello & welcome\nSecond line')
    expect(getSession(id)!.body).toContain('> Original')
    expect(getSession(id)!.body).toContain('> > Older')
    expect(getSession(id)!.body).not.toContain('<p>')
  })

  it('converts the current edited rich body instead of restoring an older source', async () => {
    const id = openPane('wysiwyg', '<p>Edited <strong>answer</strong></p><blockquote><p>Original<br>Second line</p></blockquote>')
    await userEvent.click(screen.getByRole('tab', { name: 'Markdown' }))
    expect(getSession(id)!.body).toContain('Edited **answer**')
    expect(getSession(id)!.body).toContain('> Original')
    expect(getSession(id)!.body).not.toContain('<blockquote>')
  })

  it('treats HTML-looking plain text as literal text when entering Rich', async () => {
    const id = openPane('plaintext', '<p>Example HTML</p>')
    await userEvent.click(screen.getByRole('tab', { name: 'Rich' }))
    expect(getSession(id)!.body).toContain('&lt;p&gt;Example HTML&lt;/p&gt;')
  })
})

describe('reply through Markdown preview', () => {
  it('keeps source lines and the sender address when returning to Plain after Preview', async () => {
    const source = 'On Sat, 10 Oct 2026, Revolut <no-reply@revolut.com> wrote:\n\n> Update your Vault information\n> Hi Jan,\n> Please follow these steps:\n> Log in to your app'
    const id = openPane('plaintext', source)
    await userEvent.click(screen.getByRole('tab', { name: 'Markdown' }))
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    expect(getSession(id)!.body).toBe(source)
    await userEvent.click(screen.getByRole('tab', { name: 'Plain' }))
    expect(getSession(id)!.body).toBe(source)
  })
})

describe('default signature in Rich', () => {
  it('renders a Markdown signature instead of showing its markup', async () => {
    signatures.set([{ id: 7, name: 'Work', kind: 'footer', format: 'markdown', content: '**Jan**\n[site](https://example.com)' }])
    const id = openPane('wysiwyg', '<p>Hello</p>', false)
    await vi.waitFor(() => expect(getSession(id)!.signaturesApplied).toBe(true))
    expect(getSession(id)!.body).toContain('<p>-- </p><p><strong>Jan</strong><br><a href="https://example.com">site</a></p>')
  })
})

describe('caret across editor modes', () => {
  // the source editors, reached the way a click would reach them: through the
  // element that holds the caret.
  async function sourceEditor(): Promise<EditorView> {
    const content = await vi.waitFor(() => {
      const el = document.querySelector<HTMLElement>('.cm-content')
      expect(el).not.toBeNull()
      return el!
    })
    return EditorView.findFromDOM(content)!
  }

  async function richEditor(): Promise<Editor> {
    return vi.waitFor(() => {
      const el = document.querySelector('.ProseMirror') as (HTMLElement & { editor?: Editor }) | null
      expect(el?.editor).toBeDefined()
      return el!.editor!
    })
  }

  it('opens a reply with the caret at the top of the body', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'ann@example.com', body: '\n\nOn x wrote:\n> hi', signaturesApplied: true })
    render(Compose, { session: getSession(id)! })
    const view = await sourceEditor()
    expect(view.hasFocus).toBe(true)
    expect(view.state.selection.main.head).toBe(0)
  })

  it('leaves a new message without recipients unfocused', async () => {
    openPane('plaintext', '')
    expect((await sourceEditor()).hasFocus).toBe(false)
  })

  it('keeps the caret and focus going from Plain to Markdown', async () => {
    openPane('plaintext', 'Hello world\n\n> quoted')
    ;(await sourceEditor()).dispatch({ selection: { anchor: 5 } })
    await userEvent.click(screen.getByRole('tab', { name: 'Markdown' }))
    await vi.waitFor(async () => expect((await sourceEditor()).state.selection.main.head).toBe(5))
    expect((await sourceEditor()).hasFocus).toBe(true)
  })

  it('puts the caret at the same text in Rich and gives it focus', async () => {
    openPane('plaintext', 'Hello world\n\n> quoted line')
    ;(await sourceEditor()).dispatch({ selection: { anchor: 'Hello world\n\n> quo'.length } })
    await userEvent.click(screen.getByRole('tab', { name: 'Rich' }))
    const rich = await richEditor()
    const { $head } = rich.state.selection
    expect($head.parent.textContent).toBe('quoted line')
    expect($head.parentOffset).toBe(3)
    await vi.waitFor(() => expect(rich.view.dom.contains(document.activeElement)).toBe(true))
  })

  it('carries the caret back from Rich into the source text', async () => {
    const id = openPane('wysiwyg', '<p>Hello world</p><blockquote><p>quoted line</p></blockquote>')
    const rich = await richEditor()
    let at = 0
    rich.state.doc.descendants((node, pos) => {
      if (node.isText && node.text === 'quoted line') at = pos + 3
    })
    rich.commands.setTextSelection(at)
    await userEvent.click(screen.getByRole('tab', { name: 'Plain' }))
    const view = await sourceEditor()
    expect(getSession(id)!.body.slice(view.state.selection.main.head)).toBe('ted line')
    expect(view.hasFocus).toBe(true)
  })

  it('starts at the top when the caret cannot be read', async () => {
    openPane('markdown', 'Hello world')
    await sourceEditor()
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await userEvent.click(screen.getByRole('tab', { name: 'Rich' }))
    const rich = await richEditor()
    expect(rich.state.selection.head).toBe(1)
    await vi.waitFor(() => expect(rich.view.dom.contains(document.activeElement)).toBe(true))
  })

  it('returns to the caret after the Markdown preview', async () => {
    openPane('markdown', 'Hello world')
    ;(await sourceEditor()).dispatch({ selection: { anchor: 5 } })
    const toggle = screen.getByRole('button', { name: 'Preview' })
    await userEvent.click(toggle)
    await userEvent.click(toggle)
    const view = await sourceEditor()
    expect(view.state.selection.main.head).toBe(5)
    expect(view.hasFocus).toBe(true)
  })
})
