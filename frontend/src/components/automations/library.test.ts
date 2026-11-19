import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import { catalogFixture, scriptFixture } from '../../test/scriptfixtures'
import type { ScriptLibrary, ScriptLibraryNote } from '../../lib/scripting'

const api = vi.hoisted(() => ({
  checkScriptLua: vi.fn(),
  updateScriptLibrary: vi.fn(),
  deleteScriptLibrary: vi.fn(),
  scriptLibraryImpact: vi.fn(),
  scriptLibraryNote: vi.fn(),
  listScripts: vi.fn(),
  setScriptEnabled: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const { default: LibraryEditor } = await import('./LibraryEditor.svelte')
const { default: LibraryReviewDialog } = await import('./LibraryReviewDialog.svelte')
const { default: LibraryNote } = await import('./LibraryNote.svelte')
const store = await import('../../stores/scripts')
const { confirmRequest } = await import('../../stores/confirm')
const { toasts } = await import('../../stores/toast')
const { prefs } = await import('../../stores/prefs')

const util: ScriptLibrary = { id: 7, name: 'util', description: '', source: 'return { a = 1 }', origin: 'user', createdAt: '', updatedAt: 'u1' }
const uses = scriptFixture({ id: 3, name: 'Uses util', permissions: ['mail.read'], approved: [], enabled: false })
const note: ScriptLibraryNote = {
  scriptId: 3,
  exists: true,
  library: 'util',
  when: 'today',
  deleted: false,
  diff: [
    { op: '-', text: 'return { a = 1 }' },
    { op: '+', text: 'return { a = 2 }' },
  ],
}

beforeEach(() => {
  prefs.update((p) => ({ ...p, reduceMotion: true }))
  Object.values(api).forEach((f) => f.mockReset())
  api.checkScriptLua.mockResolvedValue('')
  api.listScripts.mockResolvedValue([uses])
  api.scriptLibraryNote.mockResolvedValue(note)
  store.libraries.set([util])
  store.libraryReview.set(null)
  store.scripts.set([uses])
  confirmRequest.set(null)
  toasts.set([])
})

describe('saving a library that scripts load', () => {
  it('says how many switched-on scripts it will switch off before saving, then offers the review', async () => {
    api.scriptLibraryImpact.mockResolvedValue([
      { id: 3, name: 'Uses util', enabled: true },
      { id: 4, name: 'Approved but off', enabled: false },
    ])
    api.updateScriptLibrary.mockResolvedValue({ library: { ...util, source: 'return { a = 2 }', updatedAt: 'u2' }, switchedOff: [{ id: 3, name: 'Uses util', enabled: true }] })
    render(LibraryEditor, { props: { library: util, catalog: catalogFixture } })
    await userEvent.type(screen.getByRole('textbox', { name: 'Library name' }), 's')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(get(confirmRequest)).not.toBeNull())
    const ask = get(confirmRequest)!
    expect(ask.title).toBe('Switch off 1 script?')
    expect(ask.body).toContain('Uses util')
    expect(ask.body).not.toContain('Approved but off')
    expect(api.updateScriptLibrary).not.toHaveBeenCalled()
    ask.resolve(true)
    await waitFor(() => expect(api.updateScriptLibrary).toHaveBeenCalled())
    await waitFor(() => expect(get(store.libraryReview)).toEqual({ library: 'util', scripts: [{ id: 3, name: 'Uses util', enabled: true }] }))
  })

  it('does not save when the user backs out', async () => {
    api.scriptLibraryImpact.mockResolvedValue([{ id: 3, name: 'Uses util', enabled: true }])
    render(LibraryEditor, { props: { library: util, catalog: catalogFixture } })
    await userEvent.type(screen.getByRole('textbox', { name: 'Library name' }), 's')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(get(confirmRequest)).not.toBeNull())
    get(confirmRequest)!.resolve(false)
    await waitFor(() => expect(get(confirmRequest)).toBeNull())
    expect(api.updateScriptLibrary).not.toHaveBeenCalled()
  })

  it('names the scripts a delete switches off and offers the review afterwards', async () => {
    api.scriptLibraryImpact.mockResolvedValue([{ id: 3, name: 'Uses util', enabled: true }])
    api.deleteScriptLibrary.mockResolvedValue([{ id: 3, name: 'Uses util', enabled: true }])
    render(LibraryEditor, { props: { library: util, catalog: catalogFixture } })
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(get(confirmRequest)).not.toBeNull())
    expect(get(confirmRequest)!.body).toContain('This switches off a script that loads the library: Uses util.')
    get(confirmRequest)!.resolve(true)
    await waitFor(() => expect(get(store.libraryReview)?.library).toBe('util'))
  })
})

describe('LibraryReviewDialog', () => {
  it('lists the switched-off scripts and walks through them with the change', async () => {
    api.setScriptEnabled.mockResolvedValue({ ...uses, enabled: true, approved: ['mail.read'] })
    const onClose = vi.fn()
    render(LibraryReviewDialog, { props: { review: { library: 'util', scripts: [{ id: 3, name: 'Uses util', enabled: true }] }, catalog: catalogFixture }, events: { close: onClose } })
    expect(screen.getByRole('dialog', { name: 'The library util changed' })).toBeInTheDocument()
    expect(screen.getByRole('listitem')).toHaveTextContent('Uses util')
    await userEvent.click(screen.getByRole('button', { name: 'Review and re-approve' }))
    expect(screen.getByRole('dialog', { name: 'Review Uses util (1 of 1)' })).toBeInTheDocument()
    expect(await screen.findByText(/return \{ a = 2 \}/)).toBeInTheDocument()
    expect(api.scriptLibraryNote).toHaveBeenCalledWith(3)
    await userEvent.click(screen.getByRole('button', { name: 'Approve and switch on' }))
    expect(api.setScriptEnabled).toHaveBeenCalledWith(3, true, ['mail.read'])
    expect(onClose).toHaveBeenCalled()
    expect(get(toasts).map((x) => x.message)).toContain('1 script approved and switched on again.')
  })

  it('skips a script without approving it', async () => {
    const onClose = vi.fn()
    render(LibraryReviewDialog, { props: { review: { library: 'util', scripts: [{ id: 3, name: 'Uses util', enabled: true }] }, catalog: catalogFixture }, events: { close: onClose } })
    await userEvent.click(screen.getByRole('button', { name: 'Review and re-approve' }))
    await userEvent.click(screen.getByRole('button', { name: 'Skip' }))
    expect(api.setScriptEnabled).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })
})

describe('LibraryNote', () => {
  it('says on the script page why it was switched off and what changed', async () => {
    render(LibraryNote, { props: { script: uses } })
    expect(await screen.findByText('Switched off because the library util changed today. Check what changed, then switch the script on again.')).toBeInTheDocument()
    await userEvent.click(screen.getByText('What changed'))
    expect(screen.getByText(/return \{ a = 1 \}/)).toBeVisible()
  })

  it('stays away from a switched-on script', async () => {
    render(LibraryNote, { props: { script: { ...uses, enabled: true } } })
    await Promise.resolve()
    expect(api.scriptLibraryNote).not.toHaveBeenCalled()
    expect(screen.queryByText(/Switched off because/)).toBeNull()
  })
})
