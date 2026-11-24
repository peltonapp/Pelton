import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  getNote: vi.fn(),
  saveNote: vi.fn(),
  deleteNote: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  getNote: api.getNote,
  saveNote: api.saveNote,
  deleteNote: api.deleteNote,
}))

import NoteCard from './NoteCard.svelte'
import { noteRequest } from '../../lib/organize'

beforeEach(() => {
  api.getNote.mockReset()
  api.saveNote.mockReset()
  api.deleteNote.mockReset()
  api.saveNote.mockImplementation((_id: number, _conv: boolean, body: string) => Promise.resolve({ body, updatedAt: '2026-11-15T10:00:00Z' }))
  api.deleteNote.mockResolvedValue(undefined)
  noteRequest.set(null)
})

describe('NoteCard', () => {
  it('stays out of the way while there is no note', async () => {
    api.getNote.mockResolvedValue({ body: '', updatedAt: '' })
    render(NoteCard, { props: { messageId: 1 } })
    await waitFor(() => expect(api.getNote).toHaveBeenCalledWith(1, false))
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('shows an existing note and saves edits as you type', async () => {
    const user = userEvent.setup()
    api.getNote.mockResolvedValue({ body: 'Call Ada', updatedAt: '2026-11-15T09:00:00Z' })
    render(NoteCard, { props: { messageId: 7, threadId: 3 } })
    const field = await screen.findByRole('textbox', { name: 'Private note on this conversation' })
    expect(field).toHaveValue('Call Ada')
    expect(api.getNote).toHaveBeenCalledWith(7, true)

    await user.type(field, ' on Friday')
    expect(screen.getByText('Saving')).toBeInTheDocument()
    await waitFor(() => expect(api.saveNote).toHaveBeenCalledWith(7, true, 'Call Ada on Friday'), { timeout: 2000 })
    await waitFor(() => expect(screen.getByText('Saved')).toBeInTheDocument())
    expect(api.saveNote).toHaveBeenCalledTimes(1)
  })

  it('opens empty for editing when asked, and deletes', async () => {
    const user = userEvent.setup()
    api.getNote.mockResolvedValue({ body: '', updatedAt: '' })
    render(NoteCard, { props: { messageId: 2 } })
    await waitFor(() => expect(api.getNote).toHaveBeenCalled())
    noteRequest.set(2)
    const field = await screen.findByRole('textbox', { name: 'Private note' })
    await waitFor(() => expect(field).toHaveFocus())
    expect(screen.getByText('Kept on this device. Never sent.')).toBeInTheDocument()

    await user.type(field, 'Remember the venue')
    await user.click(screen.getByRole('button', { name: 'Delete note' }))
    expect(api.deleteNote).toHaveBeenCalledWith(2, false)
    await waitFor(() => expect(screen.queryByRole('textbox')).not.toBeInTheDocument())
  })
})
