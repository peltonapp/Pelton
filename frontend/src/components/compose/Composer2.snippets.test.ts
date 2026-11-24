import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import { EditorView } from '@codemirror/view'

const api = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  composeProtectionStatus: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 0 }),
}))

import Composer2 from './Composer2.svelte'
import { composeSessions, getSession, openCompose, updateCompose, type ComposeSession } from '../../stores/compose'
import { snippets } from '../../stores/snippets'
import { extraPaletteCommands } from '../../lib/palettesources'
import type { CommandStep } from '../../lib/commands'
import { prefs } from '../../stores/prefs'
import { t } from '../../lib/i18n'
import type { Snippet } from '../../lib/types'

let unsubscribe: (() => void) | null = null

function mount(id: number): void {
  updateCompose(id, { signaturesApplied: true })
  const session = getSession(id) as ComposeSession
  const { rerender } = render(Composer2, { props: { session, layout: 'window' } })
  unsubscribe = composeSessions.subscribe((list) => {
    const next = list.find((s) => s.id === id)
    if (next) {
      void rerender({ session: next, layout: 'window' })
    }
  })
}

const thanks: Snippet = {
  id: 1,
  name: 'Thanks',
  body: 'Thanks, {first name}. Talk on {day}.',
  subject: 'Thank you',
  to: '',
  cc: '',
  trigger: 'thanks',
  position: 0,
}

// cmView finds the plain text editor the composer uses in vim mode.
async function cmView(): Promise<EditorView> {
  const content = await waitFor(() => {
    const el = document.querySelector<HTMLElement>('.cm-content')
    expect(el).not.toBeNull()
    return el as HTMLElement
  })
  return EditorView.findFromDOM(content) as EditorView
}

beforeEach(() => {
  api.sendMessage.mockReset()
  api.composeProtectionStatus.mockReset().mockResolvedValue({ canSign: false, canEncrypt: false, suggested: 'none', recipients: [] })
  prefs.update((p) => ({ ...p, reduceMotion: true, composeVimMode: true, composeChips: false }))
  snippets.set([thanks, { ...thanks, id: 2, name: 'Address', body: 'Main street 1', subject: '', trigger: 'addr' }])
})

afterEach(() => {
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
  snippets.set([])
})

describe('snippets in the composer', () => {
  it('inserts one picked from the toolbar list, filling the empty subject and the name', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'Ada Lovelace <ada@example.com>' })
    mount(id)
    await cmView()

    await userEvent.click(screen.getByRole('button', { name: 'Insert snippet' }))
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search snippets' }), 'than')
    await userEvent.keyboard('{Enter}')

    await waitFor(() => expect(getSession(id)?.body).toContain('Thanks, Ada. Talk on {day}.'))
    expect(getSession(id)?.subject).toBe('Thank you')
    const view = await cmView()
    const sel = view.state.selection.main
    expect(view.state.sliceDoc(sel.from, sel.to)).toBe('{day}')
  })

  it('inserts one picked in the command palette', async () => {
    const id = openCompose(1, 'plaintext')
    mount(id)
    await cmView()

    const entry = extraPaletteCommands(get(t)).find((c) => c.id === 'compose:compose-insert-snippet')
    const step = entry?.run() as CommandStep
    expect(step.items.map((i) => i.label)).toEqual(['Thanks', 'Address'])
    expect(step.items[1].hint).toBe(';addr')
    step.items[1].run()

    await waitFor(() => expect(getSession(id)?.body).toContain('Main street 1'))
  })

  it('turns a trigger word into its snippet on Tab', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { body: 'Hello ;addr' })
    mount(id)
    const view = await cmView()
    view.dispatch({ selection: { anchor: view.state.doc.length } })

    await fireEvent.keyDown(view.contentDOM, { key: 'Tab' })

    await waitFor(() => expect(getSession(id)?.body).toBe('Hello Main street 1'))
  })

  it('leaves Tab alone after a word that is no trigger', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { body: 'Hello ;nothing' })
    mount(id)
    const view = await cmView()
    view.dispatch({ selection: { anchor: view.state.doc.length } })

    await fireEvent.keyDown(view.contentDOM, { key: ' ' })

    expect(getSession(id)?.body).toBe('Hello ;nothing')
  })

  it('walks the placeholders with Tab', async () => {
    const id = openCompose(1, 'plaintext')
    snippets.set([{ ...thanks, body: 'See you {day} at {time}.', subject: '' }])
    updateCompose(id, { body: ';thanks' })
    mount(id)
    const view = await cmView()
    view.dispatch({ selection: { anchor: view.state.doc.length } })

    await fireEvent.keyDown(view.contentDOM, { key: ' ' })
    await waitFor(() => {
      const sel = view.state.selection.main
      expect(view.state.sliceDoc(sel.from, sel.to)).toBe('{day}')
    })

    await fireEvent.keyDown(view.contentDOM, { key: 'Tab' })
    const sel = view.state.selection.main
    expect(view.state.sliceDoc(sel.from, sel.to)).toBe('{time}')
  })
})
