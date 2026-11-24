import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  listSnippets: vi.fn(),
  saveSnippet: vi.fn(),
  deleteSnippet: vi.fn(),
  reorderSnippets: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

vi.mock('../../../stores/confirm', () => ({ askConfirm: vi.fn().mockResolvedValue(true) }))

import Snippets from './Snippets.svelte'
import { snippets } from '../../../stores/snippets'
import type { Snippet } from '../../../lib/types'

function snippet(id: number, name: string, over: Partial<Snippet> = {}): Snippet {
  return { id, name, body: `${name} text`, subject: '', to: '', cc: '', trigger: '', position: id, ...over }
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  api.listSnippets.mockResolvedValue([snippet(1, 'Thanks', { trigger: 'thanks' }), snippet(2, 'Address')])
  api.saveSnippet.mockImplementation(async (s: Snippet) => ({ ...s, id: s.id || 9, trigger: s.trigger.replace(/^;/, '').toLowerCase() }))
  api.deleteSnippet.mockResolvedValue(undefined)
  api.reorderSnippets.mockResolvedValue(undefined)
})

afterEach(() => {
  document.documentElement.removeAttribute('data-reduce-motion')
  vi.clearAllMocks()
  snippets.set([])
})

describe('the Snippets page', () => {
  it('lists the snippets with their triggers', async () => {
    render(Snippets)
    expect(await screen.findByText('Thanks')).toBeInTheDocument()
    expect(screen.getByText(';thanks')).toBeInTheDocument()
    expect(screen.getByText('Address')).toBeInTheDocument()
  })

  it('creates a snippet with a placeholder put in from its button', async () => {
    render(Snippets)
    await screen.findByText('Thanks')
    await userEvent.click(screen.getByRole('button', { name: 'New snippet' }))
    await userEvent.type(screen.getByLabelText('Name'), 'Intro')
    await userEvent.type(screen.getByLabelText('Trigger'), 'intro')
    await userEvent.type(screen.getByLabelText('Text'), 'Hello ')
    await userEvent.click(screen.getByRole('button', { name: '{first name}' }))
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.saveSnippet).toHaveBeenCalledOnce())
    expect(api.saveSnippet.mock.calls[0][0]).toMatchObject({ id: 0, name: 'Intro', trigger: 'intro', body: 'Hello {first name}' })
    expect(get(snippets).map((s) => s.name)).toEqual(['Thanks', 'Address', 'Intro'])
  })

  it('cannot save a snippet without a name', async () => {
    render(Snippets)
    await screen.findByText('Thanks')
    await userEvent.click(screen.getByRole('button', { name: 'New snippet' }))
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('edits a snippet', async () => {
    render(Snippets)
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Address' }))
    const subject = screen.getByLabelText('Subject')
    await userEvent.type(subject, 'Where to find us')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.saveSnippet).toHaveBeenCalledWith(expect.objectContaining({ id: 2, subject: 'Where to find us' })))
  })

  it('moves a snippet down with its arrow', async () => {
    render(Snippets)
    await userEvent.click(await screen.findByRole('button', { name: 'Move Thanks down' }))
    expect(api.reorderSnippets).toHaveBeenCalledWith([2, 1])
    expect(get(snippets).map((s) => s.id)).toEqual([2, 1])
  })

  it('deletes a snippet after asking', async () => {
    render(Snippets)
    await userEvent.click(await screen.findByRole('button', { name: 'Delete Thanks' }))
    await waitFor(() => expect(api.deleteSnippet).toHaveBeenCalledWith(1))
    expect(get(snippets).map((s) => s.id)).toEqual([2])
  })
})
